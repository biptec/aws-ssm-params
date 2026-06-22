package app

import (
	"strings"

	crerr "github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/fileio"
	secretfmt "github.com/biptec/aws-ssm-params/internal/format"
	"github.com/biptec/aws-ssm-params/internal/ssm"
)

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
	if fieldAllowed(cfg.Fields, "tier") {
		opts.Tier = tier
	}
	if fieldAllowed(cfg.Fields, "data-type") {
		opts.DataType = dataType
	}
	if fieldAllowed(cfg.Fields, "description") {
		opts.Description = ctx.String("default-description")
	}
	if fieldAllowed(cfg.Fields, "policies") {
		opts.Policies = policies
	}
	return opts, nil
}

func importOptionsForRecord(record secretfmt.Record, cloud ssm.Metadata, exists bool, defaults ssm.PutParameterOptions, cfg Config) (ssm.PutParameterOptions, error) {
	opts := defaults
	if exists {
		if fieldAllowed(cfg.Fields, "tier") && strings.TrimSpace(cloud.Tier) != "" {
			tier, err := ssm.ParseParameterTier(cloud.Tier)
			if err != nil {
				return ssm.PutParameterOptions{}, crerr.Wrap(err, "parse cloud tier")
			}
			opts.Tier = tier
		}
		if fieldAllowed(cfg.Fields, "data-type") && strings.TrimSpace(cloud.DataType) != "" {
			dataType, err := ssm.ParseParameterDataType(cloud.DataType)
			if err != nil {
				return ssm.PutParameterOptions{}, crerr.Wrap(err, "parse cloud data type")
			}
			opts.DataType = dataType
		}
		if fieldAllowed(cfg.Fields, "description") {
			opts.Description = cloud.Description
		}
		if fieldAllowed(cfg.Fields, "policies") {
			opts.Policies = cloud.Policies
		}
	}
	if fieldAllowed(cfg.Fields, "tier") && recordHasField(record, "tier") && strings.TrimSpace(record.Tier) != "" {
		tier, err := ssm.ParseParameterTier(record.Tier)
		if err != nil {
			return ssm.PutParameterOptions{}, crerr.Wrap(err, "parse record tier")
		}
		opts.Tier = tier
	}
	if fieldAllowed(cfg.Fields, "data-type") && recordHasField(record, "data-type") && strings.TrimSpace(record.DataType) != "" {
		dataType, err := ssm.ParseParameterDataType(record.DataType)
		if err != nil {
			return ssm.PutParameterOptions{}, crerr.Wrap(err, "parse record data type")
		}
		opts.DataType = dataType
	}
	if fieldAllowed(cfg.Fields, "description") && recordHasField(record, "description") && strings.TrimSpace(record.Description) != "" {
		opts.Description = record.Description
	}
	if fieldAllowed(cfg.Fields, "policies") && recordHasField(record, "policies") {
		if strings.TrimSpace(record.Policies) == "" {
			opts.Policies = "[{}]"
			opts.PoliciesSet = true
		} else {
			opts.Policies = record.Policies
		}
	}
	return opts, nil
}
