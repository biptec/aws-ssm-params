package importer

import (
	"context"
	"strings"

	"github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/app"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	ssmclient "github.com/biptec/aws-ssm-params/internal/ssm/client"
	"github.com/biptec/aws-ssm-params/internal/textio"
)

// recordResolver owns every decision that turns one textual record into an
// SSM write: target region, existing metadata, parameter type, and put options.
type recordResolver struct {
	fields         textio.Fields
	defaultRegion  string
	defaultType    ssm.ParameterType
	defaultOptions ssm.PutParameterOptions
	metadata       map[string]ssm.Metadata
	metadataErrors map[string]error
}

func newRecordResolver(ctx context.Context, client ssmclient.Client, records app.Records, opts *Options) *recordResolver {
	resolver := &recordResolver{
		fields:         opts.Fields,
		defaultRegion:  opts.Region,
		defaultType:    opts.DefaultType,
		defaultOptions: opts.DefaultOptions,
	}
	resolver.scopeDefaultOptions()
	resolver.loadMetadata(ctx, client, records)

	return resolver
}

func (resolver *recordResolver) resolveExisting(record *textio.Record) (region string, existing ssm.Metadata, exists bool, err error) {
	region = resolver.region(record)
	key := resolver.key(region, record.Path)
	existing, exists = resolver.metadata[key]

	if metadataErr, ok := resolver.metadataErrors[key]; ok {
		if errors.Is(metadataErr, ssm.ErrNotFound) {
			return region, existing, false, nil
		}

		return region, existing, false, metadataErr
	}

	return region, existing, exists, nil
}

func (resolver *recordResolver) region(record *textio.Record) string {
	if resolver.fields.Allows(textio.FieldRegion) && record.HasField(textio.FieldRegion) && strings.TrimSpace(record.Region) != "" {
		return strings.TrimSpace(record.Region)
	}

	return resolver.defaultRegion
}

func (resolver *recordResolver) parameterType(existing *ssm.Metadata, exists bool, record *textio.Record) (ssm.ParameterType, error) {
	recordType := ""
	if resolver.fields.Allows(textio.FieldType) && record.HasField(textio.FieldType) && strings.TrimSpace(record.Type) != "" {
		recordType = record.Type
	}

	existingType := ""
	if exists {
		existingType = existing.Type
	}

	defaultType := resolver.defaultType
	if !resolver.fields.Allows(textio.FieldType) {
		defaultType = ""
	}

	for _, candidate := range []string{recordType, existingType, string(defaultType)} {
		if strings.TrimSpace(candidate) == "" {
			continue
		}

		parameterType, err := ssm.ParseParameterType(candidate)
		if err != nil {
			return "", errors.Wrap(err, "parse parameter type")
		}

		return parameterType, nil
	}

	return ssm.DefaultParameterType, nil
}

func (resolver *recordResolver) putOptions(record *textio.Record, cloud *ssm.Metadata, exists bool) (ssm.PutParameterOptions, error) {
	options := resolver.defaultOptions

	if exists {
		if resolver.fields.Allows(textio.FieldTier) && strings.TrimSpace(cloud.Tier) != "" {
			tier, err := ssm.ParseParameterTier(cloud.Tier)
			if err != nil {
				return ssm.PutParameterOptions{}, errors.Wrap(err, "parse cloud tier")
			}

			options.Tier = tier
		}

		if resolver.fields.Allows(textio.FieldDataType) && strings.TrimSpace(cloud.DataType) != "" {
			dataType, err := ssm.ParseParameterDataType(cloud.DataType)
			if err != nil {
				return ssm.PutParameterOptions{}, errors.Wrap(err, "parse cloud data type")
			}

			options.DataType = dataType
		}

		if resolver.fields.Allows(textio.FieldDescription) {
			options.Description = cloud.Description
		}

		if resolver.fields.Allows(textio.FieldPolicies) {
			options.Policies = cloud.Policies
		}
	}

	if resolver.fields.Allows(textio.FieldTier) && record.HasField(textio.FieldTier) && strings.TrimSpace(record.Tier) != "" {
		tier, err := ssm.ParseParameterTier(record.Tier)
		if err != nil {
			return ssm.PutParameterOptions{}, errors.Wrap(err, "parse record tier")
		}

		options.Tier = tier
	}

	if resolver.fields.Allows(textio.FieldDataType) && record.HasField(textio.FieldDataType) && strings.TrimSpace(record.DataType) != "" {
		dataType, err := ssm.ParseParameterDataType(record.DataType)
		if err != nil {
			return ssm.PutParameterOptions{}, errors.Wrap(err, "parse record data type")
		}

		options.DataType = dataType
	}

	if resolver.fields.Allows(textio.FieldDescription) && record.HasField(textio.FieldDescription) && strings.TrimSpace(record.Description) != "" {
		options.Description = record.Description
	}

	if resolver.fields.Allows(textio.FieldPolicies) && record.HasField(textio.FieldPolicies) {
		if strings.TrimSpace(record.Policies) == "" {
			options.Policies = "[{}]"
			options.PoliciesSet = true
		} else {
			options.Policies = record.Policies
		}
	}

	return options, nil
}

func (resolver *recordResolver) scopeDefaultOptions() {
	if !resolver.fields.Allows(textio.FieldTier) {
		resolver.defaultOptions.Tier = ""
	}

	if !resolver.fields.Allows(textio.FieldDataType) {
		resolver.defaultOptions.DataType = ""
	}

	if !resolver.fields.Allows(textio.FieldDescription) {
		resolver.defaultOptions.Description = ""
	}

	if !resolver.fields.Allows(textio.FieldPolicies) {
		resolver.defaultOptions.Policies = ""
		resolver.defaultOptions.PoliciesSet = false
	}
}

func (resolver *recordResolver) loadMetadata(ctx context.Context, client ssmclient.Client, records app.Records) {
	pathsByRegion := map[string][]string{}
	seen := map[string]bool{}

	for idx := range records {
		record := &records[idx]
		region := resolver.region(record)

		key := resolver.key(region, record.Path)
		if seen[key] {
			continue
		}

		seen[key] = true

		pathsByRegion[region] = append(pathsByRegion[region], record.Path)
	}

	resolver.metadata = map[string]ssm.Metadata{}
	resolver.metadataErrors = map[string]error{}

	for region, paths := range pathsByRegion {
		metadata, metadataErrors := resolver.loadRegionMetadata(
			ctx,
			client.ForRegion(region),
			paths,
		)

		for path := range metadata {
			item := metadata[path]
			if item.Region == "" {
				item.Region = region
			}

			resolver.metadata[resolver.key(region, path)] = item
		}

		for path, metadataErr := range metadataErrors {
			resolver.metadataErrors[resolver.key(region, path)] = metadataErr
		}
	}
}

func (resolver *recordResolver) loadRegionMetadata(ctx context.Context, client ssmclient.Client, paths []string) (metadataByPath map[string]ssm.Metadata, errorsByPath map[string]error) {
	return client.DescribeManyStrict(ctx, paths)
}

func (*recordResolver) key(region, path string) string {
	return strings.TrimSpace(region) + "\x00" + strings.TrimSpace(path)
}
