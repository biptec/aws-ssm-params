package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/urfave/cli/v3"

	importcmd "github.com/biptec/aws-ssm-params/internal/app/import"
	"github.com/biptec/aws-ssm-params/internal/fileio"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/biptec/aws-ssm-params/internal/textio"
)

const (
	importCommandName = "import"

	importFlagMapField            = "map-field"
	importFlagMapPath             = "map-path"
	importFlagFormat              = "format"
	importFlagKeyField            = "key-field"
	importFlagOnCreate            = "on-create"
	importFlagOnUpdate            = "on-update"
	importFlagContinueOnError     = "continue-on-error"
	importFlagSummary             = "summary"
	importFlagDryRun              = "dry-run"
	importFlagDefaultType         = "default-type"
	importFlagDefaultTier         = "default-tier"
	importFlagDefaultDataType     = "default-data-type"
	importFlagDefaultRegion       = "default-region"
	importFlagDefaultDescription  = "default-description"
	importFlagDefaultPolicies     = "default-policies"
	importFlagDefaultPoliciesFile = "default-policies-file"

	importPolicyNoneValue  = "none"
	importPolicySkipValue  = "skip"
	importPolicyErrorValue = "error"
	importPolicyAskValue   = "ask"

	importEnvMapField            = envVarPrefix + "MAP_FIELD"
	importEnvMapPath             = envVarPrefix + "MAP_PATH"
	importEnvFormat              = envVarPrefix + "FORMAT"
	importEnvKeyField            = envVarPrefix + "KEY_FIELD"
	importEnvOnCreate            = envVarPrefix + "ON_CREATE"
	importEnvOnUpdate            = envVarPrefix + "ON_UPDATE"
	importEnvContinueOnError     = envVarPrefix + "CONTINUE_ON_ERROR"
	importEnvSummary             = envVarPrefix + "SUMMARY"
	importEnvDryRun              = envVarPrefix + "DRY_RUN"
	importEnvDefaultType         = envVarPrefix + "DEFAULT_TYPE"
	importEnvDefaultTier         = envVarPrefix + "DEFAULT_TIER"
	importEnvDefaultDataType     = envVarPrefix + "DEFAULT_DATA_TYPE"
	importEnvDefaultRegion       = envVarPrefix + "DEFAULT_REGION"
	importEnvDefaultDescription  = envVarPrefix + "DEFAULT_DESCRIPTION"
	importEnvDefaultPolicies     = envVarPrefix + "DEFAULT_POLICIES"
	importEnvDefaultPoliciesFile = envVarPrefix + "DEFAULT_POLICIES_FILE"
)

func importFlags() []cli.Flag {
	flags := []cli.Flag{
		&cli.StringSliceFlag{Name: importFlagMapField, Sources: cli.EnvVars(importEnvMapField), Usage: "field mapping as aws_field:file_field; repeat for multiple mappings"},
		&cli.StringSliceFlag{Name: importFlagMapPath, Sources: cli.EnvVars(importEnvMapPath), Usage: "path mapping as aws_path:file_path; repeat for multiple mappings"},
		&cli.StringFlag{Name: importFlagFormat, Value: string(textio.FormatDotenv), Sources: cli.EnvVars(importEnvFormat), Usage: "input format: dotenv, json, or yaml"},
		&cli.StringFlag{Name: importFlagKeyField, Sources: cli.EnvVars(importEnvKeyField), Usage: "AWS field to use as object/map key for JSON or YAML records"},
		&cli.StringFlag{Name: importFlagOnCreate, Value: importPolicyNoneValue, Sources: cli.EnvVars(importEnvOnCreate), Usage: "when an imported parameter does not exist: none, skip, error, or ask"},
		&cli.StringFlag{Name: importFlagOnUpdate, Value: importPolicyAskValue, Sources: cli.EnvVars(importEnvOnUpdate), Usage: "when an imported parameter already exists: none, skip, error, or ask"},
		&cli.BoolFlag{Name: importFlagContinueOnError, Sources: cli.EnvVars(importEnvContinueOnError), Usage: "continue importing remaining records after per-record errors"},
		&cli.BoolFlag{Name: importFlagSummary, Sources: cli.EnvVars(importEnvSummary), Usage: "print an import summary after processing records"},
		&cli.BoolFlag{Name: importFlagDryRun, Sources: cli.EnvVars(importEnvDryRun), Usage: "validate and show writes without changing Parameter Store"},
		&cli.StringFlag{Name: importFlagDefaultType, Sources: cli.EnvVars(importEnvDefaultType), Usage: "default parameter type: string, string-list, or secure-string"},
		&cli.StringFlag{Name: importFlagDefaultTier, Sources: cli.EnvVars(importEnvDefaultTier), Usage: "default parameter tier: standard, advanced, or intelligent-tiering"},
		&cli.StringFlag{Name: importFlagDefaultDataType, Sources: cli.EnvVars(importEnvDefaultDataType), Usage: "default parameter data type: text, aws:ec2:image, or aws:ssm:integration"},
		&cli.StringFlag{Name: importFlagDefaultRegion, Sources: cli.EnvVars(importEnvDefaultRegion), Usage: "default AWS region for imported records without region metadata"},
		&cli.StringFlag{Name: importFlagDefaultDescription, Sources: cli.EnvVars(importEnvDefaultDescription), Usage: "default parameter description"},
		&cli.StringFlag{Name: importFlagDefaultPolicies, Sources: cli.EnvVars(importEnvDefaultPolicies), Usage: "default parameter policies JSON"},
		&cli.StringFlag{Name: importFlagDefaultPoliciesFile, Sources: cli.EnvVars(importEnvDefaultPoliciesFile), Usage: "read default parameter policies JSON from file"},
	}

	sort.Sort(cli.FlagsByName(flags))

	return flags
}

func importCommand() *cli.Command {
	return &cli.Command{
		Name:      importCommandName,
		Usage:     "Import parameter values from stdin",
		UsageText: appName + " [global options] " + importCommandName + " [command options]",
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			return ctx, rejectCommaSeparatedFlagArgs(cmd.Args().Slice(), importFlagMapField, importFlagMapPath)
		},
		Flags: importFlags(),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return runWithLogging(ctx, cmd, false, func(ctx context.Context) error {
				options, err := importOptionsFromCLI(ctx, cmd)
				if err != nil {
					return err
				}

				input, err := requiredStdin(importCommandName)
				if err != nil {
					return err
				}

				return importcmd.Run(ctx, options, input, os.Stderr)
			})
		},
	}
}

func importOptionsFromCLI(ctx context.Context, cmd *cli.Command) (*importcmd.Options, error) {
	global, err := globalOptionsFromCLI(ctx, cmd)
	if err != nil {
		return &importcmd.Options{}, err
	}

	if global.AllRegions {
		return &importcmd.Options{}, fmt.Errorf(
			"--%s is not supported for %s; specify --%s",
			flagAllRegions,
			importCommandName,
			flagRegion,
		)
	}

	if len(global.Regions) > 1 {
		return &importcmd.Options{}, fmt.Errorf(
			"multiple --%s values are only supported for %s and %s",
			flagRegion,
			tuiCommandName,
			exportCommandName,
		)
	}

	fieldMappings, err := parseFieldMappings(
		stringSliceFlagValue(cmd, importFlagMapField, importEnvMapField),
		importFlagMapField,
	)
	if err != nil {
		return &importcmd.Options{}, err
	}

	pathMappings, err := parsePathMappings(
		stringSliceFlagValue(cmd, importFlagMapPath, importEnvMapPath),
		importFlagMapPath,
	)
	if err != nil {
		return &importcmd.Options{}, err
	}

	defaultType, err := ssm.ParseParameterType(
		stringFlagValueAny(cmd, importFlagDefaultType, "", importEnvDefaultType),
	)
	if err != nil {
		return &importcmd.Options{}, errors.Wrap(err, "parse default type")
	}

	defaultOptions, err := importDefaultOptionsFromCLI(cmd)
	if err != nil {
		return &importcmd.Options{}, err
	}

	policy, err := importPolicyFromCLI(cmd)
	if err != nil {
		return &importcmd.Options{}, err
	}

	format := textio.FormatType(stringFlagValueAny(
		cmd,
		importFlagFormat,
		string(textio.FormatDotenv),
		importEnvFormat,
	))
	keyField := strings.TrimSpace(stringFlagValueAny(cmd, importFlagKeyField, "", importEnvKeyField))
	defaultRegion := strings.TrimSpace(stringFlagValueAny(
		cmd,
		importFlagDefaultRegion,
		"",
		importEnvDefaultRegion,
	))

	return &importcmd.Options{
		Options:         global.Options,
		Format:          format,
		FieldMappings:   fieldMappings,
		KeyField:        keyField,
		PathMappings:    pathMappings,
		DefaultRegion:   defaultRegion,
		DefaultType:     defaultType,
		DefaultOptions:  defaultOptions,
		Policy:          policy,
		ContinueOnError: boolFlagValueAny(cmd, importFlagContinueOnError, importEnvContinueOnError),
		Summary:         boolFlagValueAny(cmd, importFlagSummary, importEnvSummary),
		DryRun:          boolFlagValueAny(cmd, importFlagDryRun, importEnvDryRun),
	}, nil
}

func importDefaultOptionsFromCLI(cmd *cli.Command) (ssm.PutParameterOptions, error) {
	tier, err := ssm.ParseParameterTier(
		stringFlagValueAny(cmd, importFlagDefaultTier, "", importEnvDefaultTier),
	)
	if err != nil {
		return ssm.PutParameterOptions{}, errors.Wrap(err, "parse default tier")
	}

	dataType, err := ssm.ParseParameterDataType(
		stringFlagValueAny(cmd, importFlagDefaultDataType, "", importEnvDefaultDataType),
	)
	if err != nil {
		return ssm.PutParameterOptions{}, errors.Wrap(err, "parse default data type")
	}

	policies := stringFlagValueAny(cmd, importFlagDefaultPolicies, "", importEnvDefaultPolicies)
	if policiesFile := strings.TrimSpace(stringFlagValueAny(
		cmd,
		importFlagDefaultPoliciesFile,
		"",
		importEnvDefaultPoliciesFile,
	)); policiesFile != "" {
		data, err := fileio.ReadFile(policiesFile)
		if err != nil {
			return ssm.PutParameterOptions{}, errors.Wrapf(err, "read default policies file %s", policiesFile)
		}

		policies = string(data)
	}

	return ssm.PutParameterOptions{
		Tier:        tier,
		DataType:    dataType,
		Description: stringFlagValueAny(cmd, importFlagDefaultDescription, "", importEnvDefaultDescription),
		Policies:    policies,
	}, nil
}

func importPolicyFromCLI(cmd *cli.Command) (importcmd.Policy, error) {
	onCreate, err := parseImportPolicyAction(
		stringFlagValueAny(cmd, importFlagOnCreate, importPolicyNoneValue, importEnvOnCreate),
		importFlagOnCreate,
	)
	if err != nil {
		return importcmd.Policy{}, err
	}

	onUpdate, err := parseImportPolicyAction(
		stringFlagValueAny(cmd, importFlagOnUpdate, importPolicyAskValue, importEnvOnUpdate),
		importFlagOnUpdate,
	)
	if err != nil {
		return importcmd.Policy{}, err
	}

	return importcmd.Policy{OnCreate: onCreate, OnUpdate: onUpdate}, nil
}

func parseImportPolicyAction(value, flagName string) (importcmd.PolicyAction, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", importPolicyNoneValue:
		return importcmd.PolicyNone, nil
	case importPolicySkipValue:
		return importcmd.PolicySkip, nil
	case importPolicyErrorValue:
		return importcmd.PolicyError, nil
	case importPolicyAskValue:
		return importcmd.PolicyAsk, nil
	default:
		return "", fmt.Errorf(
			"unsupported --%s value %q; use none, skip, error, or ask",
			flagName,
			value,
		)
	}
}
