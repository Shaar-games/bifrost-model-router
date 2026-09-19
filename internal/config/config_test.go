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
