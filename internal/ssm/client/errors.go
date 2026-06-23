package client

import (
	"strings"

	"github.com/aws/smithy-go"
	"github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/ssm"
)

// isThrottlingError reports whether an AWS API error is a throttling or
// rate-limit failure. The AWS SDK exposes service-specific error codes, so the
// check intentionally matches known throttling families instead of one exact
// value.
func isThrottlingError(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}

	code := strings.ToLower(apiErr.ErrorCode())

	return strings.Contains(code, "throttl") ||
		strings.Contains(code, "toomanyrequests") ||
		strings.Contains(code, "requestlimitexceeded") ||
		strings.Contains(code, "provisionedthroughputexceeded") ||
		strings.Contains(code, "slowdown")
}

// normalizeAWSError converts AWS-specific "not found" errors into the domain
// sentinel used by import/export/TUI code. Unknown AWS errors stay untouched so
// callers keep their original diagnostic detail.
func normalizeAWSError(err error) error {
	if err == nil {
		return nil
	}

	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "ParameterNotFound", "ParameterVersionNotFound", "ParameterPatternMismatchException":
			return ssm.ErrNotFound
		}
	}

	return err
}
