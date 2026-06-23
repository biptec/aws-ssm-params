package main

import (
	"context"
	"os"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/urfave/cli/v3"

	tuicmd "github.com/biptec/aws-ssm-params/internal/app/tui"
	"github.com/biptec/aws-ssm-params/internal/inventory"
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

func tuiCLICommand() *cli.Command {
	return &cli.Command{
		Name:      tuiCommandName,
		Usage:     "Open the TUI",
		UsageText: appName + " [global options] " + tuiCommandName + " [command options]",
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			return ctx, rejectCommaSeparatedFlagArgs(cmd.Args().Slice(), tuiFlagShowColumn)
		},
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: tuiFlagWithDecryption, Sources: cli.EnvVars(tuiEnvWithDecryption), Usage: "decrypt SecureString values"},
			&cli.StringSliceFlag{Name: tuiFlagShowColumn, Sources: cli.EnvVars(tuiEnvShowColumn), Usage: "optional column to show in the TUI; repeat for multiple columns; env accepts comma-separated values"},
			&cli.StringSliceFlag{Name: tuiFlagSortBy, Sources: cli.EnvVars(tuiEnvSortBy), Usage: "initial sort as field:asc or field:desc; repeat for multiple fields; env accepts comma-separated values"},
			&cli.BoolFlag{Name: tuiFlagNoConfirmOverwriteFile, Sources: cli.EnvVars(tuiEnvNoConfirmOverwriteFile), Usage: "do not ask before overwriting local files from the TUI"},
			&cli.BoolFlag{Name: tuiFlagNoConfirmWriteSecureValue, Sources: cli.EnvVars(tuiEnvNoConfirmWriteSecureValue), Usage: "do not ask before writing SecureString values to local files in plaintext"},
			&cli.BoolFlag{Name: tuiFlagNoConfirmDeleteOne, Sources: cli.EnvVars(tuiEnvNoConfirmDeleteOne), Usage: "do not ask before deleting one parameter in the TUI"},
			&cli.BoolFlag{Name: tuiFlagNoConfirmDeleteAll, Sources: cli.EnvVars(tuiEnvNoConfirmDeleteAll), Usage: "do not ask before deleting all visible parameters in the TUI"},
		},
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
	stdinItems, useInputTTY, err := loadTUIInventoryFromStdin()
	if err != nil {
		return &tuicmd.Options{}, err
	}
	global.InventoryItems = append(global.InventoryItems, stdinItems...)
	global.WithDecryption = boolFlagValueAny(
		cmd,
		tuiFlagWithDecryption,
		false,
		tuiEnvWithDecryption,
	)
	return &tuicmd.Options{
		Options:                   global.Options,
		NoColor:                   global.NoColor,
		Keymap:                    global.Keymap,
		ShowColumns:               showColumns,
		SortColumns:               compactStrings(stringSliceFlagValue(cmd, tuiFlagSortBy, tuiEnvSortBy)),
		NoConfirmOverwriteFile:    boolFlagValueAny(cmd, tuiFlagNoConfirmOverwriteFile, false, tuiEnvNoConfirmOverwriteFile),
		NoConfirmWriteSecureValue: boolFlagValueAny(cmd, tuiFlagNoConfirmWriteSecureValue, false, tuiEnvNoConfirmWriteSecureValue),
		NoConfirmDeleteOne:        boolFlagValueAny(cmd, tuiFlagNoConfirmDeleteOne, false, tuiEnvNoConfirmDeleteOne),
		NoConfirmDeleteAll:        boolFlagValueAny(cmd, tuiFlagNoConfirmDeleteAll, false, tuiEnvNoConfirmDeleteAll),
		UseInputTTY:               useInputTTY,
	}, nil
}

func loadTUIInventoryFromStdin() (inventory.Items, bool, error) {
	info, err := os.Stdin.Stat()
	if err != nil {
		return nil, false, errors.Wrap(err, "stat stdin")
	}
	if info.Mode()&os.ModeCharDevice != 0 {
		return nil, false, nil
	}
	items, err := inventory.LoadPaths(os.Stdin, "stdin")
	if err != nil {
		return nil, true, errors.Wrap(err, "load TUI inventory from stdin")
	}
	return items, true, nil
}
