package importer

import (
	"context"
	"strings"

	"github.com/biptec/aws-ssm-params/internal/app"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/biptec/aws-ssm-params/internal/textio"
	"github.com/cockroachdb/errors"
)

type strictMetadataDescriber interface {
	DescribeManyStrict(context.Context, []string) (map[string]ssm.Metadata, map[string]error)
}

// MetadataResolver loads existing SSM metadata grouped by record region.
type MetadataResolver struct {
	client  ssm.Client
	records app.Records
	opts    *Options
	fields  textio.Fields
}

func recordKey(region, path string) string {
	return strings.TrimSpace(region) + "\x00" + strings.TrimSpace(path)
}

func metadataForPaths(ctx context.Context, client ssm.Client, paths []string) (metadataByPath map[string]ssm.Metadata, errorsByPath map[string]error) {
	if describer, ok := client.(strictMetadataDescriber); ok {
		return describer.DescribeManyStrict(ctx, paths)
	}

	metas := client.DescribeMany(ctx, paths)
	errs := map[string]error{}

	for _, path := range paths {
		if _, ok := metas[path]; !ok {
			errs[path] = ssm.ErrNotFound
		}
	}

	return metas, errs
}

func wrapParameterType(parameterType ssm.ParameterType, err error) (ssm.ParameterType, error) {
	if err != nil {
		return "", errors.Wrap(err, "parse parameter type")
	}

	return parameterType, nil
}

func recordRegion(record *textio.Record, opts *Options, fields textio.Fields) string {
	if fields.Allows(textio.FieldRegion) && record.HasField(textio.FieldRegion) && strings.TrimSpace(record.Region) != "" {
		return strings.TrimSpace(record.Region)
	}

	return opts.Region
}

func (resolver *MetadataResolver) resolve(ctx context.Context) (metadataByKey map[string]ssm.Metadata, errorsByKey map[string]error) {
	pathsByRegion := map[string][]string{}
	seen := map[string]bool{}

	for i := range resolver.records {
		record := &resolver.records[i]
		region := recordRegion(record, resolver.opts, resolver.fields)

		key := recordKey(region, record.Path)
		if seen[key] {
			continue
		}

		seen[key] = true

		pathsByRegion[region] = append(pathsByRegion[region], record.Path)
	}

	metadata := map[string]ssm.Metadata{}
	errs := map[string]error{}

	for region, paths := range pathsByRegion {
		metas, regionErrs := metadataForPaths(ctx, resolver.client.ForRegion(region), paths)
		for path := range metas {
			meta := metas[path]
			if meta.Region == "" {
				meta.Region = region
			}

			metadata[recordKey(region, path)] = meta
		}

		for path, err := range regionErrs {
			errs[recordKey(region, path)] = err
		}
	}

	return metadata, errs
}

func resolveType(defaultType ssm.ParameterType, existing *ssm.Metadata, exists bool, record *textio.Record, fields textio.Fields) (ssm.ParameterType, error) {
	recordType := ""
	if fields.Allows(textio.FieldType) && record.HasField(textio.FieldType) && strings.TrimSpace(record.Type) != "" {
		recordType = record.Type
	}

	existingType := ""
	if exists {
		existingType = existing.Type
	}

	if !fields.Allows(textio.FieldType) {
		defaultType = ""
	}

	for _, candidate := range []string{recordType, existingType, string(defaultType)} {
		if strings.TrimSpace(candidate) == "" {
			continue
		}

		return wrapParameterType(ssm.ParseParameterType(candidate))
	}

	return ssm.DefaultParameterType, nil
}
