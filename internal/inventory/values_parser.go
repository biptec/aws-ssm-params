package inventory

import "strings"

// yamlLine is the narrow representation needed by the values.yaml subset parser.
// It intentionally does not try to model arbitrary YAML.
type yamlLine struct {
	indent int
	text   string
}

type yamlLines []yamlLine

// parseValues converts the small subset of YAML used by app values files into valuesData.
func parseValues(text string) valuesData {
	lines := parseYAMLLines(text)
	values := valuesData{Components: map[string]componentData{}}

	if app := lines.topBlock("app"); len(app) > 0 {
		values.AppName = app.directValue(2, "name")
	}

	if global := lines.topBlock("global"); len(global) > 0 {
		values.GlobalEnvironment = global.directValue(2, "environment")
	}

	if secrets := lines.topBlock("secrets"); len(secrets) > 0 {
		values.SecretsEnabled = parseBool(secrets.directValue(2, "enabled"))
		values.SecretsProvider = secrets.directValue(2, "provider")
		values.SecretsPrefix = secrets.directValue(2, "prefix")
		values.SecretsEnvironment = secrets.directValue(2, "environment")
		values.SecretsOrg = secrets.directValue(2, "org")
		values.SecretsName = secrets.directValue(2, "name")
		values.SecretsKeys = secrets.directList(2, "keys")
	}

	for name, block := range lines.topBlock("components").childBlocks(2) {
		values.Components[name] = parseComponent(block)
	}

	return values
}

func parseComponent(block yamlLines) componentData {
	component := componentData{Enabled: parseBool(block.directValue(4, "enabled"))}

	ingress := block.nestedBlock(4, "ingress")
	if len(ingress) == 0 {
		return component
	}

	component.IngressEnabled = parseBool(ingress.directValue(6, "enabled"))

	tls := ingress.nestedBlock(6, "tls")
	if len(tls) == 0 {
		return component
	}

	component.TLSEnabled = parseBool(tls.directValue(8, "enabled"))
	component.TLSMode = tls.directValue(8, "mode")
	component.TLSSecretName = tls.directValue(8, "secretName")

	if externalSecret := tls.nestedBlock(8, "externalSecret"); len(externalSecret) > 0 {
		component.TLSCertKey = externalSecret.directValue(10, "certKey")
		component.TLSKeyKey = externalSecret.directValue(10, "keyKey")
	}

	if backup := tls.nestedBlock(8, "backupToSsm"); len(backup) > 0 {
		component.BackupEnabled = parseBool(backup.directValue(10, "enabled"))
		component.BackupPrefix = backup.directValue(10, "prefix")
		component.BackupEnvironment = backup.directValue(10, "environment")
		component.BackupDomain = backup.directValue(10, "domain")
		component.BackupDomains = backup.directList(10, "domains")
	}

	return component
}

func parseYAMLLines(text string) yamlLines {
	var lines yamlLines

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

func (lines yamlLines) topBlock(key string) yamlLines {
	return lines.nestedBlock(0, key)
}

func (lines yamlLines) nestedBlock(indent int, key string) yamlLines {
	needle := key + ":"
	for index, line := range lines {
		if line.indent != indent || line.text != needle {
			continue
		}

		end := lines.blockEnd(index+1, indent)

		return lines[index+1 : end]
	}

	return nil
}

func (lines yamlLines) childBlocks(indent int) map[string]yamlLines {
	blocks := map[string]yamlLines{}

	for index := 0; index < len(lines); index++ {
		line := lines[index]
		if line.indent != indent || strings.HasPrefix(line.text, "- ") || !strings.HasSuffix(line.text, ":") {
			continue
		}

		name := strings.TrimSuffix(line.text, ":")
		end := lines.blockEnd(index+1, indent)
		blocks[name] = lines[index+1 : end]
		index = end - 1
	}

	return blocks
}

func (lines yamlLines) blockEnd(start, parentIndent int) int {
	end := start
	for end < len(lines) && lines[end].indent > parentIndent {
		end++
	}

	return end
}

func (lines yamlLines) directValue(indent int, key string) string {
	prefix := key + ":"
	for _, line := range lines {
		if line.indent == indent && strings.HasPrefix(line.text, prefix) {
			return cleanValue(strings.TrimSpace(strings.TrimPrefix(line.text, prefix)))
		}
	}

	return ""
}

func (lines yamlLines) directList(indent int, key string) []string {
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
