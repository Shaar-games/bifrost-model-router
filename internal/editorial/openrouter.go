package editorial

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const DefaultOpenRouterModelsURL = "https://openrouter.ai/api/v1/models"

type catalogEntry struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ContextLength int64  `json:"context_length"`
}

type catalogEnvelope struct {
	Data []catalogEntry `json:"data"`
}

type modelMetadata struct {
	name          string
	contextLength int64
}

// Resolver caches OpenRouter's editorial names and model context lengths.
// Availability, routing, and permissions continue to come from the configured
// provider. Provider-returned metadata takes precedence at the call site.
type Resolver struct {
	url       string
	client    *http.Client
	ttl       time.Duration
	retryTTL  time.Duration
	mu        sync.Mutex
	expiresAt time.Time
	byID      map[string]modelMetadata
	byLeaf    map[string]modelMetadata
}

func NewOpenRouterResolver() *Resolver {
	return NewResolver(DefaultOpenRouterModelsURL, &http.Client{Timeout: 3 * time.Second}, 6*time.Hour)
}

func NewResolver(url string, client *http.Client, ttl time.Duration) *Resolver {
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	if ttl <= 0 {
		ttl = 6 * time.Hour
	}
	return &Resolver{url: url, client: client, ttl: ttl, retryTTL: 5 * time.Minute}
}

// Lookup returns a standardized editorial name when OpenRouter has an exact
// model ID match or an unambiguous match for the upstream ID without a creator
// prefix. Failures are cached briefly and degrade to the caller's fallback.
func (r *Resolver) Lookup(_ string, upstreamModel string) (string, bool) {
	name, _, ok := r.LookupMetadata("", upstreamModel)
	return name, ok && name != ""
}

// LookupMetadata returns dynamic catalog metadata for an exact model ID or an
// unambiguous creator-less model ID. It never determines model availability.
func (r *Resolver) LookupMetadata(_ string, upstreamModel string) (string, int64, bool) {
	if r == nil || strings.TrimSpace(upstreamModel) == "" {
		return "", 0, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if time.Now().After(r.expiresAt) {
		if err := r.refresh(); err != nil {
			r.expiresAt = time.Now().Add(r.retryTTL)
		}
	}
	key := normalizeID(upstreamModel)
	if metadata, ok := r.byID[key]; ok {
		return metadata.name, metadata.contextLength, true
	}
	if !strings.Contains(key, "/") {
		metadata, ok := r.byLeaf[key]
		return metadata.name, metadata.contextLength, ok
	}
	return "", 0, false
}

func (r *Resolver) refresh() error {
	req, err := http.NewRequest(http.MethodGet, r.url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "bifrost-model-router")
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return errors.New("OpenRouter model catalog returned a non-success status")
	}
	var envelope catalogEnvelope
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&envelope); err != nil {
		return err
	}
	byID := make(map[string]modelMetadata, len(envelope.Data))
	leafCandidates := make(map[string]map[string]bool)
	for _, entry := range envelope.Data {
		id := normalizeID(entry.ID)
		name := cleanName(entry.Name)
		if id == "" || name == "" {
			continue
		}
		// Prefer the canonical listing over marketplace variants such as :free.
		variant := strings.LastIndexByte(entry.ID, ':') > strings.LastIndexByte(entry.ID, '/')
		if _, exists := byID[id]; !variant || !exists {
			byID[id] = modelMetadata{name: name, contextLength: entry.ContextLength}
		}
		leaf := id
		if slash := strings.LastIndexByte(id, '/'); slash >= 0 {
			leaf = id[slash+1:]
		}
		if leafCandidates[leaf] == nil {
			leafCandidates[leaf] = make(map[string]bool)
		}
		leafCandidates[leaf][id] = true
	}
	byLeaf := make(map[string]modelMetadata)
	for leaf, candidates := range leafCandidates {
		if len(candidates) != 1 {
			continue
		}
		for id := range candidates {
			byLeaf[leaf] = byID[id]
		}
	}
	r.byID, r.byLeaf = byID, byLeaf
	r.expiresAt = time.Now().Add(r.ttl)
	return nil
}

func normalizeID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if colon := strings.LastIndexByte(value, ':'); colon > strings.LastIndexByte(value, '/') {
		value = value[:colon]
	}
	return value
}

func cleanName(value string) string {
	value = strings.TrimSpace(value)
	for _, suffix := range []string{" (free)", " (nitro)", " (extended)"} {
		value = strings.TrimSuffix(value, suffix)
	}
	return strings.TrimSpace(value)
}
