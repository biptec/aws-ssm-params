package app

import (
	"context"

	"github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/ssm"
)

const allRegionsSeedRegion = "us-east-1"

// EnsureRegions guarantees that non-all-regions operations have one usable AWS region.
// It asks the AWS SDK profile configuration when the runtime config has no region, then mirrors
// the resolved primary region into cfg.Regions when no explicit region list is present.
func EnsureRegions(ctx context.Context, cfg *Config) error {
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
func ensureAllRegionsSeedRegion(cfg *Config) {
	if cfg.Region == "" {
		cfg.Region = allRegionsSeedRegion
	}
}
