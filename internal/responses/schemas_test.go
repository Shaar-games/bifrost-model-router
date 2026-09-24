package responses

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestFlattenRootVariantsMergesNestedVariants(t *testing.T) {
	body := []byte(`{"model":"managed/text-model","tools":[{"type":"namespace","name":"mcp__codex_app","tools":[{"type":"function","name":"automation_update","parameters":{"type":"object","properties":{},"oneOf":[{"$ref":"#/$defs/view"},{"$ref":"#/$defs/create"}],"$defs":{"view":{"type":"object","properties":{"id":{"$ref":"#/$defs/id"},"mode":{"type":"string","enum":["view"]}},"required":["mode","id"]},"create":{"oneOf":[{"type":"object","properties":{"mode":{"type":"string","enum":["create"]},"name":{"type":"string"}},"required":["mode","name"]},{"type":"object","properties":{"mode":{"type":"string","enum":["create"]},"prompt":{"type":"string"}},"required":["mode"]}]},"id":{"type":"string"}}}}]}]}`)

	got, err := FlattenRootVariants(body)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Tools []struct {
			Tools []struct {
				Parameters map[string]any `json:"parameters"`
			} `json:"tools"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatal(err)
	}
	params := decoded.Tools[0].Tools[0].Parameters
	for _, keyword := range []string{"oneOf", "anyOf"} {
		if _, ok := params[keyword]; ok {
			t.Fatalf("root %s survived: %#v", keyword, params)
		}
	}
	properties := params["properties"].(map[string]any)
	names := make([]string, 0, len(properties))
	for name := range properties {
		names = append(names, name)
	}
	if len(names) != 4 || properties["id"] == nil || properties["name"] == nil || properties["prompt"] == nil {
		t.Fatalf("properties = %v", names)
	}
	mode := properties["mode"].(map[string]any)["anyOf"].([]any)
	if len(mode) != 2 {
		t.Fatalf("mode = %#v", properties["mode"])
	}
	if !reflect.DeepEqual(params["required"], []any{"mode"}) {
		t.Fatalf("required = %#v", params["required"])
	}
	if params["$defs"] == nil {
		t.Fatal("$defs must be kept for nested references")
	}
}

func TestFlattenRootVariantsLeavesOtherSchemasUntouched(t *testing.T) {
	for _, body := range []string{
		`{"model":"m","input":"hi"}`,
		`{"model":"m","tools":[{"type":"function","name":"f","parameters":{"type":"object","properties":{"a":{"oneOf":[{"type":"string"},{"type":"integer"}]}}}}]}`,
		`{"model":"m","tools":[{"type":"function","name":"f","parameters":{"oneOf":[{"$ref":"#/$defs/a"}],"$defs":{"a":{"$ref":"#/$defs/a"}}}}]}`,
		`{"model":"m","tools":[{"type":"function","name":"f","parameters":{"oneOf":[{"$ref":"https://example.com/s.json"}]}}]}`,
		`{"model":"m","tools":[{"type":"function","name":"f","parameters":{"allOf":[{"type":"object"}]}}]}`,
	} {
		got, err := FlattenRootVariants([]byte(body))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != body {
			t.Fatalf("schema changed:\n got %s\nwant %s", got, body)
		}
	}
}
