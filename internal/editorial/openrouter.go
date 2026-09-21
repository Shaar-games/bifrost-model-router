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
	ID   string `json:"id"`
	Name string `json:"name"`
}

type catalogEnvelope struct {
	Data []catalogEntry `json:"data"`
}

// Resolver caches OpenRouter's editorial model names. Availability, routing,
// permissions, and capabilities continue to come from the configured provider.
type Resolver struct {
	url       string
	client    *http.Client
	ttl       time.Duration
	retryTTL  time.Duration
	mu        sync.Mutex
	expiresAt time.Time
	byID      map[string]string
	byLeaf    map[string]string
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
	if r == nil || strings.TrimSpace(upstreamModel) == "" {
		return "", false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if time.Now().After(r.expiresAt) {
		if err := r.refresh(); err != nil {
			r.expiresAt = time.Now().Add(r.retryTTL)
		}
	}
	key := normalizeID(upstreamModel)
	if name := r.byID[key]; name != "" {
		return name, true
	}
	if !strings.Contains(key, "/") {
		name, ok := r.byLeaf[key]
		return name, ok && name != ""
	}
	return "", false
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
	byID := make(map[string]string, len(envelope.Data))
	leafCandidates := make(map[string]map[string]bool)
	for _, entry := range envelope.Data {
		id := normalizeID(entry.ID)
		name := cleanName(entry.Name)
		if id == "" || name == "" {
			continue
		}
		// Prefer the canonical listing over marketplace variants such as :free.
		variant := strings.LastIndexByte(entry.ID, ':') > strings.LastIndexByte(entry.ID, '/')
		if !variant || byID[id] == "" {
			byID[id] = name
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
	byLeaf := make(map[string]string)
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
