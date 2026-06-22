package textio

import (
	"fmt"
	"io"
)

// FormatType identifies a supported textual import/export format.
type FormatType string

// Supported text formats.
const (
	FormatDotenv FormatType = "dotenv"
	FormatJSON   FormatType = "json"
	FormatYAML   FormatType = "yaml"
	FormatYML    FormatType = "yml"
)

// Reader decodes records from the input stream captured by NewReader.
// Field mappings describe file-field names, while keyField identifies a field encoded as an object key.
type Reader interface {
	Import(fieldMaps FieldMappings, keyField string) (Records, error)
}

// Writer encodes records to the output stream captured by NewWriter.
// ExportScalar writes one selected field; Export writes complete mapped records.
type Writer interface {
	ExportScalar(records Records, field, keyField string) error
	Export(records Records, fieldMaps FieldMappings, keyField string) error
}

// TextIO is the complete codec contract implemented by every supported format.
// Callers normally depend on Reader or Writer and obtain them from the corresponding factory.
type TextIO interface {
	Reader
	Writer
}

type unsupportedTextIO struct {
	formatType FormatType
}

var (
	_ TextIO = (*DotEnv)(nil)
	_ TextIO = (*JSON)(nil)
	_ TextIO = (*YAML)(nil)
	_ TextIO = unsupportedTextIO{}
)

// NewReader binds reader to the codec selected by formatType.
// Unsupported formats return a Reader whose operations report an unsupported-format error.
func NewReader(formatType FormatType, reader io.Reader) Reader {
	switch formatType {
	case FormatDotenv:
		return &DotEnv{reader: reader}
	case FormatJSON:
		return &JSON{reader: reader}
	case FormatYAML, FormatYML:
		return &YAML{reader: reader}
	default:
		return unsupportedTextIO{formatType: formatType}
	}
}

// NewWriter binds writer to the codec selected by formatType.
// Unsupported formats return a Writer whose operations report an unsupported-format error.
func NewWriter(formatType FormatType, writer io.Writer) Writer {
	switch formatType {
	case FormatDotenv:
		return &DotEnv{writer: writer}
	case FormatJSON:
		return &JSON{writer: writer}
	case FormatYAML, FormatYML:
		return &YAML{writer: writer}
	default:
		return unsupportedTextIO{formatType: formatType}
	}
}

func (unsupported unsupportedTextIO) Import(FieldMappings, string) (Records, error) {
	return nil, unsupported.err()
}

func (unsupported unsupportedTextIO) ExportScalar(Records, string, string) error {
	return unsupported.err()
}

func (unsupported unsupportedTextIO) Export(Records, FieldMappings, string) error {
	return unsupported.err()
}

func (unsupported unsupportedTextIO) err() error {
	return fmt.Errorf("unsupported format: %s", unsupported.formatType)
}
