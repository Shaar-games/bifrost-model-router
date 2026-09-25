package responses

import (
	"encoding/json"
	"slices"
	"sort"
	"strings"

	"github.com/applyinnovations/bifrost-model-router/internal/config"
)

// HostedToolFallback describes a whole-request reroute to a native Responses
// model. The caller's request-scoped OpenAI credential is selected separately
// by the transport credential policy after the model has been rewritten.
type HostedToolFallback struct {
	OriginalModel string
	FallbackModel string
	ToolTypes     []string
}

// HostedToolRouting is the outcome of applying the per-tool hosted tool
// policies to one Responses request.
type HostedToolRouting struct {
	Body     []byte
	Fallback *HostedToolFallback
	// Bridges lists the function names that replaced bridged hosted tools.
	// The caller must execute calls to them; they are not Codex tools.
	Bridges []string
}

type rawTool struct {
	Type  string    `json:"type"`
	Tools []rawTool `json:"tools,omitempty"`
}

// ApplyHostedToolFallback applies the hosted tool policies for a caller that
// cannot execute bridged calls, so bridged tools are stripped instead.
func ApplyHostedToolFallback(body []byte, cfg config.Config) ([]byte, *HostedToolFallback, error) {
	routing, err := routeHostedTools(body, cfg, false)
	return routing.Body, routing.Fallback, err
}

// RouteHostedTools rewrites only Chat Completions-polyfilled requests
// containing server-side tools. Namespace tools are deliberately excluded:
// Bifrost core flattens their nested functions for non-namespace-capable wires
// and restores the namespaced tool calls on the response path.
//
// A reject policy leaves the request unchanged for the adapter to refuse, a
// fallback policy reroutes the whole request, and otherwise each tool is
// stripped or replaced by its bridge function.
func RouteHostedTools(body []byte, cfg config.Config) (HostedToolRouting, error) {
	return routeHostedTools(body, cfg, true)
}

func routeHostedTools(body []byte, cfg config.Config, allowBridge bool) (HostedToolRouting, error) {
	unchanged := HostedToolRouting{Body: body}
	var envelope struct {
		Model string    `json:"model"`
		Tools []rawTool `json:"tools"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return unchanged, err
	}
	requested, ok := cfg.ResolveModel(envelope.Model)
	if !ok || requested.Model.ResponsesMode != config.ResponsesChatPolyfill {
		return unchanged, nil
	}

	types := hostedToolTypes(envelope.Tools)
	if len(types) == 0 {
		return unchanged, nil
	}
	policyFor := func(toolType string) config.HostedToolPolicy {
		policy := cfg.HostedToolPolicyFor(toolType)
		family := config.HostedToolFamily(toolType)
		if policy == config.HostedToolBridge && (!allowBridge || !slices.Contains(config.BridgeableHostedTools, family)) {
			return config.HostedToolStrip
		}
		return policy
	}
	fallback := false
	for _, toolType := range types {
		switch policyFor(toolType) {
		case config.HostedToolReject:
			return unchanged, nil
		case config.HostedToolFallback:
			fallback = true
		}
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return unchanged, err
	}
	if fallback {
		if cfg.HostedToolFallbackModel == "" {
			return unchanged, nil
		}
		encodedModel, err := json.Marshal(cfg.HostedToolFallbackModel)
		if err != nil {
			return unchanged, err
		}
		fields["model"] = encodedModel
		routed, err := json.Marshal(fields)
		if err != nil {
			return unchanged, err
		}
		return HostedToolRouting{Body: routed, Fallback: &HostedToolFallback{
			OriginalModel: envelope.Model,
			FallbackModel: cfg.HostedToolFallbackModel,
			ToolTypes:     types,
		}}, nil
	}
	return rewriteHostedTools(fields, policyFor)
}

// rewriteHostedTools keeps the request on the polyfilled model by removing
// the server-side tools it cannot execute and replacing bridged top-level
// tools with their function definitions, including a tool_choice naming one.
func rewriteHostedTools(fields map[string]json.RawMessage, policyFor func(string) config.HostedToolPolicy) (HostedToolRouting, error) {
	var tools []json.RawMessage
	if err := json.Unmarshal(fields["tools"], &tools); err != nil {
		return HostedToolRouting{}, err
	}
	functionNames := make(map[string]bool)
	for _, raw := range tools {
		var tool struct {
			Type string `json:"type"`
			Name string `json:"name"`
		}
		if json.Unmarshal(raw, &tool) == nil && tool.Type == "function" {
			functionNames[tool.Name] = true
		}
	}

	bridged := make(map[string]string)
	kept := make([]json.RawMessage, 0, len(tools))
	for _, raw := range tools {
		var tool map[string]json.RawMessage
		if err := json.Unmarshal(raw, &tool); err != nil {
			return HostedToolRouting{}, err
		}
		var toolType string
		_ = json.Unmarshal(tool["type"], &toolType)
		if isHostedToolType(toolType) {
			family := config.HostedToolFamily(toolType)
			if policyFor(toolType) != config.HostedToolBridge || functionNames[family] {
				continue
			}
			if _, done := bridged[family]; !done {
				bridged[family] = family
				kept = append(kept, BridgeFunctionTool(family))
			}
			continue
		}
		if nested, ok := tool["tools"]; ok {
			var children []json.RawMessage
			if err := json.Unmarshal(nested, &children); err != nil {
				return HostedToolRouting{}, err
			}
			children, err := withoutHostedTools(children)
			if err != nil {
				return HostedToolRouting{}, err
			}
			encoded, err := json.Marshal(children)
			if err != nil {
				return HostedToolRouting{}, err
			}
			tool["tools"] = encoded
			if raw, err = json.Marshal(tool); err != nil {
				return HostedToolRouting{}, err
			}
		}
		kept = append(kept, raw)
	}

	if len(kept) == 0 {
		delete(fields, "tools")
		delete(fields, "tool_choice")
		delete(fields, "parallel_tool_calls")
	} else {
		encoded, err := json.Marshal(kept)
		if err != nil {
			return HostedToolRouting{}, err
		}
		fields["tools"] = encoded
		var choice rawTool
		if json.Unmarshal(fields["tool_choice"], &choice) == nil && isHostedToolType(choice.Type) {
			if name, ok := bridged[config.HostedToolFamily(choice.Type)]; ok {
				fields["tool_choice"], _ = json.Marshal(map[string]string{"type": "function", "name": name})
			} else {
				delete(fields, "tool_choice")
			}
		}
	}
	routed, err := json.Marshal(fields)
	if err != nil {
		return HostedToolRouting{}, err
	}
	names := make([]string, 0, len(bridged))
	for name := range bridged {
		names = append(names, name)
	}
	sort.Strings(names)
	return HostedToolRouting{Body: routed, Bridges: names}, nil
}

func withoutHostedTools(tools []json.RawMessage) ([]json.RawMessage, error) {
	kept := make([]json.RawMessage, 0, len(tools))
	for _, raw := range tools {
		var tool map[string]json.RawMessage
		if err := json.Unmarshal(raw, &tool); err != nil {
			return nil, err
		}
		var toolType string
		_ = json.Unmarshal(tool["type"], &toolType)
		if isHostedToolType(toolType) {
			continue
		}
		if nested, ok := tool["tools"]; ok {
			var children []json.RawMessage
			if err := json.Unmarshal(nested, &children); err != nil {
				return nil, err
			}
			children, err := withoutHostedTools(children)
			if err != nil {
				return nil, err
			}
			encoded, err := json.Marshal(children)
			if err != nil {
				return nil, err
			}
			tool["tools"] = encoded
			if raw, err = json.Marshal(tool); err != nil {
				return nil, err
			}
		}
		kept = append(kept, raw)
	}
	return kept, nil
}

func hostedToolTypes(tools []rawTool) []string {
	seen := make(map[string]bool)
	var visit func([]rawTool)
	visit = func(items []rawTool) {
		for _, tool := range items {
			if isHostedToolType(tool.Type) {
				seen[tool.Type] = true
			}
			visit(tool.Tools)
		}
	}
	visit(tools)

	result := make([]string, 0, len(seen))
	for toolType := range seen {
		result = append(result, toolType)
	}
	sort.Strings(result)
	return result
}

func isHostedToolType(toolType string) bool {
	t := strings.ToLower(strings.TrimSpace(toolType))
	switch t {
	case "file_search", "computer_use_preview", "web_fetch", "mcp",
		"code_interpreter", "image_generation", "memory", "tool_search",
		"x_search", "advisor":
		return true
	}
	return strings.HasPrefix(t, "web_search") ||
		strings.HasPrefix(t, "computer_") ||
		strings.HasPrefix(t, "code_execution") ||
		strings.HasPrefix(t, "tool_search_tool_") ||
		strings.HasPrefix(t, "memory_") ||
		strings.HasPrefix(t, "advisor_")
}
