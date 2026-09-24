package runtimeconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const routerPluginName = "codex-model-router"

// StageStatic copies a declarative Bifrost configuration into an application
// directory and removes the shared-object path from the router plugin. The
// portable server resolves that plugin from its compile-time registry.
func StageStatic(source, appDir string) (string, error) {
	contents, err := os.ReadFile(source)
	if err != nil {
		return "", fmt.Errorf("read config: %w", err)
	}
	var root map[string]any
	if err := json.Unmarshal(contents, &root); err != nil {
		return "", fmt.Errorf("decode config: %w", err)
	}
	plugins, ok := root["plugins"].([]any)
	if !ok {
		return "", fmt.Errorf("config plugins must be an array")
	}
	found := false
	for _, raw := range plugins {
		plugin, ok := raw.(map[string]any)
		if !ok || plugin["name"] != routerPluginName {
			continue
		}
		delete(plugin, "path")
		found = true
	}
	if !found {
		return "", fmt.Errorf("config does not contain the %s plugin", routerPluginName)
	}
	if store, ok := root["config_store"].(map[string]any); ok {
		if storeConfig, ok := store["config"].(map[string]any); ok {
			storeConfig["path"] = filepath.Join(appDir, "config.db")
		}
	}
	staged, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode config: %w", err)
	}
	if err := os.MkdirAll(appDir, 0o700); err != nil {
		return "", fmt.Errorf("create application directory: %w", err)
	}
	destination := filepath.Join(appDir, "config.json")
	temporary, err := os.CreateTemp(appDir, "config-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create staged config: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return "", fmt.Errorf("secure staged config: %w", err)
	}
	if _, err := temporary.Write(append(staged, '\n')); err != nil {
		temporary.Close()
		return "", fmt.Errorf("write staged config: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("close staged config: %w", err)
	}
	if err := os.Rename(temporaryName, destination); err != nil {
		return "", fmt.Errorf("install staged config: %w", err)
	}
	return destination, nil
}
