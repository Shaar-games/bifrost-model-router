package responses

import (
	"fmt"
	"strings"

	"github.com/maximhq/bifrost/core/schemas"
)

type CompatibilityError struct {
	Code    string
	Message string
}

func (e *CompatibilityError) Error() string { return e.Message }

type Adapter interface {
	Name() string
	Validate(*schemas.BifrostResponsesRequest) *CompatibilityError
	Normalize(*schemas.BifrostResponsesRequest) *CompatibilityError
}

func Get(name string) (Adapter, bool) {
	switch name {
	case "native":
		return nativeAdapter{}, true
	case "openai-chat":
		return chatAdapter{name: name}, true
	case "single-system-message":
		return singleSystemAdapter{}, true
	case "strict-text-only":
		return chatAdapter{name: name}, true
	default:
		return nil, false
	}
}

type nativeAdapter struct{}

func (nativeAdapter) Name() string { return "native" }

func (nativeAdapter) Validate(*schemas.BifrostResponsesRequest) *CompatibilityError { return nil }

func (nativeAdapter) Normalize(*schemas.BifrostResponsesRequest) *CompatibilityError { return nil }

type chatAdapter struct{ name string }

type singleSystemAdapter struct{}

func (singleSystemAdapter) Name() string { return "single-system-message" }

func (singleSystemAdapter) Validate(req *schemas.BifrostResponsesRequest) *CompatibilityError {
	return chatAdapter{name: "single-system-message"}.Validate(req)
}

func (a singleSystemAdapter) Normalize(req *schemas.BifrostResponsesRequest) *CompatibilityError {
	if err := a.Validate(req); err != nil {
		return err
	}
	parts := make([]string, 0, len(req.Input)+1)
	if req.Params != nil && req.Params.Instructions != nil && *req.Params.Instructions != "" {
		parts = append(parts, *req.Params.Instructions)
	}
	remaining := make([]schemas.ResponsesMessage, 0, len(req.Input))
	for _, message := range req.Input {
		if !isInstructionMessage(message) {
			remaining = append(remaining, message)
			continue
		}
		text, ok := instructionText(message.Content)
		if !ok {
			return compatError("unsupported_instruction_content", "system and developer messages must contain only text for the single-system-message adapter")
		}
		parts = append(parts, text)
	}
	if len(remaining) == len(req.Input) {
		return nil
	}
	if req.Params == nil {
		req.Params = &schemas.ResponsesParameters{}
	}
	merged := strings.Join(parts, "\n\n")
	req.Params.Instructions = &merged
	req.Input = remaining
	return nil
}

func (a chatAdapter) Name() string { return a.name }

func (a chatAdapter) Validate(req *schemas.BifrostResponsesRequest) *CompatibilityError {
	if req == nil {
		return compatError("invalid_request", "Responses request is missing")
	}
	if req.Params != nil {
		if req.Params.Background != nil && *req.Params.Background {
			return compatError("background_unsupported", "background Responses are unavailable through a Chat Completions polyfill")
		}
		if req.Params.Conversation != nil && *req.Params.Conversation != "" {
			return compatError("conversation_unsupported", "server-side conversations are unavailable through a Chat Completions polyfill")
		}
		if req.Params.PreviousResponseID != nil && *req.Params.PreviousResponseID != "" {
			return compatError("previous_response_unsupported", "previous_response_id is unavailable through a Chat Completions polyfill")
		}
		for _, tool := range req.Params.Tools {
			// Namespace tools are portable after Bifrost core flattens their
			// nested functions for the Chat Completions wire. Server-side tools
			// are routed to a native Responses model by the transport hook.
			if tool.Type != schemas.ResponsesToolTypeFunction && tool.Type != schemas.ResponsesToolTypeNamespace {
				return compatError("hosted_tool_unsupported", fmt.Sprintf("tool type %q is unavailable through a Chat Completions polyfill", tool.Type))
			}
		}
	}
	for _, message := range req.Input {
		if message.Content == nil {
			continue
		}
		for _, block := range message.Content.ContentBlocks {
			switch block.Type {
			case schemas.ResponsesInputMessageContentBlockTypeText,
				schemas.ResponsesOutputMessageContentTypeText,
				schemas.ResponsesOutputMessageContentTypeReasoning:
			default:
				return compatError("modality_unsupported", fmt.Sprintf("content type %q is unavailable through this text-only polyfill", block.Type))
			}
		}
	}
	return nil
}

func (a chatAdapter) Normalize(req *schemas.BifrostResponsesRequest) *CompatibilityError {
	// Bifrost core owns the actual Responses-to-Chat conversion. In particular,
	// ToChatRequest carries top-level instructions into a leading system message,
	// normalizes developer roles, preserves function tools, and backfills the
	// eventual Responses result. Keeping this method a no-op avoids maintaining a
	// second, subtly divergent mux.
	return a.Validate(req)
}

func isInstructionMessage(message schemas.ResponsesMessage) bool {
	if message.Role == nil || (*message.Role != schemas.ResponsesInputMessageRoleSystem && *message.Role != schemas.ResponsesInputMessageRoleDeveloper) {
		return false
	}
	return message.Type == nil || *message.Type == schemas.ResponsesMessageTypeMessage
}

func instructionText(content *schemas.ResponsesMessageContent) (string, bool) {
	if content == nil {
		return "", false
	}
	if content.ContentStr != nil {
		return *content.ContentStr, true
	}
	parts := make([]string, 0, len(content.ContentBlocks))
	for _, block := range content.ContentBlocks {
		if block.Type != schemas.ResponsesInputMessageContentBlockTypeText || block.Text == nil {
			return "", false
		}
		parts = append(parts, *block.Text)
	}
	return strings.Join(parts, "\n"), true
}

func compatError(code, message string) *CompatibilityError {
	return &CompatibilityError{Code: code, Message: message}
}
