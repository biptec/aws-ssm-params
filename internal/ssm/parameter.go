package ssm

import (
	"errors"
	"fmt"
	"strings"
)

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
	PoliciesSet bool
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
