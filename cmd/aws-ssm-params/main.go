// Package main wires CLI flags and commands to the internal application layer.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/biptec/aws-ssm-params/internal/app"
)

func globalFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringSliceFlag{Name: "region", Sources: cli.EnvVars("AWS_SSM_PARAMS_REGION"), Usage: "AWS region; repeat the flag for multiple regions; env accepts comma-separated values"},
		&cli.BoolFlag{Name: "all-regions", Sources: cli.EnvVars("AWS_SSM_PARAMS_ALL_REGIONS"), Usage: "search parameters across all enabled AWS regions"},
		&cli.StringFlag{Name: "profile", Sources: cli.EnvVars("AWS_SSM_PARAMS_PROFILE"), Usage: "AWS profile"},
		&cli.BoolFlag{Name: "no-color", Sources: cli.EnvVars("AWS_SSM_PARAMS_NO_COLOR"), Usage: "disable colored output"},
		&cli.StringFlag{Name: "keymap", Value: "emacs", Sources: cli.EnvVars("AWS_SSM_PARAMS_KEYMAP"), Usage: "keyboard navigation style: emacs or vi"},
		&cli.StringFlag{Name: "log-level", Value: "off", Sources: cli.EnvVars("AWS_SSM_PARAMS_LOG_LEVEL"), Usage: "log level: trace, debug, info, warn, error, or off"},
		&cli.StringFlag{Name: "filters-file", Sources: cli.EnvVars("AWS_SSM_PARAMS_FILTER_FILE"), Usage: "file with filter groups; one OR group per line"},
		&cli.StringSliceFlag{Name: "filter", Sources: cli.EnvVars("AWS_SSM_PARAMS_FILTER"), Usage: "filter group; conditions inside one value are separated by semicolons; env accepts comma-separated values"},
	}
}

// newCLIApp builds the command-line application definition and delegates business logic to internal/app.
func newCLIApp(rawArgs []string) *cli.Command {
	return &cli.Command{
		Name:                  "aws-ssm-params",
		Usage:                 "Manage AWS SSM parameters",
		UsageText:             "aws-ssm-params [global options] <command> [command options]",
		EnableShellCompletion: true,
		Before: func(ctx context.Context, _ *cli.Command) (context.Context, error) {
			return ctx, app.RejectCommaSeparatedFlagArgs(rawArgs, "region", "filter", "output-field", "map-field", "show-column")
		},
		Flags: globalFlags(),
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() > 0 {
				return fmt.Errorf("unknown command: %s", cmd.Args().First())
			}
			return cli.ShowRootCommandHelp(cmd)
		},
		Commands: []*cli.Command{
			{
				Name:      "tui",
				Usage:     "Open the TUI",
				UsageText: "aws-ssm-params [global options] tui [command options]",
				Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
					return ctx, app.RejectCommaSeparatedFlagArgs(cmd.Args().Slice(), "show-column")
				},
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "with-decryption", Sources: cli.EnvVars("AWS_SSM_PARAMS_WITH_DECRYPTION"), Usage: "decrypt SecureString values"},
					&cli.StringSliceFlag{Name: "show-column", Sources: cli.EnvVars("AWS_SSM_PARAMS_SHOW_COLUMN"), Usage: "optional column to show in the TUI; repeat for multiple columns; env accepts comma-separated values"},
					&cli.StringSliceFlag{Name: "sort-by", Sources: cli.EnvVars("AWS_SSM_PARAMS_SORT_BY"), Usage: "initial sort as field:asc or field:desc; repeat for multiple fields; env accepts comma-separated values"},
					&cli.BoolFlag{Name: "no-confirm-overwrite-file", Sources: cli.EnvVars("AWS_SSM_PARAMS_NO_CONFIRM_OVERWRITE_FILE"), Usage: "do not ask before overwriting local files from the TUI"},
					&cli.BoolFlag{Name: "no-confirm-write-securestring", Sources: cli.EnvVars("AWS_SSM_PARAMS_NO_CONFIRM_WRITE_SECURESTRING"), Usage: "do not ask before writing SecureString values to local files in plaintext"},
					&cli.BoolFlag{Name: "no-confirm-delete-one", Sources: cli.EnvVars("AWS_SSM_PARAMS_NO_CONFIRM_DELETE_ONE"), Usage: "do not ask before deleting one parameter in the TUI"},
					&cli.BoolFlag{Name: "no-confirm-delete-all", Sources: cli.EnvVars("AWS_SSM_PARAMS_NO_CONFIRM_DELETE_ALL"), Usage: "do not ask before deleting all visible parameters in the TUI"},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return app.RunWithLogging(app.NewCLIContext(ctx, cmd), true, app.Interactive)
				},
			},
			{
				Name:      "import",
				Usage:     "Import parameter values from stdin",
				UsageText: "aws-ssm-params [global options] import [command options]",
				Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
					return ctx, app.RejectCommaSeparatedFlagArgs(cmd.Args().Slice(), "map-field")
				},
				Flags: []cli.Flag{
					&cli.StringSliceFlag{Name: "map-field", Usage: "field mapping as aws_field:file_field; repeat for multiple mappings"},
					&cli.StringFlag{Name: "format", Value: "dotenv", Usage: "input format: dotenv, json, or yaml"},
					&cli.StringFlag{Name: "key-field", Usage: "AWS field to use as object/map key for JSON or YAML records"},
					&cli.StringFlag{Name: "root-path", Usage: "SSM path prefix for relative imported names"},
					&cli.StringFlag{Name: "on-create", Usage: "when an imported parameter does not exist: skip, error, or ask"},
					&cli.StringFlag{Name: "on-update", Usage: "when an imported parameter already exists: skip, error, or ask"},
					&cli.BoolFlag{Name: "continue-on-error", Usage: "continue importing remaining records after per-record errors"},
					&cli.BoolFlag{Name: "summary", Usage: "print an import summary after processing records"},
					&cli.StringFlag{Name: "default-type", Usage: "default parameter type: string, string-list, or secure-string"},
					&cli.StringFlag{Name: "default-tier", Usage: "default parameter tier: standard, advanced, or intelligent-tiering"},
					&cli.StringFlag{Name: "default-data-type", Usage: "default parameter data type: text, aws:ec2:image, or aws:ssm:integration"},
					&cli.StringFlag{Name: "default-region", Usage: "default AWS region for imported records without region metadata"},
					&cli.StringFlag{Name: "default-description", Usage: "default parameter description"},
					&cli.StringFlag{Name: "default-policies", Usage: "default parameter policies JSON"},
					&cli.StringFlag{Name: "default-policies-file", Usage: "read default parameter policies JSON from file"},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return app.RunWithLogging(app.NewCLIContext(ctx, cmd), false, app.Import)
				},
			},
			{
				Name:      "export",
				Usage:     "Export parameter values to stdout",
				UsageText: "aws-ssm-params [global options] export [command options]",
				Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
					return ctx, app.RejectCommaSeparatedFlagArgs(cmd.Args().Slice(), "output-field", "map-field", "sort-by")
				},
				Flags: []cli.Flag{
					&cli.StringSliceFlag{Name: "output-field", Sources: cli.EnvVars("AWS_SSM_PARAMS_OUTPUT_FIELD"), Usage: "AWS field to include in export output; repeat for multiple fields"},
					&cli.StringSliceFlag{Name: "map-field", Sources: cli.EnvVars("AWS_SSM_PARAMS_MAP_FIELD"), Usage: "field mapping as aws_field:file_field; repeat for multiple mappings"},
					&cli.StringSliceFlag{Name: "sort-by", Sources: cli.EnvVars("AWS_SSM_PARAMS_SORT_BY"), Usage: "export sort as field:asc or field:desc; repeat for multiple fields; env accepts comma-separated values"},
					&cli.BoolFlag{Name: "with-decryption", Sources: cli.EnvVars("AWS_SSM_PARAMS_WITH_DECRYPTION"), Usage: "decrypt SecureString values"},
					&cli.StringFlag{Name: "format", Value: "dotenv", Usage: "output format: dotenv, json, or yaml"},
					&cli.StringFlag{Name: "key-field", Usage: "AWS field to use as object/map key for JSON or YAML records"},
					&cli.BoolFlag{Name: "scalar", Usage: "write exactly one selected --output-field as scalar values instead of records"},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return app.RunWithLogging(app.NewCLIContext(ctx, cmd), false, app.Export)
				},
			},
		},
	}
}

func main() {
	ctx := context.Background()
	cliApp := newCLIApp(os.Args[1:])
	if err := cliApp.Run(ctx, os.Args); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
}
