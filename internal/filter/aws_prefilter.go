package filter

import (
	"strings"
)

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
