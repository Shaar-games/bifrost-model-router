package responses

import (
	"encoding/json"
	"testing"

	"github.com/applyinnovations/bifrost-model-router/internal/config"
)

func fallbackTestConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := config.Config{
		Version:                 1,
		HostedToolFallbackModel: "luna",
		Providers: map[string]config.ProviderProfile{
			"openai": {CredentialMode: config.CredentialRequestPassthrough, ResponsesMode: config.ResponsesNative},
			"voke":   {CredentialMode: config.CredentialBifrost, ResponsesMode: config.ResponsesChatPolyfill},
		},
		Models: map[string]config.ModelProfile{
			"openai/luna": {Aliases: []string{"luna"}, Codex: config.CodexProfile{}},
			"voke/flash":  {Codex: config.CodexProfile{}},
		},
	}
	if err := cfg.ApplyDefaultsAndValidate(); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestApplyHostedToolFallbackRewritesWholeRequest(t *testing.T) {
	body := []byte(`{"model":"voke/flash","input":"hello","tools":[{"type":"namespace","name":"functions","tools":[{"type":"function","name":"shell"}]},{"type":"web_search"}]}`)
	routed, decision, err := ApplyHostedToolFallback(body, fallbackTestConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	if decision == nil || decision.OriginalModel != "voke/flash" || decision.FallbackModel != "openai/luna" {
		t.Fatalf("decision = %#v", decision)
	}
	if len(decision.ToolTypes) != 1 || decision.ToolTypes[0] != "web_search" {
		t.Fatalf("tool types = %#v", decision.ToolTypes)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(routed, &got); err != nil {
		t.Fatal(err)
	}
	var model string
	if err := json.Unmarshal(got["model"], &model); err != nil || model != "openai/luna" {
		t.Fatalf("model = %q, err = %v", model, err)
	}
	if _, ok := got["input"]; !ok {
		t.Fatal("request fields were not preserved")
	}
}

func TestApplyHostedToolFallbackLeavesNamespaceForCore(t *testing.T) {
	body := []byte(`{"model":"voke/flash","tools":[{"type":"namespace","name":"codex","tools":[{"type":"function","name":"shell"}]}]}`)
	routed, decision, err := ApplyHostedToolFallback(body, fallbackTestConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	if decision != nil || string(routed) != string(body) {
		t.Fatalf("unexpected fallback: decision=%#v body=%s", decision, routed)
	}
}

func TestApplyHostedToolFallbackFindsNestedHostedTool(t *testing.T) {
	body := []byte(`{"model":"voke/flash","tools":[{"type":"namespace","name":"mixed","tools":[{"type":"file_search"}]}]}`)
	_, decision, err := ApplyHostedToolFallback(body, fallbackTestConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	if decision == nil || len(decision.ToolTypes) != 1 || decision.ToolTypes[0] != "file_search" {
		t.Fatalf("decision = %#v", decision)
	}
}
