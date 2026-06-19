// Package ssm wraps AWS Systems Manager Parameter Store access behind a testable interface.
package ssm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
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

	cfgMu  sync.Mutex
	cfg    aws.Config
	cfgErr error
	loaded bool

	clientMu  sync.Mutex
	ssmClient ssmAPI
	ec2Client ec2API
	stsClient stsAPI
}

type ssmAPI interface {
	GetParameters(context.Context, *awsssm.GetParametersInput, ...func(*awsssm.Options)) (*awsssm.GetParametersOutput, error)
	DescribeParameters(context.Context, *awsssm.DescribeParametersInput, ...func(*awsssm.Options)) (*awsssm.DescribeParametersOutput, error)
	PutParameter(context.Context, *awsssm.PutParameterInput, ...func(*awsssm.Options)) (*awsssm.PutParameterOutput, error)
	DeleteParameters(context.Context, *awsssm.DeleteParametersInput, ...func(*awsssm.Options)) (*awsssm.DeleteParametersOutput, error)
}

type ec2API interface {
	DescribeRegions(context.Context, *ec2.DescribeRegionsInput, ...func(*ec2.Options)) (*ec2.DescribeRegionsOutput, error)
}

type stsAPI interface {
	GetCallerIdentity(context.Context, *sts.GetCallerIdentityInput, ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error)
}

// Parameter is the normalized subset of AWS SSM GetParameters output needed by the app.
type Parameter struct {
	Name        string
	Region      string
	Type        string
	Value       string
	ValueHidden bool
	Version     int64
	Modified    string
}

// Metadata is the normalized subset of AWS SSM DescribeParameters output shown in the UI/export status.
type Metadata struct {
	Name        string
	Region      string
	Type        string
	Tier        string
	DataType    string
	Policies    string
	Description string
	User        string
	Modified    string
}

// ParameterFilter is a safe AWS SSM DescribeParameters string filter.
type ParameterFilter struct {
	Key    string
	Option string
	Values []string
}

// PutParameterOptions contains optional AWS SSM PutParameter fields.
type PutParameterOptions struct {
	Description string
	Tier        ParameterTier
	DataType    ParameterDataType
	Policies    string
	Overwrite   bool
}

// ParameterDataType is an AWS SSM data type accepted by PutParameter.
type ParameterDataType string

const (
	// ParameterDataTypeText stores an ordinary text value without additional service-side validation.
	ParameterDataTypeText ParameterDataType = "text"
	// ParameterDataTypeEC2Image validates that the value is an EC2 AMI id.
	ParameterDataTypeEC2Image ParameterDataType = "aws:ec2:image"
	// ParameterDataTypeSSMIntegration is used by AWS SSM service integrations.
	ParameterDataTypeSSMIntegration ParameterDataType = "aws:ssm:integration"
)

// DefaultParameterDataType is used when AWS does not report a data type.
const DefaultParameterDataType = ParameterDataTypeText

// ParseParameterDataType normalizes user-facing data type names into AWS SSM data types.
func ParseParameterDataType(value string) (ParameterDataType, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "text":
		return ParameterDataTypeText, nil
	case "aws:ec2:image", "ec2:image", "ami", "image":
		return ParameterDataTypeEC2Image, nil
	case "aws:ssm:integration", "ssm:integration", "integration":
		return ParameterDataTypeSSMIntegration, nil
	default:
		return "", fmt.Errorf("unsupported parameter data type %q; use text, aws:ec2:image, or aws:ssm:integration", value)
	}
}

// String returns the AWS API spelling of the parameter data type.
func (t ParameterDataType) String() string { return string(t) }

// IsValid reports whether the data type is supported by AWS SSM Parameter Store.
func (t ParameterDataType) IsValid() bool {
	switch t {
	case ParameterDataTypeText, ParameterDataTypeEC2Image, ParameterDataTypeSSMIntegration:
		return true
	default:
		return false
	}
}

// ParameterTier is an AWS SSM parameter tier accepted by PutParameter.
type ParameterTier string

const (
	// ParameterTierStandard stores parameters in the AWS Standard tier.
	ParameterTierStandard ParameterTier = "Standard"
	// ParameterTierAdvanced stores parameters in the AWS Advanced tier.
	ParameterTierAdvanced ParameterTier = "Advanced"
	// ParameterTierIntelligentTiering lets AWS choose Standard or Advanced as needed.
	ParameterTierIntelligentTiering ParameterTier = "Intelligent-Tiering"
)

// DefaultParameterTier is used for new parameters and non-interactive writes.
const DefaultParameterTier = ParameterTierIntelligentTiering

// ParseParameterTier normalizes user-facing tier names into AWS SSM tier names.
func ParseParameterTier(value string) (ParameterTier, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "intelligent-tiering", "intelligent_tiering", "intelligenttiering":
		return ParameterTierIntelligentTiering, nil
	case "standard":
		return ParameterTierStandard, nil
	case "advanced":
		return ParameterTierAdvanced, nil
	default:
		return "", fmt.Errorf("unsupported parameter tier %q; use standard, advanced, or intelligent-tiering", value)
	}
}

// String returns the AWS API spelling of the parameter tier.
func (t ParameterTier) String() string { return string(t) }

// IsValid reports whether the tier is one of the AWS SSM supported parameter tiers.
func (t ParameterTier) IsValid() bool {
	switch t {
	case ParameterTierStandard, ParameterTierAdvanced, ParameterTierIntelligentTiering:
		return true
	default:
		return false
	}
}

// ParameterType is an AWS SSM parameter type accepted by PutParameter.
type ParameterType string

const (
	// ParameterTypeString stores plaintext scalar values.
	ParameterTypeString ParameterType = "String"
	// ParameterTypeStringList stores comma-separated plaintext lists.
	ParameterTypeStringList ParameterType = "StringList"
	// ParameterTypeSecureString stores encrypted values using AWS KMS.
	ParameterTypeSecureString ParameterType = "SecureString"
)

// DefaultParameterType is used when creating a new parameter and no type was supplied by the user or import file.
const DefaultParameterType = ParameterTypeSecureString

// ParseParameterType normalizes user-facing type names into AWS SSM type names.
// It accepts AWS spelling and CLI-friendly aliases such as secure-string and string-list.
func ParseParameterType(value string) (ParameterType, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "securestring", "secure-string", "secure_string":
		return ParameterTypeSecureString, nil
	case "string":
		return ParameterTypeString, nil
	case "stringlist", "string-list", "string_list":
		return ParameterTypeStringList, nil
	default:
		return "", fmt.Errorf("unsupported parameter type %q; use string, string-list, or secure-string", value)
	}
}

// String returns the AWS API spelling of the parameter type.
func (t ParameterType) String() string { return string(t) }

// IsValid reports whether the type is one of the AWS SSM supported parameter types.
func (t ParameterType) IsValid() bool {
	switch t {
	case ParameterTypeString, ParameterTypeStringList, ParameterTypeSecureString:
		return true
	default:
		return false
	}
}

// ErrNotFound reports that a requested SSM parameter does not exist.
var ErrNotFound = errors.New("parameter not found")

// NewAWSClient constructs an AWS SDK backed client for one profile/region pair.
func NewAWSClient(profile, region string) *AWSClient {
	return &AWSClient{Profile: profile, Region: region, WithDecryption: true}
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
	out, err := c.ec2(ctx).DescribeRegions(ctx, &ec2.DescribeRegionsInput{AllRegions: aws.Bool(true)})
	if err != nil {
		return nil, c.logAPIError(ctx, operation, err)
	}
	regions := make([]string, 0, len(out.Regions))
	for _, region := range out.Regions {
		name := aws.ToString(region.RegionName)
		if name == "" || aws.ToString(region.OptInStatus) == "not-opted-in" {
			continue
		}
		regions = append(regions, name)
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
	return &AWSClient{Profile: c.Profile, Region: region, WithDecryption: c.WithDecryption, Logger: c.Logger}
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
	result := map[string]Metadata{}
	for _, chunk := range chunkStrings(paths, 10) {
		if len(chunk) == 0 {
			continue
		}
		operation := "ssm.DescribeParameters"
		c.logDebug(ctx, "aws api request", slog.String("operation", operation), slog.String("region", c.Region), slog.Any("names", chunk))
		out, err := c.ssm(ctx).DescribeParameters(ctx, &awsssm.DescribeParametersInput{
			ParameterFilters: []ssmtypes.ParameterStringFilter{{
				Key:    aws.String("Name"),
				Option: aws.String("Equals"),
				Values: chunk,
			}},
		})
		if err != nil {
			_ = c.logAPIError(ctx, operation, err, slog.Any("names", chunk))
			continue
		}
		c.logInfo(ctx, "aws api request completed", slog.String("operation", operation), slog.String("region", c.Region), slog.Int("count", len(out.Parameters)))
		for i := range out.Parameters {
			param := out.Parameters[i]
			name := aws.ToString(param.Name)
			if name == "" {
				continue
			}
			result[name] = metadataFromSDK(c.Region, param)
		}
	}
	return result
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
	if strings.TrimSpace(opts.Policies) != "" {
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

func (c *AWSClient) ec2(ctx context.Context) ec2API {
	c.clientMu.Lock()
	defer c.clientMu.Unlock()
	if c.ec2Client != nil {
		return c.ec2Client
	}
	cfg, err := c.sdkConfig(ctx)
	if err != nil {
		return errorEC2{err: err}
	}
	c.ec2Client = ec2.NewFromConfig(cfg)
	return c.ec2Client
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

func loadSDKConfig(ctx context.Context, profile, region string) (aws.Config, error) {
	opts := []func(*config.LoadOptions) error{}
	if profile != "" {
		opts = append(opts, config.WithSharedConfigProfile(profile))
	}
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}
	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return aws.Config{}, crerr.Wrap(err, "load AWS SDK config")
	}
	return cfg, nil
}

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

func metadataFromSDK(region string, param ssmtypes.ParameterMetadata) Metadata {
	return Metadata{
		Name:        aws.ToString(param.Name),
		Region:      region,
		Type:        string(param.Type),
		Tier:        string(param.Tier),
		DataType:    aws.ToString(param.DataType),
		Policies:    formatPolicies(param.Policies),
		Description: aws.ToString(param.Description),
		User:        aws.ToString(param.LastModifiedUser),
		Modified:    formatModifiedTime(param.LastModifiedDate),
	}
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

type errorEC2 struct{ err error }

func (e errorEC2) DescribeRegions(context.Context, *ec2.DescribeRegionsInput, ...func(*ec2.Options)) (*ec2.DescribeRegionsOutput, error) {
	return nil, e.err
}

type errorSTS struct{ err error }

func (e errorSTS) GetCallerIdentity(context.Context, *sts.GetCallerIdentityInput, ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
	return nil, e.err
}

func formatModifiedTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.Format(time.RFC1123)
}

// formatModifiedDate normalizes legacy JSON date shapes into a readable RFC1123 string.
// It is kept for tests and import/export compatibility around previously parsed records.
func formatModifiedDate(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case float64:
		if v <= 0 {
			return ""
		}
		return time.Unix(int64(v), 0).Format(time.RFC1123)
	case string:
		if v == "" {
			return ""
		}
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return time.Unix(int64(f), 0).Format(time.RFC1123)
		}
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t.Format(time.RFC1123)
		}
		return v
	default:
		return fmt.Sprint(v)
	}
}

func formatPolicies(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case []ssmtypes.ParameterInlinePolicy:
		if len(v) == 0 {
			return ""
		}
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	trimmed := strings.TrimSpace(string(encoded))
	if trimmed == "null" || trimmed == "[]" || trimmed == "{}" {
		return ""
	}
	return trimmed
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
