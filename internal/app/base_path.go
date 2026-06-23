package app

import (
	"fmt"
	"strings"

	"github.com/cockroachdb/errors"
)

// BasePath is an optional absolute SSM path used to resolve relative names on import
// and produce relative names on export.
type BasePath string

// ParseBasePath normalizes a CLI base path while preserving an empty value as disabled.
func ParseBasePath(value string) (BasePath, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if !strings.HasPrefix(value, "/") {
		return "", errors.New("--base-path must start with /")
	}
	value = strings.TrimRight(value, "/")
	if value == "" {
		value = "/"
	}
	return BasePath(value), nil
}

// Resolve returns an absolute SSM path. Absolute names are preserved; relative
// names are joined to the configured base path.
func (base BasePath) Resolve(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("parameter name is required")
	}
	if strings.HasPrefix(name, "/") {
		return name, nil
	}
	if base == "" {
		return "", fmt.Errorf("relative parameter name %q requires --base-path", name)
	}
	if base == "/" {
		return "/" + strings.TrimLeft(name, "/"), nil
	}
	return string(base) + "/" + strings.TrimLeft(name, "/"), nil
}

// Relativize removes the configured base path from an absolute SSM name.
// An empty base leaves the name unchanged.
func (base BasePath) Relativize(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("parameter name is required")
	}
	if base == "" {
		return name, nil
	}
	if !strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("parameter name %q is not an absolute SSM path", name)
	}
	if base == "/" {
		relative := strings.TrimLeft(name, "/")
		if relative == "" {
			return "", fmt.Errorf("parameter name %q has no path relative to --base-path %q", name, base)
		}
		return relative, nil
	}
	prefix := string(base) + "/"
	relative, ok := strings.CutPrefix(name, prefix)
	if !ok || relative == "" {
		return "", fmt.Errorf("parameter name %q is outside --base-path %q", name, base)
	}
	return relative, nil
}
