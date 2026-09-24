package codexprofile

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	quickDefaultsBegin = "# BEGIN bifrost-model-router quickstart defaults (managed)"
	quickDefaultsEnd   = "# END bifrost-model-router quickstart defaults (managed)"
	quickProviderBegin = "# BEGIN bifrost-model-router quickstart provider (managed)"
	quickProviderEnd   = "# END bifrost-model-router quickstart provider (managed)"
)

type QuickstartOptions struct {
	Provider        string
	BaseURL         string
	VirtualKey      string
	Model           string
	ReasoningEffort string
}

func InstallQuickstart(path string, options QuickstartOptions, now time.Time) (string, error) {
	options, err := normalizeQuickstartOptions(options)
	if err != nil {
		return "", err
	}
	existing, mode, err := readExisting(path)
	if err != nil {
		return "", err
	}
	merged, err := mergeQuickstart(existing, options)
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

func UninstallQuickstart(path string, now time.Time) (string, bool, error) {
	existing, mode, err := readExisting(path)
	if err != nil {
		return "", false, err
	}
	cleaned, found, err := removeQuickstart(existing)
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

func normalizeQuickstartOptions(options QuickstartOptions) (QuickstartOptions, error) {
	if options.Provider == "" {
		options.Provider = "bifrost-router"
	}
	if options.BaseURL == "" {
		options.BaseURL = "http://127.0.0.1:8080/v1"
	}
	if options.Model == "" {
		options.Model = "gpt-5.6-sol"
	}
	if options.ReasoningEffort == "" {
		options.ReasoningEffort = "medium"
	}
	if !safeTOMLKey(options.Provider) {
		return options, errors.New("provider name may contain only letters, digits, '-' and '_'")
	}
	parsed, err := url.Parse(options.BaseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || !strings.HasSuffix(parsed.Path, "/v1") || strings.HasSuffix(options.BaseURL, "/") {
		return options, errors.New("base URL must be an http(s) API root ending in /v1 without a trailing slash")
	}
	if options.VirtualKey == "" {
		return options, errors.New("virtual key cannot be empty")
	}
	if !safeModel(options.Model) {
		return options, errors.New("model contains unsupported characters")
	}
	switch options.ReasoningEffort {
	case "minimal", "low", "medium", "high", "xhigh":
	default:
		return options, fmt.Errorf("invalid reasoning effort %q", options.ReasoningEffort)
	}
	return options, nil
}

func mergeQuickstart(existing string, options QuickstartOptions) (string, error) {
	cleaned, _, err := removeQuickstart(existing)
	if err != nil {
		return "", err
	}
	cleaned = removeRootDefaults(cleaned)
	cleaned = strings.Trim(cleaned, " \t\r\n")

	var output strings.Builder
	fmt.Fprintln(&output, quickDefaultsBegin)
	fmt.Fprintf(&output, "model = %q\n", options.Model)
	fmt.Fprintf(&output, "model_provider = %q\n", options.Provider)
	fmt.Fprintf(&output, "model_reasoning_effort = %q\n", options.ReasoningEffort)
	fmt.Fprintln(&output, quickDefaultsEnd)
	if cleaned != "" {
		output.WriteString("\n")
		output.WriteString(cleaned)
		output.WriteString("\n")
	}
	output.WriteString("\n")
	fmt.Fprintln(&output, quickProviderBegin)
	fmt.Fprintf(&output, "[model_providers.%s]\n", options.Provider)
	fmt.Fprintln(&output, `name = "Local Bifrost Router"`)
	fmt.Fprintf(&output, "base_url = %q\n", options.BaseURL)
	fmt.Fprintln(&output, `wire_api = "responses"`)
	fmt.Fprintln(&output, "requires_openai_auth = true")
	fmt.Fprintf(&output, "http_headers = { \"x-bf-vk\" = %q }\n", options.VirtualKey)
	fmt.Fprintln(&output, quickProviderEnd)
	return output.String(), nil
}

func removeQuickstart(existing string) (string, bool, error) {
	cleaned := existing
	found := false
	for _, markers := range [][2]string{{quickDefaultsBegin, quickDefaultsEnd}, {quickProviderBegin, quickProviderEnd}, {beginMarker, endMarker}} {
		var removed bool
		var err error
		cleaned, removed, err = removeMarked(cleaned, markers[0], markers[1])
		if err != nil {
			return "", false, err
		}
		found = found || removed
	}
	var removed bool
	cleaned, removed = removeProviderTable(cleaned, "bifrost-router")
	found = found || removed
	cleaned = strings.TrimSpace(cleaned)
	if cleaned != "" {
		cleaned += "\n"
	}
	return cleaned, found, nil
}

func removeMarked(input, begin, end string) (string, bool, error) {
	start := strings.Index(input, begin)
	finish := strings.Index(input, end)
	if start < 0 && finish < 0 {
		return input, false, nil
	}
	if start < 0 || finish < start {
		return "", false, fmt.Errorf("malformed managed block %q", begin)
	}
	finish += len(end)
	if strings.Contains(input[finish:], begin) || strings.Contains(input[finish:], end) {
		return "", false, fmt.Errorf("multiple managed blocks %q", begin)
	}
	return input[:start] + strings.TrimPrefix(strings.TrimPrefix(input[finish:], "\r"), "\n"), true, nil
}

func removeProviderTable(input, provider string) (string, bool) {
	lines := strings.Split(strings.ReplaceAll(input, "\r\n", "\n"), "\n")
	prefix := "[model_providers." + provider
	out := make([]string, 0, len(lines))
	skipping := false
	found := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, prefix) && (strings.HasSuffix(trimmed, "]") || strings.Contains(trimmed, ".")) {
			skipping = true
			found = true
			continue
		}
		if skipping && strings.HasPrefix(trimmed, "[") {
			skipping = false
		}
		if !skipping {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n"), found
}

func removeRootDefaults(input string) string {
	lines := strings.Split(strings.ReplaceAll(input, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	atRoot := true
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			atRoot = false
		}
		if atRoot && (strings.HasPrefix(trimmed, "model =") || strings.HasPrefix(trimmed, "model_provider =") || strings.HasPrefix(trimmed, "model_reasoning_effort =")) {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func safeModel(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("._:/-", char) {
			continue
		}
		return false
	}
	return true
}
