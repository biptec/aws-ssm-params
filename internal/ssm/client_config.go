package ssm

import (
	"context"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	awsssm "github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	crerr "github.com/cockroachdb/errors"

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

// ForRegion returns a client targeting another region while preserving the selected AWS profile.
// When the requested region is empty or already current, it reuses the receiver to avoid unnecessary allocations.
func (c *AWSClient) ForRegion(region string) Client {
	if region == "" || region == c.Region {
		return c
	}
	return &AWSClient{Profile: c.Profile, Region: region, WithDecryption: c.WithDecryption, Logger: c.Logger, sharedCfg: c.ensureSharedConfig()}
}

func (c *AWSClient) ensureSharedConfig() *awsConfigCache {
	c.cfgMu.Lock()
	defer c.cfgMu.Unlock()
	if c.sharedCfg == nil {
		c.sharedCfg = newAWSConfigCache(c.Profile, c.Region)
	}
	return c.sharedCfg
}

// DefaultRegion returns the region associated with this client.
func (c *AWSClient) DefaultRegion() string {
	return c.Region
}

func (c *AWSClient) ssm(ctx context.Context) ssmAPI {
	c.clientMu.Lock()
	defer c.clientMu.Unlock()
	if c.ssmClient != nil {
		return c.ssmClient
	}
	cfg, err := c.sdkConfig(ctx)
	if err != nil {
		return errorSSM{err: err}
	}
	c.ssmClient = awsssm.NewFromConfig(cfg)
	return c.ssmClient
}

func (c *AWSClient) region(context.Context) regionAPI {
	c.clientMu.Lock()
	defer c.clientMu.Unlock()
	if c.regionClient != nil {
		return c.regionClient
	}
	c.regionClient = signedEC2RegionAPI{}
	return c.regionClient
}

func (c *AWSClient) sts(ctx context.Context) stsAPI {
	c.clientMu.Lock()
	defer c.clientMu.Unlock()
	if c.stsClient != nil {
		return c.stsClient
	}
	cfg, err := c.sdkConfig(ctx)
	if err != nil {
		return errorSTS{err: err}
	}
	c.stsClient = sts.NewFromConfig(cfg)
	return c.stsClient
}

func (c *AWSClient) sdkConfig(ctx context.Context) (aws.Config, error) {
	if sharedCfg := c.ensureSharedConfig(); sharedCfg != nil {
		cfg, err := sharedCfg.load(ctx)
		if err != nil {
			return aws.Config{}, err
		}
		if c.Region != "" {
			cfg.Region = c.Region
		}
		return cfg, nil
	}

	c.cfgMu.Lock()
	defer c.cfgMu.Unlock()
	if c.loaded {
		return c.cfg, c.cfgErr
	}
	c.cfg, c.cfgErr = loadSDKConfig(ctx, c.Profile, c.Region)
	c.loaded = true
	return c.cfg, c.cfgErr
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
		return aws.Config{}, crerr.Wrap(err, "load AWS SDK config")
	}
	return cfg, nil
}
