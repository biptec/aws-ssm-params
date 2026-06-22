package app

import (
	"fmt"

	"github.com/biptec/aws-ssm-params/internal/filter"
	outputfmt "github.com/biptec/aws-ssm-params/internal/format"
)

func fieldAllowed(fields []string, field string) bool {
	if len(fields) == 0 || field == "name" {
		return true
	}
	for _, candidate := range fields {
		if candidate == field {
			return true
		}
	}
	return false
}

func includeValuesForFields(fields []string) bool {
	if len(fields) == 0 {
		return true
	}
	for _, field := range fields {
		switch field {
		case "value", "len", "sha256", "version":
			return true
		}
	}
	return false
}

func includeValuesForFilterGroups(groups []filter.Group) bool {
	for _, group := range groups {
		if group.HasField(filter.FieldValue) {
			return true
		}
	}
	return false
}

func filterRecordsByGroups(records []outputfmt.Record, groups []filter.Group) []outputfmt.Record {
	if len(groups) == 0 {
		return records
	}
	out := make([]outputfmt.Record, 0, len(records))
	for i := range records {
		if filter.MatchAny(groups, filter.Record{
			Name:        records[i].Path,
			Region:      records[i].Region,
			Type:        records[i].Type,
			Tier:        records[i].Tier,
			DataType:    records[i].DataType,
			Description: records[i].Description,
			Policies:    records[i].Policies,
			Value:       records[i].Value,
		}) {
			out = append(out, records[i])
		}
	}
	return out
}

func requireFieldForCommand(cfg Config, field, command string) error {
	if !fieldAllowed(cfg.Fields, field) {
		return fmt.Errorf("%s requires field %q; remove --output-field restrictions or include %s", command, field, field)
	}
	return nil
}

func recordHasField(record outputfmt.Record, field string) bool {
	for _, candidate := range record.Fields {
		if candidate == field {
			return true
		}
	}
	return len(record.Fields) == 0
}

func effectiveFieldMappings(overrides []outputfmt.FieldMapping) []outputfmt.FieldMapping {
	base := outputfmt.DefaultFieldMappings()
	if len(overrides) == 0 {
		return base
	}
	byField := map[string]string{}
	for _, mapping := range base {
		byField[mapping.AWSName] = mapping.FileName
	}
	for _, mapping := range overrides {
		byField[mapping.AWSName] = mapping.FileName
	}
	for i := range base {
		base[i].FileName = byField[base[i].AWSName]
	}
	return base
}
