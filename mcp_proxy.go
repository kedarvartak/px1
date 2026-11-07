package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const defaultMCPEndpoint = "https://mcp.px1.dev/mcp"

// runMCP serves px1's hosted MCP tools to a local agent over stdio. The CLI
// has no hosted data of its own: it lists the tools the API key may use on the
// hosted server and forwards each call there.
func runMCP(args []string) error {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	endpoint := fs.String("url", defaultMCPEndpoint, "hosted px1 MCP endpoint")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: PX1_API_KEY=px1k_... px1 mcp [--url <endpoint>]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	key := strings.TrimSpace(os.Getenv("PX1_API_KEY"))
	if !strings.HasPrefix(key, "px1k_") {
		return errors.New("set PX1_API_KEY to a px1 API key; keys are never accepted as flags so they stay out of shell history and process lists")
	}
	client, err := mcpHTTPClient(*endpoint, key)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return serveMCPProxy(ctx, &mcp.StreamableClientTransport{Endpoint: *endpoint, HTTPClient: client}, &mcp.StdioTransport{})
}

func serveMCPProxy(ctx context.Context, upstream, local mcp.Transport) error {
	client := mcp.NewClient(&mcp.Implementation{Name: "px1-cli", Version: version}, nil)
	session, err := client.Connect(ctx, upstream, nil)
	if err != nil {
		return fmt.Errorf("connect to px1 MCP: %w", err)
	}
	defer session.Close()
	server := mcp.NewServer(&mcp.Implementation{Name: "px1", Title: "px1 review", Version: version}, nil)
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			return fmt.Errorf("list px1 MCP tools: %w", err)
		}
		name := tool.Name
		server.AddTool(tool, func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: request.Params.Arguments})
		})
	}
	return server.Run(ctx, local)
}

// mcpHTTPClient attaches the key only to requests for the configured host and
// refuses redirects, so the key cannot follow a redirect to another origin.
func mcpHTTPClient(endpoint, key string) (*http.Client, error) {
	parsed, err := url.Parse(endpoint)
	loopback := parsed != nil && (parsed.Hostname() == "localhost" || parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "::1")
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "https" && !(parsed.Scheme == "http" && loopback)) {
		return nil, errors.New("--url must be an https URL (http only on loopback) without credentials")
	}
	return &http.Client{
		Transport: bearerTransport{host: parsed.Host, key: key, base: http.DefaultTransport},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("px1 MCP endpoint redirected; refusing to forward the API key")
		},
	}, nil
}

type bearerTransport struct {
	host, key string
	base      http.RoundTripper
}

func (t bearerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Host != t.host {
		return nil, fmt.Errorf("refusing to send the px1 API key to %s", request.URL.Host)
	}
	request = request.Clone(request.Context())
	request.Header.Set("Authorization", "Bearer "+t.key)
	return t.base.RoundTrip(request)
}
