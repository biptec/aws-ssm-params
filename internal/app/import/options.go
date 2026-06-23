package importer

import (
	"strings"

	"github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/biptec/aws-ssm-params/internal/textio"
)

// OptionsResolver combines command defaults, existing metadata, and imported record metadata.
type OptionsResolver struct {
	defaults ssm.PutParameterOptions
	fields   textio.Fields
}

func defaultOptionsForFields(options ssm.PutParameterOptions, fields textio.Fields) ssm.PutParameterOptions {
	if !fields.Allows(textio.FieldTier) {
		options.Tier = ""
	}
	if !fields.Allows(textio.FieldDataType) {
		options.DataType = ""
	}
	if !fields.Allows(textio.FieldDescription) {
		options.Description = ""
	}
	if !fields.Allows(textio.FieldPolicies) {
		options.Policies = ""
		options.PoliciesSet = false
	}
	return options
}

func (resolver OptionsResolver) forRecord(record textio.Record, cloud ssm.Metadata, exists bool) (ssm.PutParameterOptions, error) {
	opts := resolver.defaults
	fields := resolver.fields
	if exists {
		if fields.Allows(textio.FieldTier) && strings.TrimSpace(cloud.Tier) != "" {
			tier, err := ssm.ParseParameterTier(cloud.Tier)
			if err != nil {
				return ssm.PutParameterOptions{}, errors.Wrap(err, "parse cloud tier")
			}
			opts.Tier = tier
		}
		if fields.Allows(textio.FieldDataType) && strings.TrimSpace(cloud.DataType) != "" {
			dataType, err := ssm.ParseParameterDataType(cloud.DataType)
			if err != nil {
				return ssm.PutParameterOptions{}, errors.Wrap(err, "parse cloud data type")
			}
			opts.DataType = dataType
		}
		if fields.Allows(textio.FieldDescription) {
			opts.Description = cloud.Description
		}
		if fields.Allows(textio.FieldPolicies) {
			opts.Policies = cloud.Policies
		}
	}
	if fields.Allows(textio.FieldTier) && record.HasField(textio.FieldTier) && strings.TrimSpace(record.Tier) != "" {
		tier, err := ssm.ParseParameterTier(record.Tier)
		if err != nil {
			return ssm.PutParameterOptions{}, errors.Wrap(err, "parse record tier")
		}
		opts.Tier = tier
	}
	if fields.Allows(textio.FieldDataType) &&
		record.HasField(textio.FieldDataType) &&
		strings.TrimSpace(record.DataType) != "" {
		dataType, err := ssm.ParseParameterDataType(record.DataType)
		if err != nil {
			return ssm.PutParameterOptions{}, errors.Wrap(err, "parse record data type")
		}
		opts.DataType = dataType
	}
	if fields.Allows(textio.FieldDescription) &&
		record.HasField(textio.FieldDescription) &&
		strings.TrimSpace(record.Description) != "" {
		opts.Description = record.Description
	}
	if fields.Allows(textio.FieldPolicies) && record.HasField(textio.FieldPolicies) {
		if strings.TrimSpace(record.Policies) == "" {
			opts.Policies = "[{}]"
			opts.PoliciesSet = true
		} else {
			opts.Policies = record.Policies
		}
	}
	return opts, nil
}
