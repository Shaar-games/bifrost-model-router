package codexprofile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	beginMarker = "# BEGIN bifrost-model-router (managed)"
	endMarker   = "# END bifrost-model-router (managed)"
)

type Options struct {
	Provider string
	BaseURL  string
}

func Render(options Options) (string, error) {
	if options.Provider == "" {
		options.Provider = "bifrost-router"
	}
	if options.BaseURL == "" {
		options.BaseURL = "http://127.0.0.1:8080/v1"
	}
	if !safeTOMLKey(options.Provider) {
		return "", errors.New("provider name may contain only letters, digits, '-' and '_'")
	}
	if !strings.HasPrefix(options.BaseURL, "http://") && !strings.HasPrefix(options.BaseURL, "https://") {
		return "", errors.New("base URL must use http or https")
	}
	return fmt.Sprintf(`%s
[model_providers.%s]
name = "Bifrost Router"
base_url = %q
wire_api = "responses"
requires_openai_auth = true
%s
`, beginMarker, options.Provider, options.BaseURL, endMarker), nil
}

func Merge(existing string, block string) (string, error) {
	cleaned, found, err := removeManaged(existing)
	if err != nil {
		return "", err
	}
	_ = found
	cleaned = strings.TrimRight(cleaned, " \t\r\n")
	if cleaned == "" {
		return block, nil
	}
	return cleaned + "\n\n" + block, nil
}

func Remove(existing string) (string, bool, error) {
	cleaned, found, err := removeManaged(existing)
	if err != nil {
		return "", false, err
	}
	if !found {
		return existing, false, nil
	}
	cleaned = strings.TrimRight(cleaned, " \t\r\n")
	if cleaned != "" {
		cleaned += "\n"
	}
	return cleaned, true, nil
}

func Install(path string, options Options, now time.Time) (string, error) {
	block, err := Render(options)
	if err != nil {
		return "", err
	}
	existing, mode, err := readExisting(path)
	if err != nil {
		return "", err
	}
	merged, err := Merge(existing, block)
	if err != nil {
		return "", err
	}
	backup, err := backupFile(path, existing, now)
	if err != nil {
		return "", err
	}
	if err := atomicWrite(path, []byte(merged), mode); err != nil {
		return backup, err
	}
	return backup, nil
}

func Uninstall(path string, now time.Time) (string, bool, error) {
	existing, mode, err := readExisting(path)
	if err != nil {
		return "", false, err
	}
	cleaned, found, err := Remove(existing)
	if err != nil || !found {
		return "", found, err
	}
	backup, err := backupFile(path, existing, now)
	if err != nil {
		return "", false, err
	}
	if err := atomicWrite(path, []byte(cleaned), mode); err != nil {
		return backup, false, err
	}
	return backup, true, nil
}

func Restore(path, backup string) error {
	info, err := os.Lstat(backup)
	if err != nil {
		return fmt.Errorf("inspect backup: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("backup must be a regular file")
	}
	data, err := os.ReadFile(backup)
	if err != nil {
		return fmt.Errorf("read backup: %w", err)
	}
	return atomicWrite(path, data, info.Mode().Perm())
}

func removeManaged(existing string) (string, bool, error) {
	start := strings.Index(existing, beginMarker)
	end := strings.Index(existing, endMarker)
	if start < 0 && end < 0 {
		return existing, false, nil
	}
	if start < 0 || end < start {
		return "", false, errors.New("malformed managed Codex profile block")
	}
	end += len(endMarker)
	if strings.Contains(existing[end:], beginMarker) || strings.Contains(existing[end:], endMarker) {
		return "", false, errors.New("multiple managed Codex profile blocks found")
	}
	return existing[:start] + strings.TrimPrefix(existing[end:], "\r\n"), true, nil
}

func readExisting(path string) (string, os.FileMode, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", 0o600, nil
	}
	if err != nil {
		return "", 0, fmt.Errorf("inspect config: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", 0, errors.New("Codex config must be a regular file, not a symlink or special file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, fmt.Errorf("read config: %w", err)
	}
	return string(data), info.Mode().Perm(), nil
}

func backupFile(path, existing string, now time.Time) (string, error) {
	if existing == "" {
		return "", nil
	}
	backup := fmt.Sprintf("%s.bifrost-router.%s.bak", path, now.UTC().Format("20060102T150405.000000000Z"))
	if err := os.WriteFile(backup, []byte(existing), 0o600); err != nil {
		return "", fmt.Errorf("write backup: %w", err)
	}
	return backup, nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".bifrost-router-config-*")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

func safeTOMLKey(value string) bool {
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return value != ""
}
