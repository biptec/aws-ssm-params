// Package textio imports and exports SSM parameters in dotenv, JSON, and YAML formats.
package textio

import (
	"strconv"
)

// FieldMapping maps an AWS field name to a file field name.
type FieldMapping struct {
	AWSName  string
	FileName string
}

// Fields is an ordered set of canonical parameter field names.
type Fields []string

// Contains reports whether field is explicitly present.
func (fields Fields) Contains(field string) bool {
	for _, candidate := range fields {
		if candidate == field {
			return true
		}
	}
	return false
}

// Includes reports whether field is represented. An empty set represents all fields.
func (fields Fields) Includes(field string) bool {
	return len(fields) == 0 || fields.Contains(field)
}

// Allows applies output-field semantics: name is always available and an empty set allows every field.
func (fields Fields) Allows(field string) bool {
	return field == "name" || fields.Includes(field)
}

// With returns a copy containing each non-empty field once, preserving order.
func (fields Fields) With(additions ...string) Fields {
	out := append(Fields(nil), fields...)
	for _, field := range additions {
		if field == "" || out.Contains(field) {
			continue
		}
		out = append(out, field)
	}
	return out
}

// RequiresValues reports whether loading parameter values is required to provide these fields.
func (fields Fields) RequiresValues() bool {
	if len(fields) == 0 {
		return true
	}
	return fields.Contains("value") || fields.Contains("len") || fields.Contains("sha256") || fields.Contains("version")
}

// Record is the import/export representation of one SSM parameter.
// Path is the canonical or relative SSM name, Value is the parameter value, and Type optionally carries the AWS SSM
// parameter type when an import/export format preserves it.
type Record struct {
	Path        string
	Fields      Fields
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

// Records is an ordered collection of import/export records.
type Records []Record

func (r Record) includesField(field string) bool {
	return r.Fields.Includes(field)
}

// HasField reports whether the record contains field. Records without an explicit field set represent all fields.
func (r Record) HasField(field string) bool {
	return r.includesField(field)
}

func (r Record) fieldAny(field string) any {
	switch field {
	case "name":
		return r.Path
	case "region":
		return r.Region
	case "type":
		return r.Type
	case "tier":
		return r.Tier
	case "data-type":
		return r.DataType
	case "policies":
		return r.Policies
	case "description":
		return r.Description
	case "value":
		return r.Value
	case "date":
		return r.Date
	case "version":
		if r.Version == 0 {
			return ""
		}
		return r.Version
	case "len":
		if r.Len == 0 {
			return ""
		}
		return r.Len
	case "sha256":
		return r.SHA256
	case "user":
		return r.User
	default:
		return ""
	}
}

func (r Record) fieldValue(field string) string {
	value := r.fieldAny(field)
	switch typed := value.(type) {
	case string:
		return typed
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	default:
		return ""
	}
}

func (r *Record) setFieldValue(field, value string) {
	switch field {
	case "name":
		r.Path = value
	case "region":
		r.Region = value
	case "type":
		r.Type = value
	case "tier":
		r.Tier = value
	case "data-type":
		r.DataType = value
	case "policies":
		r.Policies = value
	case "description":
		r.Description = value
	case "value":
		r.Value = value
	case "date":
		r.Date = value
	case "version":
		r.Version, _ = strconv.ParseInt(value, 10, 64)
	case "len":
		r.Len, _ = strconv.Atoi(value)
	case "sha256":
		r.SHA256 = value
	case "user":
		r.User = value
	}
}
