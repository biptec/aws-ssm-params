package format

import (
	"encoding/json"
	"strconv"

	"github.com/biptec/aws-ssm-params/internal/inventory"
)

// FieldMappings is an ordered set of AWS-to-file field mappings.
type FieldMappings []FieldMapping

func recordsToObjects(records Records, mappings FieldMappings, keyField string) []map[string]any {
	objects := make([]map[string]any, 0, len(records))
	for i := range records {
		objects = append(objects, recordObject(records[i], mappings, keyField))
	}
	return objects
}

func recordObject(record Record, mappings FieldMappings, keyField string) map[string]any {
	object := map[string]any{}
	for _, mapping := range mappings {
		if mapping.AWSName == keyField {
			continue
		}
		value := record.fieldAny(mapping.AWSName)
		if value == nil || value == "" {
			if mapping.AWSName != "value" || !record.includesField("value") {
				continue
			}
		}
		object[mapping.FileName] = value
	}
	return object
}

func recordsFromObjects(objects []map[string]json.RawMessage, mappings FieldMappings, keyField string) Records {
	records := make(Records, 0, len(objects))
	for _, object := range objects {
		record := recordFromObject(object, mappings)
		if keyField != "" && record.fieldValue(keyField) == "" {
			continue
		}
		records = append(records, record)
	}
	return records
}

func recordFromObject(object map[string]json.RawMessage, mappings FieldMappings) Record {
	record := Record{}
	fields := make(Fields, 0, len(mappings))
	for _, mapping := range mappings {
		raw, ok := object[mapping.FileName]
		if !ok {
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err == nil {
			record.setFieldValue(mapping.AWSName, value)
			fields = append(fields, mapping.AWSName)
			continue
		}
		var number int64
		if err := json.Unmarshal(raw, &number); err == nil {
			record.setFieldValue(mapping.AWSName, strconv.FormatInt(number, 10))
			fields = append(fields, mapping.AWSName)
		}
	}
	record.Fields = fields
	if record.Alias == "" && record.Path != "" {
		record.Alias = AliasForPath(record.Path, inventory.Item{})
	}
	return record
}

// DefaultFieldMappings returns the default AWS-to-file field mappings.
func DefaultFieldMappings() FieldMappings {
	return append(FieldMappings(nil), defaultJSONMappings()...)
}

// WithDefaults overlays the receiver on the default mappings.
func (mappings FieldMappings) WithDefaults() FieldMappings {
	defaults := DefaultFieldMappings()
	if len(mappings) == 0 {
		return defaults
	}
	overrides := make(map[string]string, len(mappings))
	for _, mapping := range mappings {
		overrides[mapping.AWSName] = mapping.FileName
	}
	for i := range defaults {
		if fileName, ok := overrides[defaults[i].AWSName]; ok {
			defaults[i].FileName = fileName
		}
	}
	return defaults
}

// Find returns the mapping for one AWS field.
func (mappings FieldMappings) Find(awsName string) (FieldMapping, bool) {
	for _, mapping := range mappings {
		if mapping.AWSName == awsName {
			return mapping, true
		}
	}
	return FieldMapping{}, false
}

// ForFields returns mappings in field order.
func (mappings FieldMappings) ForFields(fields Fields) FieldMappings {
	out := make(FieldMappings, 0, len(fields))
	for _, field := range fields {
		if mapping, ok := mappings.Find(field); ok {
			out = append(out, mapping)
		}
	}
	return out
}

func defaultJSONMappings() FieldMappings {
	return FieldMappings{
		{AWSName: "name", FileName: "name"},
		{AWSName: "region", FileName: "region"},
		{AWSName: "type", FileName: "type"},
		{AWSName: "tier", FileName: "tier"},
		{AWSName: "data-type", FileName: "dataType"},
		{AWSName: "policies", FileName: "policies"},
		{AWSName: "description", FileName: "description"},
		{AWSName: "value", FileName: "value"},
		{AWSName: "date", FileName: "date"},
		{AWSName: "version", FileName: "version"},
		{AWSName: "len", FileName: "len"},
		{AWSName: "sha256", FileName: "sha256"},
		{AWSName: "user", FileName: "user"},
	}
}
