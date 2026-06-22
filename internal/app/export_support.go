package app

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	outputfmt "github.com/biptec/aws-ssm-params/internal/format"
	"github.com/biptec/aws-ssm-params/internal/natural"
	"github.com/biptec/aws-ssm-params/internal/ui"
)

type exportSortRule struct {
	field      string
	descending bool
}

func (rule exportSortRule) value(status ui.Status) string {
	switch rule.field {
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

type exportSortRules []exportSortRule

func (rules exportSortRules) requiresValues() bool {
	for _, rule := range rules {
		switch rule.field {
		case "value", "len", "sha256":
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

var allExportFields = outputfmt.Fields{"name", "region", "type", "tier", "data-type", "policies", "description", "value", "date", "version", "len", "sha256", "user"}

func exportFields(cfg Config) outputfmt.Fields {
	if len(cfg.Fields) == 0 {
		return append(outputfmt.Fields(nil), allExportFields...)
	}
	return append(outputfmt.Fields(nil), cfg.Fields...)
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

func validateKeyFieldOutputFields(keyField string, outputFields outputfmt.Fields) error {
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

func exportRecordFields(fields outputfmt.Fields, scalarField, keyField string) outputfmt.Fields {
	return fields.With(strings.TrimSpace(scalarField), strings.TrimSpace(keyField))
}

func exportRecordFromStatus(status ui.Status, fields outputfmt.Fields) outputfmt.Record {
	record := outputfmt.Record{Path: status.Item.Path, Alias: outputfmt.AliasForItem(status.Item), Fields: fields}
	if fields.Contains("region") {
		record.Region = status.Item.Region
	}
	if fields.Contains("type") {
		record.Type = status.Type
	}
	if fields.Contains("tier") {
		record.Tier = status.Tier
	}
	if fields.Contains("data-type") {
		record.DataType = status.DataType
	}
	if fields.Contains("policies") {
		record.Policies = status.Policies
	}
	if fields.Contains("description") {
		record.Description = status.Description
	}
	if fields.Contains("value") && status.Exists {
		record.Value = status.Value
	}
	if fields.Contains("date") {
		record.Date = status.Modified
	}
	if fields.Contains("version") {
		record.Version = status.Version
	}
	if fields.Contains("len") {
		record.Len = status.Length
	}
	if fields.Contains("sha256") {
		record.SHA256 = status.SHA256Prefix
	}
	if fields.Contains("user") {
		record.User = status.User
	}
	return record
}
