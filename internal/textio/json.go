package textio

import (
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strconv"
	"strings"

	crerr "github.com/cockroachdb/errors"
)

// JSON imports and exports JSON documents using streams supplied by the factories.
// It supports array records, keyed record objects, field mappings, and scalar output.
type JSON struct {
	reader io.Reader
	writer io.Writer
}

// Export writes an array when keyField is empty and a keyed object otherwise.
func (format *JSON) Export(records Records, mappings FieldMappings, keyField string) error {
	if format.writer == nil {
		return errors.New("JSON writer is not configured")
	}
	encoder := json.NewEncoder(format.writer)
	encoder.SetIndent("", "  ")
	if len(mappings) == 0 {
		mappings = defaultFieldMappings()
	}
	if keyField == "" {
		return crerr.Wrap(encoder.Encode(mappings.objects(records, "")), "encode mapped JSON export")
	}
	data := map[string]map[string]any{}
	for i := range records {
		key := records[i].fieldValue(keyField)
		if key == "" {
			continue
		}
		data[key] = mappings.object(records[i], keyField)
	}
	return crerr.Wrap(encoder.Encode(data), "encode keyed JSON export")
}

// Import accepts both arrays of objects and objects keyed by keyField.
func (format *JSON) Import(mappings FieldMappings, keyField string) (Records, error) {
	if format.reader == nil {
		return nil, errors.New("JSON reader is not configured")
	}
	if len(mappings) == 0 {
		mappings = defaultFieldMappings()
	}
	var raw json.RawMessage
	if err := json.NewDecoder(format.reader).Decode(&raw); err != nil {
		return nil, crerr.Wrap(err, "decode JSON import")
	}
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, "[") {
		var objects []map[string]json.RawMessage
		if err := json.Unmarshal(raw, &objects); err != nil {
			return nil, crerr.Wrap(err, "decode JSON array import")
		}
		return format.recordsFromObjects(objects, mappings, ""), nil
	}
	var keyed map[string]map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keyed); err != nil {
		return nil, crerr.Wrap(err, "decode keyed JSON import")
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
}

// recordsFromObjects converts decoded JSON objects while preserving only fields present in each object.
func (format *JSON) recordsFromObjects(objects []map[string]json.RawMessage, mappings FieldMappings, keyField string) Records {
	records := make(Records, 0, len(objects))
	for _, object := range objects {
		record := format.recordFromObject(object, mappings)
		if keyField != "" && record.fieldValue(keyField) == "" {
			continue
		}
		records = append(records, record)
	}
	return records
}

// recordFromObject applies file-to-AWS field mappings to one JSON object.
func (*JSON) recordFromObject(object map[string]json.RawMessage, mappings FieldMappings) Record {
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
	return record
}

// ExportScalar writes either a scalar array or a key-to-scalar object.
func (format *JSON) ExportScalar(records Records, field, keyField string) error {
	if format.writer == nil {
		return errors.New("JSON writer is not configured")
	}
	encoder := json.NewEncoder(format.writer)
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

// importLegacyRecords parses legacy path-to-value or path-to-object JSON.
func (format *JSON) importLegacyRecords() (Records, error) {
	data := map[string]json.RawMessage{}
	if err := json.NewDecoder(format.reader).Decode(&data); err != nil {
		return nil, crerr.Wrap(err, "decode JSON import")
	}
	paths := make([]string, 0, len(data))
	for path := range data {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	records := make(Records, 0, len(paths))
	for _, path := range paths {
		record, err := format.parseRecord(path, data[path])
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func (format *JSON) parseRecord(path string, raw json.RawMessage) (Record, error) {
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return Record{Path: path, Fields: Fields{"name", "value"}, Value: value}, nil
	}
	var typed jsonRecord
	if err := json.Unmarshal(raw, &typed); err != nil {
		return Record{}, crerr.Wrapf(err, "invalid JSON record for %s", path)
	}
	fields, err := format.recordFields(raw)
	if err != nil {
		return Record{}, crerr.Wrapf(err, "invalid JSON record for %s", path)
	}
	return Record{
		Path: path, Fields: fields, Region: typed.Region, Value: typed.Value, Type: typed.Type,
		Tier: typed.Tier, DataType: typed.DataType, Policies: typed.Policies, Description: typed.Description,
		Date: typed.Date, Version: typed.Version, Len: typed.Len, SHA256: typed.SHA256, User: typed.User,
	}, nil
}

func (*JSON) recordFields(raw json.RawMessage) (Fields, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, crerr.Wrap(err, "decode JSON record fields")
	}
	fields := Fields{"name"}
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
			fields = fields.With(field.field)
		}
	}
	return fields, nil
}

// exportLegacyRecords writes the legacy object keyed by SSM name.
func (format *JSON) exportLegacyRecords(records Records) error {
	encoder := json.NewEncoder(format.writer)
	encoder.SetIndent("", "  ")
	data := map[string]exportJSONRecord{}
	for i := range records {
		data[records[i].Path] = format.exportRecord(records[i])
	}
	return crerr.Wrap(encoder.Encode(data), "encode JSON export")
}

func (*JSON) exportRecord(record Record) exportJSONRecord {
	out := exportJSONRecord{}
	if record.includesField("region") {
		out.Region = record.Region
	}
	if record.includesField("type") {
		out.Type = record.Type
	}
	if record.includesField("tier") {
		out.Tier = record.Tier
	}
	if record.includesField("data-type") {
		out.DataType = record.DataType
	}
	if record.includesField("policies") {
		out.Policies = record.Policies
	}
	if record.includesField("description") {
		out.Description = record.Description
	}
	if record.includesField("value") {
		out.Value = &record.Value
	}
	if record.includesField("date") {
		out.Date = record.Date
	}
	if record.includesField("version") {
		out.Version = &record.Version
	}
	if record.includesField("len") {
		out.Len = &record.Len
	}
	if record.includesField("sha256") {
		out.SHA256 = record.SHA256
	}
	if record.includesField("user") {
		out.User = record.User
	}
	return out
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
