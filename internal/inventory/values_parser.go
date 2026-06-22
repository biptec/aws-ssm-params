package inventory

import "strings"

// yamlLine is the narrow representation needed by the values.yaml subset parser.
// It intentionally does not try to model arbitrary YAML.
type yamlLine struct {
	indent int
	text   string
}

// parseValues converts the small subset of YAML used by app values files into valuesData.
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

	for name, block := range childBlocks(topBlock(lines, "components"), 2) {
		values.Components[name] = parseComponent(block)
	}
	return values
}

func parseComponent(block []yamlLine) componentData {
	component := componentData{Enabled: parseBool(directValue(block, 4, "enabled"))}
	ingress := nestedBlock(block, 4, "ingress")
	if len(ingress) == 0 {
		return component
	}
	component.IngressEnabled = parseBool(directValue(ingress, 6, "enabled"))
	tls := nestedBlock(ingress, 6, "tls")
	if len(tls) == 0 {
		return component
	}

	component.TLSEnabled = parseBool(directValue(tls, 8, "enabled"))
	component.TLSMode = directValue(tls, 8, "mode")
	component.TLSSecretName = directValue(tls, 8, "secretName")

	if externalSecret := nestedBlock(tls, 8, "externalSecret"); len(externalSecret) > 0 {
		component.TLSCertKey = directValue(externalSecret, 10, "certKey")
		component.TLSKeyKey = directValue(externalSecret, 10, "keyKey")
	}
	if backup := nestedBlock(tls, 8, "backupToSsm"); len(backup) > 0 {
		component.BackupEnabled = parseBool(directValue(backup, 10, "enabled"))
		component.BackupPrefix = directValue(backup, 10, "prefix")
		component.BackupEnvironment = directValue(backup, 10, "environment")
		component.BackupDomain = directValue(backup, 10, "domain")
		component.BackupDomains = directList(backup, 10, "domains")
	}
	return component
}

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

func topBlock(lines []yamlLine, key string) []yamlLine {
	return nestedBlock(lines, 0, key)
}

func nestedBlock(lines []yamlLine, indent int, key string) []yamlLine {
	needle := key + ":"
	for index, line := range lines {
		if line.indent != indent || line.text != needle {
			continue
		}
		end := yamlBlockEnd(lines, index+1, indent)
		return lines[index+1 : end]
	}
	return nil
}

func childBlocks(lines []yamlLine, indent int) map[string][]yamlLine {
	blocks := map[string][]yamlLine{}
	for index := 0; index < len(lines); index++ {
		line := lines[index]
		if line.indent != indent || strings.HasPrefix(line.text, "- ") || !strings.HasSuffix(line.text, ":") {
			continue
		}
		name := strings.TrimSuffix(line.text, ":")
		end := yamlBlockEnd(lines, index+1, indent)
		blocks[name] = lines[index+1 : end]
		index = end - 1
	}
	return blocks
}

func yamlBlockEnd(lines []yamlLine, start, parentIndent int) int {
	end := start
	for end < len(lines) && lines[end].indent > parentIndent {
		end++
	}
	return end
}

func directValue(lines []yamlLine, indent int, key string) string {
	prefix := key + ":"
	for _, line := range lines {
		if line.indent == indent && strings.HasPrefix(line.text, prefix) {
			return cleanValue(strings.TrimSpace(strings.TrimPrefix(line.text, prefix)))
		}
	}
	return ""
}

func directList(lines []yamlLine, indent int, key string) []string {
	needle := key + ":"
	for index, line := range lines {
		if line.indent != indent || line.text != needle {
			continue
		}
		var values []string
		for nextIndex := index + 1; nextIndex < len(lines); nextIndex++ {
			next := lines[nextIndex]
			if next.indent < indent || next.indent == indent && !strings.HasPrefix(next.text, "- ") {
				break
			}
			if strings.HasPrefix(next.text, "- ") {
				values = append(values, cleanValue(strings.TrimSpace(strings.TrimPrefix(next.text, "- "))))
			}
		}
		return values
	}
	return nil
}

func cleanValue(value string) string {
	return strings.Trim(strings.TrimSpace(value), `"'`)
}

func parseBool(value string) bool {
	return strings.EqualFold(value, "true") || strings.EqualFold(value, "yes") || value == "1"
}
