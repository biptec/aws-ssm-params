package inventory

import (
	"bufio"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
)

// LoadPathsFile reads a simple newline-based SSM inventory file.
// Empty lines and comments are ignored, inline comments are stripped, paths must be absolute SSM names,
// duplicates are removed, and the final list is sorted to make downstream output deterministic.
func LoadPathsFile(filePath string) ([]Item, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	seen := map[string]bool{}
	var items []Item
	scanner := bufio.NewScanner(file)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		if i := strings.Index(raw, "#"); i >= 0 {
			raw = strings.TrimSpace(raw[:i])
		}
		if raw == "" {
			continue
		}
		if !strings.HasPrefix(raw, "/") {
			return nil, fmt.Errorf("invalid SSM name in %s:%d: %s", filePath, lineNo, raw)
		}
		if seen[raw] {
			continue
		}
		seen[raw] = true
		items = append(items, Item{Path: raw, Kind: "path-file", Source: filePath, SecretName: path.Base(raw)})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	return items, nil
}

// AppendPathIfMissing appends one absolute SSM name to a paths file unless that path is already listed.
// It preserves the existing file order and adds the new path at the end so the file can be grown from the TUI.
func AppendPathIfMissing(filePath, parameterPath string) (bool, error) {
	parameterPath = strings.TrimSpace(parameterPath)
	if parameterPath == "" {
		return false, fmt.Errorf("SSM name is required")
	}
	if !strings.HasPrefix(parameterPath, "/") {
		return false, fmt.Errorf("invalid SSM name: %s", parameterPath)
	}

	data, err := os.ReadFile(filePath)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if pathFileContainsPath(string(data), parameterPath) {
		return false, nil
	}

	prefix := ""
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		prefix = "\n"
	}
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return false, err
	}
	defer func() { _ = file.Close() }()
	if _, err := file.WriteString(prefix + parameterPath + "\n"); err != nil {
		return false, err
	}
	return true, nil
}

func pathFileContainsPath(content, parameterPath string) bool {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		if i := strings.Index(raw, "#"); i >= 0 {
			raw = strings.TrimSpace(raw[:i])
		}
		if raw == parameterPath {
			return true
		}
	}
	return false
}

// RemovePathsIfPresent removes listed SSM names from a paths file, preserving comments and unrelated entries.
// Lines that contain a matching path followed by an inline comment are removed as a whole.
func RemovePathsIfPresent(filePath string, parameterPaths []string) (int, error) {
	targets := map[string]bool{}
	for _, parameterPath := range parameterPaths {
		parameterPath = strings.TrimSpace(parameterPath)
		if parameterPath != "" {
			targets[parameterPath] = true
		}
	}
	if len(targets) == 0 {
		return 0, nil
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return 0, err
	}
	parts := strings.SplitAfter(string(data), "\n")
	remaining := make([]string, 0, len(parts))
	removed := 0
	for _, line := range parts {
		if line == "" {
			continue
		}
		if targets[pathFileLinePath(line)] {
			removed++
			continue
		}
		remaining = append(remaining, line)
	}
	if removed == 0 {
		return 0, nil
	}
	if err := os.WriteFile(filePath, []byte(strings.Join(remaining, "")), 0o600); err != nil { // #nosec G703 -- filePath is an explicit user-configured paths file.
		return 0, err
	}
	return removed, nil
}

func pathFileLinePath(line string) string {
	raw := strings.TrimSpace(strings.TrimRight(line, "\r\n"))
	if raw == "" || strings.HasPrefix(raw, "#") {
		return ""
	}
	if i := strings.Index(raw, "#"); i >= 0 {
		raw = strings.TrimSpace(raw[:i])
	}
	return raw
}
