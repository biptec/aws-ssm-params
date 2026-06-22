package ssm

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

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

func formatModifiedTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.Format(time.RFC1123)
}

// formatModifiedDate normalizes legacy JSON date shapes into a readable RFC1123 string.
func formatModifiedDate(value any) string {
	switch value := value.(type) {
	case nil:
		return ""
	case float64:
		if value <= 0 {
			return ""
		}
		return time.Unix(int64(value), 0).Format(time.RFC1123)
	case string:
		if value == "" {
			return ""
		}
		if number, err := strconv.ParseFloat(value, 64); err == nil {
			return time.Unix(int64(number), 0).Format(time.RFC1123)
		}
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			return parsed.Format(time.RFC1123)
		}
		return value
	default:
		return fmt.Sprint(value)
	}
}

func formatPolicies(value any) string {
	switch value := value.(type) {
	case nil:
		return ""
	case string:
		return normalizePolicyJSON(value)
	case []ssmtypes.ParameterInlinePolicy:
		return policiesFromInlinePolicies(value)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return normalizePolicyJSON(string(encoded))
}

func policiesFromInlinePolicies(policies []ssmtypes.ParameterInlinePolicy) string {
	items := make([]any, 0, len(policies))
	for _, policy := range policies {
		items = appendPolicyJSON(items, aws.ToString(policy.PolicyText))
	}
	return marshalPolicyItems(items)
}

func normalizePolicyJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" || raw == "{}" {
		return ""
	}
	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return raw
	}
	return marshalPolicyItems(appendPolicyValue(nil, decoded))
}

func appendPolicyJSON(items []any, raw string) []any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return items
	}
	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return append(items, raw)
	}
	return appendPolicyValue(items, decoded)
}

func appendPolicyValue(items []any, value any) []any {
	switch value := value.(type) {
	case nil:
		return items
	case []any:
		for _, item := range value {
			items = appendPolicyValue(items, item)
		}
		return items
	case map[string]any:
		if policyText, ok := value["PolicyText"]; ok {
			return appendPolicyValue(items, policyText)
		}
		return append(items, value)
	case string:
		return appendPolicyJSON(items, value)
	default:
		return append(items, value)
	}
}

func marshalPolicyItems(items []any) string {
	if len(items) == 0 {
		return ""
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		return fmt.Sprint(items)
	}
	trimmed := strings.TrimSpace(string(encoded))
	if trimmed == "null" || trimmed == "[]" || trimmed == "{}" {
		return ""
	}
	return trimmed
}
