// Package main wires CLI flags and commands to the internal application layer.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v2"

	"github.com/biptec/aws-ssm-params/internal/app"
)

const alignedAppHelpTemplate = `NAME:
   {{template "helpNameTemplate" .}}

USAGE:
   {{if .UsageText}}{{.UsageText}}{{else}}{{.HelpName}} {{if .VisibleFlags}}[global options]{{end}}{{if .Commands}} command [command options]{{end}}{{if .ArgsUsage}} {{.ArgsUsage}}{{else}}{{if .Args}} [arguments...]{{end}}{{end}}{{end}}{{if .Version}}{{if not .HideVersion}}

VERSION:
   {{.Version}}{{end}}{{end}}{{if .Description}}

DESCRIPTION:
   {{template "descriptionTemplate" .}}{{end}}
{{- if len .Authors}}

AUTHOR{{template "authorsTemplate" .}}{{end}}{{if .VisibleCommands}}

COMMANDS:{{template "visibleCommandCategoryTemplate" .}}{{end}}{{if .VisibleFlagCategories}}

GLOBAL OPTIONS:{{template "visibleFlagCategoryTemplate" .}}{{else if .VisibleFlags}}

GLOBAL OPTIONS:{{template "visibleFlagTemplate" .}}{{end}}{{if .Copyright}}

COPYRIGHT:
   {{template "copyrightTemplate" .}}{{end}}
`

const alignedCommandHelpTemplate = `NAME:
   {{template "helpNameTemplate" .}}

USAGE:
   {{if .UsageText}}{{.UsageText}}{{else}}{{.HelpName}}{{if .VisibleFlags}} [command options]{{end}}{{if .ArgsUsage}} {{.ArgsUsage}}{{else}}{{if .Args}} [arguments...]{{end}}{{end}}{{end}}{{if .Category}}

CATEGORY:
   {{.Category}}{{end}}{{if .Description}}

DESCRIPTION:
   {{template "descriptionTemplate" .}}{{end}}{{if .VisibleFlagCategories}}

OPTIONS:{{template "visibleFlagCategoryTemplate" .}}{{else if .VisibleFlags}}

OPTIONS:{{template "visibleFlagTemplate" .}}{{end}}
`

// newCLIApp builds the command-line application definition and delegates business logic to internal/app.
func newCLIApp(rawArgs []string) *cli.App {
	return &cli.App{
		Name:                  "aws-ssm-params",
		Usage:                 "Manage AWS SSM parameters",
		UsageText:             "aws-ssm-params [global options] <command> [command options]",
		EnableBashCompletion:  true,
		CustomAppHelpTemplate: alignedAppHelpTemplate,
		Before: func(_ *cli.Context) error {
			return app.RejectCommaSeparatedFlagArgs(rawArgs, "region", "name", "filter", "field", "show-column")
		},
		Flags: []cli.Flag{
			&cli.StringSliceFlag{Name: "region", EnvVars: []string{"AWS_SSM_PARAMS_REGIONS", "AWS_SSM_PARAMS_REGION"}, Usage: "AWS region; repeat the flag for multiple regions; env accepts comma-separated values"},
			&cli.BoolFlag{Name: "all-regions", EnvVars: []string{"AWS_SSM_PARAMS_ALL_REGIONS"}, Usage: "search parameters across all enabled AWS regions"},
			&cli.StringFlag{Name: "profile", EnvVars: []string{"AWS_SSM_PARAMS_PROFILE", "AWS_PROFILE"}, Usage: "AWS profile"},
			&cli.BoolFlag{Name: "no-color", EnvVars: []string{"AWS_SSM_PARAMS_NO_COLOR", "NO_COLOR"}, Usage: "disable colored output"},
			&cli.StringFlag{Name: "keymap", Value: "emacs", EnvVars: []string{"AWS_SSM_PARAMS_KEYMAP"}, Usage: "keyboard navigation style: emacs or vi"},
			&cli.StringFlag{Name: "log-level", Value: "off", EnvVars: []string{"AWS_SSM_PARAMS_LOG_LEVEL"}, Usage: "log level: debug, info, warn, error, or off"},
			&cli.StringFlag{Name: "log-target", Value: "stderr", EnvVars: []string{"AWS_SSM_PARAMS_LOG_TARGET"}, Usage: "log target: stderr, stdout, or file"},
			&cli.StringFlag{Name: "log-file", Value: "./debug.log", EnvVars: []string{"AWS_SSM_PARAMS_LOG_FILE"}, Usage: "log file path used when --log-target=file"},
		},
		Action: func(ctx *cli.Context) error {
			return app.RunWithLogging(ctx, false, func(ctx *cli.Context) error {
				if ctx.NArg() > 0 {
					return fmt.Errorf("unknown command: %s", ctx.Args().First())
				}
				return cli.ShowAppHelp(ctx)
			})
		},
		Commands: []*cli.Command{
			{
				Name:               "interactive",
				Aliases:            []string{"tui"},
				Usage:              "Open the interactive TUI",
				UsageText:          "aws-ssm-params [global options] interactive [command options]",
				CustomHelpTemplate: alignedCommandHelpTemplate,
				Before: func(ctx *cli.Context) error {
					return app.RejectCommaSeparatedFlagArgs(ctx.Args().Slice(), "filter", "show-column")
				},
				Flags: []cli.Flag{
					&cli.StringSliceFlag{Name: "name", EnvVars: []string{"AWS_SSM_PARAMS_NAME"}, Usage: "SSM parameter name to load; repeat the flag for multiple names; env accepts comma-separated values"},
					&cli.StringFlag{Name: "names-file", EnvVars: []string{"AWS_SSM_PARAMS_NAMES_FILE"}, Usage: "file with SSM parameter names to load"},
					&cli.StringFlag{Name: "filters-file", EnvVars: []string{"AWS_SSM_PARAMS_FILTERS_FILE"}, Usage: "file with filter groups; one OR group per line"},
					&cli.StringSliceFlag{Name: "filter", EnvVars: []string{"AWS_SSM_PARAMS_FILTERS", "AWS_SSM_PARAMS_FILTER"}, Usage: "filter group; conditions inside one value are separated by semicolons; env accepts comma-separated values"},
					&cli.BoolFlag{Name: "with-decryption", EnvVars: []string{"AWS_SSM_PARAMS_WITH_DECRYPTION"}, Usage: "decrypt SecureString values"},
					&cli.StringSliceFlag{Name: "show-column", EnvVars: []string{"AWS_SSM_PARAMS_SHOW_COLUMNS"}, Usage: "optional column to show in the TUI; repeat for multiple columns; env accepts comma-separated values"},
					&cli.StringFlag{Name: "sort-column", EnvVars: []string{"AWS_SSM_PARAMS_SORT_COLUMN"}, Usage: "initial sort as field:asc or field:desc"},
					&cli.BoolFlag{Name: "no-confirm-overwrite-file", EnvVars: []string{"AWS_SSM_PARAMS_NO_CONFIRM_OVERWRITE_FILE"}, Usage: "do not ask before overwriting local files from the TUI"},
					&cli.BoolFlag{Name: "no-confirm-write-securestring", EnvVars: []string{"AWS_SSM_PARAMS_NO_CONFIRM_WRITE_SECURESTRING"}, Usage: "do not ask before writing SecureString values to local files in plaintext"},
					&cli.BoolFlag{Name: "no-confirm-delete-one", EnvVars: []string{"AWS_SSM_PARAMS_NO_CONFIRM_DELETE_ONE"}, Usage: "do not ask before deleting one parameter in the TUI"},
					&cli.BoolFlag{Name: "no-confirm-delete-all", EnvVars: []string{"AWS_SSM_PARAMS_NO_CONFIRM_DELETE_ALL"}, Usage: "do not ask before deleting all visible parameters in the TUI"},
				},
				Action: func(ctx *cli.Context) error { return app.RunWithLogging(ctx, true, app.Interactive) },
			},
			{
				Name:               "get",
				Usage:              "Print one selected parameter field",
				UsageText:          "aws-ssm-params [global options] get <name> <field> [--with-decryption]",
				CustomHelpTemplate: alignedCommandHelpTemplate,
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "with-decryption", EnvVars: []string{"AWS_SSM_PARAMS_WITH_DECRYPTION"}, Usage: "decrypt SecureString values"},
				},
				Action: func(ctx *cli.Context) error { return app.RunWithLogging(ctx, false, app.Get) },
			},
			{
				Name:               "put",
				Usage:              "Put a String, StringList, or SecureString parameter value",
				UsageText:          "aws-ssm-params [global options] put <name> <value> [command options]",
				CustomHelpTemplate: alignedCommandHelpTemplate,
				Flags: []cli.Flag{
					&cli.BoolFlag{Name: "override", Usage: "overwrite existing values"},
					&cli.StringFlag{Name: "type", Usage: "parameter type: string, string-list, or secure-string; existing type is preserved when omitted"},
					&cli.StringFlag{Name: "tier", Usage: "parameter tier: standard, advanced, or intelligent-tiering"},
					&cli.StringFlag{Name: "data-type", Usage: "parameter data type: text, aws:ec2:image, or aws:ssm:integration"},
					&cli.StringFlag{Name: "region", Usage: "target AWS region; overrides the global primary region for this write"},
					&cli.StringFlag{Name: "description", Usage: "parameter description"},
					&cli.StringFlag{Name: "policies", Usage: "parameter policies JSON"},
					&cli.StringFlag{Name: "policies-file", Usage: "read parameter policies JSON from file"},
				},
				Action: func(ctx *cli.Context) error { return app.RunWithLogging(ctx, false, app.Put) },
			},
			{
				Name:               "import",
				Usage:              "Import parameter values from stdin",
				UsageText:          "aws-ssm-params [global options] import [command options]",
				CustomHelpTemplate: alignedCommandHelpTemplate,
				Before: func(ctx *cli.Context) error {
					return app.RejectCommaSeparatedFlagArgs(ctx.Args().Slice(), "filter", "field")
				},
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "filters-file", EnvVars: []string{"AWS_SSM_PARAMS_FILTERS_FILE"}, Usage: "file with filter groups; one OR group per line"},
					&cli.StringSliceFlag{Name: "filter", EnvVars: []string{"AWS_SSM_PARAMS_FILTERS", "AWS_SSM_PARAMS_FILTER"}, Usage: "filter group; conditions inside one value are separated by semicolons; env accepts comma-separated values"},
					&cli.StringSliceFlag{Name: "field", Usage: "field mapping as aws_field[:file_field]; repeat for multiple fields"},
					&cli.StringFlag{Name: "format", Value: "dotenv", Usage: "input format: dotenv or json"},
					&cli.StringFlag{Name: "json-key-field", Usage: "AWS field to use as JSON object key instead of JSON array records"},
					&cli.StringFlag{Name: "default-value", Usage: "default value for imported records without value field"},
					&cli.StringFlag{Name: "default-value-file", Usage: "read default value from file"},
					&cli.BoolFlag{Name: "default-override", Usage: "overwrite existing non-empty values when imported records do not specify override metadata"},
					&cli.StringFlag{Name: "default-type", Usage: "default parameter type: string, string-list, or secure-string"},
					&cli.StringFlag{Name: "default-tier", Usage: "default parameter tier: standard, advanced, or intelligent-tiering"},
					&cli.StringFlag{Name: "default-data-type", Usage: "default parameter data type: text, aws:ec2:image, or aws:ssm:integration"},
					&cli.StringFlag{Name: "default-region", Usage: "default AWS region for imported records without region metadata"},
					&cli.StringFlag{Name: "default-description", Usage: "default parameter description"},
					&cli.StringFlag{Name: "default-policies", Usage: "default parameter policies JSON"},
					&cli.StringFlag{Name: "default-policies-file", Usage: "read default parameter policies JSON from file"},
				},
				Action: func(ctx *cli.Context) error { return app.RunWithLogging(ctx, false, app.Import) },
			},
			{
				Name:               "export",
				Usage:              "Export parameter values to stdout",
				UsageText:          "aws-ssm-params [global options] export [command options]",
				CustomHelpTemplate: alignedCommandHelpTemplate,
				Before: func(ctx *cli.Context) error {
					return app.RejectCommaSeparatedFlagArgs(ctx.Args().Slice(), "filter", "field")
				},
				Flags: []cli.Flag{
					&cli.StringSliceFlag{Name: "filter", EnvVars: []string{"AWS_SSM_PARAMS_FILTERS", "AWS_SSM_PARAMS_FILTER"}, Usage: "filter group; conditions inside one value are separated by semicolons; env accepts comma-separated values"},
					&cli.StringSliceFlag{Name: "field", Usage: "field mapping as aws_field[:file_field]; repeat for multiple fields"},
					&cli.BoolFlag{Name: "with-decryption", EnvVars: []string{"AWS_SSM_PARAMS_WITH_DECRYPTION"}, Usage: "decrypt SecureString values"},
					&cli.StringFlag{Name: "format", Value: "dotenv", Usage: "output format: dotenv or json"},
					&cli.StringFlag{Name: "json-key-field", Usage: "AWS field to use as JSON object key instead of JSON array records"},
					&cli.BoolFlag{Name: "include-missing", Usage: "include missing parameters as empty records"},
				},
				Action: func(ctx *cli.Context) error { return app.RunWithLogging(ctx, false, app.Export) },
			},
		},
	}
}

func main() {
	ctx := context.Background()
	cliApp := newCLIApp(os.Args[1:])
	if err := cliApp.RunContext(ctx, os.Args); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
}
