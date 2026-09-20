package credentials

import (
	"testing"

	"github.com/applyinnovations/bifrost-model-router/internal/config"
)

func policyConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := config.Config{
		Version: 1,
		Providers: map[string]config.ProviderProfile{
			"openai": {CredentialMode: config.CredentialRequestPassthrough, ResponsesMode: config.ResponsesNative},
			"other":  {CredentialMode: config.CredentialBifrost, ResponsesMode: config.ResponsesChatPolyfill},
		},
		Models: map[string]config.ModelProfile{
			"openai/a": {Codex: config.CodexProfile{}},
			"other/b":  {Codex: config.CodexProfile{}},
		},
	}
	if err := cfg.ApplyDefaultsAndValidate(); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestCredentialDecision(t *testing.T) {
	cfg := policyConfig(t)
	if got := Decide([]byte(`{"model":"openai/a"}`), cfg).Decision; got != UseOpenAIPassthrough {
		t.Fatalf("openai decision = %v", got)
	}
	if got := Decide([]byte(`{"model":"other/b"}`), cfg).Decision; got != UseBifrostCredential {
		t.Fatalf("other decision = %v", got)
	}
	if got := Decide([]byte(`{"model":"missing"}`), cfg).Decision; got != Reject {
		t.Fatalf("unknown decision = %v", got)
	}
}

func TestStripProviderCredentialsCaseInsensitive(t *testing.T) {
	headers := map[string]string{
		"authorization":   "Bearer openai-canary",
		"X-API-Key":       "provider-canary",
		"X-Bf-Direct-Key": "true",
		"Content-Type":    "application/json",
	}
	StripProviderCredentials(headers)
	if len(headers) != 1 || headers["Content-Type"] == "" {
		t.Fatalf("headers after strip = %#v", headers)
	}
}

func TestVirtualKeyPresent(t *testing.T) {
	for name, test := range map[string]struct {
		headers map[string]string
		want    bool
	}{
		"valid":      {map[string]string{"X-Bf-Vk": "sk-bf-example"}, true},
		"whitespace": {map[string]string{"x-bf-vk": "  sk-bf-example  "}, true},
		"missing":    {map[string]string{}, false},
		"empty":      {map[string]string{"x-bf-vk": ""}, false},
		"wrong kind": {map[string]string{"x-bf-vk": "provider-key"}, false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := VirtualKeyPresent(test.headers); got != test.want {
				t.Fatalf("VirtualKeyPresent() = %v, want %v", got, test.want)
			}
		})
	}
}
