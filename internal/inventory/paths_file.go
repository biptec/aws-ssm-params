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
// Empty lines and comments are ignored, inline comments are stripped, paths must be absolute SSM paths,
// duplicates are removed, and the final list is sorted to make downstream output deterministic.
func LoadPathsFile(filePath string) ([]Item, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

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
			return nil, fmt.Errorf("invalid SSM path in %s:%d: %s", filePath, lineNo, raw)
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
