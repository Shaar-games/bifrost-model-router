package configwatch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFingerprintChangesWithFileContents(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	envPath := filepath.Join(dir, "providers.env")
	if err := os.WriteFile(configPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	missingEnv, err := Fingerprint([]string{configPath, envPath})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(envPath, []byte("TOKEN=one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	withEnv, err := Fingerprint([]string{configPath, envPath})
	if err != nil {
		t.Fatal(err)
	}
	if missingEnv == withEnv {
		t.Fatal("creating providers.env did not change the fingerprint")
	}
	if err := os.WriteFile(configPath, []byte("{}\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	edited, err := Fingerprint([]string{configPath, envPath})
	if err != nil {
		t.Fatal(err)
	}
	if edited == withEnv {
		t.Fatal("editing config.json did not change the fingerprint")
	}
	again, err := Fingerprint([]string{configPath, envPath})
	if err != nil {
		t.Fatal(err)
	}
	if again != edited {
		t.Fatal("fingerprint was not stable")
	}
}

func TestObserveWaitsUntilWritesSettle(t *testing.T) {
	start := time.Unix(0, 0)
	state := State{Baseline: "a"}
	if state.Observe(start, "a", QuietWindow) {
		t.Fatal("unchanged files restarted")
	}
	if state.Observe(start.Add(100*time.Millisecond), "b", QuietWindow) {
		t.Fatal("restarted before the quiet window")
	}
	if state.Observe(start.Add(400*time.Millisecond), "c", QuietWindow) {
		t.Fatal("a second write did not postpone the restart")
	}
	if !state.Observe(start.Add(400*time.Millisecond+QuietWindow), "c", QuietWindow) {
		t.Fatal("settled change did not restart")
	}
	if state.Observe(start.Add(2*time.Second), "c", QuietWindow) {
		t.Fatal("restarted again without a new change")
	}
}
