// Package inventory discovers and stores SSM parameter inventory items.
package inventory

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Discover scans the original GitOps repository layout and builds an inventory of SSM parameters.
// It combines enabled app values.yaml files, cluster-level kustomization secrets, and Terraform Flux token config,
// then deduplicates and sorts everything so generated path files and UI rows are stable.
func Discover(repoRoot, env string, enabledOnly bool) ([]Item, error) {
	envDir := filepath.Join(repoRoot, "clusters", env)
	appsDir := filepath.Join(envDir, "apps")
	if !exists(appsDir) {
		return nil, fmt.Errorf("apps directory not found: %s", appsDir)
	}

	enabledApps, err := discoverEnabledApps(envDir)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(appsDir)
	if err != nil {
		return nil, err
	}

	var items []Item
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		appName := entry.Name()
		if enabledOnly && !enabledApps[appName] {
			continue
		}
		valuesPath := filepath.Join(appsDir, appName, "values.yaml")
		if !exists(valuesPath) {
			continue
		}
		appItems, err := scanValuesFile(valuesPath, appName)
		if err != nil {
			return nil, err
		}
		items = append(items, appItems...)
	}

	kItems, err := scanKustomizationForSecrets(envDir, env)
	if err != nil {
		return nil, err
	}
	items = append(items, kItems...)

	fItems, err := scanTerraformFluxToken(repoRoot, env)
	if err != nil {
		return nil, err
	}
	items = append(items, fItems...)

	items = dedupe(items)
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	return items, nil
}

// discoverEnabledApps parses an environment kustomization.yaml and returns apps that are actively referenced.
// This lets discovery ignore app directories that exist in the repo but are not deployed in the selected environment.
func discoverEnabledApps(envDir string) (map[string]bool, error) {
	result := map[string]bool{}
	data, err := os.ReadFile(filepath.Join(envDir, "kustomization.yaml"))
	if err != nil {
		return result, err
	}
	re := regexp.MustCompile(`apps/([^/]+)/helmrelease\.yaml`)
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := re.FindStringSubmatch(line)
		if len(m) == 2 {
			result[m[1]] = true
		}
	}
	return result, nil
}

// scanKustomizationForSecrets finds cluster-level SSM names embedded directly in kustomization.yaml.
// Currently it extracts GHCR token references because those are shared infrastructure secrets, not app-local values.
func scanKustomizationForSecrets(envDir, env string) ([]Item, error) {
	path := filepath.Join(envDir, "kustomization.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var items []Item
	re := regexp.MustCompile(`/app-infra/` + regexp.QuoteMeta(env) + `/ghcr/token`)
	for _, match := range re.FindAllString(string(data), -1) {
		if !seen[match] {
			seen[match] = true
			items = append(items, Item{Path: match, Kind: "ghcr-token", Source: filepath.ToSlash(path)})
		}
	}
	return items, nil
}

// scanTerraformFluxToken resolves the Flux GitHub token SSM name from terraform.tfvars.
// If the Terraform variable is absent, it returns the conventional default path so the inventory still includes the token.
func scanTerraformFluxToken(repoRoot, env string) ([]Item, error) {
	path := filepath.Join(repoRoot, "terraform", "environments", env, "terraform.tfvars")
	value := "/flux/github/token"
	if data, err := os.ReadFile(path); err == nil {
		re := regexp.MustCompile(`(?m)^\s*gitops_token_ssm_parameter\s*=\s*"([^"]+)"`)
		m := re.FindStringSubmatch(string(data))
		if len(m) == 2 && m[1] != "" {
			value = m[1]
		}
	}
	return []Item{{Path: value, Kind: "flux-token", Source: filepath.ToSlash(path)}}, nil
}

// dedupe merges multiple discoveries of the same SSM name into one inventory item.
// Metadata fields are concatenated instead of discarded so users can still see every source/kind that referenced the path.
func dedupe(items []Item) []Item {
	byPath := map[string]Item{}
	for _, item := range items {
		if item.Path == "" {
			continue
		}
		if old, ok := byPath[item.Path]; ok {
			old.Kind = merge(old.Kind, item.Kind)
			old.Source = merge(old.Source, item.Source)
			old.App = merge(old.App, item.App)
			old.Component = merge(old.Component, item.Component)
			old.SecretName = merge(old.SecretName, item.SecretName)
			byPath[item.Path] = old
			continue
		}
		byPath[item.Path] = item
	}

	out := make([]Item, 0, len(byPath))
	for _, item := range byPath {
		out = append(out, item)
	}
	return out
}

// merge appends metadata value b to value a unless it is empty or already included.
func merge(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" || strings.Contains(a, b) {
		return a
	}
	return a + "," + b
}

// exists reports whether a path exists and can be statted.
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
