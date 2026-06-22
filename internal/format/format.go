// Package format imports and exports SSM parameters in dotenv, JSON, and YAML formats.
package format

import (
	"strconv"

	"github.com/biptec/aws-ssm-params/internal/inventory"
)

// FieldMapping maps an AWS field name to a file field name.
type FieldMapping struct {
	AWSName  string
	FileName string
}

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
		if r.Alias == "" && value != "" {
			r.Alias = AliasForPath(value, inventory.Item{})
		}
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
