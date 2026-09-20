package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/maximhq/bifrost/core/schemas"
)

func initTestPlugin(t *testing.T) {
	t.Helper()
	err := Init(map[string]any{
		"version":                    1,
		"hosted_tool_fallback_model": "openai/luna",
		"providers": map[string]any{
			"openai": map[string]any{"credential_mode": "request_passthrough", "responses_mode": "native"},
			"other":  map[string]any{"credential_mode": "bifrost", "responses_mode": "chat_polyfill"},
		},
		"models": map[string]any{
			"openai/a":    map[string]any{"codex": map[string]any{}},
			"openai/luna": map[string]any{"codex": map[string]any{}},
			"other/b":     map[string]any{"codex": map[string]any{}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPreAuthHostedToolFallsBackWithForwardedOpenAIAuth(t *testing.T) {
	initTestPlugin(t)
	body := []byte(`{"model":"other/b","input":"hello","tools":[{"type":"web_search"}]}`)
	req := &schemas.HTTPRequest{Method: "POST", Path: "/v1/responses", Headers: map[string]string{
		"Authorization": "Bearer openai-canary",
		"x-bf-vk":       "sk-bf-canary",
	}, Body: body}
	resp, err := HTTPTransportPreAuthHook(nil, req)
	if err != nil || resp != nil {
		t.Fatalf("resp=%v err=%v", resp, err)
	}
	var envelope struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(req.Body, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Model != "openai/luna" {
		t.Fatalf("model = %q", envelope.Model)
	}
	if req.Headers["Authorization"] != "Bearer openai-canary" || req.Headers["x-bf-direct-key"] != "true" {
		t.Fatalf("forwarded auth was not selected: %#v", req.Headers)
	}
}

func TestPreAuthNamespaceStaysOnManagedProvider(t *testing.T) {
	initTestPlugin(t)
	body := []byte(`{"model":"other/b","input":"hello","tools":[{"type":"namespace","name":"codex","tools":[{"type":"function","name":"shell"}]}]}`)
	req := &schemas.HTTPRequest{Method: "POST", Path: "/v1/responses", Headers: map[string]string{
		"Authorization": "Bearer openai-canary",
		"x-bf-vk":       "sk-bf-canary",
	}, Body: body}
	resp, err := HTTPTransportPreAuthHook(nil, req)
	if err != nil || resp != nil {
		t.Fatalf("resp=%v err=%v", resp, err)
	}
	if string(req.Body) != string(body) {
		t.Fatalf("namespace request was rerouted: %s", req.Body)
	}
	if len(req.Headers) != 1 || req.Headers["x-bf-vk"] == "" {
		t.Fatalf("managed-provider credential policy not applied: %#v", req.Headers)
	}
}

func requestBody(model string) []byte {
	body, _ := json.Marshal(map[string]any{"model": model, "input": "hello"})
	return body
}

func TestPreAuthRoutesCredentials(t *testing.T) {
	initTestPlugin(t)
	t.Run("openai", func(t *testing.T) {
		req := &schemas.HTTPRequest{Method: "POST", Path: "/v1/responses", Headers: map[string]string{"Authorization": "Bearer openai-canary", "x-bf-vk": "sk-bf-canary"}, Body: requestBody("openai/a")}
		resp, err := HTTPTransportPreAuthHook(nil, req)
		if err != nil || resp != nil {
			t.Fatalf("resp=%v err=%v", resp, err)
		}
		if req.Headers["Authorization"] == "" || req.Headers["x-bf-direct-key"] != "true" {
			t.Fatalf("headers = %#v", req.Headers)
		}
	})
	t.Run("openai requires Bifrost virtual key", func(t *testing.T) {
		req := &schemas.HTTPRequest{Method: "POST", Path: "/v1/responses", Headers: map[string]string{"Authorization": "Bearer openai-canary"}, Body: requestBody("openai/a")}
		resp, err := HTTPTransportPreAuthHook(nil, req)
		if err != nil || resp == nil || resp.StatusCode != 401 {
			t.Fatalf("resp=%v err=%v", resp, err)
		}
		if req.Headers["x-bf-direct-key"] != "" {
			t.Fatalf("direct key enabled without gateway auth: %#v", req.Headers)
		}
	})
	t.Run("bifrost", func(t *testing.T) {
		req := &schemas.HTTPRequest{Method: "POST", Path: "/v1/responses", Headers: map[string]string{"Authorization": "Bearer openai-canary", "x-bf-direct-key": "true"}, Body: requestBody("other/b")}
		resp, err := HTTPTransportPreAuthHook(nil, req)
		if err != nil || resp != nil {
			t.Fatalf("resp=%v err=%v", resp, err)
		}
		if len(req.Headers) != 0 {
			t.Fatalf("credential leaked: %#v", req.Headers)
		}
	})
}

func TestPreAuthRoutesChatGPTPassthroughCredentials(t *testing.T) {
	initTestPlugin(t)
	req := &schemas.HTTPRequest{
		Method: "POST",
		Path:   "/chatgpt_passthrough/backend-api/codex/responses",
		Headers: map[string]string{
			"Authorization": "Bearer openai-canary",
			"x-bf-vk":       "sk-bf-canary",
		},
		Body: requestBody("a"),
	}
	resp, err := HTTPTransportPreAuthHook(nil, req)
	if err != nil || resp != nil {
		t.Fatalf("resp=%v err=%v", resp, err)
	}
	if req.Headers["Authorization"] == "" || req.Headers["x-bf-direct-key"] != "true" {
		t.Fatalf("headers = %#v", req.Headers)
	}
}

func TestPreLLMSelectsPolyfill(t *testing.T) {
	initTestPlugin(t)
	ctx := schemas.NewBifrostContext(context.Background(), time.Now().Add(time.Minute))
	defer ctx.Cancel()
	req := &schemas.BifrostRequest{RequestType: schemas.ResponsesStreamRequest, ResponsesRequest: &schemas.BifrostResponsesRequest{Provider: "other", Model: "b"}}
	_, short, err := PreLLMHook(ctx, req)
	if err != nil || short != nil {
		t.Fatalf("short=%v err=%v", short, err)
	}
	if got, _ := ctx.Value(schemas.BifrostContextKeyChangeRequestType).(schemas.RequestType); got != schemas.ChatCompletionRequest {
		t.Fatalf("change request type = %q", got)
	}
}

func TestPostHookHydratesCodexCatalog(t *testing.T) {
	initTestPlugin(t)
	req := &schemas.HTTPRequest{Method: "GET", Path: "/v1/models", Query: map[string]string{"client_version": "test"}}
	resp := &schemas.HTTPResponse{StatusCode: 200, Body: []byte(`{"data":[{"id":"openai/a"}]}`)}
	if err := HTTPTransportPostHook(nil, req, resp); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(resp.Body, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["models"]; !ok {
		t.Fatalf("not a Codex catalog: %s", resp.Body)
	}
}
