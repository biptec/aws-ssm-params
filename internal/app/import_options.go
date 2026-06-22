package app

import (
	"strings"

	crerr "github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/fileio"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/biptec/aws-ssm-params/internal/textio"
)

type importOptionsResolver struct {
	defaults ssm.PutParameterOptions
	cfg      Config
}

func importDefaultOptions(ctx *CLIContext, cfg Config) (ssm.PutParameterOptions, error) {
	tier, err := ssm.ParseParameterTier(ctx.String("default-tier"))
	if err != nil {
		return ssm.PutParameterOptions{}, crerr.Wrap(err, "parse default tier")
	}
	dataType, err := ssm.ParseParameterDataType(ctx.String("default-data-type"))
	if err != nil {
		return ssm.PutParameterOptions{}, crerr.Wrap(err, "parse default data type")
	}
	policies := ctx.String("default-policies")
	if policiesFile := strings.TrimSpace(ctx.String("default-policies-file")); policiesFile != "" {
		data, err := fileio.ReadFile(policiesFile)
		if err != nil {
			return ssm.PutParameterOptions{}, crerr.Wrapf(err, "read default policies file %s", policiesFile)
		}
		policies = string(data)
	}
	opts := ssm.PutParameterOptions{}
	if cfg.Fields.Allows("tier") {
		opts.Tier = tier
	}
	if cfg.Fields.Allows("data-type") {
		opts.DataType = dataType
	}
	if cfg.Fields.Allows("description") {
		opts.Description = ctx.String("default-description")
	}
	if cfg.Fields.Allows("policies") {
		opts.Policies = policies
	}
	return opts, nil
}

func (resolver importOptionsResolver) forRecord(record textio.Record, cloud ssm.Metadata, exists bool) (ssm.PutParameterOptions, error) {
	opts := resolver.defaults
	cfg := resolver.cfg
	if exists {
		if cfg.Fields.Allows("tier") && strings.TrimSpace(cloud.Tier) != "" {
			tier, err := ssm.ParseParameterTier(cloud.Tier)
			if err != nil {
				return ssm.PutParameterOptions{}, crerr.Wrap(err, "parse cloud tier")
			}
			opts.Tier = tier
		}
		if cfg.Fields.Allows("data-type") && strings.TrimSpace(cloud.DataType) != "" {
			dataType, err := ssm.ParseParameterDataType(cloud.DataType)
			if err != nil {
				return ssm.PutParameterOptions{}, crerr.Wrap(err, "parse cloud data type")
			}
			opts.DataType = dataType
		}
		if cfg.Fields.Allows("description") {
			opts.Description = cloud.Description
		}
		if cfg.Fields.Allows("policies") {
			opts.Policies = cloud.Policies
		}
	}
	if cfg.Fields.Allows("tier") && record.HasField("tier") && strings.TrimSpace(record.Tier) != "" {
		tier, err := ssm.ParseParameterTier(record.Tier)
		if err != nil {
			return ssm.PutParameterOptions{}, crerr.Wrap(err, "parse record tier")
		}
		opts.Tier = tier
	}
	if cfg.Fields.Allows("data-type") && record.HasField("data-type") && strings.TrimSpace(record.DataType) != "" {
		dataType, err := ssm.ParseParameterDataType(record.DataType)
		if err != nil {
			return ssm.PutParameterOptions{}, crerr.Wrap(err, "parse record data type")
		}
		opts.DataType = dataType
	}
	if cfg.Fields.Allows("description") && record.HasField("description") && strings.TrimSpace(record.Description) != "" {
		opts.Description = record.Description
	}
	if cfg.Fields.Allows("policies") && record.HasField("policies") {
		if strings.TrimSpace(record.Policies) == "" {
			opts.Policies = "[{}]"
			opts.PoliciesSet = true
		} else {
			opts.Policies = record.Policies
		}
	}
	return opts, nil
}
