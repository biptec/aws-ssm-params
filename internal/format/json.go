package format

import (
	"encoding/json"
	"io"
	"sort"
	"strings"

	crerr "github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/inventory"
)

// ExportJSON writes a stable JSON object keyed by SSM name.
// Every record uses the same object shape, even value-only exports, for example
// {"/path":{"value":"..."}} or {"/path":{"type":"SecureString","value":"..."}}.
func ExportJSON(w io.Writer, records Records) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	data := map[string]exportJSONRecord{}
	for i := range records {
		data[records[i].Path] = records[i].exportJSONRecord()
	}
	return crerr.Wrap(encoder.Encode(data), "encode JSON export")
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

// ImportJSON parses either legacy path-to-value JSON or typed path-to-object JSON and returns records sorted by path.
// Sorting makes imports deterministic and keeps progress output stable across runs.
func ImportJSON(r io.Reader) (Records, error) {
	data := map[string]json.RawMessage{}
	decoder := json.NewDecoder(r)
	if err := decoder.Decode(&data); err != nil {
		return nil, crerr.Wrap(err, "decode JSON import")
	}
	paths := make([]string, 0, len(data))
	for path := range data {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	records := make(Records, 0, len(paths))
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
		return Record{}, crerr.Wrapf(err, "invalid JSON record for %s", path)
	}
	fields, err := jsonRecordFields(raw)
	if err != nil {
		return Record{}, crerr.Wrapf(err, "invalid JSON record for %s", path)
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

func jsonRecordFields(raw json.RawMessage) (Fields, error) {
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

// ExportJSONMapped writes records as either an array of objects or an object keyed by a selected AWS field.
func ExportJSONMapped(w io.Writer, records Records, mappings FieldMappings, keyField string) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if len(mappings) == 0 && keyField == "" {
		return crerr.Wrap(encoder.Encode(recordsToObjects(records, defaultJSONMappings(), "")), "encode mapped JSON export")
	}
	if len(mappings) == 0 {
		mappings = defaultJSONMappings()
	}
	if keyField == "" {
		return crerr.Wrap(encoder.Encode(recordsToObjects(records, mappings, "")), "encode mapped JSON export")
	}
	data := map[string]map[string]any{}
	for i := range records {
		key := records[i].fieldValue(keyField)
		if key == "" {
			continue
		}
		data[key] = recordObject(records[i], mappings, keyField)
	}
	return crerr.Wrap(encoder.Encode(data), "encode keyed JSON export")
}

// ImportJSONMapped reads either JSON array records or a JSON object keyed by key-field.
func ImportJSONMapped(r io.Reader, mappings FieldMappings, keyField string) (Records, error) {
	if len(mappings) == 0 {
		mappings = defaultJSONMappings()
	}
	var raw json.RawMessage
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, crerr.Wrap(err, "decode JSON import")
	}
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, "[") {
		var objects []map[string]json.RawMessage
		if err := json.Unmarshal(raw, &objects); err != nil {
			return nil, crerr.Wrap(err, "decode JSON array import")
		}
		return recordsFromObjects(objects, mappings, ""), nil
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
		record := recordFromObject(keyed[key], mappings)
		if keyField != "" {
			record.setFieldValue(keyField, key)
		} else if record.Path == "" {
			record.Path = key
		}
		records = append(records, record)
	}
	return records, nil
}
