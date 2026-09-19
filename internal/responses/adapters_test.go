package responses

import (
	"testing"

	"github.com/maximhq/bifrost/core/schemas"
)

func TestChatAdapterAcceptsTextAndFunctionTools(t *testing.T) {
	adapter, _ := Get("openai-chat")
	text := "hello"
	req := &schemas.BifrostResponsesRequest{
		Input: []schemas.ResponsesMessage{{
			Content: &schemas.ResponsesMessageContent{ContentStr: &text},
		}},
		Params: &schemas.ResponsesParameters{Tools: []schemas.ResponsesTool{{
			Type: schemas.ResponsesToolTypeFunction,
		}}},
	}
	if err := adapter.Validate(req); err != nil {
		t.Fatal(err)
	}
}

func TestChatAdapterRejectsStatefulAndHostedFeatures(t *testing.T) {
	adapter, _ := Get("single-system-message")
	t.Run("previous response", func(t *testing.T) {
		previous := "resp_123"
		err := adapter.Validate(&schemas.BifrostResponsesRequest{Params: &schemas.ResponsesParameters{PreviousResponseID: &previous}})
		if err == nil || err.Code != "previous_response_unsupported" {
			t.Fatalf("error = %#v", err)
		}
	})
	t.Run("hosted tool", func(t *testing.T) {
		err := adapter.Validate(&schemas.BifrostResponsesRequest{Params: &schemas.ResponsesParameters{Tools: []schemas.ResponsesTool{{Type: schemas.ResponsesToolTypeWebSearch}}}})
		if err == nil || err.Code != "hosted_tool_unsupported" {
			t.Fatalf("error = %#v", err)
		}
	})
	t.Run("image", func(t *testing.T) {
		err := adapter.Validate(&schemas.BifrostResponsesRequest{Input: []schemas.ResponsesMessage{{Content: &schemas.ResponsesMessageContent{ContentBlocks: []schemas.ResponsesMessageContentBlock{{Type: schemas.ResponsesInputMessageContentBlockTypeImage}}}}}})
		if err == nil || err.Code != "modality_unsupported" {
			t.Fatalf("error = %#v", err)
		}
	})
}
