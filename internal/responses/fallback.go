package responses

import (
	"encoding/json"
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

type rawTool struct {
	Type  string    `json:"type"`
	Tools []rawTool `json:"tools,omitempty"`
}

// ApplyHostedToolFallback rewrites only Chat Completions-polyfilled requests
// containing server-side tools. Namespace tools are deliberately excluded:
// Bifrost core flattens their nested functions for non-namespace-capable wires
// and restores the namespaced tool calls on the response path.
func ApplyHostedToolFallback(body []byte, cfg config.Config) ([]byte, *HostedToolFallback, error) {
	var envelope struct {
		Model string    `json:"model"`
		Tools []rawTool `json:"tools"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return body, nil, err
	}
	requested, ok := cfg.ResolveModel(envelope.Model)
	if !ok || requested.Model.ResponsesMode != config.ResponsesChatPolyfill || cfg.HostedToolFallbackModel == "" {
		return body, nil, nil
	}

	types := hostedToolTypes(envelope.Tools)
	if len(types) == 0 {
		return body, nil, nil
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return body, nil, err
	}
	encodedModel, err := json.Marshal(cfg.HostedToolFallbackModel)
	if err != nil {
		return body, nil, err
	}
	fields["model"] = encodedModel
	routed, err := json.Marshal(fields)
	if err != nil {
		return body, nil, err
	}
	return routed, &HostedToolFallback{
		OriginalModel: envelope.Model,
		FallbackModel: cfg.HostedToolFallbackModel,
		ToolTypes:     types,
	}, nil
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
