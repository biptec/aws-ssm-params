package app

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	secretfmt "github.com/biptec/aws-ssm-params/internal/format"
	"github.com/biptec/aws-ssm-params/internal/natural"
	"github.com/biptec/aws-ssm-params/internal/ui"
)

type exportSortRule struct {
	field      string
	descending bool
}

func includeValuesForSortColumns(values []string) bool {
	for _, rule := range parseExportSortRules(values) {
		switch rule.field {
		case "value", "len", "sha256":
			return true
		}
	}
	return false
}

func parseExportSortRules(values []string) []exportSortRule {
	rules := make([]exportSortRule, 0, len(values))
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
		rules = upsertExportSortRule(rules, field, descending)
	}
	return rules
}

func normalizeExportSortField(field string) (string, bool) {
	field = strings.ToLower(strings.TrimSpace(field))
	switch field {
	case "name", "path":
		return "name", true
	case "region", "type", "tier", "policies", "description", "value", "date", "version", "len", "sha256", "user":
		return field, true
	case "data-type", "datatype", "data_type":
		return "data-type", true
	default:
		return "", false
	}
}

func upsertExportSortRule(rules []exportSortRule, field string, descending bool) []exportSortRule {
	out := make([]exportSortRule, 0, len(rules)+1)
	for _, rule := range rules {
		if rule.field != field {
			out = append(out, rule)
		}
	}
	return append(out, exportSortRule{field: field, descending: descending})
}

func sortStatusesForExport(statuses []ui.Status, values []string) {
	rules := parseExportSortRules(values)
	if len(rules) == 0 {
		return
	}
	sort.SliceStable(statuses, func(i, j int) bool {
		left := statuses[i]
		right := statuses[j]
		for _, rule := range rules {
			cmp := natural.Compare(exportStatusSortValue(left, rule.field), exportStatusSortValue(right, rule.field))
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

func exportStatusSortValue(status ui.Status, field string) string {
	switch field {
	case "name":
		return status.Item.Path
	case "region":
		return status.Item.Region
	case "type":
		return status.Type
	case "tier":
		return status.Tier
	case "data-type":
		return status.DataType
	case "policies":
		return status.Policies
	case "description":
		return status.Description
	case "value":
		return status.Value
	case "date":
		return status.Modified
	case "version":
		return fmt.Sprint(status.Version)
	case "len":
		return fmt.Sprint(status.Length)
	case "sha256":
		return status.SHA256Prefix
	case "user":
		return status.User
	default:
		return ""
	}
}

var allExportFields = []string{"name", "region", "type", "tier", "data-type", "policies", "description", "value", "date", "version", "len", "sha256", "user"}

func exportFields(cfg Config) []string {
	if len(cfg.Fields) == 0 {
		return append([]string(nil), allExportFields...)
	}
	return append([]string(nil), cfg.Fields...)
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

func validateKeyFieldOutputFields(keyField string, outputFields []string) error {
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

func exportFieldMappings(fields []string, overrides []secretfmt.FieldMapping) []secretfmt.FieldMapping {
	effective := effectiveFieldMappings(overrides)
	out := make([]secretfmt.FieldMapping, 0, len(fields))
	for _, field := range fields {
		for _, mapping := range effective {
			if mapping.AWSName == field {
				out = append(out, mapping)
				break
			}
		}
	}
	return out
}

func exportRecordFields(fields []string, scalarField, keyField string) []string {
	out := append([]string(nil), fields...)
	for _, field := range []string{scalarField, keyField} {
		field = strings.TrimSpace(field)
		if field == "" || hasExportField(out, field) {
			continue
		}
		out = append(out, field)
	}
	return out
}

func hasExportField(fields []string, field string) bool {
	for _, candidate := range fields {
		if candidate == field {
			return true
		}
	}
	return false
}

func exportRecordFromStatus(status ui.Status, fields []string) secretfmt.Record {
	record := secretfmt.Record{Path: status.Item.Path, Alias: secretfmt.AliasForItem(status.Item), Fields: fields}
	if hasExportField(fields, "region") {
		record.Region = status.Item.Region
	}
	if hasExportField(fields, "type") {
		record.Type = status.Type
	}
	if hasExportField(fields, "tier") {
		record.Tier = status.Tier
	}
	if hasExportField(fields, "data-type") {
		record.DataType = status.DataType
	}
	if hasExportField(fields, "policies") {
		record.Policies = status.Policies
	}
	if hasExportField(fields, "description") {
		record.Description = status.Description
	}
	if hasExportField(fields, "value") && status.Exists {
		record.Value = status.Value
	}
	if hasExportField(fields, "date") {
		record.Date = status.Modified
	}
	if hasExportField(fields, "version") {
		record.Version = status.Version
	}
	if hasExportField(fields, "len") {
		record.Len = status.Length
	}
	if hasExportField(fields, "sha256") {
		record.SHA256 = status.SHA256Prefix
	}
	if hasExportField(fields, "user") {
		record.User = status.User
	}
	return record
}
