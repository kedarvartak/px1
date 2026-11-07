package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type ProviderError struct {
	ProviderName string
	StatusCode   int
	Retry        bool
	// Detail is the provider's own error message, bounded. It explains
	// failures such as an unknown model, and is meant for the operator's
	// terminal: callers must not copy it into reports or traces.
	Detail string
}

func (e ProviderError) Error() string {
	message := fmt.Sprintf("%s LLM request failed with status %d", e.ProviderName, e.StatusCode)
	if e.Detail != "" {
		message += ": " + e.Detail
	}
	return message
}

func (e ProviderError) Temporary() bool { return e.Retry }

type OpenAIProvider struct {
	APIKey   string
	Endpoint string
	HTTP     *http.Client
}

func (OpenAIProvider) Name() string { return "openai" }

func (p OpenAIProvider) Complete(ctx context.Context, request Request) (Response, error) {
	payload := map[string]any{
		"model":             request.Model,
		"input":             []map[string]string{{"role": "system", "content": request.System}, {"role": "user", "content": request.Prompt}},
		"max_output_tokens": request.MaxOutputTokens,
	}
	endpoint := p.Endpoint
	if endpoint == "" {
		endpoint = "https://api.openai.com/v1/responses"
	}
	var decoded struct {
		ID     string `json:"id"`
		Output []struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Usage struct {
			InputTokens  int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
			InputDetails struct {
				CachedTokens int64 `json:"cached_tokens"`
			} `json:"input_tokens_details"`
		} `json:"usage"`
	}
	if err := postJSON(ctx, p.HTTP, endpoint, map[string]string{"Authorization": "Bearer " + p.APIKey}, payload, &decoded, "openai"); err != nil {
		return Response{}, err
	}
	var text strings.Builder
	for _, output := range decoded.Output {
		for _, content := range output.Content {
			text.WriteString(content.Text)
		}
	}
	return Response{Text: text.String(), RequestID: decoded.ID, Usage: Usage{InputTokens: decoded.Usage.InputTokens, OutputTokens: decoded.Usage.OutputTokens, CacheReadTokens: decoded.Usage.InputDetails.CachedTokens}}, nil
}

type AnthropicProvider struct {
	APIKey   string
	Endpoint string
	Version  string
	HTTP     *http.Client
}

func (AnthropicProvider) Name() string { return "anthropic" }

func (p AnthropicProvider) Complete(ctx context.Context, request Request) (Response, error) {
	payload := map[string]any{
		"model": request.Model, "system": request.System,
		"messages":   []map[string]string{{"role": "user", "content": request.Prompt}},
		"max_tokens": request.MaxOutputTokens, "temperature": request.Temperature,
	}
	endpoint := p.Endpoint
	if endpoint == "" {
		endpoint = "https://api.anthropic.com/v1/messages"
	}
	version := p.Version
	if version == "" {
		version = "2023-06-01"
	}
	var decoded struct {
		ID      string `json:"id"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens          int64 `json:"input_tokens"`
			OutputTokens         int64 `json:"output_tokens"`
			CacheReadInputTokens int64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
	}
	if err := postJSON(ctx, p.HTTP, endpoint, map[string]string{"x-api-key": p.APIKey, "anthropic-version": version}, payload, &decoded, "anthropic"); err != nil {
		return Response{}, err
	}
	var text strings.Builder
	for _, content := range decoded.Content {
		text.WriteString(content.Text)
	}
	return Response{Text: text.String(), RequestID: decoded.ID, Usage: Usage{InputTokens: decoded.Usage.InputTokens, OutputTokens: decoded.Usage.OutputTokens, CacheReadTokens: decoded.Usage.CacheReadInputTokens}}, nil
}

// BedrockInvoker is implemented by the deployment's AWS SDK adapter. Keeping
// signing outside this package makes the gateway testable and avoids handling
// long-lived AWS credentials.
type BedrockInvoker interface {
	InvokeModel(context.Context, string, Request) (Response, error)
}

type BedrockProvider struct{ Invoker BedrockInvoker }

func (BedrockProvider) Name() string { return "bedrock" }

func (p BedrockProvider) Complete(ctx context.Context, request Request) (Response, error) {
	if p.Invoker == nil {
		return Response{}, fmt.Errorf("bedrock invoker is required")
	}
	return p.Invoker.InvokeModel(ctx, request.Model, request)
}

func postJSON(ctx context.Context, client *http.Client, endpoint string, headers map[string]string, payload any, output any, provider string) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return ProviderError{ProviderName: provider, StatusCode: response.StatusCode, Retry: response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500, Detail: errorDetail(response.Body)}
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(output); err != nil {
		return fmt.Errorf("decode %s LLM response: %w", provider, err)
	}
	return nil
}

// errorDetail extracts the message from a provider's JSON error body. It reads
// at most 4 KiB and returns at most 300 characters; anything that is not the
// usual {"error":{"message":...}} shape yields no detail.
func errorDetail(body io.Reader) string {
	data, _ := io.ReadAll(io.LimitReader(body, 4<<10))
	var decoded struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &decoded) != nil {
		return ""
	}
	message := strings.Join(strings.Fields(strings.ToValidUTF8(decoded.Error.Message, "")), " ")
	if len(message) > 300 {
		message = strings.ToValidUTF8(message[:300], "") + "…"
	}
	return message
}
