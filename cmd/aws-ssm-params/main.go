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
// The root command intentionally only shows help; users must choose interactive, import, export, set, or get explicitly.
func newCLIApp(rawArgs []string) *cli.App {
	return &cli.App{
		Name:                  "aws-ssm-params",
		Usage:                 "Manage AWS SSM parameters",
		UsageText:             "aws-ssm-params [global options] <command> [command options]",
		EnableBashCompletion:  true,
		CustomAppHelpTemplate: alignedAppHelpTemplate,
		Before: func(ctx *cli.Context) error {
			return app.RejectRepeatedFlagArgs(rawArgs, "regions")
		},
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "regions", EnvVars: []string{"AWS_SSM_PARAMS_REGIONS"}, Usage: "comma-separated AWS regions; when omitted, AWS SDK resolves it from env/profile config"},
			&cli.BoolFlag{Name: "all-regions", EnvVars: []string{"AWS_SSM_PARAMS_ALL_REGIONS"}, Usage: "search parameters across all enabled AWS regions"},
			&cli.StringFlag{Name: "profile", EnvVars: []string{"AWS_SSM_PARAMS_PROFILE", "AWS_PROFILE"}, Usage: "AWS profile"},
			&cli.BoolFlag{Name: "no-color", EnvVars: []string{"AWS_SSM_PARAMS_NO_COLOR", "NO_COLOR"}, Usage: "disable colored output"},
			&cli.StringFlag{Name: "names-file", EnvVars: []string{"AWS_SSM_PARAMS_NAMES_FILE"}, Usage: "optional file with SSM parameter names to load/filter"},
			&cli.StringSliceFlag{Name: "names", EnvVars: []string{"AWS_SSM_PARAMS_NAMES"}, Usage: "comma-separated SSM parameter names to load/filter"},
			&cli.StringSliceFlag{Name: "fields", EnvVars: []string{"AWS_SSM_PARAMS_FIELDS"}, Usage: "comma-separated parameter fields to load/show/import/export; omitted means all fields"},
			&cli.BoolFlag{Name: "without-decryption", EnvVars: []string{"AWS_SSM_PARAMS_WITHOUT_DECRYPTION"}, Usage: "load SecureString values without KMS decryption"},
		},
		Action: func(ctx *cli.Context) error {
			return cli.ShowAppHelp(ctx)
		},
		Commands: []*cli.Command{
			{
				Name:               "interactive",
				Aliases:            []string{"tui"},
				Usage:              "Open the interactive TUI",
				UsageText:          "aws-ssm-params [global options] interactive [command options]",
				CustomHelpTemplate: alignedCommandHelpTemplate,
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "show-columns", EnvVars: []string{"AWS_SSM_PARAMS_SHOW_COLUMNS"}, Usage: "comma-separated optional columns to show in the TUI"},
					&cli.StringFlag{Name: "sort", EnvVars: []string{"AWS_SSM_PARAMS_SORT"}, Usage: "initial sort as field,asc or field,desc"},
					&cli.StringFlag{Name: "keymap", Value: "emacs", EnvVars: []string{"AWS_SSM_PARAMS_KEYMAP"}, Usage: "keyboard navigation style: emacs or vi"},
					&cli.BoolFlag{Name: "allow-names-file-update", EnvVars: []string{"AWS_SSM_PARAMS_ALLOW_NAMES_FILE_UPDATE"}, Usage: "allow the TUI to update --names-file when creating or deleting parameters"},
					&cli.BoolFlag{Name: "show-secure-values", EnvVars: []string{"AWS_SSM_PARAMS_SHOW_SECURE_VALUES"}, Usage: "show SecureString values by default in the TUI"},
					&cli.BoolFlag{Name: "no-confirm-overwrite-file", EnvVars: []string{"AWS_SSM_PARAMS_NO_CONFIRM_OVERWRITE_FILE"}, Usage: "do not ask before overwriting local files from the TUI"},
					&cli.BoolFlag{Name: "no-confirm-write-securestring", EnvVars: []string{"AWS_SSM_PARAMS_NO_CONFIRM_WRITE_SECURESTRING"}, Usage: "do not ask before writing SecureString values to local files in plaintext"},
					&cli.BoolFlag{Name: "no-confirm-delete-one", EnvVars: []string{"AWS_SSM_PARAMS_NO_CONFIRM_DELETE_ONE"}, Usage: "do not ask before deleting one parameter in the TUI"},
					&cli.BoolFlag{Name: "no-confirm-delete-all", EnvVars: []string{"AWS_SSM_PARAMS_NO_CONFIRM_DELETE_ALL"}, Usage: "do not ask before deleting all visible parameters in the TUI"},
				},
				Action: app.Interactive,
			},
			{
				Name:               "get",
				Usage:              "Print one selected parameter field",
				UsageText:          "aws-ssm-params [global options] get <name> [--field field] [--file path]",
				CustomHelpTemplate: alignedCommandHelpTemplate,
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "field", Value: "value", Usage: "single parameter field to print; default is value"},
					&cli.StringFlag{Name: "file", Usage: "write selected field to file instead of stdout"},
				},
				Action: app.Get,
			},
			{
				Name:               "set",
				Usage:              "Set a String, StringList, or SecureString parameter value",
				UsageText:          "aws-ssm-params [global options] set <name> <value> [command options]\n   aws-ssm-params [global options] set <name> --file path [command options]",
				CustomHelpTemplate: alignedCommandHelpTemplate,
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "file", Usage: "read value from file"},
					&cli.BoolFlag{Name: "override", Usage: "overwrite existing values"},
					&cli.StringFlag{Name: "type", Aliases: []string{"t"}, Usage: "parameter type: string, string-list, or secure-string; existing type is preserved when omitted"},
					&cli.StringFlag{Name: "tier", Usage: "parameter tier: standard, advanced, or intelligent-tiering"},
					&cli.StringFlag{Name: "data-type", Usage: "parameter data type: text, aws:ec2:image, or aws:ssm:integration"},
					&cli.StringFlag{Name: "region", Usage: "target AWS region; overrides the global primary region for this write"},
					&cli.StringFlag{Name: "description", Usage: "parameter description"},
					&cli.StringFlag{Name: "policies", Usage: "parameter policies JSON"},
					&cli.StringFlag{Name: "policies-file", Usage: "read parameter policies JSON from file"},
				},
				Action: app.Set,
			},
			{
				Name:               "import",
				Usage:              "Import parameter values from stdin or file",
				UsageText:          "aws-ssm-params [global options] import --from-file path [command options]",
				CustomHelpTemplate: alignedCommandHelpTemplate,
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "from-file", Usage: "read import data from file; stdin is used when omitted"},
					&cli.StringFlag{Name: "format", Value: "dotenv", Usage: "input format: dotenv or json"},
					&cli.BoolFlag{Name: "default-override", Usage: "overwrite existing non-empty values when imported records do not specify override metadata"},
					&cli.StringFlag{Name: "default-type", Aliases: []string{"t"}, Usage: "default parameter type: string, string-list, or secure-string"},
					&cli.StringFlag{Name: "default-tier", Usage: "default parameter tier: standard, advanced, or intelligent-tiering"},
					&cli.StringFlag{Name: "default-data-type", Usage: "default parameter data type: text, aws:ec2:image, or aws:ssm:integration"},
					&cli.StringFlag{Name: "default-region", Usage: "default AWS region for imported records without region metadata"},
					&cli.StringFlag{Name: "default-description", Usage: "default parameter description"},
				},
				Action: app.Import,
			},
			{
				Name:               "export",
				Usage:              "Export parameter values to stdout or file",
				UsageText:          "aws-ssm-params [global options] export [--format dotenv|json] [--to-file path] [--include-missing]",
				CustomHelpTemplate: alignedCommandHelpTemplate,
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "to-file", Usage: "write export data to file; stdout is used when omitted"},
					&cli.StringFlag{Name: "format", Value: "dotenv", Usage: "output format: dotenv or json"},
					&cli.BoolFlag{Name: "include-missing", Usage: "include missing parameters as empty values"},
				},
				Action: app.Export,
			},
		},
	}

}

func main() {
	cliApp := newCLIApp(os.Args[1:])
	if err := cliApp.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
}
