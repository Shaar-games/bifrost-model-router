package credentials

import (
	"encoding/json"
	"strings"

	"github.com/applyinnovations/bifrost-model-router/internal/config"
)

const DirectKeyHeader = "x-bf-direct-key"

type Decision int

const (
	NoDecision Decision = iota
	UseOpenAIPassthrough
	UseBifrostCredential
	Reject
)

type Result struct {
	Decision Decision
	Model    string
	Message  string
}

func Decide(body []byte, cfg config.Config) Result {
	var envelope struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Model == "" {
		return Result{Decision: Reject, Message: "a configured model is required before credential routing"}
	}
	resolved, ok := cfg.ResolveModel(envelope.Model)
	if !ok {
		return Result{Decision: Reject, Model: envelope.Model, Message: "model is not present in the router catalog"}
	}
	if resolved.Provider.CredentialMode == config.CredentialRequestPassthrough {
		return Result{Decision: UseOpenAIPassthrough, Model: resolved.Slug}
	}
	return Result{Decision: UseBifrostCredential, Model: resolved.Slug}
}

func SetHeader(headers map[string]string, name, value string) {
	DeleteHeader(headers, name)
	headers[name] = value
}

func DeleteHeader(headers map[string]string, name string) {
	for key := range headers {
		if strings.EqualFold(key, name) {
			delete(headers, key)
		}
	}
}

func BearerPresent(headers map[string]string) bool {
	for key, value := range headers {
		if strings.EqualFold(key, "Authorization") {
			parts := strings.Fields(value)
			return len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") && parts[1] != ""
		}
	}
	return false
}

func VirtualKeyPresent(headers map[string]string) bool {
	for key, value := range headers {
		if strings.EqualFold(key, "x-bf-vk") {
			return strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "sk-bf-")
		}
	}
	return false
}

func StripProviderCredentials(headers map[string]string) {
	for _, name := range []string{"Authorization", "x-api-key", "x-goog-api-key", DirectKeyHeader} {
		DeleteHeader(headers, name)
	}
}
