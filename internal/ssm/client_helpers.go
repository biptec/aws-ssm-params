package ssm

import (
	"context"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsssm "github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/aws/smithy-go"
	crerr "github.com/cockroachdb/errors"
)

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
