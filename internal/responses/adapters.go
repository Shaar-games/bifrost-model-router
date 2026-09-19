package responses

import (
	"fmt"

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
		return chatAdapter{name: name}, true
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
			if tool.Type != schemas.ResponsesToolTypeFunction {
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

func compatError(code, message string) *CompatibilityError {
	return &CompatibilityError{Code: code, Message: message}
}
