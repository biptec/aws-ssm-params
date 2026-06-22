package format

import (
	"encoding/json"
	"fmt"
	"io"

	crerr "github.com/cockroachdb/errors"
	"gopkg.in/yaml.v3"
)

// ExportScalarLines writes one selected field per record, one value per line.
func ExportScalarLines(w io.Writer, records Records, field string) error {
	for i := range records {
		if _, err := fmt.Fprintln(w, records[i].fieldValue(field)); err != nil {
			return crerr.Wrap(err, "write scalar export")
		}
	}
	return nil
}

// ExportJSONScalar writes one selected field as JSON scalar values.
// Without keyField it writes an array; with keyField it writes an object keyed by the selected field.
func ExportJSONScalar(w io.Writer, records Records, field, keyField string) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if keyField == "" {
		values := make([]any, 0, len(records))
		for i := range records {
			values = append(values, records[i].fieldAny(field))
		}
		return crerr.Wrap(encoder.Encode(values), "encode scalar JSON export")
	}
	values := map[string]any{}
	for i := range records {
		key := records[i].fieldValue(keyField)
		if key == "" {
			continue
		}
		values[key] = records[i].fieldAny(field)
	}
	return crerr.Wrap(encoder.Encode(values), "encode keyed scalar JSON export")
}

// ExportYAMLScalar writes one selected field as YAML scalar values.
// Without keyField it writes a list; with keyField it writes a map keyed by the selected field.
func ExportYAMLScalar(w io.Writer, records Records, field, keyField string) error {
	var data any
	if keyField == "" {
		values := make([]any, 0, len(records))
		for i := range records {
			values = append(values, records[i].fieldAny(field))
		}
		data = values
	} else {
		values := map[string]any{}
		for i := range records {
			key := records[i].fieldValue(keyField)
			if key == "" {
				continue
			}
			values[key] = records[i].fieldAny(field)
		}
		data = values
	}
	encoder := yaml.NewEncoder(w)
	encoder.SetIndent(2)
	defer func() { _ = encoder.Close() }()
	return crerr.Wrap(encoder.Encode(data), "encode scalar YAML export")
}
