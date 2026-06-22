package inventory

import (
	"path/filepath"
	"strings"

	crerr "github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/fileio"
)

// valuesData contains only the values.yaml fields that can reference SSM names.
// Keeping this structure narrow makes discovery independent from unrelated Helm values.
type valuesData struct {
	AppName            string
	GlobalEnvironment  string
	SecretsEnabled     bool
	SecretsProvider    string
	SecretsPrefix      string
	SecretsEnvironment string
	SecretsOrg         string
	SecretsName        string
	SecretsKeys        []string
	Components         map[string]componentData
}

// componentData contains per-component ingress/TLS backup settings that can generate SSM names.
type componentData struct {
	Enabled           bool
	IngressEnabled    bool
	TLSEnabled        bool
	TLSMode           string
	TLSSecretName     string
	TLSCertKey        string
	TLSKeyKey         string
	BackupEnabled     bool
	BackupPrefix      string
	BackupEnvironment string
	BackupDomain      string
	BackupDomains     []string
}

// scanValuesFile extracts all SSM-backed secrets from one app values.yaml file.
// It supports two families of references: app secrets declared under secrets.keys and TLS certificate/key paths
// either provided directly via externalSecret or generated from backupToSsm domain settings.
func scanValuesFile(path, fallbackAppName string) ([]Item, error) {
	data, err := fileio.ReadFile(path)
	if err != nil {
		return nil, crerr.Wrapf(err, "read values file %s", path)
	}

	values := parseValues(string(data))
	if values.AppName == "" {
		values.AppName = fallbackAppName
	}
	if values.GlobalEnvironment == "" {
		values.GlobalEnvironment = values.SecretsEnvironment
	}

	var items []Item
	if values.SecretsEnabled && values.SecretsProvider == "aws-ssm-params" {
		prefix := defaultString(values.SecretsPrefix, "/app-infra")
		env := defaultString(values.SecretsEnvironment, values.GlobalEnvironment)
		for _, key := range values.SecretsKeys {
			items = append(items, Item{
				Path:   joinSSM(prefix, env, values.SecretsOrg, values.SecretsName, key),
				Kind:   "app-secret",
				Source: filepath.ToSlash(path),
				App:    values.AppName,
			})
		}
	}

	for componentName := range values.Components {
		component := values.Components[componentName]
		if !component.Enabled || !component.IngressEnabled || !component.TLSEnabled {
			continue
		}

		if component.TLSMode == "externalSecret" {
			if component.TLSCertKey != "" {
				items = append(items, Item{Path: component.TLSCertKey, Kind: "tls.crt", Source: filepath.ToSlash(path), App: values.AppName, Component: componentName, SecretName: component.TLSSecretName})
			}
			if component.TLSKeyKey != "" {
				items = append(items, Item{Path: component.TLSKeyKey, Kind: "tls.key", Source: filepath.ToSlash(path), App: values.AppName, Component: componentName, SecretName: component.TLSSecretName})
			}
		}

		if component.BackupEnabled {
			prefix := defaultString(component.BackupPrefix, "/app-infra")
			env := defaultString(component.BackupEnvironment, values.GlobalEnvironment)
			domains := component.BackupDomains
			if len(domains) == 0 && component.BackupDomain != "" {
				domains = []string{component.BackupDomain}
			}
			for _, domain := range domains {
				items = append(
					items,
					Item{Path: joinSSM(prefix, env, "tls", domain, "tls.crt"), Kind: "tls.crt", Source: filepath.ToSlash(path), App: values.AppName, Component: componentName, SecretName: component.TLSSecretName},
					Item{Path: joinSSM(prefix, env, "tls", domain, "tls.key"), Kind: "tls.key", Source: filepath.ToSlash(path), App: values.AppName, Component: componentName, SecretName: component.TLSSecretName},
				)
			}
		}
	}

	return items, nil
}

// joinSSM joins path fragments into a normalized absolute SSM name.
// Empty fragments are skipped and duplicate slashes around fragments are removed.
func joinSSM(parts ...string) string {
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(part, "/")
		if part != "" {
			clean = append(clean, part)
		}
	}
	return "/" + strings.Join(clean, "/")
}

// defaultString returns fallback when value is empty.
func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
