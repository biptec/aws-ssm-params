// Package exportplan contains the shared export planning logic used by the CLI and TUI.
package exportplan

import (
	"strings"

	"github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/textio"
)

// DefaultFields is the ordered field set used by export when the user does not restrict output fields.
var DefaultFields = textio.Fields{
	textio.FieldName,
	textio.FieldRegion,
	textio.FieldType,
	textio.FieldTier,
	textio.FieldDataType,
	textio.FieldPolicies,
	textio.FieldDescription,
	textio.FieldValue,
	textio.FieldDate,
	textio.FieldVersion,
	textio.FieldLen,
	textio.FieldSHA256,
	textio.FieldUser,
}

// RecordFields returns the exact field set that must be loaded before writing an export.
func RecordFields(fields textio.Fields, scalarField, keyField string) textio.Fields {
	out := append(textio.Fields(nil), fields...)
	if len(out) == 0 {
		out = append(out, DefaultFields...)
	}

	return out.With(
		strings.TrimSpace(scalarField),
		strings.TrimSpace(keyField),
	)
}

// Mappings returns the output mappings used by structured exports.
func Mappings(fieldMaps textio.FieldMappings, recordFields textio.Fields) textio.FieldMappings {
	return fieldMaps.WithDefaults().ForFields(recordFields)
}

// Write writes records using the same scalar and structured export rules as the CLI export command.
func Write(writer textio.Writer, records textio.Records, fieldMaps textio.FieldMappings, recordFields textio.Fields, keyField, scalarField string) error {
	if scalarField != "" {
		return errors.Wrap(writer.ExportScalar(records, scalarField, keyField), "write scalar export")
	}

	return errors.Wrap(writer.Export(records, Mappings(fieldMaps, recordFields), keyField), "write export")
}
