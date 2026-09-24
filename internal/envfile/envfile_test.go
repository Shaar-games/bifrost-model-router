package envfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.env")
	if err := os.WriteFile(path, []byte("# comment\nFIRST=value\nSECOND=\"quoted value\"\nexport THIRD='literal'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FIRST", "existing")
	if err := Load(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("FIRST"); got != "existing" {
		t.Fatalf("FIRST=%q", got)
	}
	if got := os.Getenv("SECOND"); got != "quoted value" {
		t.Fatalf("SECOND=%q", got)
	}
	if got := os.Getenv("THIRD"); got != "literal" {
		t.Fatalf("THIRD=%q", got)
	}
}

func TestLoadRejectsInvalidAssignment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.env")
	if err := os.WriteFile(path, []byte("NOT VALID\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Load(path); err == nil {
		t.Fatal("expected invalid assignment to fail")
	}
}
