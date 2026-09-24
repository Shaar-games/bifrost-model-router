package responses

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
)

// maxRefDepth bounds $ref chains and variant nesting so cyclic definitions are
// left untouched.
const maxRefDepth = 16

// FlattenRootVariants rewrites function tool parameters whose root is a oneOf or
// anyOf of object variants into one object schema: the union of the variants'
// properties, requiring only the fields every variant requires. Some Chat
// Completions upstreams reject root-level variants outright (grok on VokeAPI
// answers such a tool with a misleading 503 "model temporarily unavailable").
// The client still validates the arguments against its original schema.
func FlattenRootVariants(body []byte) ([]byte, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, err
	}
	rawTools, ok := envelope["tools"]
	if !ok {
		return body, nil
	}
	var tools []map[string]any
	if err := json.Unmarshal(rawTools, &tools); err != nil {
		return nil, err
	}
	if !flattenToolVariants(tools) {
		return body, nil
	}
	encoded, err := json.Marshal(tools)
	if err != nil {
		return nil, err
	}
	envelope["tools"] = encoded
	return json.Marshal(envelope)
}

func flattenToolVariants(tools []map[string]any) bool {
	changed := false
	for _, tool := range tools {
		if nested, ok := tool["tools"].([]any); ok {
			children := make([]map[string]any, 0, len(nested))
			for _, child := range nested {
				if object, ok := child.(map[string]any); ok {
					children = append(children, object)
				}
			}
			changed = flattenToolVariants(children) || changed
		}
		schema, ok := tool["parameters"].(map[string]any)
		if !ok {
			continue
		}
		if flattened, ok := flattenSchemaVariants(schema); ok {
			tool["parameters"] = flattened
			changed = true
		}
	}
	return changed
}

func flattenSchemaVariants(schema map[string]any) (map[string]any, bool) {
	if _, ok := schema["allOf"]; ok {
		return nil, false
	}
	if _, hasOneOf := schema["oneOf"]; !hasOneOf {
		if _, hasAnyOf := schema["anyOf"]; !hasAnyOf {
			return nil, false
		}
	}
	variants, ok := collectVariants(schema, schema, 0)
	if !ok || len(variants) == 0 {
		return nil, false
	}
	properties := map[string]any{}
	var required map[string]bool
	for _, variant := range variants {
		props, _ := variant["properties"].(map[string]any)
		for name, prop := range props {
			properties[name] = mergePropertySchema(properties[name], prop)
		}
		variantRequired := map[string]bool{}
		for _, name := range stringList(variant["required"]) {
			variantRequired[name] = true
		}
		if required == nil {
			required = variantRequired
			continue
		}
		for name := range required {
			if !variantRequired[name] {
				delete(required, name)
			}
		}
	}
	flattened := map[string]any{"type": "object", "properties": properties}
	for _, key := range []string{"$defs", "definitions", "description", "title"} {
		if value, ok := schema[key]; ok {
			flattened[key] = value
		}
	}
	if len(required) > 0 {
		names := make([]string, 0, len(required))
		for name := range required {
			names = append(names, name)
		}
		sort.Strings(names)
		flattened["required"] = names
	}
	return flattened, true
}

// collectVariants returns the object leaves of a oneOf/anyOf tree, following
// local references. Root-level properties apply to every variant.
func collectVariants(root, node map[string]any, depth int) ([]map[string]any, bool) {
	if depth > maxRefDepth {
		return nil, false
	}
	if ref, ok := node["$ref"].(string); ok {
		target, ok := lookupDefinition(root, ref)
		if !ok {
			return nil, false
		}
		return collectVariants(root, target, depth+1)
	}
	branches, _ := node["oneOf"].([]any)
	if anyOf, ok := node["anyOf"].([]any); ok {
		branches = append(branches, anyOf...)
	}
	if len(branches) == 0 {
		if _, isObject := node["properties"]; !isObject && node["type"] != "object" {
			return nil, false
		}
		return []map[string]any{node}, true
	}
	var variants []map[string]any
	for _, branch := range branches {
		object, ok := branch.(map[string]any)
		if !ok {
			return nil, false
		}
		leaves, ok := collectVariants(root, object, depth+1)
		if !ok {
			return nil, false
		}
		variants = append(variants, leaves...)
	}
	if props, ok := node["properties"].(map[string]any); ok && len(props) > 0 {
		variants = append(variants, map[string]any{"properties": props})
	}
	return variants, true
}

func mergePropertySchema(existing, next any) any {
	if existing == nil {
		return next
	}
	if reflect.DeepEqual(existing, next) {
		return existing
	}
	options := []any{existing}
	if object, ok := existing.(map[string]any); ok && len(object) == 1 {
		if anyOf, ok := object["anyOf"].([]any); ok {
			options = anyOf
		}
	}
	for _, option := range options {
		if reflect.DeepEqual(option, next) {
			return existing
		}
	}
	return map[string]any{"anyOf": append(append([]any{}, options...), next)}
}

func lookupDefinition(root map[string]any, ref string) (map[string]any, bool) {
	for _, prefix := range []string{"#/$defs/", "#/definitions/"} {
		name, ok := strings.CutPrefix(ref, prefix)
		if !ok || strings.Contains(name, "/") {
			continue
		}
		defs, ok := root[strings.TrimSuffix(strings.TrimPrefix(prefix, "#/"), "/")].(map[string]any)
		if !ok {
			return nil, false
		}
		definition, ok := defs[name].(map[string]any)
		return definition, ok
	}
	return nil, false
}

func stringList(value any) []string {
	items, _ := value.([]any)
	result := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			result = append(result, text)
		}
	}
	return result
}
