package config

import (
	"strings"
	"testing"
)

func TestDecodeDefaultsAndResolveAliases(t *testing.T) {
	cfg, err := Decode(strings.NewReader(`
version: 1
providers:
  openai:
    credential_mode: request_passthrough
    responses_mode: native
models:
  openai/gpt-test:
    aliases: [gpt-test]
    codex: {}
`))
	if err != nil {
		t.Fatal(err)
	}
	resolved, ok := cfg.ResolveModel("gpt-test")
	if !ok || resolved.Slug != "openai/gpt-test" {
		t.Fatalf("resolution = %#v, %v", resolved, ok)
	}
	if resolved.Model.Codex.ContextWindow != 131072 {
		t.Fatalf("context window = %d", resolved.Model.Codex.ContextWindow)
	}
}

func TestRejectsPassthroughForNonOpenAI(t *testing.T) {
	_, err := Decode(strings.NewReader(`
version: 1
providers:
  other:
    credential_mode: request_passthrough
    responses_mode: native
models:
  other/model:
    codex: {}
`))
	if err == nil || !strings.Contains(err.Error(), "only openai") {
		t.Fatalf("expected passthrough rejection, got %v", err)
	}
}

func TestRejectsAliasCollision(t *testing.T) {
	_, err := Decode(strings.NewReader(`
version: 1
providers:
  one: {credential_mode: bifrost, responses_mode: native}
models:
  one/a: {aliases: [shared], codex: {}}
  one/b: {aliases: [shared], codex: {}}
`))
	if err == nil || !strings.Contains(err.Error(), "owned by both") {
		t.Fatalf("expected collision rejection, got %v", err)
	}
}

func TestDecodeRejectsUnknownFields(t *testing.T) {
	_, err := Decode(strings.NewReader(`
version: 1
unexpected: true
providers:
  openai: {credential_mode: request_passthrough, responses_mode: native}
models:
  openai/test: {codex: {}}
`))
	if err == nil || !strings.Contains(err.Error(), "field unexpected not found") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func TestFromAnyRejectsUnknownFields(t *testing.T) {
	_, err := FromAny(map[string]any{
		"version":    1,
		"unexpected": true,
		"providers": map[string]any{
			"openai": map[string]any{"credential_mode": "request_passthrough", "responses_mode": "native"},
		},
		"models": map[string]any{"openai/test": map[string]any{"codex": map[string]any{}}},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func TestHostedToolFallbackMustBeNativeRequestPassthroughModel(t *testing.T) {
	t.Run("canonicalizes alias", func(t *testing.T) {
		cfg, err := Decode(strings.NewReader(`
version: 1
hosted_tool_fallback_model: luna
providers:
  openai: {credential_mode: request_passthrough, responses_mode: native}
models:
  openai/luna: {aliases: [luna], codex: {}}
`))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.HostedToolFallbackModel != "openai/luna" {
			t.Fatalf("fallback model = %q", cfg.HostedToolFallbackModel)
		}
	})

	t.Run("rejects managed provider", func(t *testing.T) {
		_, err := Decode(strings.NewReader(`
version: 1
hosted_tool_fallback_model: other/chat
providers:
  other: {credential_mode: bifrost, responses_mode: chat_polyfill}
models:
  other/chat: {codex: {}}
`))
		if err == nil || !strings.Contains(err.Error(), "native Responses with request_passthrough") {
			t.Fatalf("expected fallback validation error, got %v", err)
		}
	})
}
