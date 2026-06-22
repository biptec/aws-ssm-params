// Package inventory discovers and stores SSM parameter inventory items.
package inventory

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/fileio"
)

// Discoverer scans one GitOps repository/environment pair.
type Discoverer struct {
	RepoRoot    string
	Environment string
	EnabledOnly bool
}

// Discover scans the original GitOps repository layout and builds an inventory of SSM parameters.
// It combines enabled app values.yaml files, cluster-level kustomization secrets, and Terraform Flux token config,
// then deduplicates and sorts everything so generated path files and UI rows are stable.
func (discoverer Discoverer) Discover() (Items, error) {
	envDir := discoverer.environmentDir()
	appsDir := filepath.Join(envDir, "apps")
	if !exists(appsDir) {
		return nil, fmt.Errorf("apps directory not found: %s", appsDir)
	}

	enabledApps, err := discoverer.enabledApps()
	if err != nil {
		return nil, errors.Wrap(err, "discover enabled apps")
	}

	entries, err := os.ReadDir(appsDir)
	if err != nil {
		return nil, errors.Wrapf(err, "read apps directory %s", appsDir)
	}

	var items Items
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		appName := entry.Name()
		if discoverer.EnabledOnly && !enabledApps[appName] {
			continue
		}
		valuesPath := filepath.Join(appsDir, appName, "values.yaml")
		if !exists(valuesPath) {
			continue
		}
		appItems, err := scanValuesFile(valuesPath, appName)
		if err != nil {
			return nil, errors.Wrapf(err, "scan values file %s", valuesPath)
		}
		items = append(items, appItems...)
	}

	kItems, err := discoverer.scanKustomizationForSecrets()
	if err != nil {
		return nil, errors.Wrap(err, "scan kustomization secrets")
	}
	items = append(items, kItems...)

	fItems, err := discoverer.scanTerraformFluxToken()
	if err != nil {
		return nil, errors.Wrap(err, "scan Terraform Flux token")
	}
	items = append(items, fItems...)

	items = items.MergeDuplicates()
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	return items, nil
}

func (discoverer Discoverer) environmentDir() string {
	return filepath.Join(discoverer.RepoRoot, "clusters", discoverer.Environment)
}

// enabledApps parses an environment kustomization.yaml and returns apps that are actively referenced.
// This lets discovery ignore app directories that exist in the repo but are not deployed in the selected environment.
func (discoverer Discoverer) enabledApps() (map[string]bool, error) {
	result := map[string]bool{}
	path := filepath.Join(discoverer.environmentDir(), "kustomization.yaml")
	data, err := fileio.ReadFile(path)
	if err != nil {
		return result, errors.Wrapf(err, "read kustomization %s", path)
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
func (discoverer Discoverer) scanKustomizationForSecrets() (Items, error) {
	path := filepath.Join(discoverer.environmentDir(), "kustomization.yaml")
	data, err := fileio.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(err, "read kustomization %s", path)
	}
	seen := map[string]bool{}
	var items []Item
	re := regexp.MustCompile(`/app-infra/` + regexp.QuoteMeta(discoverer.Environment) + `/ghcr/token`)
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
func (discoverer Discoverer) scanTerraformFluxToken() (Items, error) {
	path := filepath.Join(discoverer.RepoRoot, "terraform", "environments", discoverer.Environment, "terraform.tfvars")
	value := "/flux/github/token"
	if data, err := fileio.ReadFile(path); err == nil {
		re := regexp.MustCompile(`(?m)^\s*gitops_token_ssm_parameter\s*=\s*"([^"]+)"`)
		m := re.FindStringSubmatch(string(data))
		if len(m) == 2 && m[1] != "" {
			value = m[1]
		}
	}
	return Items{{Path: value, Kind: "flux-token", Source: filepath.ToSlash(path)}}, nil
}

// exists reports whether a path exists and can be statted.
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
