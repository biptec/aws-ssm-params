// Package filter parses CLI parameter filters and builds safe AWS-side prefilters plus exact local matchers.
package filter

import "strings"

// AWSFilter is a safe Systems Manager DescribeParameters prefilter.
// It is intentionally lossy: exact matching always happens locally with Group.Match.
type AWSFilter struct {
	Key    string
	Option string
	Values []string
}

// Field names accepted by the CLI filter language.
const (
	FieldName        = "name"
	FieldRegion      = "region"
	FieldType        = "type"
	FieldTier        = "tier"
	FieldDataType    = "data-type"
	FieldDescription = "description"
	FieldPolicies    = "policies"
	FieldValue       = "value"
)

// Record is the normalized parameter shape used by local filter matching.
type Record struct {
	Name        string
	Region      string
	Type        string
	Tier        string
	DataType    string
	Description string
	Policies    string
	Value       string
}

// Condition is one field:pattern expression inside a filter group.
type Condition struct {
	Field   string
	Pattern string
	matcher Matcher
}

// Group is an AND group. Groups combines multiple groups with OR semantics.
type Group struct {
	Conditions []Condition
}

// Groups is an OR collection of condition groups.
type Groups []Group

// Match reports whether record matches at least one group. No groups means match all.
func (groups Groups) Match(record Record) bool {
	if len(groups) == 0 {
		return true
	}
	for _, group := range groups {
		if group.Match(record) {
			return true
		}
	}
	return false
}

// HasField reports whether any group targets field.
func (groups Groups) HasField(field string) bool {
	for _, group := range groups {
		if group.HasField(field) {
			return true
		}
	}
	return false
}

// Matcher matches one value with shell-like extglob semantics.
type Matcher interface {
	Match(value string) bool
}

// AWSFilters returns safe DescribeParameters filters for the group. Exact local matching is still required.
func (g Group) AWSFilters() []AWSFilter {
	filters := []AWSFilter{}
	for _, condition := range g.Conditions {
		if awsFilter, ok := condition.AWSFilter(); ok {
			filters = append(filters, awsFilter)
		}
	}
	return filters
}

// Match reports whether every condition in the group matches the record.
func (g Group) Match(record Record) bool {
	for _, condition := range g.Conditions {
		if !condition.Match(record) {
			return false
		}
	}
	return true
}

// HasField reports whether any condition in the group targets field.
func (g Group) HasField(field string) bool {
	for _, condition := range g.Conditions {
		if condition.Field == field {
			return true
		}
	}
	return false
}

// ExactName returns a literal name condition when the group can be anchored to one exact parameter name.
// Additional non-name conditions are still evaluated locally after the exact parameter lookup.
func (g Group) ExactName() (string, bool) {
	name := ""
	for _, condition := range g.Conditions {
		if condition.Field != FieldName {
			continue
		}
		if hasMeta(condition.Pattern) {
			return "", false
		}
		candidate := strings.TrimSpace(condition.Pattern)
		if candidate == "" {
			return "", false
		}
		if name != "" && name != candidate {
			return "", false
		}
		name = candidate
	}
	if name == "" {
		return "", false
	}
	return name, true
}

// AWSFilter returns the strongest safe DescribeParameters prefilter for this condition.
func (c Condition) AWSFilter() (AWSFilter, bool) {
	key, ok := awsKey(c.Field)
	if !ok {
		return AWSFilter{}, false
	}
	if !hasMeta(c.Pattern) {
		return AWSFilter{Key: key, Option: "Equals", Values: []string{canonicalAWSValue(c.Field, c.Pattern)}}, true
	}
	if c.Field == FieldName {
		if prefix := literalPrefix(c.Pattern); prefix != "" {
			return AWSFilter{Key: "Name", Option: "BeginsWith", Values: []string{prefix}}, true
		}
		if literal, ok := simpleContainsLiteral(c.Pattern); ok {
			return AWSFilter{Key: "Name", Option: "Contains", Values: []string{literal}}, true
		}
	}
	return AWSFilter{}, false
}

// Match reports whether one condition matches the record.
func (c Condition) Match(record Record) bool {
	switch c.Field {
	case FieldName:
		return c.matcher.Match(record.Name)
	case FieldRegion:
		return c.matcher.Match(record.Region)
	case FieldType:
		return c.matcher.Match(canonicalAWSValue(c.Field, record.Type))
	case FieldTier:
		return c.matcher.Match(canonicalAWSValue(c.Field, record.Tier))
	case FieldDataType:
		return c.matcher.Match(canonicalAWSValue(c.Field, record.DataType))
	case FieldDescription:
		return c.matcher.Match(record.Description)
	case FieldPolicies:
		return c.matcher.Match(record.Policies)
	case FieldValue:
		return c.matcher.Match(record.Value)
	default:
		return false
	}
}
