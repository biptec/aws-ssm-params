// Package ssm wraps AWS Systems Manager Parameter Store access behind a testable interface.
package ssm

import (
	"context"
	"log/slog"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsssm "github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// Client is the small SSM capability surface used by commands and the TUI.
// The interface keeps AWS access mockable in tests and lets status-loading code operate without knowing about AWS SDK details.
type Client interface {
	CheckAccess(ctx context.Context) error
	ListRegions(ctx context.Context) ([]string, error)
	ForRegion(region string) Client
	DefaultRegion() string
	Get(ctx context.Context, path string) (Parameter, error)
	GetMany(ctx context.Context, paths []string) (map[string]Parameter, map[string]error)
	DescribeMany(ctx context.Context, paths []string) map[string]Metadata
	ListParameterMetadata(ctx context.Context) ([]Metadata, error)
	ListParameterMetadataWithFilters(ctx context.Context, filters []ParameterFilter) ([]Metadata, error)
	PutParameter(ctx context.Context, path, value string, parameterType ParameterType) error
	PutParameterWithOptions(ctx context.Context, path, value string, parameterType ParameterType, opts PutParameterOptions) error
	DeleteMany(ctx context.Context, paths []string) error
}

// AWSClient implements Client with AWS SDK for Go v2.
// It uses the default SDK credential and config chain, including AWS profiles, SSO sessions, environment variables,
// shared config files, web identity, IMDS, and any other provider supported by the SDK.
type AWSClient struct {
	Profile        string
	Region         string
	WithDecryption bool
	Logger         *slog.Logger

	cfgMu     sync.Mutex
	cfg       aws.Config
	cfgErr    error
	loaded    bool
	sharedCfg *awsConfigCache

	clientMu     sync.Mutex
	ssmClient    ssmAPI
	regionClient regionAPI
	stsClient    stsAPI
}

type ssmAPI interface {
	GetParameters(context.Context, *awsssm.GetParametersInput, ...func(*awsssm.Options)) (*awsssm.GetParametersOutput, error)
	DescribeParameters(context.Context, *awsssm.DescribeParametersInput, ...func(*awsssm.Options)) (*awsssm.DescribeParametersOutput, error)
	PutParameter(context.Context, *awsssm.PutParameterInput, ...func(*awsssm.Options)) (*awsssm.PutParameterOutput, error)
	DeleteParameters(context.Context, *awsssm.DeleteParametersInput, ...func(*awsssm.Options)) (*awsssm.DeleteParametersOutput, error)
}

type regionAPI interface {
	DescribeRegions(ctx context.Context, client *AWSClient) ([]awsRegion, error)
}

type awsRegion struct {
	Name        string
	OptInStatus string
}

type stsAPI interface {
	GetCallerIdentity(context.Context, *sts.GetCallerIdentityInput, ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error)
}

// NewAWSClient constructs an AWS SDK backed client for one profile/region pair.
func NewAWSClient(profile, region string) *AWSClient {
	return &AWSClient{Profile: profile, Region: region, WithDecryption: true, sharedCfg: newAWSConfigCache(profile, region)}
}
