package codexprofile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMergePreservesExistingAndIsIdempotent(t *testing.T) {
	block, err := Render(Options{})
	if err != nil {
		t.Fatal(err)
	}
	existing := "model = \"existing\"\n# keep this comment\n"
	first, err := Merge(existing, block)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Merge(first, block)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || !strings.Contains(first, existing[:len(existing)-1]) {
		t.Fatalf("merge was not preserving and idempotent:\n%s", second)
	}
}

func TestInstallUninstallAndRestore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	const original = "model = \"existing\"\n"
	if err := os.WriteFile(path, []byte(original), 0o640); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(123, 0)
	backup, err := Install(path, Options{BaseURL: "http://localhost:9000/v1"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if backup == "" {
		t.Fatal("expected backup")
	}
	installed, _ := os.ReadFile(path)
	if !strings.Contains(string(installed), "requires_openai_auth = true") {
		t.Fatalf("profile missing from %s", installed)
	}
	_, found, err := Uninstall(path, now.Add(time.Second))
	if err != nil || !found {
		t.Fatalf("uninstall: found=%v err=%v", found, err)
	}
	removed, _ := os.ReadFile(path)
	if string(removed) != original {
		t.Fatalf("got %q, want %q", removed, original)
	}
	if err := Restore(path, backup); err != nil {
		t.Fatal(err)
	}
	restored, _ := os.ReadFile(path)
	if string(restored) != original {
		t.Fatalf("restore got %q", restored)
	}
}

func TestRejectsMalformedManagedBlockAndSymlink(t *testing.T) {
	if _, err := Merge(beginMarker+"\n", "new"); err == nil {
		t.Fatal("expected malformed block error")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(link, Options{}, time.Now()); err == nil {
		t.Fatal("expected symlink rejection")
	}
}
