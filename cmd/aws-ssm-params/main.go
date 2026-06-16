package main

import (
	"fmt"
	"os"

	"github.com/biptec/aws-ssm-params/internal/app"
	"github.com/urfave/cli/v2"
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

// main builds the command-line application definition and delegates all business logic to the internal app package.
// The CLI intentionally stays thin: it declares global flags, subcommands, usage text, and error handling,
// while region resolution, inventory loading, AWS access, import/export, and the TUI live in internal packages.
func main() {
	cliApp := &cli.App{
		Name:                  "aws-ssm-params",
		Usage:                 "Manage AWS SSM parameters",
		UsageText:             "aws-ssm-params [global options] <command> [command options]",
		EnableBashCompletion:  true,
		CustomAppHelpTemplate: alignedAppHelpTemplate,
		Flags: []cli.Flag{
			&cli.StringSliceFlag{Name: "region", EnvVars: []string{"AWS_SSM_PARAMS_REGION"}, Usage: "AWS region; repeat or comma-separate to scan multiple regions; when omitted, AWS CLI resolves it from env/profile config"},
			&cli.BoolFlag{Name: "all-regions", EnvVars: []string{"AWS_SSM_PARAMS_ALL_REGIONS"}, Usage: "search parameters across all enabled AWS regions"},
			&cli.StringFlag{Name: "profile", EnvVars: []string{"AWS_SSM_PARAMS_PROFILE", "AWS_PROFILE"}, Usage: "AWS profile"},
			&cli.BoolFlag{Name: "no-color", EnvVars: []string{"AWS_SSM_PARAMS_NO_COLOR", "NO_COLOR"}, Usage: "disable colored output"},
			&cli.StringFlag{Name: "keymap", Value: "emacs", EnvVars: []string{"AWS_SSM_PARAMS_KEYMAP"}, Usage: "keyboard navigation style: emacs or vi"},
			&cli.StringFlag{Name: "paths-file", EnvVars: []string{"AWS_SSM_PARAMS_PATHS_FILE"}, Usage: "optional file with SSM parameter paths to load/filter"},
			&cli.StringFlag{Name: "columns", EnvVars: []string{"AWS_SSM_PARAMS_COLUMNS"}, Usage: "comma-separated optional columns to show in the TUI"},
			&cli.BoolFlag{Name: "allow-paths-file-update", EnvVars: []string{"AWS_SSM_PARAMS_ALLOW_PATHS_FILE_UPDATE"}, Usage: "allow the TUI to update --paths-file when creating or deleting parameters"},
		},
		Action: app.Interactive,
		Commands: []*cli.Command{
			{
				Name:               "get",
				Usage:              "Print a parameter value",
				UsageText:          "aws-ssm-params [global options] get <path> [--file path]",
				CustomHelpTemplate: alignedCommandHelpTemplate,
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "file", Usage: "write value to file instead of stdout"},
				},
				Action: app.Get,
			},
			{
				Name:               "set",
				Usage:              "Set a String, StringList, or SecureString parameter value",
				UsageText:          "aws-ssm-params [global options] set <path> <value> [--type string|string-list|secure-string] [--override]\n   aws-ssm-params [global options] set <path> --file path [--type string|string-list|secure-string] [--override]",
				CustomHelpTemplate: alignedCommandHelpTemplate,
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "file", Usage: "read value from file"},
					&cli.BoolFlag{Name: "override", Usage: "overwrite existing non-empty values"},
					&cli.StringFlag{Name: "type", Aliases: []string{"t"}, Usage: "parameter type: string, string-list, or secure-string; existing type is preserved when omitted"},
				},
				Action: app.Set,
			},
			{
				Name:               "import",
				Usage:              "Import parameter values from stdin or file",
				UsageText:          "aws-ssm-params [global options] import [--format dotenv] [--file path] [--paths-file paths.txt] [--type string|string-list|secure-string] [--override]",
				CustomHelpTemplate: alignedCommandHelpTemplate,
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "file", Usage: "read import data from file; stdin is used when omitted"},
					&cli.StringFlag{Name: "format", Value: "dotenv", Usage: "input format: dotenv or json"},
					&cli.BoolFlag{Name: "override", Usage: "overwrite existing non-empty values"},
					&cli.StringFlag{Name: "type", Aliases: []string{"t"}, Usage: "default parameter type for imported records without type metadata: string, string-list, or secure-string"},
				},
				Action: app.Import,
			},
			{
				Name:               "export",
				Usage:              "Export parameter values to stdout or file",
				UsageText:          "aws-ssm-params [global options] export [--format dotenv|json] [--file path] [--include-missing] [--paths-file paths.txt]",
				CustomHelpTemplate: alignedCommandHelpTemplate,
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "file", Usage: "write export data to file; stdout is used when omitted"},
					&cli.StringFlag{Name: "format", Value: "dotenv", Usage: "output format: dotenv or json"},
					&cli.BoolFlag{Name: "include-missing", Usage: "include missing parameters as empty values"},
				},
				Action: app.Export,
			},
		},
	}

	if err := cliApp.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
}
