package gateway

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
)

// debugToolsEnabled logs only tool shapes and input item types, never prompts,
// arguments, outputs or headers.
var debugToolsEnabled = os.Getenv("ROUTER_DEBUG_TOOLS") != ""

func logToolShapes(stage string, body []byte) {
	if !debugToolsEnabled {
		return
	}
	var envelope struct {
		Model      string            `json:"model"`
		Tools      []json.RawMessage `json:"tools"`
		ToolChoice json.RawMessage   `json:"tool_choice"`
		Input      json.RawMessage   `json:"input"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		log.Printf("router-debug %s: invalid JSON: %v", stage, err)
		return
	}
	shapes := make([]string, 0, len(envelope.Tools))
	for _, raw := range envelope.Tools {
		shapes = append(shapes, describeTool(raw))
	}
	log.Printf("router-debug %s: model=%s tool_choice=%s tools(%d)=[%s] input=%s",
		stage, envelope.Model, compactChoice(envelope.ToolChoice), len(shapes), strings.Join(shapes, "; "), describeInput(envelope.Input))
}

func describeTool(raw json.RawMessage) string {
	var tool struct {
		Type         string            `json:"type"`
		Name         string            `json:"name"`
		Execution    string            `json:"execution"`
		DeferLoading *bool             `json:"defer_loading"`
		Tools        []json.RawMessage `json:"tools"`
	}
	if json.Unmarshal(raw, &tool) != nil {
		return "?"
	}
	var keys map[string]json.RawMessage
	_ = json.Unmarshal(raw, &keys)
	fields := make([]string, 0, len(keys))
	for key := range keys {
		fields = append(fields, key)
	}
	sort.Strings(fields)
	desc := tool.Type
	if tool.Name != "" {
		desc += ":" + tool.Name
	}
	if tool.Execution != "" {
		desc += " execution=" + tool.Execution
	}
	if tool.DeferLoading != nil {
		desc += fmt.Sprintf(" defer_loading=%t", *tool.DeferLoading)
	}
	desc += " keys=" + strings.Join(fields, ",")
	if len(tool.Tools) > 0 {
		children := make([]string, 0, len(tool.Tools))
		for _, child := range tool.Tools {
			children = append(children, describeTool(child))
		}
		desc += " {" + strings.Join(children, ", ") + "}"
	}
	return desc
}

func describeInput(raw json.RawMessage) string {
	var items []struct {
		Type string `json:"type"`
		Role string `json:"role"`
	}
	if json.Unmarshal(raw, &items) != nil {
		return "text"
	}
	counts := map[string]int{}
	for _, item := range items {
		key := item.Type
		if key == "" || key == "message" {
			key = "message/" + item.Role
		}
		counts[key]++
	}
	parts := make([]string, 0, len(counts))
	for key, count := range counts {
		parts = append(parts, fmt.Sprintf("%s=%d", key, count))
	}
	sort.Strings(parts)
	return "{" + strings.Join(parts, ",") + "}"
}

func compactChoice(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "-"
	}
	var choice struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &choice) == nil && choice.Type != "" {
		return choice.Type + ":" + choice.Name
	}
	return strings.Trim(string(raw), `"`)
}
