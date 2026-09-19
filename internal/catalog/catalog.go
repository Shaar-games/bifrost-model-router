package catalog

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/applyinnovations/bifrost-model-router/internal/config"
)

type responseEnvelope struct {
	Models []map[string]any `json:"models"`
}

func Hydrate(body []byte, cfg config.Config) ([]byte, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode model catalog: %w", err)
	}

	models, err := decodeModels(raw)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	out := make([]map[string]any, 0, len(models)+len(cfg.Models))
	for _, original := range models {
		name := stringField(original, "slug")
		if name == "" {
			name = stringField(original, "id")
		}
		resolved, ok := cfg.ResolveModel(name)
		if !ok {
			continue
		}
		out = append(out, hydrateModel(original, resolved, cfg.Instructions))
		seen[resolved.Slug] = true
	}
	for _, slug := range cfg.ModelNames() {
		if seen[slug] {
			continue
		}
		resolved, _ := cfg.ResolveModel(slug)
		out = append(out, hydrateModel(map[string]any{"id": slug}, resolved, cfg.Instructions))
	}

	sort.SliceStable(out, func(i, j int) bool {
		pi, _ := out[i]["priority"].(int)
		pj, _ := out[j]["priority"].(int)
		if pi != pj {
			return pi < pj
		}
		return stringField(out[i], "slug") < stringField(out[j], "slug")
	})
	encoded, err := json.Marshal(responseEnvelope{Models: out})
	if err != nil {
		return nil, fmt.Errorf("encode hydrated catalog: %w", err)
	}
	return encoded, nil
}

func decodeModels(raw map[string]json.RawMessage) ([]map[string]any, error) {
	field := raw["models"]
	if len(field) == 0 {
		field = raw["data"]
	}
	if len(field) == 0 {
		return []map[string]any{}, nil
	}
	var models []map[string]any
	if err := json.Unmarshal(field, &models); err != nil {
		return nil, fmt.Errorf("decode model entries: %w", err)
	}
	return models, nil
}

func hydrateModel(original map[string]any, resolved config.ResolvedModel, instructions string) map[string]any {
	result := make(map[string]any, len(original)+32)
	for key, value := range original {
		result[key] = value
	}
	p := resolved.Model.Codex
	result["slug"] = resolved.Slug
	result["display_name"] = p.DisplayName
	result["description"] = p.Description
	result["default_reasoning_level"] = p.DefaultReasoningLevel
	result["supported_reasoning_levels"] = p.SupportedReasoningLevels
	result["shell_type"] = "unified_exec"
	result["visibility"] = "list"
	result["supported_in_api"] = true
	result["priority"] = 1
	result["additional_speed_tiers"] = []string{}
	result["service_tiers"] = []any{}
	result["availability_nux"] = nil
	result["upgrade"] = nil
	result["base_instructions"] = instructions
	result["model_messages"] = map[string]any{"instructions_template": instructions}
	result["include_skills_usage_instructions"] = false
	result["include_plugin_usage_instructions"] = false
	result["include_apps_usage_instructions"] = false
	result["supports_reasoning_summary_parameter"] = p.SupportsReasoningSummaries
	result["default_reasoning_summary"] = "auto"
	result["support_verbosity"] = p.SupportsVerbosity
	result["truncation_policy"] = map[string]any{"mode": "bytes", "limit": p.TruncationLimit}
	result["supports_image_detail_original"] = p.SupportsImageDetailOriginal
	result["context_window"] = p.ContextWindow
	result["max_context_window"] = p.ContextWindow
	result["effective_context_window_percent"] = p.EffectiveContextWindowPercent
	result["experimental_supported_tools"] = []string{}
	result["input_modalities"] = p.InputModalities
	result["supports_search_tool"] = p.SupportsSearch
	result["supports_experimental_context"] = false
	result["use_responses_lite"] = false
	result["node_repl_auto_review_required"] = false
	result["node_repl_disabled"] = false
	result["responses_mode"] = resolved.Model.ResponsesMode
	return result
}

func stringField(object map[string]any, key string) string {
	value, _ := object[key].(string)
	return value
}
