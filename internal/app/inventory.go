package app

import (
	"context"
	"strings"

	"github.com/biptec/aws-ssm-params/internal/inventory"
)

// LoadItems builds explicit inventory items from configured sources.
// When no inventory is configured, callers can still discover parameters directly from AWS SSM.
func LoadItems(cfg Config) ([]inventory.Item, error) {
	seen := map[string]bool{}
	items := []inventory.Item{}
	add := func(item inventory.Item) {
		item.Path = strings.TrimSpace(item.Path)
		if item.Path == "" || seen[item.Path] {
			return
		}
		seen[item.Path] = true
		items = append(items, item)
	}

	for _, item := range cfg.InventoryItems {
		add(item)
	}
	return items, nil
}

// PrepareItems resolves regions and explicit names for commands that load SSM parameters.
func PrepareItems(ctx context.Context, cfg *Config) ([]inventory.Item, error) {
	items, err := LoadItems(*cfg)
	if err != nil {
		return nil, err
	}
	if cfg.AllRegions {
		ensureAllRegionsSeedRegion(cfg)
		return applyInventoryRegion(items, "*"), nil
	}
	if err := ensureRegions(ctx, cfg); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	if len(cfg.Regions) > 1 {
		return applyInventoryRegion(items, "*"), nil
	}
	return applyInventoryRegion(items, cfg.Region), nil
}

func applyInventoryRegion(items []inventory.Item, region string) []inventory.Item {
	if len(items) == 0 {
		return nil
	}
	out := make([]inventory.Item, len(items))
	copy(out, items)
	for i := range out {
		if out[i].Region == "" {
			out[i].Region = region
		}
	}
	return out
}
