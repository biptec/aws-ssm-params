package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/urfave/cli/v3"

	"github.com/biptec/aws-ssm-params/internal/app"
	importcmd "github.com/biptec/aws-ssm-params/internal/app/import"
	"github.com/biptec/aws-ssm-params/internal/fileio"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/biptec/aws-ssm-params/internal/textio"
)

const (
	importCommandName = "import"

	importFlagMapField            = "map-field"
	importFlagFormat              = "format"
	importFlagKeyField            = "key-field"
	importFlagBasePath            = "base-path"
	importFlagOnCreate            = "on-create"
	importFlagOnUpdate            = "on-update"
	importFlagContinueOnError     = "continue-on-error"
	importFlagSummary             = "summary"
	importFlagDefaultType         = "default-type"
	importFlagDefaultTier         = "default-tier"
	importFlagDefaultDataType     = "default-data-type"
	importFlagDefaultRegion       = "default-region"
	importFlagDefaultDescription  = "default-description"
	importFlagDefaultPolicies     = "default-policies"
	importFlagDefaultPoliciesFile = "default-policies-file"
)

func importCLICommand() *cli.Command {
	return &cli.Command{
		Name:      importCommandName,
		Usage:     "Import parameter values from stdin",
		UsageText: appName + " [global options] " + importCommandName + " [command options]",
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			return ctx, rejectCommaSeparatedFlagArgs(cmd.Args().Slice(), importFlagMapField)
		},
		Flags: []cli.Flag{
			&cli.StringSliceFlag{Name: importFlagMapField, Usage: "field mapping as aws_field:file_field; repeat for multiple mappings"},
			&cli.StringFlag{Name: importFlagFormat, Value: string(textio.FormatDotenv), Usage: "input format: dotenv, json, or yaml"},
			&cli.StringFlag{Name: importFlagKeyField, Usage: "AWS field to use as object/map key for JSON or YAML records"},
			&cli.StringFlag{Name: importFlagBasePath, Usage: "base SSM path used to resolve relative imported names"},
			&cli.StringFlag{Name: importFlagOnCreate, Usage: "when an imported parameter does not exist: skip, error, or ask"},
			&cli.StringFlag{Name: importFlagOnUpdate, Usage: "when an imported parameter already exists: skip, error, or ask"},
			&cli.BoolFlag{Name: importFlagContinueOnError, Usage: "continue importing remaining records after per-record errors"},
			&cli.BoolFlag{Name: importFlagSummary, Usage: "print an import summary after processing records"},
			&cli.StringFlag{Name: importFlagDefaultType, Usage: "default parameter type: string, string-list, or secure-string"},
			&cli.StringFlag{Name: importFlagDefaultTier, Usage: "default parameter tier: standard, advanced, or intelligent-tiering"},
			&cli.StringFlag{Name: importFlagDefaultDataType, Usage: "default parameter data type: text, aws:ec2:image, or aws:ssm:integration"},
			&cli.StringFlag{Name: importFlagDefaultRegion, Usage: "default AWS region for imported records without region metadata"},
			&cli.StringFlag{Name: importFlagDefaultDescription, Usage: "default parameter description"},
			&cli.StringFlag{Name: importFlagDefaultPolicies, Usage: "default parameter policies JSON"},
			&cli.StringFlag{Name: importFlagDefaultPoliciesFile, Usage: "read default parameter policies JSON from file"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return runWithLogging(ctx, cmd, false, func(ctx context.Context) error {
				options, err := importOptionsFromCLI(ctx, cmd)
				if err != nil {
					return err
				}
				input, err := importInput()
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
	fieldMappings, err := parseFieldMappings(cmd.StringSlice(importFlagMapField), importFlagMapField)
	if err != nil {
		return &importcmd.Options{}, err
	}
	basePath, err := app.ParseBasePath(cmd.String(importFlagBasePath))
	if err != nil {
		return &importcmd.Options{}, fmt.Errorf("--%s: %w", importFlagBasePath, err)
	}
	defaultType, err := ssm.ParseParameterType(cmd.String(importFlagDefaultType))
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
	return &importcmd.Options{
		Options:         global.Options,
		Format:          textio.FormatType(cmd.String(importFlagFormat)),
		FieldMappings:   fieldMappings,
		KeyField:        strings.TrimSpace(cmd.String(importFlagKeyField)),
		BasePath:        basePath,
		DefaultRegion:   strings.TrimSpace(cmd.String(importFlagDefaultRegion)),
		DefaultType:     defaultType,
		DefaultOptions:  defaultOptions,
		Policy:          policy,
		ContinueOnError: cmd.Bool(importFlagContinueOnError),
		Summary:         cmd.Bool(importFlagSummary),
	}, nil
}

func importDefaultOptionsFromCLI(cmd *cli.Command) (ssm.PutParameterOptions, error) {
	tier, err := ssm.ParseParameterTier(cmd.String(importFlagDefaultTier))
	if err != nil {
		return ssm.PutParameterOptions{}, errors.Wrap(err, "parse default tier")
	}
	dataType, err := ssm.ParseParameterDataType(cmd.String(importFlagDefaultDataType))
	if err != nil {
		return ssm.PutParameterOptions{}, errors.Wrap(err, "parse default data type")
	}
	policies := cmd.String(importFlagDefaultPolicies)
	if policiesFile := strings.TrimSpace(cmd.String(importFlagDefaultPoliciesFile)); policiesFile != "" {
		data, err := fileio.ReadFile(policiesFile)
		if err != nil {
			return ssm.PutParameterOptions{}, errors.Wrapf(err, "read default policies file %s", policiesFile)
		}
		policies = string(data)
	}
	return ssm.PutParameterOptions{
		Tier:        tier,
		DataType:    dataType,
		Description: cmd.String(importFlagDefaultDescription),
		Policies:    policies,
	}, nil
}

func importPolicyFromCLI(cmd *cli.Command) (importcmd.Policy, error) {
	onCreate, err := parseImportPolicyAction(cmd.String(importFlagOnCreate), importFlagOnCreate)
	if err != nil {
		return importcmd.Policy{}, err
	}
	onUpdate, err := parseImportPolicyAction(cmd.String(importFlagOnUpdate), importFlagOnUpdate)
	if err != nil {
		return importcmd.Policy{}, err
	}
	return importcmd.Policy{OnCreate: onCreate, OnUpdate: onUpdate}, nil
}

func parseImportPolicyAction(value, flagName string) (importcmd.PolicyAction, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return importcmd.PolicyDefault, nil
	case "skip":
		return importcmd.PolicySkip, nil
	case "error":
		return importcmd.PolicyError, nil
	case "ask":
		return importcmd.PolicyAsk, nil
	default:
		return "", fmt.Errorf("unsupported --%s value %q; use skip, error, or ask", flagName, value)
	}
}

func importInput() (io.Reader, error) {
	info, err := os.Stdin.Stat()
	if err == nil && info.Mode()&os.ModeCharDevice != 0 {
		return nil, errors.New("import requires data from stdin")
	}
	return os.Stdin, nil
}
