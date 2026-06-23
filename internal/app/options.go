// Package app contains runtime configuration and shared application services.
package app

import (
	"context"
	"log/slog"

	"github.com/biptec/aws-ssm-params/internal/filter"
	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/cockroachdb/errors"
)

const allRegionsSeedRegion = "us-east-1"

// Options contains runtime settings shared by all application commands.
// It is independent of any CLI, environment-variable, or configuration-file adapter.
type Options struct {
	Logger         *slog.Logger
	FilterGroups   filter.Groups
	InventoryItems inventory.Items
	Region         string
	Regions        []string
	Profile        string
	AllRegions     bool
	WithDecryption bool
}

// PrepareItems resolves regions and explicit names for commands that load SSM parameters.
func (cfg *Options) PrepareItems(ctx context.Context) (inventory.Items, error) {
	items := cfg.InventoryItems.UniqueByPath()
	if cfg.AllRegions {
		cfg.ensureAllRegionsSeedRegion()
		return items.WithDefaultRegion("*"), nil
	}
	if err := cfg.EnsureRegions(ctx); err != nil {
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

// EnsureRegions guarantees that non-all-regions operations have one usable AWS region.
// It asks the AWS SDK profile configuration when the runtime config has no region, then mirrors
// the resolved primary region into cfg.Regions when no explicit region list is present.
func (cfg *Options) EnsureRegions(ctx context.Context) error {
	if cfg.AllRegions {
		return nil
	}
	if cfg.Region == "" {
		cfg.Region = ssm.ResolveConfiguredRegion(ctx, cfg.Profile)
	}
	if cfg.Region == "" {
		return errors.New("AWS region is required")
	}
	if len(cfg.Regions) == 0 {
		cfg.Regions = []string{cfg.Region}
	}
	return nil
}

// ensureAllRegionsSeedRegion sets a safe seed region for AWS API calls that are needed before per-region scanning.
// Listing AWS regions itself requires a region-aware AWS SDK call, so all-regions mode uses us-east-1 by default.
func (cfg *Options) ensureAllRegionsSeedRegion() {
	if cfg.Region == "" {
		cfg.Region = allRegionsSeedRegion
	}
}
