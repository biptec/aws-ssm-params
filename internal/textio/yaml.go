package textio

import (
	"fmt"
	"io"
	"sort"
	"strconv"

	"github.com/cockroachdb/errors"
	"gopkg.in/yaml.v3"
)

// YAML imports and exports YAML documents using streams supplied by the factories.
// It mirrors JSON record, mapping, key-field, and scalar semantics.
type YAML struct {
	reader io.Reader
	writer io.Writer
}

// Export writes a sequence when keyField is empty and a mapping otherwise.
func (format *YAML) Export(records Records, mappings FieldMappings, keyField string) error {
	if format.writer == nil {
		return errors.New("YAML writer is not configured")
	}

	if len(mappings) == 0 {
		mappings = defaultFieldMappings()
	}

	var data any
	if keyField == "" {
		data = mappings.objects(records, "")
	} else {
		keyed := map[string]map[string]any{}

		for i := range records {
			key := records[i].fieldValue(keyField)
			if key != "" {
				keyed[key] = mappings.object(&records[i], keyField)
			}
		}

		data = keyed
	}

	encoder := yaml.NewEncoder(format.writer)
	encoder.SetIndent(2)

	if err := encoder.Encode(data); err != nil {
		return errors.Wrap(err, "encode mapped YAML export")
	}

	return errors.Wrap(encoder.Close(), "close mapped YAML export")
}

// Import accepts a YAML sequence of records or a mapping keyed by keyField.
func (format *YAML) Import(mappings FieldMappings, keyField string) (Records, error) {
	if format.reader == nil {
		return nil, errors.New("YAML reader is not configured")
	}

	if len(mappings) == 0 {
		mappings = defaultFieldMappings()
	}

	data, err := io.ReadAll(format.reader)
	if err != nil {
		return nil, errors.Wrap(err, "read YAML import")
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, errors.Wrap(err, "decode YAML import")
	}

	if len(root.Content) == 0 {
		return nil, errors.New("empty YAML import")
	}

	switch root.Content[0].Kind {
	case yaml.DocumentNode, yaml.ScalarNode, yaml.AliasNode:
		return nil, errors.New("YAML import must be an array or object")
	case yaml.SequenceNode:
		var objects []map[string]any
		if err := yaml.Unmarshal(data, &objects); err != nil {
			return nil, errors.Wrap(err, "decode YAML array import")
		}

		return format.recordsFromObjects(objects, mappings, keyField), nil
	case yaml.MappingNode:
		var keyed map[string]map[string]any
		if err := yaml.Unmarshal(data, &keyed); err != nil {
			return nil, errors.Wrap(err, "decode keyed YAML import")
		}

		keys := make([]string, 0, len(keyed))
		for key := range keyed {
			keys = append(keys, key)
		}

		sort.Strings(keys)

		records := make(Records, 0, len(keys))
		for _, key := range keys {
			record := format.recordFromObject(keyed[key], mappings)
			if keyField != "" {
				record.setFieldValue(keyField, key)
			} else if record.Path == "" {
				record.Path = key
			}

			records = append(records, record)
		}

		return records, nil
	default:
		return nil, errors.New("YAML import must be an array or object")
	}
}

// recordsFromObjects converts sequence elements while enforcing key-field presence when requested.
func (format *YAML) recordsFromObjects(objects []map[string]any, mappings FieldMappings, keyField string) Records {
	records := make(Records, 0, len(objects))
	for _, object := range objects {
		record := format.recordFromObject(object, mappings)
		if keyField == "" || record.fieldValue(keyField) != "" {
			records = append(records, record)
		}
	}

	return records
}

// recordFromObject applies file-to-AWS field mappings to one YAML object.
func (format *YAML) recordFromObject(object map[string]any, mappings FieldMappings) Record {
	record := Record{}

	fields := make(Fields, 0, len(mappings))
	for _, mapping := range mappings {
		value, ok := object[mapping.FileName]
		if !ok {
			continue
		}

		record.setFieldValue(mapping.AWSName, format.scalarString(value))
		fields = append(fields, mapping.AWSName)
	}

	record.Fields = fields

	return record
}

// scalarString normalizes YAML scalar types into the string representation used by Record.
func (*YAML) scalarString(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	default:
		return fmt.Sprint(typed)
	}
}

// ExportScalar writes either a scalar sequence or a key-to-scalar mapping.
func (format *YAML) ExportScalar(records Records, field, keyField string) error {
	if format.writer == nil {
		return errors.New("YAML writer is not configured")
	}

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
			if key != "" {
				values[key] = records[i].fieldAny(field)
			}
		}

		data = values
	}

	encoder := yaml.NewEncoder(format.writer)

	encoder.SetIndent(2)
	defer func() { _ = encoder.Close() }()

	return errors.Wrap(encoder.Encode(data), "encode scalar YAML export")
}
