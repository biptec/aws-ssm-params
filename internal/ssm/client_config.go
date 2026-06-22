package ssm

import (
	"context"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/logging"
)

type awsConfigCache struct {
	mu      sync.Mutex
	profile string
	region  string
	cfg     aws.Config
	err     error
	loaded  bool
}

func newAWSConfigCache(profile, region string) *awsConfigCache {
	return &awsConfigCache{profile: profile, region: region}
}

func (cache *awsConfigCache) load(ctx context.Context) (aws.Config, error) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.loaded {
		return cache.cfg, cache.err
	}
	cache.cfg, cache.err = loadSDKConfig(ctx, cache.profile, cache.region)
	cache.loaded = true
	return cache.cfg, cache.err
}

// ResolveConfiguredRegion asks the AWS SDK config chain which default region is configured for a profile.
// Errors are swallowed because callers use this only as a fallback before reporting a clearer region error.
func ResolveConfiguredRegion(ctx context.Context, profile string) string {
	cfg, err := loadSDKConfig(ctx, profile, "")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cfg.Region)
}

func loadSDKConfig(ctx context.Context, profile, region string) (aws.Config, error) {
	opts := []func(*config.LoadOptions) error{}
	if profile != "" {
		opts = append(opts, config.WithSharedConfigProfile(profile))
	}
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}
	if logging.TraceEnabled(ctx) {
		opts = append(opts, config.WithHTTPClient(traceHTTPClient()))
	}
	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return aws.Config{}, errors.Wrap(err, "load AWS SDK config")
	}
	return cfg, nil
}
