// Package textio imports and exports SSM parameters in dotenv, JSON, and YAML formats.
package textio

// Canonical parameter field names shared by text formats and application commands.
// External aliases are normalized to these values at the application boundary.
const (
	FieldName        = "name"
	FieldRegion      = "region"
	FieldType        = "type"
	FieldTier        = "tier"
	FieldDataType    = "data-type"
	FieldPolicies    = "policies"
	FieldDescription = "description"
	FieldValue       = "value"
	FieldDate        = "date"
	FieldVersion     = "version"
	FieldLen         = "len"
	FieldSHA256      = "sha256"
	FieldUser        = "user"
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
	return field == FieldName || fields.Includes(field)
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

	return fields.Contains(FieldValue) ||
		fields.Contains(FieldLen) ||
		fields.Contains(FieldSHA256) ||
		fields.Contains(FieldVersion)
}
