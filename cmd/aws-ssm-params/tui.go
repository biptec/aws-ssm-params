package main

import (
	"context"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/urfave/cli/v3"

	tuicmd "github.com/biptec/aws-ssm-params/internal/app/tui"
	"github.com/biptec/aws-ssm-params/internal/ui"
)

const (
	tuiCommandName = "tui"

	tuiFlagWithDecryption            = "with-decryption"
	tuiFlagShowColumn                = "show-column"
	tuiFlagSortBy                    = "sort-by"
	tuiFlagNoConfirmOverwriteFile    = "no-confirm-overwrite-file"
	tuiFlagNoConfirmWriteSecureValue = "no-confirm-write-securestring"
	tuiFlagNoConfirmDeleteOne        = "no-confirm-delete-one"
	tuiFlagNoConfirmDeleteAll        = "no-confirm-delete-all"

	tuiEnvWithDecryption            = envVarPrefix + "WITH_DECRYPTION"
	tuiEnvShowColumn                = envVarPrefix + "SHOW_COLUMN"
	tuiEnvSortBy                    = envVarPrefix + "SORT_BY"
	tuiEnvNoConfirmOverwriteFile    = envVarPrefix + "NO_CONFIRM_OVERWRITE_FILE"
	tuiEnvNoConfirmWriteSecureValue = envVarPrefix + "NO_CONFIRM_WRITE_SECURESTRING"
	tuiEnvNoConfirmDeleteOne        = envVarPrefix + "NO_CONFIRM_DELETE_ONE"
	tuiEnvNoConfirmDeleteAll        = envVarPrefix + "NO_CONFIRM_DELETE_ALL"
)

func tuiFlags() []cli.Flag {
	flags := []cli.Flag{
		&cli.BoolFlag{Name: tuiFlagWithDecryption, Sources: cli.EnvVars(tuiEnvWithDecryption), Usage: "decrypt SecureString values"},
		&cli.StringSliceFlag{Name: tuiFlagShowColumn, Sources: cli.EnvVars(tuiEnvShowColumn), Usage: "optional column to show in the TUI; repeat for multiple columns; env accepts comma-separated values"},
		&cli.StringSliceFlag{Name: tuiFlagSortBy, Sources: cli.EnvVars(tuiEnvSortBy), Usage: "initial sort as field:asc or field:desc; repeat for multiple fields; env accepts comma-separated values"},
		&cli.BoolFlag{Name: tuiFlagNoConfirmOverwriteFile, Sources: cli.EnvVars(tuiEnvNoConfirmOverwriteFile), Usage: "do not ask before overwriting local files from the TUI"},
		&cli.BoolFlag{Name: tuiFlagNoConfirmWriteSecureValue, Sources: cli.EnvVars(tuiEnvNoConfirmWriteSecureValue), Usage: "do not ask before writing SecureString values to local files in plaintext"},
		&cli.BoolFlag{Name: tuiFlagNoConfirmDeleteOne, Sources: cli.EnvVars(tuiEnvNoConfirmDeleteOne), Usage: "do not ask before deleting one parameter in the TUI"},
		&cli.BoolFlag{Name: tuiFlagNoConfirmDeleteAll, Sources: cli.EnvVars(tuiEnvNoConfirmDeleteAll), Usage: "do not ask before deleting all visible parameters in the TUI"},
	}

	sort.Sort(cli.FlagsByName(flags))

	return flags
}

func tuiCommand() *cli.Command {
	return &cli.Command{
		Name:      tuiCommandName,
		Usage:     "Open the TUI",
		UsageText: appName + " [global options] " + tuiCommandName + " [command options]",
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			return ctx, rejectCommaSeparatedFlagArgs(cmd.Args().Slice(), tuiFlagShowColumn)
		},
		Flags: tuiFlags(),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return runWithLogging(ctx, cmd, true, func(ctx context.Context) error {
				options, err := tuiOptionsFromCLI(ctx, cmd)
				if err != nil {
					return err
				}

				return tuicmd.Run(ctx, options)
			})
		},
	}
}

func tuiOptionsFromCLI(ctx context.Context, cmd *cli.Command) (*tuicmd.Options, error) {
	global, err := globalOptionsFromCLI(ctx, cmd)
	if err != nil {
		return &tuicmd.Options{}, err
	}

	showColumns, err := ui.ParseColumnOption(strings.Join(
		stringSliceFlagValue(cmd, tuiFlagShowColumn, tuiEnvShowColumn),
		",",
	))
	if err != nil {
		return &tuicmd.Options{}, errors.Wrap(err, "parse show columns")
	}

	stdinImport, useInputTTY, err := loadTUIImportFromStdin()
	if err != nil {
		return &tuicmd.Options{}, err
	}

	global.WithDecryption = boolFlagValueAny(
		cmd,
		tuiFlagWithDecryption,
		tuiEnvWithDecryption,
	)

	return &tuicmd.Options{
		Options:                   global.Options,
		NoColor:                   global.NoColor,
		Keymap:                    global.Keymap,
		ShowColumns:               showColumns,
		SortColumns:               compactStrings(stringSliceFlagValue(cmd, tuiFlagSortBy, tuiEnvSortBy)),
		NoConfirmOverwriteFile:    boolFlagValueAny(cmd, tuiFlagNoConfirmOverwriteFile, tuiEnvNoConfirmOverwriteFile),
		NoConfirmWriteSecureValue: boolFlagValueAny(cmd, tuiFlagNoConfirmWriteSecureValue, tuiEnvNoConfirmWriteSecureValue),
		NoConfirmDeleteOne:        boolFlagValueAny(cmd, tuiFlagNoConfirmDeleteOne, tuiEnvNoConfirmDeleteOne),
		NoConfirmDeleteAll:        boolFlagValueAny(cmd, tuiFlagNoConfirmDeleteAll, tuiEnvNoConfirmDeleteAll),
		UseInputTTY:               useInputTTY,
		ImportStdin:               stdinImport,
	}, nil
}

func loadTUIImportFromStdin() ([]byte, bool, error) {
	info, err := os.Stdin.Stat()
	if err != nil {
		return nil, false, errors.Wrap(err, "stat stdin")
	}

	if info.Mode()&os.ModeCharDevice != 0 {
		return nil, false, nil
	}

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil, true, errors.Wrap(err, "read TUI import from stdin")
	}

	return data, true, nil
}
