package textio

import (
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"
)

// DotEnv imports and exports dotenv documents using streams supplied by the factories.
// Field mappings and key fields do not apply to dotenv, but its methods accept them to satisfy the shared contracts.
type DotEnv struct {
	reader io.Reader
	writer io.Writer
}

// Export writes records as dotenv assignments with SSM metadata comments before each value.
// The path comment preserves the record name independently of the mechanically generated dotenv key. The name may be
// absolute or relative to a base path already applied by the export command. The optional type comment lets a later
// import recreate String/StringList/SecureString parameters without a separate flag.
func (format *DotEnv) Export(records Records, fieldMappings FieldMappings, keyField string) error {
	_, _ = fieldMappings, keyField

	if format.writer == nil {
		return errors.New("dotenv writer is not configured")
	}

	keys := make(map[string]string, len(records))
	for i := range records {
		key := format.key(records[i].Path)
		if key == "" {
			return fmt.Errorf("cannot create dotenv key from parameter name %q", records[i].Path)
		}

		if previousPath, exists := keys[key]; exists {
			return fmt.Errorf(
				"dotenv key %q is produced by both %q and %q",
				key,
				previousPath,
				records[i].Path,
			)
		}

		keys[key] = records[i].Path
	}

	for i := range records {
		record := &records[i]
		if i > 0 {
			if _, err := fmt.Fprintln(format.writer); err != nil {
				return errors.Wrap(err, "write dotenv separator")
			}
		}

		if _, err := fmt.Fprintf(format.writer, "# ssm: %s\n", record.Path); err != nil {
			return errors.Wrap(err, "write dotenv path comment")
		}

		if record.Type != "" {
			if _, err := fmt.Fprintf(format.writer, "# type: %s\n", record.Type); err != nil {
				return errors.Wrap(err, "write dotenv type comment")
			}
		}

		if _, err := fmt.Fprintf(format.writer, "%s=%s\n", format.key(record.Path), format.quote(record.Value)); err != nil {
			return errors.Wrap(err, "write dotenv value")
		}
	}

	return nil
}

// ExportScalar writes one selected record field per line.
func (format *DotEnv) ExportScalar(records Records, field, keyField string) error {
	_ = keyField

	if format.writer == nil {
		return errors.New("dotenv writer is not configured")
	}

	for i := range records {
		if _, err := fmt.Fprintln(format.writer, records[i].fieldValue(field)); err != nil {
			return errors.Wrap(err, "write scalar export")
		}
	}

	return nil
}

// Import parses dotenv input and resolves each variable back to an SSM name.
// A preceding '# ssm: name' comment wins; otherwise the variable name is preserved as a relative SSM name.
// A preceding '# type: String|StringList|SecureString' comment is preserved on the returned record.
func (format *DotEnv) Import(fieldMappings FieldMappings, keyField string) (Records, error) {
	_, _ = fieldMappings, keyField

	if format.reader == nil {
		return nil, errors.New("dotenv reader is not configured")
	}

	data, err := io.ReadAll(format.reader)
	if err != nil {
		return nil, errors.Wrap(err, "read dotenv input")
	}

	var (
		records     Records
		pendingPath string
		pendingType string
	)

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
			path := pendingPath
			pendingPath = ""
			pendingType = ""

			if path == "" {
				path = line
			}

			records = append(records, Record{
				Path:   path,
				Fields: Fields{FieldName},
			})

			continue
		}

		alias := strings.TrimSpace(key)

		value, err := format.parseValue(strings.TrimSpace(rawValue))
		if err != nil {
			return nil, errors.Wrapf(err, "invalid dotenv value for %s on line %d", alias, lineNumber+1)
		}

		path := pendingPath
		parameterType := pendingType
		pendingPath = ""
		pendingType = ""

		if path == "" {
			path = alias
		}

		fields := []string{FieldName, FieldValue}
		if strings.TrimSpace(parameterType) != "" {
			fields = append(fields, FieldType)
		}

		records = append(records, Record{Path: path, Fields: fields, Value: value, Type: parameterType})
	}

	return records, nil
}

// quote renders a value as a quoted dotenv literal.
// Always quoting avoids surprises with spaces, newlines, hashes, equals signs, and shell-sensitive characters.
func (*DotEnv) quote(value string) string {
	if value == "" {
		return "\"\""
	}

	return strconv.Quote(value)
}

// parseValue accepts quoted and simple dotenv values.
// Double-quoted values use strconv.Unquote for escape handling, single-quoted values are unwrapped literally,
// and unquoted values are trimmed.
func (*DotEnv) parseValue(value string) (string, error) {
	if value == "" {
		return "", nil
	}

	if strings.HasPrefix(value, "\"") {
		unquoted, err := strconv.Unquote(value)
		if err != nil {
			return "", errors.Wrap(err, "unquote dotenv value")
		}

		return unquoted, nil
	}

	if strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") && len(value) >= 2 {
		return value[1 : len(value)-1], nil
	}

	return strings.TrimSpace(value), nil
}

var dotenvKeyCleanup = regexp.MustCompile(`[^A-Za-z0-9]+`)

// key converts an SSM name mechanically into an uppercase dotenv identifier.
// It intentionally contains no secret-kind, path-shape, or inventory-specific special cases.
func (*DotEnv) key(path string) string {
	value := dotenvKeyCleanup.ReplaceAllString(strings.Trim(path, "/"), "_")

	value = strings.Trim(value, "_")
	if value == "" {
		return "VALUE"
	}

	return strings.ToUpper(value)
}
