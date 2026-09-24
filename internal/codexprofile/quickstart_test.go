package codexprofile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInstallQuickstartIsIdempotentAndKeepsExistingTables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	original := "model = \"old\"\n[features]\napps = true\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	options := QuickstartOptions{VirtualKey: "sk-bf-test", Model: "gpt-5.6-sol", ReasoningEffort: "medium"}
	if _, err := InstallQuickstart(path, options, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(path)
	if _, err := InstallQuickstart(path, options, time.Unix(2, 0)); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Fatalf("installation is not idempotent:\n%s\n---\n%s", first, second)
	}
	installed := string(second)
	for _, expected := range []string{
		`model = "gpt-5.6-sol"`,
		`model_provider = "bifrost-router"`,
		`http_headers = { "x-bf-vk" = "sk-bf-test" }`,
		"[features]\napps = true",
	} {
		if !strings.Contains(installed, expected) {
			t.Fatalf("missing %q from:\n%s", expected, installed)
		}
	}
	if strings.Contains(installed, `model = "old"`) {
		t.Fatalf("old root model was retained:\n%s", installed)
	}
}

func TestUninstallQuickstart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[features]\napps = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallQuickstart(path, QuickstartOptions{VirtualKey: "key"}, time.Unix(1, 0)); err != nil {
		t.Fatal(err)
	}
	_, found, err := UninstallQuickstart(path, time.Unix(2, 0))
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	contents, _ := os.ReadFile(path)
	if strings.TrimSpace(string(contents)) != "[features]\napps = true" {
		t.Fatalf("unexpected uninstall result: %q", contents)
	}
}
