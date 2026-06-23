// Package tui implements the interactive terminal command.
package tui

import (
	"os"
	"strings"

	"github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/app"
	"github.com/biptec/aws-ssm-params/internal/filter"
	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/biptec/aws-ssm-params/internal/ui"
)

// Command owns the configuration and input mode of one TUI invocation.
type Command struct {
	ctx         *app.CLIContext
	cfg         app.Config
	useTTYInput bool
}

// Run opens the terminal UI.
func Run(ctx *app.CLIContext) error {
	command := Command{ctx: ctx}
	return command.run()
}

func (command *Command) run() error {
	if err := command.prepare(); err != nil {
		return err
	}
	client := app.NewClient(command.cfg)
	if err := client.CheckAccess(command.ctx.Context); err != nil {
		return errors.Wrap(err, "check AWS access")
	}
	regionLabel, regions, err := command.regionSelection(client)
	if err != nil {
		return err
	}
	return errors.Wrap(ui.RunInteractive(command.ctx.Context, client, command.cfg.InventoryItems, command.options(regionLabel, regions)), "run interactive")
}

func (command *Command) prepare() error {
	cfg, err := app.ConfigFromCLI(command.ctx)
	if err != nil {
		return errors.WithStack(err)
	}
	stdinItems, useTTYInput, err := loadInventoryFromStdin()
	if err != nil {
		return err
	}
	cfg.InventoryItems = append(cfg.InventoryItems, stdinItems...)
	items, err := app.PrepareItems(command.ctx.Context, &cfg)
	if err != nil {
		return errors.WithStack(err)
	}
	cfg.InventoryItems = items
	command.cfg = cfg
	command.useTTYInput = useTTYInput
	return nil
}

func (command Command) regionSelection(client ssm.Client) (regionLabel string, regions []string, err error) {
	regionLabel = command.cfg.Region
	regions = append([]string(nil), command.cfg.Regions...)
	if command.cfg.AllRegions {
		regionLabel = "all regions"
		regions, err = client.ListRegions(command.ctx.Context)
		if err != nil {
			return "", nil, errors.Wrap(err, "list AWS regions")
		}
	} else if len(regions) > 1 {
		regionLabel = strings.Join(regions, ", ")
	}
	return regionLabel, regions, nil
}

func (command Command) options(regionLabel string, regions []string) ui.Options {
	cfg := command.cfg
	return ui.Options{
		Region:                    regionLabel,
		Regions:                   regions,
		Profile:                   cfg.Profile,
		FilterGroups:              cfg.FilterGroups,
		NoColor:                   cfg.NoColor,
		Keymap:                    cfg.Keymap,
		ShowColumns:               cfg.ShowColumns,
		Sort:                      cfg.SortColumns,
		Fields:                    cfg.Fields,
		IncludeValues:             cfg.WithDecryption || cfg.Fields.RequiresValues() || cfg.FilterGroups.HasField(filter.FieldValue),
		ShowSecureValues:          cfg.WithDecryption,
		NoConfirmOverwriteFile:    cfg.NoConfirmOverwriteFile,
		NoConfirmWriteSecureValue: cfg.NoConfirmWriteSecureValue,
		NoConfirmDeleteOne:        cfg.NoConfirmDeleteOne,
		NoConfirmDeleteAll:        cfg.NoConfirmDeleteAll,
		UseInputTTY:               command.useTTYInput,
	}
}

func loadInventoryFromStdin() (inventory.Items, bool, error) {
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
