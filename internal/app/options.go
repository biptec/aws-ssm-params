// Package app contains runtime configuration and shared application services.
package app

import (
	"context"
	"log/slog"

	"github.com/biptec/aws-ssm-params/internal/filter"
	"github.com/biptec/aws-ssm-params/internal/inventory"
	ssmclient "github.com/biptec/aws-ssm-params/internal/ssm/client"
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
	if err := cfg.PrepareRegions(ctx); err != nil {
		return nil, err
	}

	if cfg.AllRegions {
		return items.WithDefaultRegion("*"), nil
	}

	if len(items) == 0 {
		return nil, nil
	}

	if len(cfg.Regions) > 1 {
		return items.WithDefaultRegion("*"), nil
	}

	return items.WithDefaultRegion(cfg.Region), nil
}

// PrepareRegions makes the runtime configuration usable for AWS calls. In
// all-regions mode it supplies the seed region needed to discover enabled
// regions; otherwise it resolves the configured default region.
func (cfg *Options) PrepareRegions(ctx context.Context) error {
	if cfg.AllRegions {
		cfg.ensureAllRegionsSeedRegion()
		return nil
	}

	return cfg.EnsureRegions(ctx)
}

// EnsureRegions guarantees that non-all-regions operations have one usable AWS region.
// It asks the AWS SDK profile configuration when the runtime config has no region, then mirrors
// the resolved primary region into cfg.Regions when no explicit region list is present.
func (cfg *Options) EnsureRegions(ctx context.Context) error {
	if cfg.AllRegions {
		return nil
	}

	if cfg.Region == "" {
		cfg.Region = ssmclient.ResolveConfiguredRegion(ctx, cfg.Profile)
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
