package configwatch

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"time"
)

// QuietWindow is how long the watched files must stay unchanged before a
// restart. Editors often write a file more than once while saving.
const QuietWindow = 500 * time.Millisecond

// State decides when a configuration fingerprint has settled on a new value.
type State struct {
	Baseline           string
	pending            bool
	pendingFingerprint string
	deadline           time.Time
}

// Fingerprint hashes the existence and contents of each path. A missing file
// is part of the fingerprint, so creating it later is a change. The contents
// are not retained.
func Fingerprint(paths []string) (string, error) {
	hash := sha256.New()
	for _, path := range paths {
		if _, err := fmt.Fprintf(hash, "%s\n", path); err != nil {
			return "", err
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return "", fmt.Errorf("read %s: %w", path, err)
			}
			if _, err := hash.Write([]byte{0}); err != nil {
				return "", err
			}
			continue
		}
		if _, err := hash.Write([]byte{1}); err != nil {
			return "", err
		}
		if _, err := hash.Write(contents); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// Observe reports whether the files have held a new fingerprint for quiet.
// Repeated changes postpone the restart until the writes stop.
func (s *State) Observe(now time.Time, fingerprint string, quiet time.Duration) bool {
	if fingerprint == s.Baseline {
		s.pending = false
		s.pendingFingerprint = ""
		return false
	}
	if !s.pending || fingerprint != s.pendingFingerprint {
		s.pending = true
		s.pendingFingerprint = fingerprint
		s.deadline = now.Add(quiet)
		return false
	}
	if now.Before(s.deadline) {
		return false
	}
	s.Baseline = fingerprint
	s.pending = false
	s.pendingFingerprint = ""
	return true
}
