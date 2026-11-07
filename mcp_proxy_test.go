package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"px1/internal/hosted/apikeys"
	"px1/internal/hosted/apikeys/apikeystest"
	"px1/internal/hosted/mcpserver"
)

type proxyBackend struct{ mcpserver.Backend }

func (proxyBackend) ListFindings(_ context.Context, tenant int64, repo string, pull int, status string) ([]mcpserver.Finding, error) {
	return []mcpserver.Finding{{ID: "f1", Path: "a.go", Title: repo, Status: status}}, nil
}

func (proxyBackend) GetFinding(context.Context, int64, string) (mcpserver.Finding, bool, error) {
	return mcpserver.Finding{}, false, nil
}

func TestMCPProxyForwardsOnlyTheKeysTools(t *testing.T) {
	manager := apikeys.Manager{Store: &apikeystest.Store{}}
	key, _, err := manager.Issue(context.Background(), 42, "laptop", []apikeys.Scope{apikeys.FindingsRead}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	hosted := httptest.NewServer(mcpserver.Handler(manager, nil, proxyBackend{}, "test"))
	defer hosted.Close()
	httpClient, err := mcpHTTPClient(hosted.URL, key)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverEnd, clientEnd := mcp.NewInMemoryTransports()
	done := make(chan error, 1)
	go func() {
		done <- serveMCPProxy(ctx, &mcp.StreamableClientTransport{Endpoint: hosted.URL, HTTPClient: httpClient}, serverEnd)
	}()
	agent := mcp.NewClient(&mcp.Implementation{Name: "agent", Version: "1"}, nil)
	session, err := agent.Connect(ctx, clientEnd, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"get_finding", "list_findings"}) {
		t.Fatalf("proxied tools = %v", names)
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_findings", Arguments: map[string]any{"repository": "acme/api", "pull_number": 3}})
	if err != nil || result.IsError {
		t.Fatalf("proxied call: %#v %v", result, err)
	}
	session.Close()
	cancel()
	<-done
}

func TestMCPHTTPClientKeepsTheKeyOnItsHost(t *testing.T) {
	for _, endpoint := range []string{"http://mcp.px1.dev/mcp", "https://user:pw@mcp.px1.dev/mcp", "mcp.px1.dev", "ftp://mcp.px1.dev"} {
		if _, err := mcpHTTPClient(endpoint, "px1k_x"); err == nil {
			t.Fatalf("%s accepted", endpoint)
		}
	}
	var leaked string
	elsewhere := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { leaked = r.Header.Get("Authorization") }))
	defer elsewhere.Close()
	redirector := httptest.NewServer(http.RedirectHandler(elsewhere.URL, http.StatusTemporaryRedirect))
	defer redirector.Close()
	client, err := mcpHTTPClient(redirector.URL, "px1k_secret")
	if err != nil {
		t.Fatal(err)
	}
	if response, err := client.Get(redirector.URL); err == nil {
		response.Body.Close()
		if response.StatusCode != http.StatusTemporaryRedirect {
			t.Fatalf("redirect followed: %d", response.StatusCode)
		}
	}
	if _, err := client.Get(elsewhere.URL); err == nil || !strings.Contains(err.Error(), "refusing to send") {
		t.Fatalf("request to another host: %v", err)
	}
	if leaked != "" {
		t.Fatalf("API key reached another host: %q", leaked)
	}
}

func TestRunMCPRequiresAKeyFromTheEnvironment(t *testing.T) {
	t.Setenv("PX1_API_KEY", "")
	if err := runMCP(nil); err == nil || !strings.Contains(err.Error(), "PX1_API_KEY") {
		t.Fatalf("missing key: %v", err)
	}
}
