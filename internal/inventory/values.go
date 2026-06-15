package inventory

import (
	"os"
	"path/filepath"
	"strings"
)

// yamlLine is a deliberately small representation of a non-empty YAML line.
// The scanner below only needs indentation and trimmed text; it is not intended to be a full YAML parser.
type yamlLine struct {
	indent int
	text   string
}

// valuesData contains only the values.yaml fields that can reference SSM paths.
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

// componentData contains per-component ingress/TLS backup settings that can generate SSM paths.
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
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
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

	for componentName, component := range values.Components {
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
				items = append(items,
					Item{Path: joinSSM(prefix, env, "tls", domain, "tls.crt"), Kind: "tls.crt", Source: filepath.ToSlash(path), App: values.AppName, Component: componentName, SecretName: component.TLSSecretName},
					Item{Path: joinSSM(prefix, env, "tls", domain, "tls.key"), Kind: "tls.key", Source: filepath.ToSlash(path), App: values.AppName, Component: componentName, SecretName: component.TLSSecretName},
				)
			}
		}
	}

	return items, nil
}

// parseValues converts the small subset of YAML used by app values files into valuesData.
// The parser is indentation-based and intentionally conservative; it only reads keys that affect SSM discovery.
func parseValues(text string) valuesData {
	lines := parseYAMLLines(text)
	values := valuesData{Components: map[string]componentData{}}

	if app := topBlock(lines, "app"); len(app) > 0 {
		values.AppName = directValue(app, 2, "name")
	}
	if global := topBlock(lines, "global"); len(global) > 0 {
		values.GlobalEnvironment = directValue(global, 2, "environment")
	}
	if secrets := topBlock(lines, "secrets"); len(secrets) > 0 {
		values.SecretsEnabled = parseBool(directValue(secrets, 2, "enabled"))
		values.SecretsProvider = directValue(secrets, 2, "provider")
		values.SecretsPrefix = directValue(secrets, 2, "prefix")
		values.SecretsEnvironment = directValue(secrets, 2, "environment")
		values.SecretsOrg = directValue(secrets, 2, "org")
		values.SecretsName = directValue(secrets, 2, "name")
		values.SecretsKeys = directList(secrets, 2, "keys")
	}

	components := topBlock(lines, "components")
	for name, block := range childBlocks(components, 2) {
		component := componentData{}
		component.Enabled = parseBool(directValue(block, 4, "enabled"))

		ingress := nestedBlock(block, 4, "ingress")
		if len(ingress) > 0 {
			component.IngressEnabled = parseBool(directValue(ingress, 6, "enabled"))
			tls := nestedBlock(ingress, 6, "tls")
			if len(tls) > 0 {
				component.TLSEnabled = parseBool(directValue(tls, 8, "enabled"))
				component.TLSMode = directValue(tls, 8, "mode")
				component.TLSSecretName = directValue(tls, 8, "secretName")

				externalSecret := nestedBlock(tls, 8, "externalSecret")
				if len(externalSecret) > 0 {
					component.TLSCertKey = directValue(externalSecret, 10, "certKey")
					component.TLSKeyKey = directValue(externalSecret, 10, "keyKey")
				}

				backup := nestedBlock(tls, 8, "backupToSsm")
				if len(backup) > 0 {
					component.BackupEnabled = parseBool(directValue(backup, 10, "enabled"))
					component.BackupPrefix = directValue(backup, 10, "prefix")
					component.BackupEnvironment = directValue(backup, 10, "environment")
					component.BackupDomain = directValue(backup, 10, "domain")
					component.BackupDomains = directList(backup, 10, "domains")
				}
			}
		}
		values.Components[name] = component
	}

	return values
}

// parseYAMLLines removes blank/comment lines and stores indentation plus trimmed content for simple block parsing.
// Inline comments are stripped only when they are separated by a space, so hashes inside values are preserved.
func parseYAMLLines(text string) []yamlLine {
	var lines []yamlLine
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if index := strings.Index(trimmed, " #"); index >= 0 {
			trimmed = strings.TrimSpace(trimmed[:index])
		}
		lines = append(lines, yamlLine{indent: countIndent(line), text: trimmed})
	}
	return lines
}

// countIndent counts leading spaces. Tabs are intentionally not treated as indentation for this simple parser.
func countIndent(line string) int {
	count := 0
	for _, r := range line {
		if r != ' ' {
			break
		}
		count++
	}
	return count
}

// topBlock returns the direct children of a top-level YAML block.
func topBlock(lines []yamlLine, key string) []yamlLine {
	return nestedBlock(lines, 0, key)
}

// nestedBlock returns the lines belonging to key: at a specific indentation level.
// The block ends when another line at the same or lower indentation appears.
func nestedBlock(lines []yamlLine, indent int, key string) []yamlLine {
	needle := key + ":"
	for index, line := range lines {
		if line.indent == indent && line.text == needle {
			end := index + 1
			for ; end < len(lines); end++ {
				if lines[end].indent <= indent {
					break
				}
			}
			return lines[index+1 : end]
		}
	}
	return nil
}

// childBlocks returns named child blocks at the requested indentation level.
// It is used for components.<name> sections where names are not known in advance.
func childBlocks(lines []yamlLine, indent int) map[string][]yamlLine {
	blocks := map[string][]yamlLine{}
	for index := 0; index < len(lines); index++ {
		line := lines[index]
		if line.indent != indent || strings.HasPrefix(line.text, "- ") || !strings.HasSuffix(line.text, ":") {
			continue
		}
		name := strings.TrimSuffix(line.text, ":")
		end := index + 1
		for ; end < len(lines); end++ {
			if lines[end].indent <= indent {
				break
			}
		}
		blocks[name] = lines[index+1 : end]
		index = end - 1
	}
	return blocks
}

// directValue reads a scalar key: value pair at the requested indentation level.
func directValue(lines []yamlLine, indent int, key string) string {
	prefix := key + ":"
	for _, line := range lines {
		if line.indent == indent && strings.HasPrefix(line.text, prefix) {
			return cleanValue(strings.TrimSpace(strings.TrimPrefix(line.text, prefix)))
		}
	}
	return ""
}

// directList reads a simple YAML list under key:.
// Only dash-prefixed scalar values are supported because SSM discovery only needs lists of secret keys/domains.
func directList(lines []yamlLine, indent int, key string) []string {
	needle := key + ":"
	for index, line := range lines {
		if line.indent != indent || line.text != needle {
			continue
		}

		var out []string
		for nextIndex := index + 1; nextIndex < len(lines); nextIndex++ {
			next := lines[nextIndex]
			if next.indent < indent {
				break
			}
			if next.indent == indent && !strings.HasPrefix(next.text, "- ") {
				break
			}
			if strings.HasPrefix(next.text, "- ") {
				out = append(out, cleanValue(strings.TrimSpace(strings.TrimPrefix(next.text, "- "))))
			}
		}
		return out
	}
	return nil
}

// cleanValue trims whitespace and removes one layer of matching quote characters from scalar values.
func cleanValue(value string) string {
	value = strings.TrimSpace(value)
	return strings.Trim(value, `"'`)
}

// parseBool accepts the truthy forms used in Helm values files.
func parseBool(value string) bool {
	return strings.EqualFold(value, "true") || strings.EqualFold(value, "yes") || value == "1"
}

// joinSSM joins path fragments into a normalized absolute SSM path.
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
