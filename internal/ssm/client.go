// Package ssm wraps AWS Systems Manager Parameter Store access behind a testable interface.
package ssm

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptrace"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/config"
	awsssm "github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"
	crerr "github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/logging"
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

// ResolveConfiguredRegion asks the AWS SDK config chain which default region is configured for a profile.
// Errors are swallowed because callers use this only as a fallback before reporting a clearer region error.
func ResolveConfiguredRegion(ctx context.Context, profile string) string {
	cfg, err := loadSDKConfig(ctx, profile, "")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cfg.Region)
}

// CheckAccess validates credentials/profile by calling STS GetCallerIdentity.
// It fails early before the UI starts so users do not wait through partial scans with broken credentials.
func (c *AWSClient) CheckAccess(ctx context.Context) error {
	operation := "sts.GetCallerIdentity"
	c.logDebug(ctx, "aws api request", slog.String("operation", operation), slog.String("region", c.Region))
	if _, err := c.sts(ctx).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{}); err != nil {
		return crerr.Wrap(c.logAPIError(ctx, operation, err), "cannot access AWS with current credentials/profile")
	}
	c.logInfo(ctx, "aws api request completed", slog.String("operation", operation), slog.String("region", c.Region))
	return nil
}

// ListRegions returns AWS regions that are available for scanning.
// Regions with OptInStatus=not-opted-in are excluded because SSM calls there would fail for the account.
func (c *AWSClient) ListRegions(ctx context.Context) ([]string, error) {
	operation := "ec2.DescribeRegions"
	c.logDebug(ctx, "aws api request", slog.String("operation", operation), slog.String("region", c.Region), slog.Bool("all_regions", true))
	out, err := c.region(ctx).DescribeRegions(ctx, c)
	if err != nil {
		return nil, c.logAPIError(ctx, operation, err)
	}
	regions := make([]string, 0, len(out))
	for _, region := range out {
		if region.Name == "" || region.OptInStatus == "not-opted-in" {
			continue
		}
		regions = append(regions, region.Name)
	}
	sort.Strings(regions)
	c.logInfo(ctx, "aws api request completed", slog.String("operation", operation), slog.String("region", c.Region), slog.Int("count", len(regions)))
	c.logDebug(ctx, "aws api response", slog.String("operation", operation), slog.String("region", c.Region), slog.Any("regions", regions))
	return regions, nil
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

// Get loads exactly one parameter by delegating to GetMany and normalizing missing values to ErrNotFound.
func (c *AWSClient) Get(ctx context.Context, path string) (Parameter, error) {
	values, errs := c.GetMany(ctx, []string{path})
	if value, ok := values[path]; ok {
		return value, nil
	}
	if err, ok := errs[path]; ok {
		return Parameter{}, err
	}
	return Parameter{}, ErrNotFound
}

// GetMany loads parameters in AWS SSM batches of up to ten names.
// It initializes every requested path as ErrNotFound, clears successful entries, and preserves per-path errors so
// callers can distinguish missing parameters from AWS/API failures.
func (c *AWSClient) GetMany(ctx context.Context, paths []string) (values map[string]Parameter, errs map[string]error) {
	values = map[string]Parameter{}
	errs = map[string]error{}
	for _, path := range paths {
		if path != "" {
			errs[path] = ErrNotFound
		}
	}

	for _, chunk := range chunkStrings(paths, 10) {
		if len(chunk) == 0 {
			continue
		}
		operation := "ssm.GetParameters"
		c.logDebug(ctx, "aws api request", slog.String("operation", operation), slog.String("region", c.Region), slog.Any("names", chunk), slog.Bool("with_decryption", c.WithDecryption))
		out, err := c.ssm(ctx).GetParameters(ctx, &awsssm.GetParametersInput{
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
			values[name] = Parameter{
				Name:        name,
				Region:      c.Region,
				Type:        string(param.Type),
				Value:       parameterValue(c.WithDecryption, param),
				ValueHidden: parameterValueHidden(c.WithDecryption, param),
				Version:     param.Version,
				Modified:    formatModifiedTime(param.LastModifiedDate),
			}
			delete(errs, name)
		}
		for _, path := range out.InvalidParameters {
			errs[path] = ErrNotFound
		}
	}

	return values, errs
}

// ListParameterMetadata returns metadata for every parameter visible in the client's region.
// Values are intentionally not included; callers can batch GetMany for the returned names when needed.
func (c *AWSClient) ListParameterMetadata(ctx context.Context) ([]Metadata, error) {
	return c.ListParameterMetadataWithFilters(ctx, nil)
}

// ListParameterMetadataWithFilters returns metadata matching AWS-side DescribeParameters prefilters.
// Callers must still apply exact local filters because AWS filters are intentionally lossy optimizations.
func (c *AWSClient) ListParameterMetadataWithFilters(ctx context.Context, filters []ParameterFilter) ([]Metadata, error) {
	var result []Metadata
	var nextToken *string
	sdkFilters := parameterFiltersToSDK(filters)
	for {
		operation := "ssm.DescribeParameters"
		c.logDebug(ctx, "aws api request", slog.String("operation", operation), slog.String("region", c.Region), slog.Int("max_results", 50), slog.Bool("has_next_token", nextToken != nil), slog.Any("filters", filters))
		out, err := c.ssm(ctx).DescribeParameters(ctx, &awsssm.DescribeParametersInput{
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
			result = append(result, metadataFromSDK(c.Region, param))
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
func (c *AWSClient) DescribeMany(ctx context.Context, paths []string) map[string]Metadata {
	result, _ := c.DescribeManyStrict(ctx, paths)
	return result
}

// DescribeManyStrict loads non-secret metadata for exact parameter paths in batches.
// It uses DescribeParameters with the Name Equals filter, whose Values array accepts up to 50 names.
func (c *AWSClient) DescribeManyStrict(ctx context.Context, paths []string) (metadataByPath map[string]Metadata, errorsByPath map[string]error) {
	result := map[string]Metadata{}
	errs := map[string]error{}
	for _, path := range paths {
		if path != "" {
			errs[path] = ErrNotFound
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
			out, err := c.ssm(ctx).DescribeParameters(ctx, &awsssm.DescribeParametersInput{
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
				result[name] = metadataFromSDK(c.Region, param)
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

// PutParameter creates or overwrites one SSM parameter using the requested AWS parameter type.
// SecureString values are encrypted by AWS SSM/KMS, while String and StringList are stored as plaintext parameters.
func (c *AWSClient) PutParameter(ctx context.Context, path, value string, parameterType ParameterType) error {
	return c.PutParameterWithOptions(ctx, path, value, parameterType, PutParameterOptions{Overwrite: true})
}

// PutParameterWithOptions creates or updates one SSM parameter with explicit metadata options.
func (c *AWSClient) PutParameterWithOptions(ctx context.Context, path, value string, parameterType ParameterType, opts PutParameterOptions) error {
	if !parameterType.IsValid() {
		return fmt.Errorf("unsupported parameter type %q; use string, string-list, or secure-string", parameterType)
	}
	tier := opts.Tier
	if !tier.IsValid() {
		tier = DefaultParameterTier
	}
	dataType := opts.DataType
	if !dataType.IsValid() {
		dataType = DefaultParameterDataType
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
	out, err := c.ssm(ctx).PutParameter(ctx, input)
	if err != nil {
		return c.logAPIError(ctx, operation, err, slog.String("name", path), slog.String("type", parameterType.String()))
	}
	c.logInfo(ctx, "aws api request completed", slog.String("operation", operation), slog.String("region", c.Region), slog.String("name", path))
	c.logDebug(ctx, "aws api response", slog.String("operation", operation), slog.String("region", c.Region), slog.Int64("version", out.Version), slog.String("tier", string(out.Tier)))
	return nil
}

// DeleteMany deletes paths in AWS SSM batches and stops at the first AWS SDK error.
func (c *AWSClient) DeleteMany(ctx context.Context, paths []string) error {
	for _, chunk := range chunkStrings(paths, 10) {
		if len(chunk) == 0 {
			continue
		}
		operation := "ssm.DeleteParameters"
		c.logDebug(ctx, "aws api request", slog.String("operation", operation), slog.String("region", c.Region), slog.Any("names", chunk))
		out, err := c.ssm(ctx).DeleteParameters(ctx, &awsssm.DeleteParametersInput{Names: chunk})
		if err != nil {
			return c.logAPIError(ctx, operation, err, slog.Any("names", chunk))
		}
		c.logInfo(ctx, "aws api request completed", slog.String("operation", operation), slog.String("region", c.Region), slog.Int("count", len(out.DeletedParameters)))
		c.logDebug(ctx, "aws api response", slog.String("operation", operation), slog.String("region", c.Region), slog.Any("deleted_parameters", out.DeletedParameters), slog.Any("invalid_parameters", out.InvalidParameters))
	}
	return nil
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

func (c *AWSClient) logger(ctx context.Context) *slog.Logger {
	if c.Logger != nil {
		return c.Logger
	}
	return logging.FromContext(ctx)
}

func (c *AWSClient) logDebug(ctx context.Context, msg string, attrs ...slog.Attr) {
	c.logger(ctx).LogAttrs(ctx, slog.LevelDebug, msg, attrs...)
}

func (c *AWSClient) logInfo(ctx context.Context, msg string, attrs ...slog.Attr) {
	c.logger(ctx).LogAttrs(ctx, slog.LevelInfo, msg, attrs...)
}

func (c *AWSClient) logAPIError(ctx context.Context, operation string, err error, attrs ...slog.Attr) error {
	normalized := normalizeAWSError(err)
	level := slog.LevelError
	message := "aws api request failed"
	if IsThrottlingError(err) {
		level = slog.LevelWarn
		message = "aws api throttling"
	}
	allAttrs := make([]slog.Attr, 0, 3+len(attrs))
	allAttrs = append(allAttrs, slog.String("operation", operation), slog.String("region", c.Region), slog.Any("error", err))
	allAttrs = append(allAttrs, attrs...)
	c.logger(ctx).LogAttrs(ctx, level, message, allAttrs...)
	return normalized
}

type signedEC2RegionAPI struct{}

type describeRegionsResponse struct {
	Regions []describeRegionsItem `xml:"regionInfo>item"`
}

type describeRegionsItem struct {
	Name        string `xml:"regionName"`
	OptInStatus string `xml:"optInStatus"`
}

func (signedEC2RegionAPI) DescribeRegions(ctx context.Context, client *AWSClient) ([]awsRegion, error) {
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
		return nil, crerr.Wrap(err, "retrieve AWS credentials")
	}
	body := "Action=DescribeRegions&Version=2016-11-15&AllRegions=true"
	endpoint := fmt.Sprintf("https://ec2.%s.amazonaws.com/", region)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		return nil, crerr.Wrap(err, "create EC2 DescribeRegions request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	sum := sha256.Sum256([]byte(body))
	payloadHash := hex.EncodeToString(sum[:])
	if err := v4.NewSigner().SignHTTP(ctx, credentials, req, payloadHash, "ec2", region, time.Now()); err != nil {
		return nil, crerr.Wrap(err, "sign EC2 DescribeRegions request")
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, crerr.Wrap(err, "call EC2 DescribeRegions")
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, crerr.Wrap(err, "read EC2 DescribeRegions response")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("EC2 DescribeRegions failed with HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	var out describeRegionsResponse
	if err := xml.Unmarshal(responseBody, &out); err != nil {
		return nil, crerr.Wrap(err, "parse EC2 DescribeRegions response")
	}
	regions := make([]awsRegion, 0, len(out.Regions))
	for _, region := range out.Regions {
		regions = append(regions, awsRegion(region))
	}
	return regions, nil
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

func traceHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	return &http.Client{Transport: traceRoundTripper{base: transport}}
}

type traceRoundTripper struct {
	base http.RoundTripper
}

func (transport traceRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	base := transport.base
	if base == nil {
		base = http.DefaultTransport
	}
	ctx := req.Context()
	logger := logging.FromContext(ctx)
	started := time.Now()
	host := req.URL.Host
	attrs := []slog.Attr{
		slog.String("method", req.Method),
		slog.String("host", host),
		slog.String("url", req.URL.String()),
	}

	var dnsStarted, connectStarted, tlsStarted, wroteRequestAt time.Time
	trace := &httptrace.ClientTrace{
		DNSStart: func(info httptrace.DNSStartInfo) {
			dnsStarted = time.Now()
			logger.LogAttrs(ctx, logging.LevelTrace, "aws http trace", append(attrs, slog.String("phase", "dns_start"), slog.String("dns_host", info.Host))...)
		},
		DNSDone: func(info httptrace.DNSDoneInfo) {
			logger.LogAttrs(ctx, logging.LevelTrace, "aws http trace", append(attrs, slog.String("phase", "dns_done"), slog.Int64("duration_ms", elapsedMillis(dnsStarted)), slog.Any("addrs", info.Addrs), slog.Any("error", info.Err))...)
		},
		ConnectStart: func(network, addr string) {
			connectStarted = time.Now()
			logger.LogAttrs(ctx, logging.LevelTrace, "aws http trace", append(attrs, slog.String("phase", "connect_start"), slog.String("network", network), slog.String("addr", addr))...)
		},
		ConnectDone: func(network, addr string, err error) {
			logger.LogAttrs(ctx, logging.LevelTrace, "aws http trace", append(attrs, slog.String("phase", "connect_done"), slog.Int64("duration_ms", elapsedMillis(connectStarted)), slog.String("network", network), slog.String("addr", addr), slog.Any("error", err))...)
		},
		TLSHandshakeStart: func() {
			tlsStarted = time.Now()
			logger.LogAttrs(ctx, logging.LevelTrace, "aws http trace", append(attrs, slog.String("phase", "tls_start"))...)
		},
		TLSHandshakeDone: func(_ tlsConnectionState, err error) {
			logger.LogAttrs(ctx, logging.LevelTrace, "aws http trace", append(attrs, slog.String("phase", "tls_done"), slog.Int64("duration_ms", elapsedMillis(tlsStarted)), slog.Any("error", err))...)
		},
		GotConn: func(info httptrace.GotConnInfo) {
			logger.LogAttrs(ctx, logging.LevelTrace, "aws http trace", append(attrs, slog.String("phase", "got_conn"), slog.Bool("reused", info.Reused), slog.Bool("was_idle", info.WasIdle), slog.Int64("idle_ms", int64(info.IdleTime/time.Millisecond)))...)
		},
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			wroteRequestAt = time.Now()
			logger.LogAttrs(ctx, logging.LevelTrace, "aws http trace", append(attrs, slog.String("phase", "wrote_request"), slog.Int64("elapsed_ms", elapsedMillis(started)), slog.Any("error", info.Err))...)
		},
		GotFirstResponseByte: func() {
			logger.LogAttrs(ctx, logging.LevelTrace, "aws http trace", append(attrs, slog.String("phase", "first_response_byte"), slog.Int64("elapsed_ms", elapsedMillis(started)), slog.Int64("server_wait_ms", elapsedMillis(wroteRequestAt)))...)
		},
	}

	tracedRequest := req.WithContext(httptrace.WithClientTrace(ctx, trace))
	resp, err := base.RoundTrip(tracedRequest)
	statusCode := 0
	if resp != nil {
		statusCode = resp.StatusCode
	}
	logger.LogAttrs(ctx, logging.LevelTrace, "aws http request completed", append(attrs, slog.Int("status", statusCode), slog.Int64("duration_ms", elapsedMillis(started)), slog.Any("error", err))...)
	if err != nil {
		return resp, crerr.Wrap(err, "aws http request")
	}
	return resp, nil
}

func elapsedMillis(started time.Time) int64 {
	if started.IsZero() {
		return 0
	}
	return int64(time.Since(started) / time.Millisecond)
}

type tlsConnectionState = tls.ConnectionState

func parameterFiltersToSDK(filters []ParameterFilter) []ssmtypes.ParameterStringFilter {
	if len(filters) == 0 {
		return nil
	}
	out := make([]ssmtypes.ParameterStringFilter, 0, len(filters))
	for _, filter := range filters {
		if strings.TrimSpace(filter.Key) == "" || len(filter.Values) == 0 {
			continue
		}
		out = append(out, ssmtypes.ParameterStringFilter{
			Key:    aws.String(filter.Key),
			Option: aws.String(filter.Option),
			Values: append([]string(nil), filter.Values...),
		})
	}
	return out
}

func parametersForLog(withDecryption bool, params []ssmtypes.Parameter) []map[string]any {
	out := make([]map[string]any, 0, len(params))
	for _, param := range params {
		parameterType := ParameterType(param.Type)
		out = append(out, map[string]any{
			"name":    aws.ToString(param.Name),
			"type":    string(param.Type),
			"version": param.Version,
			"value":   valueForLog(parameterType, aws.ToString(param.Value)),
		})
		if !withDecryption && parameterType == ParameterTypeSecureString {
			out[len(out)-1]["value"] = "[secure]"
		}
	}
	return out
}

func valueForLog(parameterType ParameterType, value string) string {
	if parameterType == ParameterTypeSecureString {
		return "[secure]"
	}
	return value
}

// IsThrottlingError reports whether an AWS API error is a throttling/rate-limit failure.
func IsThrottlingError(err error) bool {
	var apiErr smithy.APIError
	if !crerr.As(err, &apiErr) {
		return false
	}
	code := strings.ToLower(apiErr.ErrorCode())
	return strings.Contains(code, "throttl") ||
		strings.Contains(code, "toomanyrequests") ||
		strings.Contains(code, "requestlimitexceeded") ||
		strings.Contains(code, "provisionedthroughputexceeded") ||
		strings.Contains(code, "slowdown")
}

func parameterValue(withDecryption bool, param ssmtypes.Parameter) string {
	if parameterValueHidden(withDecryption, param) {
		return ""
	}
	return aws.ToString(param.Value)
}

func parameterValueHidden(withDecryption bool, param ssmtypes.Parameter) bool {
	return !withDecryption && param.Type == ssmtypes.ParameterTypeSecureString
}

func normalizeAWSError(err error) error {
	if err == nil {
		return nil
	}
	var apiErr smithy.APIError
	if crerr.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "ParameterNotFound", "ParameterVersionNotFound", "ParameterPatternMismatchException":
			return ErrNotFound
		}
	}
	return err
}

type errorSSM struct{ err error }

func (e errorSSM) GetParameters(context.Context, *awsssm.GetParametersInput, ...func(*awsssm.Options)) (*awsssm.GetParametersOutput, error) {
	return nil, e.err
}

func (e errorSSM) DescribeParameters(context.Context, *awsssm.DescribeParametersInput, ...func(*awsssm.Options)) (*awsssm.DescribeParametersOutput, error) {
	return nil, e.err
}

func (e errorSSM) PutParameter(context.Context, *awsssm.PutParameterInput, ...func(*awsssm.Options)) (*awsssm.PutParameterOutput, error) {
	return nil, e.err
}

func (e errorSSM) DeleteParameters(context.Context, *awsssm.DeleteParametersInput, ...func(*awsssm.Options)) (*awsssm.DeleteParametersOutput, error) {
	return nil, e.err
}

type errorSTS struct{ err error }

func (e errorSTS) GetCallerIdentity(context.Context, *sts.GetCallerIdentityInput, ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
	return nil, e.err
}

// chunkStrings splits a slice into non-empty chunks.
// AWS SSM get/delete APIs accept limited batch sizes, so callers use this to stay within those limits.
func chunkStrings(values []string, size int) [][]string {
	if size <= 0 {
		size = 10
	}
	var chunks [][]string
	for start := 0; start < len(values); start += size {
		end := start + size
		if end > len(values) {
			end = len(values)
		}
		chunks = append(chunks, values[start:end])
	}
	return chunks
}
