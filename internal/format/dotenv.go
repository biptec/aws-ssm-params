package format

import (
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	crerr "github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/inventory"
)

// ExportDotenv writes records as dotenv assignments with SSM metadata comments before each value.
// The path comment makes import lossless even when aliases are duplicated or later renamed; the optional type
// comment lets a future import recreate String/StringList/SecureString parameters without a separate flag.
func ExportDotenv(w io.Writer, records []Record) error {
	for i := range records {
		record := &records[i]
		if i > 0 {
			if _, err := fmt.Fprintln(w); err != nil {
				return crerr.Wrap(err, "write dotenv separator")
			}
		}
		if _, err := fmt.Fprintf(w, "# ssm: %s\n", record.Path); err != nil {
			return crerr.Wrap(err, "write dotenv path comment")
		}
		if record.Type != "" {
			if _, err := fmt.Fprintf(w, "# type: %s\n", record.Type); err != nil {
				return crerr.Wrap(err, "write dotenv type comment")
			}
		}
		if _, err := fmt.Fprintf(w, "%s=%s\n", record.Alias, quoteDotenv(record.Value)); err != nil {
			return crerr.Wrap(err, "write dotenv value")
		}
	}
	return nil
}

// ImportDotenv parses dotenv input and resolves each variable back to an SSM name.
// A preceding '# ssm: /path' comment wins; otherwise the variable name is matched against aliases derived from inventory.
// A preceding '# type: String|StringList|SecureString' comment is preserved on the returned record.
// Ambiguous aliases are rejected so the tool never writes a value to the wrong parameter silently.
func ImportDotenv(r io.Reader, items []inventory.Item) ([]Record, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, crerr.Wrap(err, "read dotenv input")
	}

	aliases := AliasMap(items)
	var records []Record
	var pendingPath string
	var pendingType string
	for lineNumber, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			if path, ok := strings.CutPrefix(line, "# ssm:"); ok {
				pendingPath = strings.TrimSpace(path)
			}
			if parameterType, ok := strings.CutPrefix(line, "# type:"); ok {
				pendingType = strings.TrimSpace(parameterType)
			}
			continue
		}

		key, rawValue, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("invalid dotenv line %d", lineNumber+1)
		}
		alias := strings.TrimSpace(key)
		value, err := parseDotenvValue(strings.TrimSpace(rawValue))
		if err != nil {
			return nil, crerr.Wrapf(err, "invalid dotenv value for %s on line %d", alias, lineNumber+1)
		}

		path := pendingPath
		parameterType := pendingType
		pendingPath = ""
		pendingType = ""
		if path == "" {
			matches := aliases[alias]
			if len(matches) > 1 {
				return nil, fmt.Errorf("dotenv key %s is ambiguous: %s", alias, strings.Join(matches, ", "))
			}
			if len(matches) == 1 {
				path = matches[0]
			} else {
				path = alias
			}
		}
		fields := []string{"name", "value"}
		if strings.TrimSpace(parameterType) != "" {
			fields = append(fields, "type")
		}
		records = append(records, Record{Path: path, Alias: alias, Fields: fields, Value: value, Type: parameterType})
	}
	return records, nil
}

// AliasForItem derives a dotenv-safe alias from an inventory item and its kind metadata.
func AliasForItem(item inventory.Item) string {
	return AliasForPath(item.Path, item)
}

// AliasForPath converts an SSM name into a readable environment variable name.
// Special cases keep common secret types predictable: app secrets use the final segment, GHCR/Flux tokens use fixed names,
// and TLS material includes the domain plus CRT/KEY suffix so certificate pairs stay easy to identify.
func AliasForPath(path string, item inventory.Item) string {
	if strings.Contains(item.Kind, "app-secret") {
		return normalizeAlias(lastSegment(path))
	}
	if strings.Contains(item.Kind, "ghcr-token") || strings.HasSuffix(path, "/ghcr/token") {
		return "GHCR_TOKEN"
	}
	if strings.Contains(item.Kind, "flux-token") || path == "/flux/github/token" {
		return "FLUX_GITHUB_TOKEN"
	}
	if strings.Contains(item.Kind, "tls.crt") || strings.HasSuffix(path, "/tls.crt") {
		return "TLS_" + normalizeAlias(tlsDomain(path)) + "_CRT"
	}
	if strings.Contains(item.Kind, "tls.key") || strings.HasSuffix(path, "/tls.key") {
		return "TLS_" + normalizeAlias(tlsDomain(path)) + "_KEY"
	}
	return normalizeAlias(strings.Trim(path, "/"))
}

// AliasMap groups inventory paths by generated dotenv alias so imports can detect ambiguous variable names.
func AliasMap(items []inventory.Item) map[string][]string {
	out := map[string][]string{}
	for _, item := range items {
		alias := AliasForItem(item)
		out[alias] = append(out[alias], item.Path)
	}
	return out
}

// quoteDotenv renders a value as a quoted dotenv literal.
// Always quoting avoids surprises with spaces, newlines, hashes, equals signs, and shell-sensitive characters.
func quoteDotenv(value string) string {
	if value == "" {
		return "\"\""
	}
	return strconv.Quote(value)
}

// parseDotenvValue accepts quoted and simple dotenv values.
// Double-quoted values use strconv.Unquote for escape handling, single-quoted values are unwrapped literally,
// and unquoted values are trimmed.
func parseDotenvValue(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if strings.HasPrefix(value, "\"") {
		unquoted, err := strconv.Unquote(value)
		if err != nil {
			return "", crerr.Wrap(err, "unquote dotenv value")
		}
		return unquoted, nil
	}
	if strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") && len(value) >= 2 {
		return value[1 : len(value)-1], nil
	}
	return strings.TrimSpace(value), nil
}

// lastSegment returns the final slash-separated segment of an SSM name.
func lastSegment(path string) string {
	trimmed := strings.Trim(path, "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) == 0 {
		return path
	}
	return parts[len(parts)-1]
}

// tlsDomain extracts the domain portion from conventional /.../tls/<domain>/tls.crt|tls.key paths.
func tlsDomain(path string) string {
	trimmed := strings.Trim(path, "/")
	parts := strings.Split(trimmed, "/")
	for i := 0; i < len(parts)-2; i++ {
		if parts[i] == "tls" {
			return parts[i+1]
		}
	}
	return lastSegment(path)
}

var aliasCleanup = regexp.MustCompile(`[^A-Za-z0-9]+`)

// normalizeAlias converts arbitrary path text into an uppercase dotenv-compatible identifier.
// Non-alphanumeric runs collapse to underscores and empty aliases fall back to VALUE.
func normalizeAlias(value string) string {
	value = aliasCleanup.ReplaceAllString(value, "_")
	value = strings.Trim(value, "_")
	if value == "" {
		return "VALUE"
	}
	return strings.ToUpper(value)
}
