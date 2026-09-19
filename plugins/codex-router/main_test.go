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
		"version": 1,
		"providers": map[string]any{
			"openai": map[string]any{"credential_mode": "request_passthrough", "responses_mode": "native"},
			"other":  map[string]any{"credential_mode": "bifrost", "responses_mode": "chat_polyfill"},
		},
		"models": map[string]any{
			"openai/a": map[string]any{"codex": map[string]any{}},
			"other/b":  map[string]any{"codex": map[string]any{}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func requestBody(model string) []byte {
	body, _ := json.Marshal(map[string]any{"model": model, "input": "hello"})
	return body
}

func TestPreAuthRoutesCredentials(t *testing.T) {
	initTestPlugin(t)
	t.Run("openai", func(t *testing.T) {
		req := &schemas.HTTPRequest{Method: "POST", Path: "/v1/responses", Headers: map[string]string{"Authorization": "Bearer openai-canary"}, Body: requestBody("openai/a")}
		resp, err := HTTPTransportPreAuthHook(nil, req)
		if err != nil || resp != nil {
			t.Fatalf("resp=%v err=%v", resp, err)
		}
		if req.Headers["Authorization"] == "" || req.Headers["x-bf-direct-key"] != "true" {
			t.Fatalf("headers = %#v", req.Headers)
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
