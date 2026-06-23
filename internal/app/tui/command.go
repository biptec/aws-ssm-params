// Package tui implements the interactive terminal command.
package tui

import (
	"context"
	"strings"

	"github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/app"
	"github.com/biptec/aws-ssm-params/internal/filter"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/biptec/aws-ssm-params/internal/textio"
	"github.com/biptec/aws-ssm-params/internal/ui"
)

// Options contains the complete runtime configuration for one TUI session.
type Options struct {
	Config                    app.Config
	NoColor                   bool
	Keymap                    string
	ShowColumns               []string
	SortColumns               []string
	Fields                    textio.Fields
	NoConfirmOverwriteFile    bool
	NoConfirmWriteSecureValue bool
	NoConfirmDeleteOne        bool
	NoConfirmDeleteAll        bool
	UseInputTTY               bool
}

// Command owns the configuration and input mode of one TUI invocation.
type Command struct {
	ctx     context.Context
	options Options
}

// Run opens the terminal UI.
func Run(ctx context.Context, options Options) error {
	command := Command{ctx: ctx, options: options}
	return command.run()
}

func (command *Command) run() error {
	if err := command.prepare(); err != nil {
		return err
	}
	client := app.NewClient(command.options.Config)
	if err := client.CheckAccess(command.ctx); err != nil {
		return errors.Wrap(err, "check AWS access")
	}
	regionLabel, regions, err := command.regionSelection(client)
	if err != nil {
		return err
	}
	return errors.Wrap(
		ui.RunInteractive(command.ctx, client, command.options.Config.InventoryItems, command.uiOptions(regionLabel, regions)),
		"run interactive",
	)
}

func (command *Command) prepare() error {
	cfg := command.options.Config
	items, err := app.PrepareItems(command.ctx, &cfg)
	if err != nil {
		return errors.WithStack(err)
	}
	cfg.InventoryItems = items
	command.options.Config = cfg
	return nil
}

func (command Command) regionSelection(client ssm.Client) (regionLabel string, regions []string, err error) {
	cfg := command.options.Config
	regionLabel = cfg.Region
	regions = append([]string(nil), cfg.Regions...)
	if cfg.AllRegions {
		regionLabel = "all regions"
		regions, err = client.ListRegions(command.ctx)
		if err != nil {
			return "", nil, errors.Wrap(err, "list AWS regions")
		}
	} else if len(regions) > 1 {
		regionLabel = strings.Join(regions, ", ")
	}
	return regionLabel, regions, nil
}

func (command Command) uiOptions(regionLabel string, regions []string) ui.Options {
	cfg := command.options.Config
	options := command.options
	return ui.Options{
		Region:                    regionLabel,
		Regions:                   regions,
		Profile:                   cfg.Profile,
		FilterGroups:              cfg.FilterGroups,
		NoColor:                   options.NoColor,
		Keymap:                    options.Keymap,
		ShowColumns:               options.ShowColumns,
		Sort:                      options.SortColumns,
		Fields:                    options.Fields,
		IncludeValues:             cfg.WithDecryption || options.Fields.RequiresValues() || cfg.FilterGroups.HasField(filter.FieldValue),
		ShowSecureValues:          cfg.WithDecryption,
		NoConfirmOverwriteFile:    options.NoConfirmOverwriteFile,
		NoConfirmWriteSecureValue: options.NoConfirmWriteSecureValue,
		NoConfirmDeleteOne:        options.NoConfirmDeleteOne,
		NoConfirmDeleteAll:        options.NoConfirmDeleteAll,
		UseInputTTY:               options.UseInputTTY,
	}
}
