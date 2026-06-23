package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli/v3"

	deletecmd "github.com/biptec/aws-ssm-params/internal/app/delete"
	"github.com/biptec/aws-ssm-params/internal/textio"
)

const (
	deleteCommandName = "delete"

	deleteFlagFormat    = "format"
	deleteFlagKeyField  = "key-field"
	deleteFlagMapField  = "map-field"
	deleteFlagMapPath   = "map-path"
	deleteFlagNoConfirm = "no-confirm"
	deleteFlagDryRun    = "dry-run"

	deleteEnvMapPath = envVarPrefix + "MAP_PATH"
)

func deleteCLICommand() *cli.Command {
	return &cli.Command{
		Name:      deleteCommandName,
		Usage:     "Delete parameters described by records from stdin",
		UsageText: appName + " [global options] " + deleteCommandName + " [command options]",
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			return ctx, rejectCommaSeparatedFlagArgs(cmd.Args().Slice(), deleteFlagMapField, deleteFlagMapPath)
		},
		Flags: []cli.Flag{
			&cli.StringFlag{Name: deleteFlagFormat, Value: string(textio.FormatDotenv), Usage: "input format: dotenv, json, or yaml"},
			&cli.StringFlag{Name: deleteFlagKeyField, Usage: "field represented by JSON or YAML object keys: name or region"},
			&cli.StringSliceFlag{Name: deleteFlagMapField, Usage: "input field mapping as name:file_field or region:file_field; repeat for both mappings"},
			&cli.StringSliceFlag{Name: deleteFlagMapPath, Sources: cli.EnvVars(deleteEnvMapPath), Usage: "path mapping as aws_path:file_path; repeat for multiple mappings"},
			&cli.BoolFlag{Name: deleteFlagNoConfirm, Usage: "delete every filtered input record without interactive confirmation"},
			&cli.BoolFlag{Name: deleteFlagDryRun, Usage: "show parameters that would be deleted without deleting them"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return runWithLogging(ctx, cmd, false, func(ctx context.Context) error {
				options, err := deleteOptionsFromCLI(ctx, cmd)
				if err != nil {
					return err
				}

				input, err := requiredStdin(deleteCommandName)
				if err != nil {
					return err
				}

				return deletecmd.Run(ctx, options, input, os.Stderr)
			})
		},
	}
}

func deleteOptionsFromCLI(ctx context.Context, cmd *cli.Command) (*deletecmd.Options, error) {
	global, err := globalOptionsFromCLI(ctx, cmd)
	if err != nil {
		return &deletecmd.Options{}, err
	}

	fieldMappings, err := parseFieldMappings(cmd.StringSlice(deleteFlagMapField), deleteFlagMapField)
	if err != nil {
		return &deletecmd.Options{}, err
	}

	if err := validateDeleteFieldMappings(fieldMappings); err != nil {
		return &deletecmd.Options{}, err
	}

	keyField, err := parseDeleteKeyField(cmd.String(deleteFlagKeyField))
	if err != nil {
		return &deletecmd.Options{}, err
	}

	pathMappings, err := parsePathMappings(
		stringSliceFlagValue(cmd, deleteFlagMapPath, deleteEnvMapPath),
		deleteFlagMapPath,
	)
	if err != nil {
		return &deletecmd.Options{}, err
	}

	return &deletecmd.Options{
		Options:       global.Options,
		Format:        textio.FormatType(strings.ToLower(strings.TrimSpace(cmd.String(deleteFlagFormat)))),
		FieldMappings: fieldMappings,
		KeyField:      keyField,
		PathMappings:  pathMappings,
		NoConfirm:     cmd.Bool(deleteFlagNoConfirm),
		DryRun:        cmd.Bool(deleteFlagDryRun),
	}, nil
}

func parseDeleteKeyField(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "", textio.FieldName, textio.FieldRegion:
		return value, nil
	default:
		return "", fmt.Errorf(
			"unsupported --%s value %q; use %s or %s",
			deleteFlagKeyField,
			value,
			textio.FieldName,
			textio.FieldRegion,
		)
	}
}

func validateDeleteFieldMappings(mappings textio.FieldMappings) error {
	fileFields := make(map[string]string, len(mappings))

	for _, mapping := range mappings {
		switch mapping.AWSName {
		case textio.FieldName, textio.FieldRegion:
		default:
			return fmt.Errorf(
				"--%s supports only %s and %s AWS fields",
				deleteFlagMapField,
				textio.FieldName,
				textio.FieldRegion,
			)
		}

		if awsField, ok := fileFields[mapping.FileName]; ok && awsField != mapping.AWSName {
			return fmt.Errorf(
				"--%s file field %q cannot map both %s and %s",
				deleteFlagMapField,
				mapping.FileName,
				awsField,
				mapping.AWSName,
			)
		}

		fileFields[mapping.FileName] = mapping.AWSName
	}

	return nil
}
