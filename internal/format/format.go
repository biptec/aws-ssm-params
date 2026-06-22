// Package format imports and exports SSM parameters in dotenv, JSON, and YAML formats.
package format

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
