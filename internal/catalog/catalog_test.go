package catalog

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/applyinnovations/bifrost-model-router/internal/config"
)

func FuzzHydrateDeterministic(f *testing.F) {
	cfg := testConfig(f)
	f.Add([]byte(`{"data":[{"id":"openai/a","future":{"keep":true}}]}`))
	f.Add([]byte(`{"models":[]}`))
	f.Fuzz(func(t *testing.T, body []byte) {
		first, err := Hydrate(body, cfg)
		if err != nil {
			return
		}
		second, err := Hydrate(body, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, second) {
			t.Fatalf("hydration is not deterministic")
		}
		var decoded any
		if err := json.Unmarshal(first, &decoded); err != nil {
			t.Fatalf("invalid hydrated JSON: %v", err)
		}
	})
}

func testConfig(t testing.TB) config.Config {
	t.Helper()
	cfg := config.Config{
		Version: 1,
		Providers: map[string]config.ProviderProfile{
			"openai": {CredentialMode: config.CredentialRequestPassthrough, ResponsesMode: config.ResponsesNative},
			"other":  {CredentialMode: config.CredentialBifrost, ResponsesMode: config.ResponsesChatPolyfill},
		},
		Models: map[string]config.ModelProfile{
			"openai/a": {Aliases: []string{"a"}, Codex: config.CodexProfile{ContextWindow: 42}},
			"other/b":  {Aliases: []string{"b"}, Codex: config.CodexProfile{ContextWindow: 84}},
		},
	}
	if err := cfg.ApplyDefaultsAndValidate(); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestHydratePreservesUnknownFieldsAndAddsConfiguredModels(t *testing.T) {
	body := []byte(`{"object":"list","data":[{"id":"a","owned_by":"upstream","future":{"x":1}}]}`)
	out, err := Hydrate(body, testConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Models) != 2 {
		t.Fatalf("got %d models: %s", len(decoded.Models), out)
	}
	first := decoded.Models[0]
	if first["slug"] != "openai/a" || first["owned_by"] != "upstream" || first["future"] == nil {
		t.Fatalf("unexpected hydrated model: %#v", first)
	}
}

func TestHydrateIsDeterministic(t *testing.T) {
	cfg := testConfig(t)
	one, err := Hydrate([]byte(`{"data":[]}`), cfg)
	if err != nil {
		t.Fatal(err)
	}
	two, err := Hydrate([]byte(`{"data":[]}`), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if string(one) != string(two) {
		t.Fatalf("hydration is not deterministic:\n%s\n%s", one, two)
	}
}
