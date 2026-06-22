package app

import (
	"os"
	"strings"

	crerr "github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/ui"
)

// Interactive opens the terminal UI.
func Interactive(ctx *CLIContext) error {
	cfg, err := ConfigFromCLI(ctx)
	if err != nil {
		return err
	}
	stdinItems, useTTYInput, err := loadInteractiveInventoryFromStdin()
	if err != nil {
		return err
	}
	cfg.InventoryItems = append(cfg.InventoryItems, stdinItems...)
	items, err := PrepareItems(ctx.Context, &cfg)
	if err != nil {
		return err
	}
	client := NewClient(cfg)
	if err := client.CheckAccess(ctx.Context); err != nil {
		return crerr.Wrap(err, "check AWS access")
	}
	regionLabel := cfg.Region
	regions := append([]string(nil), cfg.Regions...)
	if cfg.AllRegions {
		regionLabel = "all regions"
		regions, err = client.ListRegions(ctx.Context)
		if err != nil {
			return crerr.Wrap(err, "list AWS regions")
		}
	} else if len(regions) > 1 {
		regionLabel = strings.Join(regions, ", ")
	}
	return crerr.Wrap(ui.RunInteractive(ctx.Context, client, items, ui.Options{
		Region:                    regionLabel,
		Regions:                   regions,
		Profile:                   cfg.Profile,
		FilterGroups:              cfg.FilterGroups,
		NoColor:                   cfg.NoColor,
		Keymap:                    cfg.Keymap,
		ShowColumns:               cfg.ShowColumns,
		Sort:                      cfg.SortColumns,
		Fields:                    cfg.Fields,
		IncludeValues:             cfg.WithDecryption || includeValuesForFields(cfg.Fields) || includeValuesForFilterGroups(cfg.FilterGroups),
		ShowSecureValues:          cfg.WithDecryption,
		NoConfirmOverwriteFile:    cfg.NoConfirmOverwriteFile,
		NoConfirmWriteSecureValue: cfg.NoConfirmWriteSecureValue,
		NoConfirmDeleteOne:        cfg.NoConfirmDeleteOne,
		NoConfirmDeleteAll:        cfg.NoConfirmDeleteAll,
		UseInputTTY:               useTTYInput,
	}), "run interactive")
}

func loadInteractiveInventoryFromStdin() ([]inventory.Item, bool, error) {
	info, err := os.Stdin.Stat()
	if err != nil {
		return nil, false, crerr.Wrap(err, "stat stdin")
	}
	if info.Mode()&os.ModeCharDevice != 0 {
		return nil, false, nil
	}

	items, err := inventory.LoadPaths(os.Stdin, "stdin")
	if err != nil {
		return nil, true, crerr.Wrap(err, "load TUI inventory from stdin")
	}
	return items, true, nil
}
