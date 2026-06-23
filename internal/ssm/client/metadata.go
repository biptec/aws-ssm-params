package client

import (
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"

	"github.com/biptec/aws-ssm-params/internal/ssm"
)

// metadataFromSDK maps AWS SDK metadata into the domain model used by the rest
// of the application. It deliberately does not include secret values; callers
// load values separately through GetMany when needed.
func metadataFromSDK(region string, param *ssmtypes.ParameterMetadata) ssm.Metadata {
	return ssm.Metadata{
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

// formatModifiedTime keeps zero AWS timestamps out of exported/TUI metadata.
func formatModifiedTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}

	return value.Format(time.RFC1123)
}
