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

// runner owns the configuration and input mode of one TUI invocation.
type runner struct {
	ctx     context.Context
	options Options
}

// Run opens the terminal UI.
func Run(ctx context.Context, options Options) error {
	r := runner{ctx: ctx, options: options}
	return r.run()
}

func (r *runner) run() error {
	if err := r.prepare(); err != nil {
		return err
	}
	cfg := r.options.Config
	client := ssm.NewClient(ssm.ClientConfig{
		Profile:        cfg.Profile,
		Region:         cfg.Region,
		WithDecryption: cfg.WithDecryption,
		Logger:         cfg.Logger,
	})
	if err := client.CheckAccess(r.ctx); err != nil {
		return errors.Wrap(err, "check AWS access")
	}
	regionLabel, regions, err := r.regionSelection(client)
	if err != nil {
		return err
	}
	return errors.Wrap(
		ui.RunInteractive(r.ctx, client, r.options.Config.InventoryItems, r.uiOptions(regionLabel, regions)),
		"run interactive",
	)
}

func (r *runner) prepare() error {
	cfg := r.options.Config
	items, err := app.PrepareItems(r.ctx, &cfg)
	if err != nil {
		return errors.WithStack(err)
	}
	cfg.InventoryItems = items
	r.options.Config = cfg
	return nil
}

func (r runner) regionSelection(client ssm.Client) (regionLabel string, regions []string, err error) {
	cfg := r.options.Config
	regionLabel = cfg.Region
	regions = append([]string(nil), cfg.Regions...)
	if cfg.AllRegions {
		regionLabel = "all regions"
		regions, err = client.ListRegions(r.ctx)
		if err != nil {
			return "", nil, errors.Wrap(err, "list AWS regions")
		}
	} else if len(regions) > 1 {
		regionLabel = strings.Join(regions, ", ")
	}
	return regionLabel, regions, nil
}

func (r runner) uiOptions(regionLabel string, regions []string) ui.Options {
	cfg := r.options.Config
	options := r.options
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
