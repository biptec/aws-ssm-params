// Package client provides AWS Systems Manager Parameter Store clients.
package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awsssm "github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/biptec/aws-ssm-params/internal/logging"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/cockroachdb/errors"
)

// Client is the single SSM capability surface used by commands and the TUI.
// Methods without an explicit region operate in DefaultRegion(). Operations
// that naturally contain mixed regions, such as Delete, receive a request
// object and keep regional routing inside this package.
type Client interface {
	// CheckAccess validates that the selected AWS profile and credentials can
	// call AWS before a long-running command or TUI scan starts.
	CheckAccess(context.Context) error

	// ListRegions returns enabled AWS regions available to the current account.
	ListRegions(context.Context) ([]string, error)

	// ForRegion returns a client view that targets region while preserving the
	// same profile, logger, decryption setting, and shared AWS config cache.
	ForRegion(string) Client

	// DefaultRegion returns the region used by methods that do not receive an
	// explicit region.
	DefaultRegion() string

	// GetMany loads parameter values for exact names in the client's region. It
	// returns values and per-name errors so callers can handle partial results.
	GetMany(context.Context, []string) (map[string]ssm.Parameter, map[string]error)

	// DescribeMany loads metadata for exact names in the client's region and
	// ignores per-name metadata errors. Use DescribeManyStrict when missing/error
	// information matters.
	DescribeMany(context.Context, []string) map[string]ssm.Metadata

	// DescribeManyStrict loads metadata for exact names in the client's region
	// and returns per-name errors for missing parameters or failed batches.
	DescribeManyStrict(context.Context, []string) (map[string]ssm.Metadata, map[string]error)

	// ListParameterMetadata returns metadata for every parameter visible in the
	// client's region without loading secret values.
	ListParameterMetadata(context.Context) ([]ssm.Metadata, error)

	// ListParameterMetadataWithFilters returns metadata matching AWS-side
	// DescribeParameters prefilters. Callers should still apply exact local
	// filters because AWS filters are intentionally coarse.
	ListParameterMetadataWithFilters(context.Context, []ssm.ParameterFilter) ([]ssm.Metadata, error)

	// PutParameterWithOptions creates or updates one parameter in the client's
	// region with explicit SSM metadata options.
	PutParameterWithOptions(context.Context, string, string, ssm.ParameterType, ssm.PutParameterOptions) error

	// Delete removes parameters described by req. The request may contain
	// multiple regions; the client handles regional grouping and AWS batch sizes.
	Delete(context.Context, *DeleteRequest) error
}

// DeleteRequest describes the parameters that should be removed. A request may
// contain parameters from multiple regions; the client groups them into AWS SSM
// batch calls internally.
type DeleteRequest struct {
	Parameters []DeleteParameter
}

// DeleteParameter identifies one parameter in one concrete AWS region. An empty
// Region means "use the client's current/default region".
type DeleteParameter struct {
	Name   string
	Region string
}

// New creates an SSM client from runtime configuration.
func New(cfg Config) Client {
	client := newClient(cfg.Profile, cfg.Region)
	client.WithDecryption = cfg.WithDecryption
	client.Logger = cfg.Logger

	return client
}

// client implements Client with AWS SDK for Go v2.
// It uses the default SDK credential and config chain, including AWS profiles, SSO sessions, environment variables,
// shared config files, web identity, IMDS, and any other provider supported by the SDK.
type client struct {
	Profile        string
	Region         string
	WithDecryption bool
	Logger         *slog.Logger

	sharedCfgMu sync.Mutex
	// sharedCfg is intentionally shared between regional client clones. The AWS
	// SDK config chain can be expensive and may involve SSO/profile resolution,
	// so ForRegion reuses the loaded credentials/profile and only overrides the
	// request region.
	sharedCfg *sdkConfigCache

	clientMu  sync.Mutex
	ssmClient *awsssm.Client
	stsClient *sts.Client

	// Function hooks are package-private test seams. Keeping them on the
	// concrete client avoids introducing extra public or private interfaces that
	// would fragment the package API.
	getParametersFunc      func(context.Context, *awsssm.GetParametersInput, ...func(*awsssm.Options)) (*awsssm.GetParametersOutput, error)
	describeParametersFunc func(context.Context, *awsssm.DescribeParametersInput, ...func(*awsssm.Options)) (*awsssm.DescribeParametersOutput, error)
	putParameterFunc       func(context.Context, *awsssm.PutParameterInput, ...func(*awsssm.Options)) (*awsssm.PutParameterOutput, error)
	deleteParametersFunc   func(context.Context, *awsssm.DeleteParametersInput, ...func(*awsssm.Options)) (*awsssm.DeleteParametersOutput, error)
	getCallerIdentityFunc  func(context.Context, *sts.GetCallerIdentityInput, ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error)
	describeRegionsFunc    func(context.Context, *client) ([]string, error)
}

// newClient constructs an AWS SDK backed client for one profile/region pair.
func newClient(profile, region string) *client {
	return &client{Profile: profile, Region: region, WithDecryption: true, sharedCfg: newSDKConfigCache(profile, region)}
}

// ForRegion returns a client targeting another region while preserving the selected AWS profile.
// When the requested region is empty or already current, it reuses the receiver to avoid unnecessary allocations.
func (c *client) ForRegion(region string) Client {
	return c.forRegion(region)
}

// forRegion returns the concrete client so internal orchestration can keep
// using private helpers without widening the public Client interface.
func (c *client) forRegion(region string) *client {
	if region == "" || region == c.Region {
		return c
	}

	return &client{
		Profile:        c.Profile,
		Region:         region,
		WithDecryption: c.WithDecryption,
		Logger:         c.Logger,
		sharedCfg:      c.ensureSharedConfig(),

		getParametersFunc:      c.getParametersFunc,
		describeParametersFunc: c.describeParametersFunc,
		putParameterFunc:       c.putParameterFunc,
		deleteParametersFunc:   c.deleteParametersFunc,
		getCallerIdentityFunc:  c.getCallerIdentityFunc,
		describeRegionsFunc:    c.describeRegionsFunc,
	}
}

// ensureSharedConfig guarantees that regional clones keep one shared config
// cache even when a test constructs client directly without using New.
func (c *client) ensureSharedConfig() *sdkConfigCache {
	c.sharedCfgMu.Lock()
	defer c.sharedCfgMu.Unlock()

	if c.sharedCfg == nil {
		c.sharedCfg = newSDKConfigCache(c.Profile, c.Region)
	}

	return c.sharedCfg
}

// DefaultRegion returns the region associated with this client.
func (c *client) DefaultRegion() string {
	return c.Region
}

// ssm lazily builds the AWS SSM SDK client for the receiver region. Lazy
// creation keeps command startup cheap until an operation actually needs AWS.
func (c *client) ssm(ctx context.Context) (*awsssm.Client, error) {
	c.clientMu.Lock()
	defer c.clientMu.Unlock()

	if c.ssmClient != nil {
		return c.ssmClient, nil
	}

	cfg, err := c.sdkConfig(ctx)
	if err != nil {
		return nil, err
	}

	c.ssmClient = awsssm.NewFromConfig(cfg)

	return c.ssmClient, nil
}

// sts lazily builds the AWS STS SDK client used only for early access checks.
func (c *client) sts(ctx context.Context) (*sts.Client, error) {
	c.clientMu.Lock()
	defer c.clientMu.Unlock()

	if c.stsClient != nil {
		return c.stsClient, nil
	}

	cfg, err := c.sdkConfig(ctx)
	if err != nil {
		return nil, err
	}

	c.stsClient = sts.NewFromConfig(cfg)

	return c.stsClient, nil
}

// getParameters centralizes the SDK call, test hook, and external error
// wrapping. The business methods above it should not know whether they are
// talking to the real AWS SDK or to a package-local test hook.
func (c *client) getParameters(ctx context.Context, input *awsssm.GetParametersInput) (*awsssm.GetParametersOutput, error) {
	if c.getParametersFunc != nil {
		return c.getParametersFunc(ctx, input)
	}

	sdk, err := c.ssm(ctx)
	if err != nil {
		return nil, err
	}

	out, err := sdk.GetParameters(ctx, input)

	return out, errors.Wrap(err, "get SSM parameters")
}

// describeParameters centralizes DescribeParameters for both list and exact
// metadata lookups.
func (c *client) describeParameters(ctx context.Context, input *awsssm.DescribeParametersInput) (*awsssm.DescribeParametersOutput, error) {
	if c.describeParametersFunc != nil {
		return c.describeParametersFunc(ctx, input)
	}

	sdk, err := c.ssm(ctx)
	if err != nil {
		return nil, err
	}

	out, err := sdk.DescribeParameters(ctx, input)

	return out, errors.Wrap(err, "describe SSM parameters")
}

// putParameter centralizes PutParameter so write behavior has one logging and
// error-normalization path in PutParameterWithOptions.
func (c *client) putParameter(ctx context.Context, input *awsssm.PutParameterInput) (*awsssm.PutParameterOutput, error) {
	if c.putParameterFunc != nil {
		return c.putParameterFunc(ctx, input)
	}

	sdk, err := c.ssm(ctx)
	if err != nil {
		return nil, err
	}

	out, err := sdk.PutParameter(ctx, input)

	return out, errors.Wrap(err, "put SSM parameter")
}

// deleteParameters performs one raw AWS batch delete. Higher-level regional
// grouping stays in Delete.
func (c *client) deleteParameters(ctx context.Context, input *awsssm.DeleteParametersInput) (*awsssm.DeleteParametersOutput, error) {
	if c.deleteParametersFunc != nil {
		return c.deleteParametersFunc(ctx, input)
	}

	sdk, err := c.ssm(ctx)
	if err != nil {
		return nil, err
	}

	out, err := sdk.DeleteParameters(ctx, input)

	return out, errors.Wrap(err, "delete SSM parameters")
}

// getCallerIdentity is kept behind the same hook pattern as SSM calls so access
// checks remain testable without another interface.
func (c *client) getCallerIdentity(ctx context.Context, input *sts.GetCallerIdentityInput) (*sts.GetCallerIdentityOutput, error) {
	if c.getCallerIdentityFunc != nil {
		return c.getCallerIdentityFunc(ctx, input)
	}

	sdk, err := c.sts(ctx)
	if err != nil {
		return nil, err
	}

	out, err := sdk.GetCallerIdentity(ctx, input)

	return out, errors.Wrap(err, "get AWS caller identity")
}

// describeRegions discovers enabled AWS regions through EC2 because SSM itself
// does not expose a region-discovery endpoint.
func (c *client) describeRegions(ctx context.Context) ([]string, error) {
	if c.describeRegionsFunc != nil {
		return c.describeRegionsFunc(ctx, c)
	}

	return describeAWSRegions(ctx, c)
}

// sdkConfig loads the AWS SDK config once per shared root config and then
// applies the receiver region. This keeps profile/credential resolution shared
// while still allowing cheap region-specific clients.
func (c *client) sdkConfig(ctx context.Context) (aws.Config, error) {
	cfg, err := c.ensureSharedConfig().load(ctx)
	if err != nil {
		return aws.Config{}, err
	}

	if c.Region != "" {
		cfg.Region = c.Region
	}

	return cfg, nil
}

// logger uses the explicit client logger when provided, otherwise it inherits
// the logger stored in context by the CLI/runtime.
func (c *client) logger(ctx context.Context) *slog.Logger {
	if c.Logger != nil {
		return c.Logger
	}

	return logging.FromContext(ctx)
}

func (c *client) logDebug(ctx context.Context, msg string, attrs ...slog.Attr) {
	c.logger(ctx).LogAttrs(ctx, slog.LevelDebug, msg, attrs...)
}

func (c *client) logInfo(ctx context.Context, msg string, attrs ...slog.Attr) {
	c.logger(ctx).LogAttrs(ctx, slog.LevelInfo, msg, attrs...)
}

// logAPIError records the original AWS error, returns the normalized domain
// error used by callers, and downgrades throttling to warn because retries or
// smaller scans are usually the next operator action.
func (c *client) logAPIError(ctx context.Context, operation string, err error, attrs ...slog.Attr) error {
	normalized := normalizeAWSError(err)
	level := slog.LevelError
	message := "aws api request failed"

	if isThrottlingError(err) {
		level = slog.LevelWarn
		message = "aws api throttling"
	}

	allAttrs := make([]slog.Attr, 0, 3+len(attrs))
	allAttrs = append(allAttrs, slog.String("operation", operation), slog.String("region", c.Region), slog.Any("error", err))
	allAttrs = append(allAttrs, attrs...)
	c.logger(ctx).LogAttrs(ctx, level, message, allAttrs...)

	return normalized
}

// GetMany loads parameters in AWS SSM batches of up to ten names.
// It initializes every requested path as ErrNotFound, clears successful entries, and preserves per-path errors so
// callers can distinguish missing parameters from AWS/API failures.
func (c *client) GetMany(ctx context.Context, paths []string) (values map[string]ssm.Parameter, errs map[string]error) {
	values = map[string]ssm.Parameter{}
	errs = map[string]error{}

	for _, path := range paths {
		if path != "" {
			errs[path] = ssm.ErrNotFound
		}
	}

	for _, chunk := range chunkStrings(paths, 10) {
		if len(chunk) == 0 {
			continue
		}

		operation := "ssm.GetParameters"
		c.logDebug(ctx, "aws api request", slog.String("operation", operation), slog.String("region", c.Region), slog.Any("names", chunk), slog.Bool("with_decryption", c.WithDecryption))

		out, err := c.getParameters(ctx, &awsssm.GetParametersInput{
			Names:          chunk,
			WithDecryption: aws.Bool(c.WithDecryption),
		})
		if err != nil {
			normalized := c.logAPIError(ctx, operation, err, slog.Any("names", chunk))
			for _, path := range chunk {
				errs[path] = normalized
			}

			continue
		}

		c.logInfo(ctx, "aws api request completed", slog.String("operation", operation), slog.String("region", c.Region), slog.Int("count", len(out.Parameters)))
		c.logDebug(ctx, "aws api response", slog.String("operation", operation), slog.String("region", c.Region), slog.Any("parameters", parametersForLog(c.WithDecryption, out.Parameters)), slog.Any("invalid_parameters", out.InvalidParameters))

		for _, param := range out.Parameters {
			name := aws.ToString(param.Name)
			if name == "" {
				continue
			}

			values[name] = ssm.Parameter{
				Name:        name,
				Region:      c.Region,
				Type:        string(param.Type),
				Value:       parameterValue(c.WithDecryption, &param),
				ValueHidden: parameterValueHidden(c.WithDecryption, &param),
				Version:     param.Version,
				Modified:    formatModifiedTime(param.LastModifiedDate),
			}
			delete(errs, name)
		}

		for _, path := range out.InvalidParameters {
			errs[path] = ssm.ErrNotFound
		}
	}

	return values, errs
}

// ListParameterMetadata returns metadata for every parameter visible in the client's region.
// Values are intentionally not included; callers can batch GetMany for the returned names when needed.
func (c *client) ListParameterMetadata(ctx context.Context) ([]ssm.Metadata, error) {
	return c.ListParameterMetadataWithFilters(ctx, nil)
}

// ListParameterMetadataWithFilters returns metadata matching AWS-side DescribeParameters prefilters.
// Callers must still apply exact local filters because AWS filters are intentionally lossy optimizations.
func (c *client) ListParameterMetadataWithFilters(ctx context.Context, filters []ssm.ParameterFilter) ([]ssm.Metadata, error) {
	var (
		result    []ssm.Metadata
		nextToken *string
	)

	sdkFilters := parameterFiltersToSDK(filters)

	for {
		operation := "ssm.DescribeParameters"
		c.logDebug(ctx, "aws api request", slog.String("operation", operation), slog.String("region", c.Region), slog.Int("max_results", 50), slog.Bool("has_next_token", nextToken != nil), slog.Any("filters", filters))

		out, err := c.describeParameters(ctx, &awsssm.DescribeParametersInput{
			MaxResults:       aws.Int32(50),
			NextToken:        nextToken,
			ParameterFilters: sdkFilters,
		})
		if err != nil {
			return nil, c.logAPIError(ctx, operation, err, slog.Any("filters", filters))
		}

		c.logInfo(ctx, "aws api request completed", slog.String("operation", operation), slog.String("region", c.Region), slog.Int("count", len(out.Parameters)))

		for i := range out.Parameters {
			param := out.Parameters[i]
			if aws.ToString(param.Name) == "" {
				continue
			}

			result = append(result, metadataFromSDK(c.Region, &param))
		}

		if aws.ToString(out.NextToken) == "" {
			break
		}

		nextToken = out.NextToken
	}

	return result, nil
}

// DescribeMany loads non-secret metadata for parameter paths in batches.
// Describe failures are ignored per batch because metadata is supplementary; GetMany still provides the authoritative value status.
func (c *client) DescribeMany(ctx context.Context, paths []string) map[string]ssm.Metadata {
	result, _ := c.DescribeManyStrict(ctx, paths)
	return result
}

// DescribeManyStrict loads non-secret metadata for exact parameter paths in batches.
// It uses DescribeParameters with the Name Equals filter, whose Values array accepts up to 50 names.
func (c *client) DescribeManyStrict(ctx context.Context, paths []string) (metadataByPath map[string]ssm.Metadata, errorsByPath map[string]error) {
	result := map[string]ssm.Metadata{}
	errs := map[string]error{}

	for _, path := range paths {
		if path != "" {
			errs[path] = ssm.ErrNotFound
		}
	}

	for _, chunk := range chunkStrings(paths, 50) {
		if len(chunk) == 0 {
			continue
		}

		var nextToken *string

		for {
			operation := "ssm.DescribeParameters"
			c.logDebug(ctx, "aws api request", slog.String("operation", operation), slog.String("region", c.Region), slog.Any("names", chunk), slog.Bool("has_next_token", nextToken != nil))

			out, err := c.describeParameters(ctx, &awsssm.DescribeParametersInput{
				MaxResults: aws.Int32(50),
				NextToken:  nextToken,
				ParameterFilters: []ssmtypes.ParameterStringFilter{{
					Key:    aws.String("Name"),
					Option: aws.String("Equals"),
					Values: chunk,
				}},
			})
			if err != nil {
				normalized := c.logAPIError(ctx, operation, err, slog.Any("names", chunk))
				for _, path := range chunk {
					errs[path] = normalized
				}

				break
			}

			c.logInfo(ctx, "aws api request completed", slog.String("operation", operation), slog.String("region", c.Region), slog.Int("count", len(out.Parameters)))

			for i := range out.Parameters {
				param := out.Parameters[i]

				name := aws.ToString(param.Name)
				if name == "" {
					continue
				}

				result[name] = metadataFromSDK(c.Region, &param)
				delete(errs, name)
			}

			if aws.ToString(out.NextToken) == "" {
				break
			}

			nextToken = out.NextToken
		}
	}

	return result, errs
}

// PutParameterWithOptions creates or updates one SSM parameter with explicit metadata options.
func (c *client) PutParameterWithOptions(ctx context.Context, path, value string, parameterType ssm.ParameterType, opts ssm.PutParameterOptions) error {
	if !parameterType.IsValid() {
		return fmt.Errorf("unsupported parameter type %q; use string, string-list, or secure-string", parameterType)
	}

	tier := opts.Tier
	if !tier.IsValid() {
		tier = ssm.DefaultParameterTier
	}

	dataType := opts.DataType
	if !dataType.IsValid() {
		dataType = ssm.DefaultParameterDataType
	}

	input := &awsssm.PutParameterInput{
		Name:      aws.String(path),
		Type:      ssmtypes.ParameterType(parameterType.String()),
		Tier:      ssmtypes.ParameterTier(tier.String()),
		DataType:  aws.String(dataType.String()),
		Value:     aws.String(value),
		Overwrite: aws.Bool(opts.Overwrite),
	}
	if strings.TrimSpace(opts.Description) != "" {
		input.Description = aws.String(opts.Description)
	}

	if opts.PoliciesSet || strings.TrimSpace(opts.Policies) != "" {
		input.Policies = aws.String(opts.Policies)
	}

	operation := "ssm.PutParameter"
	c.logDebug(ctx, "aws api request", slog.String("operation", operation), slog.String("region", c.Region), slog.String("name", path), slog.String("type", parameterType.String()), slog.String("tier", tier.String()), slog.String("data_type", dataType.String()), slog.Bool("overwrite", opts.Overwrite), slog.String("value", valueForLog(parameterType, value)))

	out, err := c.putParameter(ctx, input)
	if err != nil {
		return c.logAPIError(ctx, operation, err, slog.String("name", path), slog.String("type", parameterType.String()))
	}

	c.logInfo(ctx, "aws api request completed", slog.String("operation", operation), slog.String("region", c.Region), slog.String("name", path))
	c.logDebug(ctx, "aws api response", slog.String("operation", operation), slog.String("region", c.Region), slog.Int64("version", out.Version), slog.String("tier", string(out.Tier)))

	return nil
}

// Delete removes parameters grouped by region. Empty names are ignored,
// duplicate region/name pairs are deleted once, and processing stops on the
// first AWS error.
func (c *client) Delete(ctx context.Context, req *DeleteRequest) error {
	if req == nil {
		return errors.New("delete request is required")
	}

	pathsByRegion := make(map[string][]string)
	seen := make(map[string]bool, len(req.Parameters))

	for _, parameter := range req.Parameters {
		name := strings.TrimSpace(parameter.Name)
		region := strings.TrimSpace(parameter.Region)

		if name == "" {
			continue
		}

		key := region + "\x00" + name
		if seen[key] {
			continue
		}

		seen[key] = true

		// Preserve input order inside each region so file-driven deletes happen
		// in the same order the user reviewed them, while regions stay sorted for
		// deterministic execution and tests.
		pathsByRegion[region] = append(pathsByRegion[region], name)
	}

	regions := make([]string, 0, len(pathsByRegion))
	for region := range pathsByRegion {
		regions = append(regions, region)
	}

	sort.Strings(regions)

	for _, region := range regions {
		if err := c.forRegion(region).deleteMany(ctx, pathsByRegion[region]); err != nil {
			return errors.Wrapf(err, "delete parameters from %s", region)
		}
	}

	return nil
}

// deleteMany deletes paths in AWS SSM batches of up to ten names, matching the
// DeleteParameters API limit.
func (c *client) deleteMany(ctx context.Context, paths []string) error {
	for _, chunk := range chunkStrings(paths, 10) {
		if len(chunk) == 0 {
			continue
		}

		operation := "ssm.DeleteParameters"
		c.logDebug(ctx, "aws api request", slog.String("operation", operation), slog.String("region", c.Region), slog.Any("names", chunk))

		out, err := c.deleteParameters(ctx, &awsssm.DeleteParametersInput{Names: chunk})
		if err != nil {
			return c.logAPIError(ctx, operation, err, slog.Any("names", chunk))
		}

		c.logInfo(ctx, "aws api request completed", slog.String("operation", operation), slog.String("region", c.Region), slog.Int("count", len(out.DeletedParameters)))
		c.logDebug(ctx, "aws api response", slog.String("operation", operation), slog.String("region", c.Region), slog.Any("deleted_parameters", out.DeletedParameters), slog.Any("invalid_parameters", out.InvalidParameters))
	}

	return nil
}

// CheckAccess validates credentials/profile by calling STS GetCallerIdentity.
// It fails early before the UI starts so users do not wait through partial scans with broken credentials.
func (c *client) CheckAccess(ctx context.Context) error {
	operation := "sts.GetCallerIdentity"
	c.logDebug(ctx, "aws api request", slog.String("operation", operation), slog.String("region", c.Region))

	if _, err := c.getCallerIdentity(ctx, &sts.GetCallerIdentityInput{}); err != nil {
		return errors.Wrap(c.logAPIError(ctx, operation, err), "cannot access AWS with current credentials/profile")
	}

	c.logInfo(ctx, "aws api request completed", slog.String("operation", operation), slog.String("region", c.Region))

	return nil
}

// ListRegions returns AWS regions that are available for scanning.
// Regions with OptInStatus=not-opted-in are excluded because SSM calls there would fail for the account.
func (c *client) ListRegions(ctx context.Context) ([]string, error) {
	operation := "ec2.DescribeRegions"
	c.logDebug(ctx, "aws api request", slog.String("operation", operation), slog.String("region", c.Region), slog.Bool("all_regions", true))

	out, err := c.describeRegions(ctx)
	if err != nil {
		return nil, c.logAPIError(ctx, operation, err)
	}

	regions := append([]string(nil), out...)
	sort.Strings(regions)
	c.logInfo(ctx, "aws api request completed", slog.String("operation", operation), slog.String("region", c.Region), slog.Int("count", len(regions)))
	c.logDebug(ctx, "aws api response", slog.String("operation", operation), slog.String("region", c.Region), slog.Any("regions", regions))

	return regions, nil
}

// describeAWSRegions signs a minimal EC2 DescribeRegions request directly.
// Using the shared AWS config keeps credentials/profile behavior identical to
// SSM calls while avoiding a second long-lived SDK client on the Client struct.
func describeAWSRegions(ctx context.Context, client *client) ([]string, error) {
	cfg, err := client.sdkConfig(ctx)
	if err != nil {
		return nil, err
	}

	region := strings.TrimSpace(client.Region)
	if region == "" {
		region = strings.TrimSpace(cfg.Region)
	}

	if region == "" {
		region = "us-east-1"
	}

	credentials, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "retrieve AWS credentials")
	}

	body := "Action=DescribeRegions&Version=2016-11-15&AllRegions=true"
	endpoint := fmt.Sprintf("https://ec2.%s.amazonaws.com/", region)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		return nil, errors.Wrap(err, "create EC2 DescribeRegions request")
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	sum := sha256.Sum256([]byte(body))

	payloadHash := hex.EncodeToString(sum[:])
	if err := v4.NewSigner().SignHTTP(ctx, credentials, req, payloadHash, "ec2", region, time.Now()); err != nil {
		return nil, errors.Wrap(err, "sign EC2 DescribeRegions request")
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "call EC2 DescribeRegions")
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, errors.Wrap(err, "read EC2 DescribeRegions response")
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("EC2 DescribeRegions failed with HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}

	var out struct {
		Regions []struct {
			Name        string `xml:"regionName"`
			OptInStatus string `xml:"optInStatus"`
		} `xml:"regionInfo>item"`
	}
	if err := xml.Unmarshal(responseBody, &out); err != nil {
		return nil, errors.Wrap(err, "parse EC2 DescribeRegions response")
	}

	regions := make([]string, 0, len(out.Regions))
	for _, region := range out.Regions {
		if region.Name == "" || region.OptInStatus == "not-opted-in" {
			continue
		}

		regions = append(regions, region.Name)
	}

	return regions, nil
}
