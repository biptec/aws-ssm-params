package ssm

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestArgsIncludesProfileAndRegionWhenSet(t *testing.T) {
	client := &AWSCLI{Profile: "prod", Region: "eu-north-1"}

	assert.Equal(t, []string{"ssm", "get-parameters", "--profile", "prod", "--region", "eu-north-1"}, client.args("ssm", "get-parameters"))
}

func TestFormatModifiedDateHandlesAWSDateShapes(t *testing.T) {
	unix := float64(1717243200)
	assert.Equal(t, time.Unix(int64(unix), 0).Format(time.RFC1123), formatModifiedDate(unix))
	assert.Equal(t, "", formatModifiedDate(float64(0)))
	assert.Equal(t, time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC).Format(time.RFC1123), formatModifiedDate("2024-06-01T12:00:00Z"))
	assert.Equal(t, "custom-date", formatModifiedDate("custom-date"))
	assert.Equal(t, "42", formatModifiedDate(42))
}

func TestChunkStringsUsesDefaultSizeAndKeepsOrder(t *testing.T) {
	values := []string{"a", "b", "c", "d", "e"}

	assert.Equal(t, [][]string{{"a", "b"}, {"c", "d"}, {"e"}}, chunkStrings(values, 2))
	assert.Equal(t, [][]string{{"a", "b", "c", "d", "e"}}, chunkStrings(values, 0))
}

func TestParseParameterTypeNormalizesSupportedAliases(t *testing.T) {
	cases := map[string]ParameterType{
		"":              ParameterTypeSecureString,
		"secure-string": ParameterTypeSecureString,
		"SecureString":  ParameterTypeSecureString,
		"string":        ParameterTypeString,
		"string-list":   ParameterTypeStringList,
		"StringList":    ParameterTypeStringList,
	}
	for input, expected := range cases {
		actual, err := ParseParameterType(input)
		assert.NoError(t, err)
		assert.Equal(t, expected, actual)
	}
}

func TestParseParameterTypeRejectsUnsupportedValues(t *testing.T) {
	actual, err := ParseParameterType("binary")

	assert.Error(t, err)
	assert.Equal(t, ParameterType(""), actual)
}
