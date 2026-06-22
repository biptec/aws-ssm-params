package inventory

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	crerr "github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/fileio"
)

// LoadPathsFile reads a simple newline-based SSM inventory file.
// Empty lines and comments are ignored, inline comments are stripped, paths must be absolute SSM names,
// duplicates are removed, and the final list is sorted to make downstream output deterministic.
func LoadPathsFile(filePath string) ([]Item, error) {
	file, err := fileio.Open(filePath)
	if err != nil {
		return nil, crerr.Wrapf(err, "open paths file %s", filePath)
	}
	defer func() { _ = file.Close() }()

	items, err := LoadPaths(file, filePath)
	if err != nil {
		return nil, err
	}
	return items, nil
}

// LoadPaths reads newline-based SSM inventory content from reader.
// Empty lines and comments are ignored, inline comments are stripped, paths must be absolute SSM names,
// duplicates are removed, and the final list is sorted to make downstream output deterministic.
func LoadPaths(reader io.Reader, source string) ([]Item, error) {
	seen := map[string]bool{}
	var items []Item
	scanner := bufio.NewScanner(reader)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		raw := pathFileLinePath(scanner.Text())
		if raw == "" {
			continue
		}
		if !strings.HasPrefix(raw, "/") {
			return nil, fmt.Errorf("invalid SSM name in %s:%d: %s", source, lineNo, raw)
		}
		if seen[raw] {
			continue
		}
		seen[raw] = true
		items = append(items, Item{Path: raw, Kind: "path-file", Source: source, SecretName: path.Base(raw)})
	}
	if err := scanner.Err(); err != nil {
		return nil, crerr.Wrapf(err, "scan paths from %s", source)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	return items, nil
}

// AppendPathIfMissing appends one absolute SSM name to a paths file unless that path is already listed.
// It preserves the existing file order and adds the new path at the end so the file can be grown from the TUI.
func AppendPathIfMissing(filePath, parameterPath string) (bool, error) {
	parameterPath = strings.TrimSpace(parameterPath)
	if parameterPath == "" {
		return false, errors.New("SSM name is required")
	}
	if !strings.HasPrefix(parameterPath, "/") {
		return false, fmt.Errorf("invalid SSM name: %s", parameterPath)
	}

	cleanPath := filepath.Clean(filePath)
	data, err := fileio.ReadFile(cleanPath)
	if err != nil && !os.IsNotExist(err) {
		return false, crerr.Wrapf(err, "read paths file %s", filePath)
	}
	if pathFileContainsPath(string(data), parameterPath) {
		return false, nil
	}

	prefix := ""
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		prefix = "\n"
	}
	file, err := fileio.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return false, crerr.Wrapf(err, "open paths file %s", filePath)
	}
	defer func() { _ = file.Close() }()
	if _, err := file.WriteString(prefix + parameterPath + "\n"); err != nil {
		return false, crerr.Wrapf(err, "append path to %s", filePath)
	}
	return true, nil
}

func pathFileContainsPath(content, parameterPath string) bool {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		if pathFileLinePath(scanner.Text()) == parameterPath {
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
	cleanPath := filepath.Clean(filePath)
	data, err := fileio.ReadFile(cleanPath)
	if err != nil {
		return 0, crerr.Wrapf(err, "read paths file %s", cleanPath)
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
	file, err := fileio.OpenFile(cleanPath, os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, crerr.Wrapf(err, "open paths file %s", cleanPath)
	}
	if _, err := file.WriteString(strings.Join(remaining, "")); err != nil {
		_ = file.Close()
		return 0, crerr.Wrapf(err, "write paths file %s", cleanPath)
	}
	if err := file.Close(); err != nil {
		return 0, crerr.Wrapf(err, "close paths file %s", cleanPath)
	}
	return removed, nil
}

func pathFileLinePath(line string) string {
	raw := strings.TrimSpace(line)
	if raw == "" || strings.HasPrefix(raw, "#") {
		return ""
	}
	if i := strings.Index(raw, "#"); i >= 0 {
		raw = strings.TrimSpace(raw[:i])
	}
	return raw
}
