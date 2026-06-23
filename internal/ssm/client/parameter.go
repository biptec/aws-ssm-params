package client

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"

	"github.com/biptec/aws-ssm-params/internal/ssm"
)

// parameterFiltersToSDK maps domain prefilters to the AWS DescribeParameters
// shape. Empty filters are ignored because AWS rejects incomplete filter
// objects.
func parameterFiltersToSDK(filters []ssm.ParameterFilter) []ssmtypes.ParameterStringFilter {
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

// parametersForLog creates a sanitized representation of AWS parameters for
// debug logs. Secure values never leave this package through logs.
func parametersForLog(withDecryption bool, params []ssmtypes.Parameter) []map[string]any {
	out := make([]map[string]any, 0, len(params))
	for _, param := range params {
		parameterType := ssm.ParameterType(param.Type)

		out = append(out, map[string]any{
			"name":    aws.ToString(param.Name),
			"type":    string(param.Type),
			"version": param.Version,
			"value":   valueForLog(parameterType, aws.ToString(param.Value)),
		})
		if !withDecryption && parameterType == ssm.ParameterTypeSecureString {
			out[len(out)-1]["value"] = "[secure]"
		}
	}

	return out
}

// valueForLog hides secure strings even when the caller already has the
// decrypted value in memory.
func valueForLog(parameterType ssm.ParameterType, value string) string {
	if parameterType == ssm.ParameterTypeSecureString {
		return "[secure]"
	}

	return value
}

// parameterValue returns a value only when it is safe and available. Secure
// strings requested without decryption are represented by ValueHidden instead.
func parameterValue(withDecryption bool, param *ssmtypes.Parameter) string {
	if parameterValueHidden(withDecryption, param) {
		return ""
	}

	return aws.ToString(param.Value)
}

// parameterValueHidden reports whether AWS intentionally withheld a secure
// value because the caller disabled decryption.
func parameterValueHidden(withDecryption bool, param *ssmtypes.Parameter) bool {
	return !withDecryption && param.Type == ssmtypes.ParameterTypeSecureString
}
