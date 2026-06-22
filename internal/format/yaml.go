package format

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"

	crerr "github.com/cockroachdb/errors"
	"gopkg.in/yaml.v3"

	"github.com/biptec/aws-ssm-params/internal/inventory"
)

// ExportYAMLMapped writes records as either an array of objects or an object keyed by a selected AWS field.
func ExportYAMLMapped(w io.Writer, records Records, mappings FieldMappings, keyField string) error {
	if len(mappings) == 0 {
		mappings = defaultJSONMappings()
	}
	var data any
	if keyField == "" {
		data = recordsToObjects(records, mappings, "")
	} else {
		keyed := map[string]map[string]any{}
		for i := range records {
			key := records[i].fieldValue(keyField)
			if key == "" {
				continue
			}
			keyed[key] = recordObject(records[i], mappings, keyField)
		}
		data = keyed
	}
	encoder := yaml.NewEncoder(w)
	encoder.SetIndent(2)
	if err := encoder.Encode(data); err != nil {
		return crerr.Wrap(err, "encode mapped YAML export")
	}
	if err := encoder.Close(); err != nil {
		return crerr.Wrap(err, "close mapped YAML export")
	}
	return nil
}

// ImportYAMLMapped reads either YAML array records or a YAML object keyed by key-field.
func ImportYAMLMapped(r io.Reader, mappings FieldMappings, keyField string) (Records, error) {
	if len(mappings) == 0 {
		mappings = defaultJSONMappings()
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, crerr.Wrap(err, "read YAML import")
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, crerr.Wrap(err, "decode YAML import")
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
			return nil, crerr.Wrap(err, "decode YAML array import")
		}
		return recordsFromYAMLObjects(objects, mappings, keyField), nil
	case yaml.MappingNode:
		var keyed map[string]map[string]any
		if err := yaml.Unmarshal(data, &keyed); err != nil {
			return nil, crerr.Wrap(err, "decode keyed YAML import")
		}
		keys := make([]string, 0, len(keyed))
		for key := range keyed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		records := make(Records, 0, len(keys))
		for _, key := range keys {
			record := recordFromYAMLObject(keyed[key], mappings)
			if keyField != "" {
				record.setFieldValue(keyField, key)
			} else if record.Path == "" {
				record.Path = key
				if record.Alias == "" {
					record.Alias = AliasForPath(key, inventory.Item{})
				}
			}
			records = append(records, record)
		}
		return records, nil
	default:
		return nil, errors.New("YAML import must be an array or object")
	}
}

func recordsFromYAMLObjects(objects []map[string]any, mappings FieldMappings, keyField string) Records {
	records := make(Records, 0, len(objects))
	for _, object := range objects {
		record := recordFromYAMLObject(object, mappings)
		if keyField != "" && record.fieldValue(keyField) == "" {
			continue
		}
		records = append(records, record)
	}
	return records
}

func recordFromYAMLObject(object map[string]any, mappings FieldMappings) Record {
	record := Record{}
	fields := make(Fields, 0, len(mappings))
	for _, mapping := range mappings {
		value, ok := object[mapping.FileName]
		if !ok {
			continue
		}
		record.setFieldValue(mapping.AWSName, yamlScalarString(value))
		fields = append(fields, mapping.AWSName)
	}
	record.Fields = fields
	if record.Alias == "" && record.Path != "" {
		record.Alias = AliasForPath(record.Path, inventory.Item{})
	}
	return record
}

func yamlScalarString(value any) string {
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
