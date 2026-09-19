package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/applyinnovations/bifrost-model-router/internal/catalog"
	"github.com/applyinnovations/bifrost-model-router/internal/config"
	"github.com/applyinnovations/bifrost-model-router/internal/credentials"
	responsescompat "github.com/applyinnovations/bifrost-model-router/internal/responses"
	"github.com/maximhq/bifrost/core/schemas"
)

var state struct {
	sync.RWMutex
	config config.Config
}

func Init(raw any) error {
	cfg, err := config.FromAny(raw)
	if err != nil {
		return err
	}
	state.Lock()
	state.config = cfg
	state.Unlock()
	return nil
}

func GetName() string { return "codex-model-router" }

func Cleanup() error { return nil }

func currentConfig() config.Config {
	state.RLock()
	defer state.RUnlock()
	return state.config
}

func HTTPTransportPreAuthHook(_ *schemas.BifrostContext, req *schemas.HTTPRequest) (*schemas.HTTPResponse, error) {
	if !isInferenceRequest(req) {
		return nil, nil
	}
	result := credentials.Decide(req.Body, currentConfig())
	switch result.Decision {
	case credentials.UseOpenAIPassthrough:
		if !credentials.BearerPresent(req.Headers) {
			return errorResponse(401, "missing_openai_auth", "OpenAI models require Codex OpenAI authentication"), nil
		}
		credentials.SetHeader(req.Headers, credentials.DirectKeyHeader, "true")
		credentials.DeleteHeader(req.Headers, "x-api-key")
		credentials.DeleteHeader(req.Headers, "x-goog-api-key")
		return nil, nil
	case credentials.UseBifrostCredential:
		credentials.StripProviderCredentials(req.Headers)
		return nil, nil
	case credentials.Reject:
		credentials.StripProviderCredentials(req.Headers)
		return errorResponse(400, "unresolved_model", result.Message), nil
	default:
		return nil, nil
	}
}

func HTTPTransportPreHook(_ *schemas.BifrostContext, _ *schemas.HTTPRequest) (*schemas.HTTPResponse, error) {
	return nil, nil
}

func HTTPTransportPostHook(_ *schemas.BifrostContext, req *schemas.HTTPRequest, resp *schemas.HTTPResponse) error {
	if !isCodexModelsRequest(req) || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil
	}
	body, err := catalog.Hydrate(resp.Body, currentConfig())
	if err != nil {
		return err
	}
	resp.Body = body
	if resp.Headers == nil {
		resp.Headers = make(map[string]string)
	}
	resp.Headers["Content-Type"] = "application/json"
	resp.Headers["Cache-Control"] = "no-store"
	return nil
}

func PreRequestHook(_ *schemas.BifrostContext, _ *schemas.BifrostRequest) error { return nil }

func PreLLMHook(ctx *schemas.BifrostContext, req *schemas.BifrostRequest) (*schemas.BifrostRequest, *schemas.LLMPluginShortCircuit, error) {
	if req == nil || (req.RequestType != schemas.ResponsesRequest && req.RequestType != schemas.ResponsesStreamRequest) {
		return req, nil, nil
	}
	_, model, _ := req.GetRequestFields()
	resolved, ok := currentConfig().ResolveModel(model)
	if !ok {
		return req, shortCircuit(400, "unresolved_model", "model is not present in the router catalog"), nil
	}
	switch resolved.Model.ResponsesMode {
	case config.ResponsesNative:
		return req, nil, nil
	case config.ResponsesChatPolyfill:
		adapter, ok := responsescompat.Get(resolved.Model.Adapter)
		if !ok {
			return req, shortCircuit(500, "invalid_router_config", "configured Responses adapter is not registered"), nil
		}
		if compatErr := adapter.Normalize(req.ResponsesRequest); compatErr != nil {
			return req, shortCircuit(400, compatErr.Code, compatErr.Message), nil
		}
		ctx.SetValue(schemas.BifrostContextKeyChangeRequestType, schemas.ChatCompletionRequest)
		return req, nil, nil
	case config.ResponsesUnsupported:
		return req, shortCircuit(400, "responses_unsupported", fmt.Sprintf("model %q does not support the Responses API", resolved.Slug)), nil
	default:
		return req, shortCircuit(500, "invalid_router_config", "model has no valid Responses mode"), nil
	}
}

func PostLLMHook(_ *schemas.BifrostContext, resp *schemas.BifrostResponse, bifrostErr *schemas.BifrostError) (*schemas.BifrostResponse, *schemas.BifrostError, error) {
	return resp, bifrostErr, nil
}

func isInferenceRequest(req *schemas.HTTPRequest) bool {
	if req == nil || !strings.EqualFold(req.Method, "POST") {
		return false
	}
	return strings.HasSuffix(req.Path, "/v1/responses") ||
		strings.HasSuffix(req.Path, "/v1/chat/completions") ||
		strings.HasSuffix(req.Path, "/v1/completions")
}

func isCodexModelsRequest(req *schemas.HTTPRequest) bool {
	return req != nil && strings.EqualFold(req.Method, "GET") &&
		strings.HasSuffix(req.Path, "/v1/models") &&
		req.CaseInsensitiveQueryLookup("client_version") != ""
}

func errorResponse(status int, code, message string) *schemas.HTTPResponse {
	body, _ := json.Marshal(map[string]any{"error": map[string]any{
		"type": "router_error", "code": code, "message": message,
	}})
	return &schemas.HTTPResponse{StatusCode: status, Headers: map[string]string{"Content-Type": "application/json"}, Body: body}
}

func shortCircuit(status int, code, message string) *schemas.LLMPluginShortCircuit {
	errorType := "router_error"
	allowFallbacks := false
	return &schemas.LLMPluginShortCircuit{Error: &schemas.BifrostError{
		IsBifrostError: true,
		StatusCode:     schemas.Ptr(status),
		AllowFallbacks: &allowFallbacks,
		Error: &schemas.ErrorField{
			Type: &errorType, Code: &code, Message: message,
		},
	}}
}
