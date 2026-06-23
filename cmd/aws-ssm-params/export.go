package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/biptec/aws-ssm-params/internal/app"
	exportcmd "github.com/biptec/aws-ssm-params/internal/app/export"
	"github.com/biptec/aws-ssm-params/internal/textio"
)

const (
	exportCommandName = "export"

	exportFlagOutputField    = "output-field"
	exportFlagMapField       = "map-field"
	exportFlagSortBy         = "sort-by"
	exportFlagWithDecryption = "with-decryption"
	exportFlagFormat         = "format"
	exportFlagKeyField       = "key-field"
	exportFlagBasePath       = "base-path"
	exportFlagScalar         = "scalar"

	exportEnvOutputField    = envVarPrefix + "OUTPUT_FIELD"
	exportEnvMapField       = envVarPrefix + "MAP_FIELD"
	exportEnvSortBy         = envVarPrefix + "SORT_BY"
	exportEnvWithDecryption = envVarPrefix + "WITH_DECRYPTION"
)

func exportCLICommand() *cli.Command {
	return &cli.Command{
		Name:      exportCommandName,
		Usage:     "Export parameter values to stdout",
		UsageText: appName + " [global options] " + exportCommandName + " [command options]",
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			return ctx, rejectCommaSeparatedFlagArgs(
				cmd.Args().Slice(),
				exportFlagOutputField,
				exportFlagMapField,
				exportFlagSortBy,
			)
		},
		Flags: []cli.Flag{
			&cli.StringSliceFlag{Name: exportFlagOutputField, Sources: cli.EnvVars(exportEnvOutputField), Usage: "AWS field to include in export output; repeat for multiple fields"},
			&cli.StringSliceFlag{Name: exportFlagMapField, Sources: cli.EnvVars(exportEnvMapField), Usage: "field mapping as aws_field:file_field; repeat for multiple mappings"},
			&cli.StringSliceFlag{Name: exportFlagSortBy, Sources: cli.EnvVars(exportEnvSortBy), Usage: "export sort as field:asc or field:desc; repeat for multiple fields; env accepts comma-separated values"},
			&cli.BoolFlag{Name: exportFlagWithDecryption, Sources: cli.EnvVars(exportEnvWithDecryption), Usage: "decrypt SecureString values"},
			&cli.StringFlag{Name: exportFlagFormat, Value: string(textio.FormatDotenv), Usage: "output format: dotenv, json, or yaml"},
			&cli.StringFlag{Name: exportFlagKeyField, Usage: "AWS field to use as object/map key for JSON or YAML records"},
			&cli.StringFlag{Name: exportFlagBasePath, Usage: "base SSM path removed from exported parameter names"},
			&cli.BoolFlag{Name: exportFlagScalar, Usage: "write exactly one selected --output-field as scalar values instead of records"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return runWithLogging(ctx, cmd, false, func(ctx context.Context) error {
				options, err := exportOptionsFromCLI(ctx, cmd)
				if err != nil {
					return err
				}

				return exportcmd.Run(ctx, options, os.Stdout)
			})
		},
	}
}

func exportOptionsFromCLI(ctx context.Context, cmd *cli.Command) (*exportcmd.Options, error) {
	global, err := globalOptionsFromCLI(ctx, cmd)
	if err != nil {
		return &exportcmd.Options{}, err
	}

	fields, err := parseOutputFields(
		stringSliceFlagValue(cmd, exportFlagOutputField, exportEnvOutputField),
		exportFlagOutputField,
	)
	if err != nil {
		return &exportcmd.Options{}, err
	}

	fieldMappings, err := parseFieldMappings(
		stringSliceFlagValue(cmd, exportFlagMapField, exportEnvMapField),
		exportFlagMapField,
	)
	if err != nil {
		return &exportcmd.Options{}, err
	}

	keyField := strings.TrimSpace(cmd.String(exportFlagKeyField))
	if err := validateKeyFieldOutputFields(keyField, fields); err != nil {
		return &exportcmd.Options{}, err
	}

	basePath, err := app.ParseBasePath(cmd.String(exportFlagBasePath))
	if err != nil {
		return &exportcmd.Options{}, fmt.Errorf("--%s: %w", exportFlagBasePath, err)
	}

	scalarField, err := exportScalarField(cmd, fields)
	if err != nil {
		return &exportcmd.Options{}, err
	}

	global.WithDecryption = boolFlagValueAny(
		cmd,
		exportFlagWithDecryption,
		exportEnvWithDecryption,
	)

	return &exportcmd.Options{
		Options:       global.Options,
		Format:        textio.FormatType(cmd.String(exportFlagFormat)),
		FieldMappings: fieldMappings,
		Fields:        fields,
		SortColumns:   compactStrings(stringSliceFlagValue(cmd, exportFlagSortBy, exportEnvSortBy)),
		KeyField:      keyField,
		BasePath:      basePath,
		ScalarField:   scalarField,
	}, nil
}

func exportScalarField(cmd *cli.Command, fields textio.Fields) (string, error) {
	if !cmd.Bool(exportFlagScalar) {
		return "", nil
	}

	rawFields := compactStrings(cmd.StringSlice(exportFlagOutputField))
	if len(rawFields) != 1 || len(fields) != 1 {
		return "", fmt.Errorf(
			"--%s requires exactly one --%s",
			exportFlagScalar,
			exportFlagOutputField,
		)
	}

	return fields[0], nil
}

func validateKeyFieldOutputFields(keyField string, outputFields textio.Fields) error {
	if keyField == "" {
		return nil
	}

	for _, field := range outputFields {
		if field == keyField {
			return fmt.Errorf(
				"--%s and --%s cannot use the same field: %s",
				exportFlagKeyField,
				exportFlagOutputField,
				keyField,
			)
		}
	}

	return nil
}
