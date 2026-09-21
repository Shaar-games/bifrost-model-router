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
	if resolved.UpstreamModel != "gpt-test" || resolved.BaseSlug != "openai/gpt-test" {
		t.Fatalf("upstream resolution = %#v", resolved)
	}
}

func TestContextVariantsResolveDeterministically(t *testing.T) {
	cfg, err := Decode(strings.NewReader(`
version: 1
providers:
  openai: {credential_mode: request_passthrough, responses_mode: native}
  voke: {credential_mode: bifrost, responses_mode: chat_polyfill}
models:
  openai/sol:
    aliases: [sol]
    upstream_model: gpt-sol-upstream
    codex: {context_window: 272000, max_context_window: 872000, effective_context_window_percent: 95}
    context_variants:
      - {context_window: 872000}
  voke/deepseek:
    aliases: [deepseek]
    codex: {context_window: 256000, max_context_window: 1000000, effective_context_window_percent: 90}
    context_variants:
      - {context_window: 256000}
      - {context_window: 1000000, effective_context_window_percent: 95}
`))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, slug, base, upstream string
		window, percent            int64
	}{
		{"sol-872k", "sol-872k", "openai/sol", "gpt-sol-upstream", 872000, 95},
		{"openai/sol-872k", "sol-872k", "openai/sol", "gpt-sol-upstream", 872000, 95},
		{"voke/deepseek-256k", "voke/deepseek-256k", "voke/deepseek", "deepseek", 256000, 90},
		{"deepseek-1m", "voke/deepseek-1m", "voke/deepseek", "deepseek", 1000000, 95},
	}
	for _, test := range tests {
		resolved, ok := cfg.ResolveModel(test.name)
		if !ok || resolved.Variant == nil || resolved.Slug != test.slug || resolved.BaseSlug != test.base || resolved.UpstreamModel != test.upstream || resolved.Model.Codex.ContextWindow != test.window || resolved.Model.Codex.EffectiveContextWindowPercent != test.percent {
			t.Errorf("ResolveModel(%q) = %#v, %v", test.name, resolved, ok)
		}
	}
	if _, ok := cfg.ResolveModel("sol-512k"); ok {
		t.Fatal("unconfigured context suffix resolved")
	}
}

func TestContextVariantValidation(t *testing.T) {
	tests := []struct{ name, variant string }{
		{"non-round", "872001"},
		{"above maximum", "873000"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Decode(strings.NewReader(`
version: 1
providers:
  openai: {credential_mode: request_passthrough, responses_mode: native}
models:
  openai/sol:
    aliases: [sol]
    codex: {context_window: 272000, max_context_window: 872000}
    context_variants: [{context_window: ` + test.variant + `}]
`))
			if err == nil {
				t.Fatal("expected context variant validation error")
			}
		})
	}
}

func TestContextVariantCollidesWithAlias(t *testing.T) {
	_, err := Decode(strings.NewReader(`
version: 1
providers:
  openai: {credential_mode: request_passthrough, responses_mode: native}
models:
  openai/sol:
    aliases: [sol, sol-872k]
    codex: {context_window: 272000, max_context_window: 872000}
    context_variants: [{context_window: 872000}]
`))
	if err == nil || !strings.Contains(err.Error(), "collides") {
		t.Fatalf("expected generated slug collision, got %v", err)
	}
}

func TestRealModelNameEndingInContextLikeSuffixIsNotRewritten(t *testing.T) {
	cfg, err := Decode(strings.NewReader(`
version: 1
providers:
  voke: {credential_mode: bifrost, responses_mode: native}
models:
  voke/native-1m:
    upstream_model: actual-native-1m
    codex: {context_window: 128000}
`))
	if err != nil {
		t.Fatal(err)
	}
	resolved, ok := cfg.ResolveModel("voke/native-1m")
	if !ok || resolved.Variant != nil || resolved.UpstreamModel != "actual-native-1m" {
		t.Fatalf("resolution = %#v, %v", resolved, ok)
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
