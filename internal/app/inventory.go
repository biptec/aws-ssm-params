package app

import (
	"context"

	"github.com/biptec/aws-ssm-params/internal/inventory"
)

// PrepareItems resolves regions and explicit names for commands that load SSM parameters.
func PrepareItems(ctx context.Context, cfg *Config) (inventory.Items, error) {
	items := cfg.InventoryItems.UniqueByPath()
	if cfg.AllRegions {
		ensureAllRegionsSeedRegion(cfg)
		return items.WithDefaultRegion("*"), nil
	}
	if err := ensureRegions(ctx, cfg); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	if len(cfg.Regions) > 1 {
		return items.WithDefaultRegion("*"), nil
	}
	return items.WithDefaultRegion(cfg.Region), nil
}
