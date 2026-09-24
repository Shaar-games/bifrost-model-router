package responses

import "encoding/json"

var bridgeFunctionTools = map[string]map[string]any{
	"web_search": {
		"type": "function",
		"name": "web_search",
		"description": "Search the web for current or external information. " +
			"Returns a factual summary of the findings with source URLs.",
		"parameters": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "A precise, self-contained search query."},
			},
			"required":             []string{"query"},
			"additionalProperties": false,
		},
	},
	"image_generation": {
		"type": "function",
		"name": "image_generation",
		"description": "Generate an image that is shown directly to the user. " +
			"Returns a confirmation, not the image itself.",
		"parameters": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"prompt": map[string]any{"type": "string", "description": "A detailed description of the image to generate."},
				"size":   map[string]any{"type": "string", "enum": []string{"auto", "1024x1024", "1536x1024", "1024x1536"}},
			},
			"required":             []string{"prompt"},
			"additionalProperties": false,
		},
	},
}

// BridgeFunctionTool returns the function definition that stands in for a
// bridged hosted tool family on the polyfilled model.
func BridgeFunctionTool(family string) json.RawMessage {
	encoded, err := json.Marshal(bridgeFunctionTools[family])
	if err != nil {
		panic(err)
	}
	return encoded
}
