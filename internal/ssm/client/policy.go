package client

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

// formatPolicies normalizes AWS policy metadata into a stable JSON array. AWS
// SDK versions and API shapes can expose policies either as raw JSON strings or
// as inline policy structs; callers should not care which representation AWS
// returned.
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
