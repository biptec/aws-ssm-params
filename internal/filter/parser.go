package filter

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

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
