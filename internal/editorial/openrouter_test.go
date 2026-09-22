package editorial

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestResolverPrefersCanonicalNamesAndMatchesUniqueLeaves(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
			{"id":"creator/model-a:free","name":"Creator: Model A (free)","context_length":64000},
			{"id":"creator/model-a","name":"Creator: Model A","context_length":1000000},
			{"id":"one/shared","name":"One: Shared"},
			{"id":"two/shared","name":"Two: Shared"}
		]}`))
	}))
	defer server.Close()

	resolver := NewResolver(server.URL, server.Client(), time.Hour)
	if got, ok := resolver.Lookup("managed", "model-a"); !ok || got != "Creator: Model A" {
		t.Fatalf("Lookup(model-a) = %q, %v", got, ok)
	}
	if name, context, ok := resolver.LookupMetadata("managed", "model-a"); !ok || name != "Creator: Model A" || context != 1000000 {
		t.Fatalf("LookupMetadata(model-a) = %q, %d, %v", name, context, ok)
	}
	if _, ok := resolver.Lookup("managed", "shared"); ok {
		t.Fatal("ambiguous leaf matched")
	}
	if got, ok := resolver.Lookup("managed", "two/shared"); !ok || got != "Two: Shared" {
		t.Fatalf("Lookup(two/shared) = %q, %v", got, ok)
	}
}

func TestResolverFallsBackAfterCatalogFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	resolver := NewResolver(server.URL, server.Client(), time.Hour)
	if _, ok := resolver.Lookup("managed", "model-a"); ok {
		t.Fatal("failed catalog returned a name")
	}
}
