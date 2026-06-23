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
	*app.Options

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
	opts *Options
}

// Run opens the terminal UI.
func Run(ctx context.Context, opts *Options) error {
	r := runner{opts: opts}
	return r.run(ctx)
}

func (r *runner) run(ctx context.Context) error {
	if err := r.prepare(ctx); err != nil {
		return err
	}

	opts := r.opts.Options

	client := ssm.NewClient(ssm.ClientConfig{
		Profile:        opts.Profile,
		Region:         opts.Region,
		WithDecryption: opts.WithDecryption,
		Logger:         opts.Logger,
	})
	if err := client.CheckAccess(ctx); err != nil {
		return errors.Wrap(err, "check AWS access")
	}

	regionLabel, regions, err := r.regionSelection(ctx, client)
	if err != nil {
		return err
	}

	return errors.Wrap(
		ui.RunInteractive(ctx, client, r.opts.InventoryItems, r.uiOptions(regionLabel, regions)),
		"run interactive",
	)
}

func (r *runner) prepare(ctx context.Context) error {
	opts := r.opts.Options

	items, err := opts.PrepareItems(ctx)
	if err != nil {
		return errors.WithStack(err)
	}

	r.opts.InventoryItems = items

	return nil
}

func (r runner) regionSelection(ctx context.Context, client ssm.Client) (regionLabel string, regions []string, err error) {
	regionLabel = r.opts.Region

	regions = append([]string(nil), r.opts.Regions...)
	if r.opts.AllRegions {
		regionLabel = "all regions"

		regions, err = client.ListRegions(ctx)
		if err != nil {
			return "", nil, errors.Wrap(err, "list AWS regions")
		}
	} else if len(regions) > 1 {
		regionLabel = strings.Join(regions, ", ")
	}

	return regionLabel, regions, nil
}

func (r runner) uiOptions(regionLabel string, regions []string) *ui.Options {
	opts := r.opts

	return &ui.Options{
		Region:                    regionLabel,
		Regions:                   regions,
		Profile:                   opts.Profile,
		FilterGroups:              opts.FilterGroups,
		NoColor:                   opts.NoColor,
		Keymap:                    opts.Keymap,
		ShowColumns:               opts.ShowColumns,
		Sort:                      opts.SortColumns,
		Fields:                    opts.Fields,
		IncludeValues:             opts.WithDecryption || opts.Fields.RequiresValues() || opts.FilterGroups.HasField(filter.FieldValue),
		ShowSecureValues:          opts.WithDecryption,
		NoConfirmOverwriteFile:    opts.NoConfirmOverwriteFile,
		NoConfirmWriteSecureValue: opts.NoConfirmWriteSecureValue,
		NoConfirmDeleteOne:        opts.NoConfirmDeleteOne,
		NoConfirmDeleteAll:        opts.NoConfirmDeleteAll,
		UseInputTTY:               opts.UseInputTTY,
	}
}
