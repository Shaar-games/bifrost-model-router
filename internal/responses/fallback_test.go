package responses

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/applyinnovations/bifrost-model-router/internal/config"
)

func fallbackTestConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := config.Config{
		Version:                 1,
		HostedToolFallbackModel: "luna",
		Providers: map[string]config.ProviderProfile{
			"openai":  {CredentialMode: config.CredentialRequestPassthrough, ResponsesMode: config.ResponsesNative},
			"managed": {CredentialMode: config.CredentialBifrost, ResponsesMode: config.ResponsesChatPolyfill},
		},
		Models: map[string]config.ModelProfile{
			"openai/luna":        {Aliases: []string{"luna"}, Codex: config.CodexProfile{}},
			"managed/text-model": {Codex: config.CodexProfile{}},
		},
	}
	if err := cfg.ApplyDefaultsAndValidate(); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestApplyHostedToolFallbackRewritesWholeRequest(t *testing.T) {
	body := []byte(`{"model":"managed/text-model","input":"hello","tools":[{"type":"namespace","name":"functions","tools":[{"type":"function","name":"shell"}]},{"type":"web_search"}]}`)
	routed, decision, err := ApplyHostedToolFallback(body, fallbackTestConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	if decision == nil || decision.OriginalModel != "managed/text-model" || decision.FallbackModel != "openai/luna" {
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
	body := []byte(`{"model":"managed/text-model","tools":[{"type":"namespace","name":"codex","tools":[{"type":"function","name":"shell"}]}]}`)
	routed, decision, err := ApplyHostedToolFallback(body, fallbackTestConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	if decision != nil || string(routed) != string(body) {
		t.Fatalf("unexpected fallback: decision=%#v body=%s", decision, routed)
	}
}

func policyTestConfig(t *testing.T, policy string) config.Config {
	t.Helper()
	cfg := fallbackTestConfig(t)
	if err := cfg.OverrideHostedToolPolicy(policy); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestStripPolicyKeepsPolyfilledModelWithoutHostedTools(t *testing.T) {
	body := []byte(`{"model":"managed/text-model","input":"hello","tool_choice":{"type":"web_search"},"tools":[{"type":"namespace","name":"mixed","tools":[{"type":"function","name":"shell"},{"type":"file_search"}]},{"type":"web_search"},{"type":"function","name":"apply_patch"}]}`)
	routed, decision, err := ApplyHostedToolFallback(body, policyTestConfig(t, "strip"))
	if err != nil {
		t.Fatal(err)
	}
	if decision != nil {
		t.Fatalf("unexpected fallback: %#v", decision)
	}
	var got struct {
		Model      string          `json:"model"`
		ToolChoice json.RawMessage `json:"tool_choice"`
		Tools      []rawTool       `json:"tools"`
	}
	if err := json.Unmarshal(routed, &got); err != nil {
		t.Fatal(err)
	}
	if got.Model != "managed/text-model" || got.ToolChoice != nil {
		t.Fatalf("model = %q tool_choice = %s", got.Model, got.ToolChoice)
	}
	if len(hostedToolTypes(got.Tools)) != 0 || len(got.Tools) != 2 || len(got.Tools[0].Tools) != 1 || got.Tools[1].Type != "function" {
		t.Fatalf("tools = %#v", got.Tools)
	}
}

func TestStripPolicyRemovesEmptyToolFields(t *testing.T) {
	body := []byte(`{"model":"managed/text-model","input":"hello","tool_choice":"auto","parallel_tool_calls":true,"tools":[{"type":"web_search"}]}`)
	routed, _, err := ApplyHostedToolFallback(body, policyTestConfig(t, "strip"))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(routed, &got); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"tools", "tool_choice", "parallel_tool_calls"} {
		if _, ok := got[field]; ok {
			t.Fatalf("%s was not removed: %s", field, routed)
		}
	}
}

func TestRejectPolicyLeavesRequestForAdapter(t *testing.T) {
	body := []byte(`{"model":"managed/text-model","tools":[{"type":"web_search"}]}`)
	routed, decision, err := ApplyHostedToolFallback(body, policyTestConfig(t, "reject"))
	if err != nil {
		t.Fatal(err)
	}
	if decision != nil || string(routed) != string(body) {
		t.Fatalf("unexpected rewrite: decision=%#v body=%s", decision, routed)
	}
}

func bridgeTestConfig(t *testing.T, overrides map[string]config.HostedToolPolicy) config.Config {
	t.Helper()
	cfg := fallbackTestConfig(t)
	cfg.HostedToolPolicy = config.HostedToolStrip
	cfg.HostedToolOverrides = overrides
	if err := cfg.ApplyDefaultsAndValidate(); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestDefaultBridgeKeepsRequestAndStripsOtherHostedTools(t *testing.T) {
	cfg := fallbackTestConfig(t)
	cfg.HostedToolPolicy = config.HostedToolBridge
	if err := cfg.ApplyDefaultsAndValidate(); err != nil {
		t.Fatal(err)
	}
	routing, err := RouteHostedTools([]byte(`{"model":"managed/text-model","tools":[{"type":"web_search"},{"type":"file_search"}]}`), cfg)
	if err != nil || routing.Fallback != nil || len(routing.Bridges) != 1 || routing.Bridges[0] != "web_search" {
		t.Fatalf("routing = %#v err = %v", routing, err)
	}
	if strings.Contains(string(routing.Body), "file_search") {
		t.Fatalf("file_search survived: %s", routing.Body)
	}
}

func TestRouteHostedToolsBridgesConfiguredTools(t *testing.T) {
	cfg := bridgeTestConfig(t, map[string]config.HostedToolPolicy{"web_search": config.HostedToolBridge, "image_generation": config.HostedToolBridge})
	body := []byte(`{"model":"managed/text-model","input":"hi","tool_choice":{"type":"web_search_preview"},"tools":[{"type":"function","name":"shell"},{"type":"web_search_preview"},{"type":"image_generation"},{"type":"code_interpreter"}]}`)
	routing, err := RouteHostedTools(body, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if routing.Fallback != nil || strings.Join(routing.Bridges, ",") != "image_generation,web_search" {
		t.Fatalf("routing = %#v", routing)
	}
	var got struct {
		Model      string `json:"model"`
		ToolChoice struct {
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"tool_choice"`
		Tools []struct {
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(routing.Body, &got); err != nil {
		t.Fatal(err)
	}
	if got.Model != "managed/text-model" || got.ToolChoice.Type != "function" || got.ToolChoice.Name != "web_search" {
		t.Fatalf("model = %q tool_choice = %#v", got.Model, got.ToolChoice)
	}
	var names []string
	for _, tool := range got.Tools {
		if tool.Type != "function" {
			t.Fatalf("hosted tool survived: %#v", got.Tools)
		}
		names = append(names, tool.Name)
	}
	if strings.Join(names, ",") != "shell,web_search,image_generation" {
		t.Fatalf("tools = %v", names)
	}

	legacyBody, decision, err := ApplyHostedToolFallback(body, cfg)
	if err != nil || decision != nil || strings.Contains(string(legacyBody), `"web_search"`) {
		t.Fatalf("non-bridging caller got decision=%#v err=%v body=%s", decision, err, legacyBody)
	}
}

func TestRouteHostedToolsSkipsBridgeWhenFunctionNameIsTaken(t *testing.T) {
	cfg := bridgeTestConfig(t, map[string]config.HostedToolPolicy{"web_search": config.HostedToolBridge})
	body := []byte(`{"model":"managed/text-model","tools":[{"type":"function","name":"web_search"},{"type":"web_search"}]}`)
	routing, err := RouteHostedTools(body, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(routing.Bridges) != 0 || strings.Count(string(routing.Body), `"web_search"`) != 1 {
		t.Fatalf("routing = %#v body = %s", routing, routing.Body)
	}
}

func TestRouteHostedToolsPerToolFallbackAndReject(t *testing.T) {
	cfg := bridgeTestConfig(t, map[string]config.HostedToolPolicy{"web_search": config.HostedToolBridge, "file_search": config.HostedToolFallback, "mcp": config.HostedToolReject})
	fallback, err := RouteHostedTools([]byte(`{"model":"managed/text-model","tools":[{"type":"web_search"},{"type":"file_search"}]}`), cfg)
	if err != nil || fallback.Fallback == nil || fallback.Fallback.FallbackModel != "openai/luna" {
		t.Fatalf("fallback routing = %#v err = %v", fallback, err)
	}
	body := []byte(`{"model":"managed/text-model","tools":[{"type":"web_search"},{"type":"mcp"}]}`)
	rejected, err := RouteHostedTools(body, cfg)
	if err != nil || rejected.Fallback != nil || len(rejected.Bridges) != 0 || string(rejected.Body) != string(body) {
		t.Fatalf("reject routing = %#v err = %v", rejected, err)
	}
}

func TestBridgeFunctionToolsCoverBridgeableFamilies(t *testing.T) {
	for _, family := range config.BridgeableHostedTools {
		var tool struct {
			Type string `json:"type"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(BridgeFunctionTool(family), &tool); err != nil || tool.Type != "function" || tool.Name != family {
			t.Fatalf("bridge tool for %s = %#v err = %v", family, tool, err)
		}
	}
}

func TestApplyHostedToolFallbackFindsNestedHostedTool(t *testing.T) {
	body := []byte(`{"model":"managed/text-model","tools":[{"type":"namespace","name":"mixed","tools":[{"type":"file_search"}]}]}`)
	_, decision, err := ApplyHostedToolFallback(body, fallbackTestConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	if decision == nil || len(decision.ToolTypes) != 1 || decision.ToolTypes[0] != "file_search" {
		t.Fatalf("decision = %#v", decision)
	}
}
