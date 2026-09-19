package config

import "strings"

type ResolvedModel struct {
	Slug     string
	Provider ProviderProfile
	Model    ModelProfile
}

func (c Config) ResolveModel(name string) (ResolvedModel, bool) {
	if model, ok := c.Models[name]; ok {
		return ResolvedModel{Slug: name, Provider: c.Providers[model.Provider], Model: model}, true
	}
	for slug, model := range c.Models {
		for _, alias := range model.Aliases {
			if name == alias {
				return ResolvedModel{Slug: slug, Provider: c.Providers[model.Provider], Model: model}, true
			}
		}
		if slash := strings.IndexByte(slug, '/'); slash >= 0 && name == slug[slash+1:] {
			return ResolvedModel{Slug: slug, Provider: c.Providers[model.Provider], Model: model}, true
		}
	}
	return ResolvedModel{}, false
}
