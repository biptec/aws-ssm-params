// Package format imports and exports SSM parameters in dotenv and JSON formats.
package format

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/biptec/aws-ssm-params/internal/inventory"
)

// Record is the import/export representation of one SSM parameter.
// Path is the canonical SSM name, Alias is the human-friendly dotenv variable name, Value is the parameter value,
// and Type optionally carries the AWS SSM parameter type when an import/export format preserves it.
type Record struct {
	Path        string
	Alias       string
	Fields      []string
	Region      string
	Value       string
	Type        string
	Tier        string
	DataType    string
	Policies    string
	Description string
	Date        string
	Version     int64
	Len         int
	SHA256      string
	User        string
}

// ExportDotenv writes records as dotenv assignments with SSM metadata comments before each value.
// The path comment makes import lossless even when aliases are duplicated or later renamed; the optional type
// comment lets a future import recreate String/StringList/SecureString parameters without a separate flag.
func ExportDotenv(w io.Writer, records []Record) error {
	for i, record := range records {
		if i > 0 {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "# ssm: %s\n", record.Path); err != nil {
			return err
		}
		if record.Type != "" {
			if _, err := fmt.Fprintf(w, "# type: %s\n", record.Type); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(w, "%s=%s\n", record.Alias, quoteDotenv(record.Value)); err != nil {
			return err
		}
	}
	return nil
}

// ExportJSON writes a stable JSON object keyed by SSM name.
// Every record uses the same object shape, even value-only exports, for example
// {"/path":{"value":"..."}} or {"/path":{"type":"SecureString","value":"..."}}.
func ExportJSON(w io.Writer, records []Record) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	data := map[string]exportJSONRecord{}
	for _, record := range records {
		data[record.Path] = record.exportJSONRecord()
	}
	return encoder.Encode(data)
}

type exportJSONRecord struct {
	Region      string  `json:"region,omitempty"`
	Type        string  `json:"type,omitempty"`
	Tier        string  `json:"tier,omitempty"`
	DataType    string  `json:"dataType,omitempty"`
	Policies    string  `json:"policies,omitempty"`
	Description string  `json:"description,omitempty"`
	Value       *string `json:"value,omitempty"`
	Date        string  `json:"date,omitempty"`
	Version     *int64  `json:"version,omitempty"`
	Len         *int    `json:"len,omitempty"`
	SHA256      string  `json:"sha256,omitempty"`
	User        string  `json:"user,omitempty"`
}

type jsonRecord struct {
	Region      string `json:"region,omitempty"`
	Type        string `json:"type,omitempty"`
	Tier        string `json:"tier,omitempty"`
	DataType    string `json:"dataType,omitempty"`
	Policies    string `json:"policies,omitempty"`
	Description string `json:"description,omitempty"`
	Value       string `json:"value"`
	Date        string `json:"date,omitempty"`
	Version     int64  `json:"version,omitempty"`
	Len         int    `json:"len,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	User        string `json:"user,omitempty"`
}

func (r Record) exportJSONRecord() exportJSONRecord {
	out := exportJSONRecord{}
	if r.includesField("region") {
		out.Region = r.Region
	}
	if r.includesField("type") {
		out.Type = r.Type
	}
	if r.includesField("tier") {
		out.Tier = r.Tier
	}
	if r.includesField("data-type") {
		out.DataType = r.DataType
	}
	if r.includesField("policies") {
		out.Policies = r.Policies
	}
	if r.includesField("description") {
		out.Description = r.Description
	}
	if r.includesField("value") {
		value := r.Value
		out.Value = &value
	}
	if r.includesField("date") {
		out.Date = r.Date
	}
	if r.includesField("version") {
		version := r.Version
		out.Version = &version
	}
	if r.includesField("len") {
		length := r.Len
		out.Len = &length
	}
	if r.includesField("sha256") {
		out.SHA256 = r.SHA256
	}
	if r.includesField("user") {
		out.User = r.User
	}
	return out
}

func (r Record) includesField(field string) bool {
	if len(r.Fields) == 0 {
		return true
	}
	for _, candidate := range r.Fields {
		if candidate == field {
			return true
		}
	}
	return false
}

// ImportDotenv parses dotenv input and resolves each variable back to an SSM name.
// A preceding '# ssm: /path' comment wins; otherwise the variable name is matched against aliases derived from inventory.
// A preceding '# type: String|StringList|SecureString' comment is preserved on the returned record.
// Ambiguous aliases are rejected so the tool never writes a value to the wrong parameter silently.
func ImportDotenv(r io.Reader, items []inventory.Item) ([]Record, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
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
			return nil, fmt.Errorf("invalid dotenv value for %s on line %d: %w", alias, lineNumber+1, err)
		}

		path := pendingPath
		parameterType := pendingType
		pendingPath = ""
		pendingType = ""
		if path == "" {
			matches := aliases[alias]
			if len(matches) == 0 {
				return nil, fmt.Errorf("cannot resolve dotenv key %s to an SSM name", alias)
			}
			if len(matches) > 1 {
				return nil, fmt.Errorf("dotenv key %s is ambiguous: %s", alias, strings.Join(matches, ", "))
			}
			path = matches[0]
		}
		records = append(records, Record{Path: path, Alias: alias, Value: value, Type: parameterType})
	}
	return records, nil
}

// ImportJSON parses either legacy path-to-value JSON or typed path-to-object JSON and returns records sorted by path.
// Sorting makes imports deterministic and keeps progress output stable across runs.
func ImportJSON(r io.Reader) ([]Record, error) {
	data := map[string]json.RawMessage{}
	decoder := json.NewDecoder(r)
	if err := decoder.Decode(&data); err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(data))
	for path := range data {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	records := make([]Record, 0, len(paths))
	for _, path := range paths {
		record, err := parseJSONRecord(path, data[path])
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func parseJSONRecord(path string, raw json.RawMessage) (Record, error) {
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return Record{Path: path, Alias: AliasForPath(path, inventory.Item{}), Fields: []string{"name", "value"}, Value: value}, nil
	}

	var typed jsonRecord
	if err := json.Unmarshal(raw, &typed); err != nil {
		return Record{}, fmt.Errorf("invalid JSON record for %s: %w", path, err)
	}
	fields, err := jsonRecordFields(raw)
	if err != nil {
		return Record{}, fmt.Errorf("invalid JSON record for %s: %w", path, err)
	}
	return Record{
		Path:        path,
		Alias:       AliasForPath(path, inventory.Item{}),
		Fields:      fields,
		Region:      typed.Region,
		Value:       typed.Value,
		Type:        typed.Type,
		Tier:        typed.Tier,
		DataType:    typed.DataType,
		Policies:    typed.Policies,
		Description: typed.Description,
		Date:        typed.Date,
		Version:     typed.Version,
		Len:         typed.Len,
		SHA256:      typed.SHA256,
		User:        typed.User,
	}, nil
}

func jsonRecordFields(raw json.RawMessage) ([]string, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, err
	}
	fields := []string{"name"}
	for _, field := range []struct {
		jsonName string
		field    string
	}{
		{"region", "region"},
		{"type", "type"},
		{"tier", "tier"},
		{"dataType", "data-type"},
		{"data_type", "data-type"},
		{"data-type", "data-type"},
		{"policies", "policies"},
		{"description", "description"},
		{"value", "value"},
		{"date", "date"},
		{"version", "version"},
		{"len", "len"},
		{"length", "len"},
		{"sha256", "sha256"},
		{"user", "user"},
	} {
		if _, ok := object[field.jsonName]; ok {
			fields = appendUniqueField(fields, field.field)
		}
	}
	return fields, nil
}

func appendUniqueField(fields []string, field string) []string {
	for _, candidate := range fields {
		if candidate == field {
			return fields
		}
	}
	return append(fields, field)
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
		return strconv.Unquote(value)
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
