package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/natural"
	"github.com/biptec/aws-ssm-params/internal/textio"
	"github.com/biptec/aws-ssm-params/internal/ui"
)

type exportSortRule struct {
	field      string
	descending bool
}

func (rule exportSortRule) value(status ui.Status) string {
	switch rule.field {
	case textio.FieldName:
		return status.Item.Path
	case textio.FieldRegion:
		return status.Item.Region
	case textio.FieldType:
		return status.Type
	case textio.FieldTier:
		return status.Tier
	case textio.FieldDataType:
		return status.DataType
	case textio.FieldPolicies:
		return status.Policies
	case textio.FieldDescription:
		return status.Description
	case textio.FieldValue:
		return status.Value
	case textio.FieldDate:
		return status.Modified
	case textio.FieldVersion:
		return fmt.Sprint(status.Version)
	case textio.FieldLen:
		return fmt.Sprint(status.Length)
	case textio.FieldSHA256:
		return status.SHA256Prefix
	case textio.FieldUser:
		return status.User
	default:
		return ""
	}
}

type exportSortRules []exportSortRule

func (rules exportSortRules) requiresValues() bool {
	for _, rule := range rules {
		switch rule.field {
		case textio.FieldValue, textio.FieldLen, textio.FieldSHA256:
			return true
		}
	}
	return false
}

func parseExportSortRules(values []string) exportSortRules {
	rules := make(exportSortRules, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		parts := strings.Split(value, ":")
		field, ok := normalizeExportSortField(parts[0])
		if !ok {
			continue
		}
		descending := false
		if len(parts) > 1 {
			switch strings.ToLower(strings.TrimSpace(parts[1])) {
			case "desc", "descending":
				descending = true
			}
		}
		rules = rules.with(field, descending)
	}
	return rules
}

func normalizeExportSortField(field string) (string, bool) {
	field = strings.ToLower(strings.TrimSpace(field))
	switch field {
	case textio.FieldName, "path":
		return textio.FieldName, true
	case textio.FieldRegion,
		textio.FieldType,
		textio.FieldTier,
		textio.FieldPolicies,
		textio.FieldDescription,
		textio.FieldValue,
		textio.FieldDate,
		textio.FieldVersion,
		textio.FieldLen,
		textio.FieldSHA256,
		textio.FieldUser:
		return field, true
	case textio.FieldDataType, "datatype", "data_type":
		return textio.FieldDataType, true
	default:
		return "", false
	}
}

func (rules exportSortRules) with(field string, descending bool) exportSortRules {
	out := make(exportSortRules, 0, len(rules)+1)
	for _, rule := range rules {
		if rule.field != field {
			out = append(out, rule)
		}
	}
	return append(out, exportSortRule{field: field, descending: descending})
}

func (rules exportSortRules) sort(statuses ui.Statuses) {
	if len(rules) == 0 {
		return
	}
	sort.SliceStable(statuses, func(i, j int) bool {
		left := statuses[i]
		right := statuses[j]
		for _, rule := range rules {
			cmp := natural.Compare(rule.value(left), rule.value(right))
			if cmp == 0 {
				continue
			}
			if rule.descending {
				return cmp > 0
			}
			return cmp < 0
		}
		if cmp := natural.Compare(left.Item.Region, right.Item.Region); cmp != 0 {
			return cmp < 0
		}
		return natural.Compare(left.Item.Path, right.Item.Path) < 0
	})
}

var allExportFields = textio.Fields{
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

func exportFields(cfg Config) textio.Fields {
	if len(cfg.Fields) == 0 {
		return append(textio.Fields(nil), allExportFields...)
	}
	return append(textio.Fields(nil), cfg.Fields...)
}

func scalarExportField(ctx *CLIContext, cfg Config) (string, error) {
	if !ctx.Bool("scalar") {
		return "", nil
	}
	rawFields := compactStrings(ctx.StringSlice("output-field"))
	if len(rawFields) != 1 || len(cfg.Fields) != 1 {
		return "", errors.New("--scalar requires exactly one --output-field")
	}
	return cfg.Fields[0], nil
}

func validateKeyFieldOutputFields(keyField string, outputFields textio.Fields) error {
	keyField = strings.TrimSpace(keyField)
	if keyField == "" {
		return nil
	}
	for _, field := range outputFields {
		if field == keyField {
			return fmt.Errorf("--key-field and --output-field cannot use the same field: %s", keyField)
		}
	}
	return nil
}

func exportRecordFields(fields textio.Fields, scalarField, keyField string) textio.Fields {
	return fields.With(strings.TrimSpace(scalarField), strings.TrimSpace(keyField))
}

func exportRecordFromStatus(status ui.Status, fields textio.Fields) textio.Record {
	record := textio.Record{Path: status.Item.Path, Fields: fields}
	if fields.Contains(textio.FieldRegion) {
		record.Region = status.Item.Region
	}
	if fields.Contains(textio.FieldType) {
		record.Type = status.Type
	}
	if fields.Contains(textio.FieldTier) {
		record.Tier = status.Tier
	}
	if fields.Contains(textio.FieldDataType) {
		record.DataType = status.DataType
	}
	if fields.Contains(textio.FieldPolicies) {
		record.Policies = status.Policies
	}
	if fields.Contains(textio.FieldDescription) {
		record.Description = status.Description
	}
	if fields.Contains(textio.FieldValue) && status.Exists {
		record.Value = status.Value
	}
	if fields.Contains(textio.FieldDate) {
		record.Date = status.Modified
	}
	if fields.Contains(textio.FieldVersion) {
		record.Version = status.Version
	}
	if fields.Contains(textio.FieldLen) {
		record.Len = status.Length
	}
	if fields.Contains(textio.FieldSHA256) {
		record.SHA256 = status.SHA256Prefix
	}
	if fields.Contains(textio.FieldUser) {
		record.User = status.User
	}
	return record
}
