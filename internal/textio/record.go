// Package textio imports and exports SSM parameters in dotenv, JSON, and YAML formats.
package textio

import (
	"strconv"
)

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

func (r *Record) includesField(field string) bool {
	return r.Fields.Includes(field)
}

// HasField reports whether the record contains field. Records without an explicit field set represent all fields.
func (r *Record) HasField(field string) bool {
	return r.includesField(field)
}

func (r *Record) fieldAny(field string) any {
	switch field {
	case FieldName:
		return r.Path
	case FieldRegion:
		return r.Region
	case FieldType:
		return r.Type
	case FieldTier:
		return r.Tier
	case FieldDataType:
		return r.DataType
	case FieldPolicies:
		return r.Policies
	case FieldDescription:
		return r.Description
	case FieldValue:
		return r.Value
	case FieldDate:
		return r.Date
	case FieldVersion:
		if r.Version == 0 {
			return ""
		}

		return r.Version
	case FieldLen:
		if r.Len == 0 {
			return ""
		}

		return r.Len
	case FieldSHA256:
		return r.SHA256
	case FieldUser:
		return r.User
	default:
		return ""
	}
}

func (r *Record) fieldValue(field string) string {
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
	case FieldName:
		r.Path = value
	case FieldRegion:
		r.Region = value
	case FieldType:
		r.Type = value
	case FieldTier:
		r.Tier = value
	case FieldDataType:
		r.DataType = value
	case FieldPolicies:
		r.Policies = value
	case FieldDescription:
		r.Description = value
	case FieldValue:
		r.Value = value
	case FieldDate:
		r.Date = value
	case FieldVersion:
		r.Version, _ = strconv.ParseInt(value, 10, 64)
	case FieldLen:
		r.Len, _ = strconv.Atoi(value)
	case FieldSHA256:
		r.SHA256 = value
	case FieldUser:
		r.User = value
	}
}
