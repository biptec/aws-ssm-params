package ssm

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsssm "github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

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
