package config

import (
	"fmt"
	"sort"
	"strings"
)

type ResolvedModel struct {
	Slug          string
	BaseSlug      string
	UpstreamModel string
	Provider      ProviderProfile
	Model         ModelProfile
	Variant       *ContextVariant
}

func (c Config) ResolveModel(name string) (ResolvedModel, bool) {
	if c.resolutionIndex != nil {
		resolved, ok := c.resolutionIndex[name]
		return resolved, ok
	}
	// Config values assembled directly by callers are supported for backwards
	// compatibility, although Decode and ApplyDefaultsAndValidate always build
	// the index once at load time.
	copy := c
	copy.buildResolutionIndex()
	resolved, ok := copy.resolutionIndex[name]
	return resolved, ok
}

func (c *Config) buildResolutionIndex() {
	c.resolutionIndex = make(map[string]ResolvedModel)
	c.resolvedModels = nil
	for _, slug := range c.ModelNames() {
		model := c.Models[slug]
		base := resolvedBase(*c, slug, model)
		c.resolvedModels = append(c.resolvedModels, base)
		for _, name := range modelNames(slug, model) {
			c.resolutionIndex[name] = base
		}
		for i := range model.ContextVariants {
			variant := model.ContextVariants[i]
			resolved := base
			resolved.Slug = preferredVariantSlug(slug, model, c.Providers[model.Provider], variant.ContextWindow)
			resolved.Variant = &variant
			resolved.Model.Codex.ContextWindow = variant.ContextWindow
			resolved.Model.Codex.MaxContextWindow = variant.ContextWindow
			resolved.Model.Codex.EffectiveContextWindowPercent = variant.EffectiveContextWindowPercent
			c.resolvedModels = append(c.resolvedModels, resolved)
			for _, name := range variantNames(slug, model, variant.ContextWindow) {
				c.resolutionIndex[name] = resolved
			}
		}
	}
}

func (c Config) ResolvedModels() []ResolvedModel {
	if c.resolvedModels == nil {
		copy := c
		copy.buildResolutionIndex()
		return append([]ResolvedModel(nil), copy.resolvedModels...)
	}
	return append([]ResolvedModel(nil), c.resolvedModels...)
}

func resolvedBase(c Config, slug string, model ModelProfile) ResolvedModel {
	return ResolvedModel{Slug: slug, BaseSlug: slug, UpstreamModel: model.UpstreamModel, Provider: c.Providers[model.Provider], Model: model}
}

func modelNames(slug string, model ModelProfile) []string {
	names := append([]string{slug}, model.Aliases...)
	if slash := strings.IndexByte(slug, '/'); slash >= 0 {
		names = append(names, slug[slash+1:])
	}
	return unique(names)
}

func variantNames(slug string, model ModelProfile, contextWindow int64) []string {
	suffix := contextSuffix(contextWindow)
	names := modelNames(slug, model)
	for i := range names {
		names[i] += suffix
	}
	return unique(names)
}

func preferredVariantSlug(slug string, model ModelProfile, provider ProviderProfile, contextWindow int64) string {
	base := slug
	if provider.CredentialMode == CredentialRequestPassthrough {
		base = model.UpstreamModel
		for _, alias := range model.Aliases {
			if !strings.Contains(alias, "/") {
				base = alias
				break
			}
		}
	}
	return base + contextSuffix(contextWindow)
}

func contextSuffix(contextWindow int64) string {
	if contextWindow >= 1000000 && contextWindow%1000000 == 0 {
		return fmt.Sprintf("-%dm", contextWindow/1000000)
	}
	return fmt.Sprintf("-%dk", contextWindow/1000)
}

func unique(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}
