package main

import (
	"fmt"
	"strings"

	"github.com/biptec/aws-ssm-params/internal/textio"
)

var supportedFields = map[string]string{
	"name":               textio.FieldName,
	"region":             textio.FieldRegion,
	"type":               textio.FieldType,
	"tier":               textio.FieldTier,
	"data-type":          textio.FieldDataType,
	"datatype":           textio.FieldDataType,
	"data_type":          textio.FieldDataType,
	"policies":           textio.FieldPolicies,
	"description":        textio.FieldDescription,
	"value":              textio.FieldValue,
	"date":               textio.FieldDate,
	"modified":           textio.FieldDate,
	"last-modified-date": textio.FieldDate,
	"version":            textio.FieldVersion,
	"len":                textio.FieldLen,
	"length":             textio.FieldLen,
	"sha256":             textio.FieldSHA256,
	"hash":               textio.FieldSHA256,
	"user":               textio.FieldUser,
}

func parseOutputFields(values []string, flagName string) (textio.Fields, error) {
	parts := compactStrings(values)
	if len(parts) == 0 {
		return nil, nil
	}
	seen := map[string]bool{}
	fields := make(textio.Fields, 0, len(parts))
	for _, part := range parts {
		if strings.Contains(part, ",") {
			return nil, fmt.Errorf("--%s accepts one value per flag; repeat --%s instead of using commas", flagName, flagName)
		}
		canonical, ok := supportedFields[strings.ToLower(strings.TrimSpace(part))]
		if !ok {
			return nil, fmt.Errorf("unsupported --%s value %q", flagName, part)
		}
		if !seen[canonical] {
			seen[canonical] = true
			fields = append(fields, canonical)
		}
	}
	return fields, nil
}

func parseFieldMappings(values []string, flagName string) (textio.FieldMappings, error) {
	parts := compactStrings(values)
	if len(parts) == 0 {
		return nil, nil
	}
	seen := map[string]bool{}
	mappings := make(textio.FieldMappings, 0, len(parts))
	for _, part := range parts {
		if strings.Contains(part, ",") {
			return nil, fmt.Errorf("--%s accepts one value per flag; repeat --%s instead of using commas", flagName, flagName)
		}
		awsField, fileField, ok := strings.Cut(part, ":")
		if !ok {
			return nil, fmt.Errorf("--%s requires aws_field:file_field", flagName)
		}
		canonical, ok := supportedFields[strings.ToLower(strings.TrimSpace(awsField))]
		if !ok {
			return nil, fmt.Errorf("unsupported --%s AWS field %q", flagName, awsField)
		}
		fileField = strings.TrimSpace(fileField)
		if fileField == "" {
			return nil, fmt.Errorf("field mapping %q has empty file field", part)
		}
		if !seen[canonical] {
			seen[canonical] = true
			mappings = append(mappings, textio.FieldMapping{AWSName: canonical, FileName: fileField})
		}
	}
	return mappings, nil
}
