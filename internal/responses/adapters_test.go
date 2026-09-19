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

func TestSingleSystemAdapterHoistsAllInstructionsAndPreservesOrder(t *testing.T) {
	adapter, _ := Get("single-system-message")
	initial := "top"
	system := schemas.ResponsesInputMessageRoleSystem
	developer := schemas.ResponsesInputMessageRoleDeveloper
	user := schemas.ResponsesInputMessageRoleUser
	messageType := schemas.ResponsesMessageTypeMessage
	nonMessageType := schemas.ResponsesMessageTypeReasoning
	sysText, devText, userOne, userTwo := "system", "developer", "one", "two"
	req := &schemas.BifrostResponsesRequest{
		Params: &schemas.ResponsesParameters{Instructions: &initial},
		Input: []schemas.ResponsesMessage{
			{Role: &user, Content: &schemas.ResponsesMessageContent{ContentStr: &userOne}},
			{Role: &system, Type: &messageType, Content: &schemas.ResponsesMessageContent{ContentStr: &sysText}},
			{Role: &user, Content: &schemas.ResponsesMessageContent{ContentStr: &userTwo}},
			{Role: &developer, Content: &schemas.ResponsesMessageContent{ContentBlocks: []schemas.ResponsesMessageContentBlock{{Type: schemas.ResponsesInputMessageContentBlockTypeText, Text: &devText}}}},
			{Role: &system, Type: &nonMessageType, Content: &schemas.ResponsesMessageContent{ContentStr: &sysText}},
		},
	}
	if err := adapter.Normalize(req); err != nil {
		t.Fatal(err)
	}
	if got := *req.Params.Instructions; got != "top\n\nsystem\n\ndeveloper" {
		t.Fatalf("instructions = %q", got)
	}
	if len(req.Input) != 3 || *req.Input[0].Content.ContentStr != "one" || *req.Input[1].Content.ContentStr != "two" || req.Input[2].Type == nil || *req.Input[2].Type != nonMessageType {
		t.Fatalf("remaining input was not preserved: %#v", req.Input)
	}
	if err := adapter.Normalize(req); err != nil {
		t.Fatal(err)
	}
	if got := *req.Params.Instructions; got != "top\n\nsystem\n\ndeveloper" {
		t.Fatalf("normalization is not idempotent: %q", got)
	}
}

func TestSingleSystemAdapterRejectsNonTextInstructions(t *testing.T) {
	adapter, _ := Get("single-system-message")
	system := schemas.ResponsesInputMessageRoleSystem
	req := &schemas.BifrostResponsesRequest{Input: []schemas.ResponsesMessage{{
		Role: &system,
		Content: &schemas.ResponsesMessageContent{ContentBlocks: []schemas.ResponsesMessageContentBlock{{
			Type: schemas.ResponsesInputMessageContentBlockTypeImage,
		}}},
	}}}
	err := adapter.Normalize(req)
	if err == nil || err.Code != "modality_unsupported" {
		t.Fatalf("error = %#v", err)
	}
}
