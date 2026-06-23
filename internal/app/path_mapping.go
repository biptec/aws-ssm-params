package app

import (
	"fmt"
	"strings"
)

// PathMapping maps one AWS path prefix to one file path prefix. Mappings are
// pure string substitutions: no path cleanup, no relative/absolute checks, and
// no boundary-aware matching.
type PathMapping struct {
	AWSPath  string
	FilePath string
}

// PathMappings is an ordered set of AWS-to-file path mappings.
type PathMappings []PathMapping

// ParsePathMappings parses values in aws_path:file_path form. The AWS side must
// be non-empty; the file side may be empty to strip the matched AWS prefix.
func ParsePathMappings(values []string) (PathMappings, error) {
	mappings := make(PathMappings, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}

		awsPath, filePath, ok := strings.Cut(value, ":")
		if !ok {
			return nil, fmt.Errorf("path mapping %q must use aws_path:file_path", value)
		}

		if awsPath == "" {
			return nil, fmt.Errorf("path mapping %q must include an aws path before the separator", value)
		}

		mappings = append(mappings, PathMapping{AWSPath: awsPath, FilePath: filePath})
	}

	return mappings, nil
}

// ToFile maps an AWS parameter name into the file namespace. When multiple
// mappings match, the longest AWS prefix wins.
func (mappings PathMappings) ToFile(name string) string {
	mapping, ok := mappings.longestAWSMatch(name)
	if !ok {
		return name
	}

	return mapping.FilePath + strings.TrimPrefix(name, mapping.AWSPath)
}

// ToAWS maps a file parameter name into the AWS namespace. When multiple
// mappings match, the longest file prefix wins.
func (mappings PathMappings) ToAWS(name string) string {
	mapping, ok := mappings.longestFileMatch(name)
	if !ok {
		return name
	}

	return mapping.AWSPath + strings.TrimPrefix(name, mapping.FilePath)
}

func (mappings PathMappings) longestAWSMatch(name string) (PathMapping, bool) {
	var (
		best  PathMapping
		found bool
	)

	for _, mapping := range mappings {
		if !strings.HasPrefix(name, mapping.AWSPath) {
			continue
		}

		if !found || len(mapping.AWSPath) > len(best.AWSPath) {
			best = mapping
			found = true
		}
	}

	return best, found
}

func (mappings PathMappings) longestFileMatch(name string) (PathMapping, bool) {
	var (
		best  PathMapping
		found bool
	)

	for _, mapping := range mappings {
		if !strings.HasPrefix(name, mapping.FilePath) {
			continue
		}

		if !found || len(mapping.FilePath) > len(best.FilePath) {
			best = mapping
			found = true
		}
	}

	return best, found
}
