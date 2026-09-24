package runtimeconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestStageStatic(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.json")
	input := `{
  "config_store":{"enabled":true,"config":{"path":"/var/lib/bifrost/data/config.db"}},
  "plugins":[{"name":"codex-model-router","path":"/plugin.so","config":{"version":1}}]
}`
	if err := os.WriteFile(source, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	appDir := filepath.Join(root, "state")
	destination, err := StageStatic(source, appDir)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	var staged map[string]any
	if err := json.Unmarshal(contents, &staged); err != nil {
		t.Fatal(err)
	}
	plugin := staged["plugins"].([]any)[0].(map[string]any)
	if _, exists := plugin["path"]; exists {
		t.Fatalf("plugin path was retained: %#v", plugin)
	}
	store := staged["config_store"].(map[string]any)["config"].(map[string]any)
	if got, want := store["path"], filepath.Join(appDir, "config.db"); got != want {
		t.Fatalf("config store path=%q want=%q", got, want)
	}
}
