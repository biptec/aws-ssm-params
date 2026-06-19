// Package filter parses CLI parameter filters and builds safe AWS-side prefilters plus exact local matchers.
package filter

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

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

// Group is an AND group. Multiple groups are ORed by MatchAny.
type Group struct {
	Conditions []Condition
}

// Matcher matches one value with shell-like extglob semantics.
type Matcher interface {
	Match(value string) bool
}

// ParseGroups parses repeated --filter values. Each value is one OR group; semicolons inside a value are AND.
func ParseGroups(values []string) ([]Group, error) {
	groups := make([]Group, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		group, err := ParseGroup(value)
		if err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	return groups, nil
}

// ParseFile parses a filters file. Blank lines and # comments are ignored.
func ParseFile(r io.Reader) ([]Group, error) {
	scanner := bufio.NewScanner(r)
	groups := []Group{}
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		if line == "" {
			continue
		}
		group, err := ParseGroup(line)
		if err != nil {
			return nil, fmt.Errorf("invalid filter line %d: %w", lineNumber, err)
		}
		groups = append(groups, group)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan filters file: %w", err)
	}
	return groups, nil
}

// ParseGroup parses an AND group such as name:/prod/*;region:eu*.
func ParseGroup(value string) (Group, error) {
	parts := strings.Split(value, ";")
	conditions := make([]Condition, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		condition, err := ParseCondition(part)
		if err != nil {
			return Group{}, err
		}
		conditions = append(conditions, condition)
	}
	if len(conditions) == 0 {
		return Group{}, fmt.Errorf("filter group is empty")
	}
	return Group{Conditions: conditions}, nil
}

// ParseCondition parses field:pattern. A bare pattern is interpreted as name:pattern.
func ParseCondition(value string) (Condition, error) {
	field := FieldName
	pattern := value
	if idx := strings.Index(value, ":"); idx > 0 {
		candidate := strings.TrimSpace(value[:idx])
		if canonical, ok := CanonicalField(candidate); ok {
			field = canonical
			pattern = value[idx+1:]
		}
	}
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return Condition{}, fmt.Errorf("filter %q has empty pattern", value)
	}
	compiledPattern := pattern
	if !hasMeta(pattern) {
		compiledPattern = canonicalAWSValue(field, pattern)
	}
	matcher, err := Compile(compiledPattern)
	if err != nil {
		return Condition{}, fmt.Errorf("compile filter %q: %w", value, err)
	}
	return Condition{Field: field, Pattern: pattern, matcher: matcher}, nil
}

// CanonicalField normalizes accepted filter fields.
func CanonicalField(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "name", "path":
		return FieldName, true
	case "region":
		return FieldRegion, true
	case "type":
		return FieldType, true
	case "tier":
		return FieldTier, true
	case "data-type", "datatype", "data_type":
		return FieldDataType, true
	case "description":
		return FieldDescription, true
	case "policies":
		return FieldPolicies, true
	case "value":
		return FieldValue, true
	default:
		return "", false
	}
}

// MatchAny reports whether a record matches at least one group. No groups means match all.
func MatchAny(groups []Group, record Record) bool {
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

// Match reports whether every condition in the group matches the record.
func (g Group) Match(record Record) bool {
	for _, condition := range g.Conditions {
		if !condition.Match(record) {
			return false
		}
	}
	return true
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

// HasField reports whether any condition in the group targets field.
func (g Group) HasField(field string) bool {
	for _, condition := range g.Conditions {
		if condition.Field == field {
			return true
		}
	}
	return false
}

// GroupsHaveField reports whether any group targets field.
func GroupsHaveField(groups []Group, field string) bool {
	for _, group := range groups {
		if group.HasField(field) {
			return true
		}
	}
	return false
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

func awsKey(field string) (string, bool) {
	switch field {
	case FieldName:
		return "Name", true
	case FieldType:
		return "Type", true
	case FieldTier:
		return "Tier", true
	case FieldDataType:
		return "DataType", true
	default:
		return "", false
	}
}

func canonicalAWSValue(field, value string) string {
	switch field {
	case FieldType:
		switch strings.ToLower(value) {
		case "string":
			return "String"
		case "stringlist", "string-list", "string_list":
			return "StringList"
		case "securestring", "secure-string", "secure_string":
			return "SecureString"
		}
	case FieldTier:
		switch strings.ToLower(value) {
		case "standard":
			return "Standard"
		case "advanced":
			return "Advanced"
		case "intelligent-tiering", "intelligent_tiering", "intelligenttiering":
			return "Intelligent-Tiering"
		}
	case FieldDataType:
		if value == "" {
			return "text"
		}
	}
	return value
}

func hasMeta(pattern string) bool {
	return strings.ContainsAny(pattern, "*?[]") || strings.Contains(pattern, "@(") || strings.Contains(pattern, "?(") || strings.Contains(pattern, "+(") || strings.Contains(pattern, "*(") || strings.Contains(pattern, "!(")
}

func literalPrefix(pattern string) string {
	var b strings.Builder
	for i := 0; i < len(pattern); i++ {
		if strings.ContainsRune("*?[]", rune(pattern[i])) {
			break
		}
		if i+1 < len(pattern) && strings.ContainsRune("@?+*!", rune(pattern[i])) && pattern[i+1] == '(' {
			break
		}
		if pattern[i] == '\\' && i+1 < len(pattern) {
			i++
			b.WriteByte(pattern[i])
			continue
		}
		b.WriteByte(pattern[i])
	}
	return b.String()
}

func simpleContainsLiteral(pattern string) (string, bool) {
	if !strings.HasPrefix(pattern, "*") || !strings.HasSuffix(pattern, "*") || len(pattern) < 3 {
		return "", false
	}
	middle := strings.Trim(pattern, "*")
	if middle == "" || hasMeta(middle) {
		return "", false
	}
	return middle, true
}
