package app

import (
	"context"
	"errors"

	"github.com/biptec/aws-ssm-params/internal/ssm"
)

const allRegionsSeedRegion = "us-east-1"

// ensureRegions guarantees that non-all-regions commands have one usable AWS region.
// It first asks the AWS SDK profile configuration if CLI/env flags did not provide a region, then mirrors the
// resolved primary region into cfg.Regions when the user did not pass an explicit list.
func ensureRegions(ctx context.Context, cfg *Config) error {
	if cfg.AllRegions {
		return nil
	}
	if cfg.Region == "" {
		cfg.Region = ssm.ResolveConfiguredRegion(ctx, cfg.Profile)
	}
	if cfg.Region == "" {
		return errors.New("AWS region is required; pass --region, set AWS_REGION/AWS_DEFAULT_REGION, or configure a default region in the AWS profile")
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
