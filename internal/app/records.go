package app

import (
	"sort"
	"strings"

	"github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/filter"
	"github.com/biptec/aws-ssm-params/internal/textio"
)

// Records is the application-level collection of textual parameter records.
// It centralizes name resolution, region expansion, filtering, and identity
// normalization shared by commands that consume exported parameter data.
type Records []textio.Record

// MapNamesToAWS returns a copy whose names are mapped from file paths to AWS
// paths. Records that do not match any mapping are preserved as-is.
func (records Records) MapNamesToAWS(mappings PathMappings) (Records, error) {
	mapped := make(Records, 0, len(records))
	for idx := range records {
		record := records[idx]

		record.Path = strings.TrimSpace(record.Path)
		if record.Path == "" {
			return nil, errors.New("parameter name is required")
		}

		record.Path = mappings.ToAWS(record.Path)
		mapped = append(mapped, record)
	}

	return mapped, nil
}

// ExpandRegions returns one record per concrete region. A region explicitly
// carried by a record wins; records without one are copied into every supplied
// default region.
func (records Records) ExpandRegions(regions []string) (Records, error) {
	defaults := uniqueNonEmptyStrings(regions)
	expanded := make(Records, 0, len(records))

	for idx := range records {
		record := records[idx]
		record.Region = strings.TrimSpace(record.Region)

		if record.Region != "" {
			expanded = append(expanded, record)
			continue
		}

		if len(defaults) == 0 {
			return nil, errors.Wrapf(
				errors.New("AWS region is required"),
				"resolve region for parameter %s",
				record.Path,
			)
		}

		for _, region := range defaults {
			regionalRecord := record
			regionalRecord.Region = region
			regionalRecord.Fields = regionalRecord.Fields.With(textio.FieldRegion)
			expanded = append(expanded, regionalRecord)
		}
	}

	return expanded, nil
}

// Filter returns records matching the supplied global filter groups.
func (records Records) Filter(groups filter.Groups) Records {
	if len(groups) == 0 {
		return records
	}

	out := make(Records, 0, len(records))
	for idx := range records {
		record := &records[idx]

		filterRecord := filter.Record{
			Name:        record.Path,
			Region:      record.Region,
			Type:        record.Type,
			Tier:        record.Tier,
			DataType:    record.DataType,
			Description: record.Description,
			Policies:    record.Policies,
			Value:       record.Value,
		}
		if groups.Match(&filterRecord) {
			out = append(out, *record)
		}
	}

	return out
}

// UniqueByIdentity trims names and regions and keeps the first record for each
// region/name pair.
func (records Records) UniqueByIdentity() Records {
	seen := make(map[string]bool, len(records))
	out := make(Records, 0, len(records))

	for idx := range records {
		record := records[idx]
		record.Path = strings.TrimSpace(record.Path)
		record.Region = strings.TrimSpace(record.Region)

		if record.Path == "" {
			continue
		}

		key := record.Region + "\x00" + record.Path
		if seen[key] {
			continue
		}

		seen[key] = true

		out = append(out, record)
	}

	return out
}

// SortByIdentity orders records by region and then parameter name.
func (records Records) SortByIdentity() {
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].Region != records[j].Region {
			return records[i].Region < records[j].Region
		}

		return records[i].Path < records[j].Path
	})
}

// HasMissingRegion reports whether any record needs a command-level region.
func (records Records) HasMissingRegion() bool {
	for idx := range records {
		if strings.TrimSpace(records[idx].Region) == "" {
			return true
		}
	}

	return false
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))

	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}

		seen[value] = true
		out = append(out, value)
	}

	return out
}
