package ssm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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

func TestParseParameterTierNormalizesSupportedAliases(t *testing.T) {
	cases := map[string]ParameterTier{
		"":                    ParameterTierIntelligentTiering,
		"intelligent-tiering": ParameterTierIntelligentTiering,
		"IntelligentTiering":  ParameterTierIntelligentTiering,
		"standard":            ParameterTierStandard,
		"Advanced":            ParameterTierAdvanced,
	}

	for input, expected := range cases {
		actual, err := ParseParameterTier(input)
		assert.NoError(t, err)
		assert.Equal(t, expected, actual)
	}
}

func TestParseParameterTierRejectsUnsupportedValues(t *testing.T) {
	actual, err := ParseParameterTier("basic")

	assert.Error(t, err)
	assert.Equal(t, ParameterTier(""), actual)
}

func TestParseParameterTypeRejectsUnsupportedValues(t *testing.T) {
	actual, err := ParseParameterType("binary")

	assert.Error(t, err)
	assert.Equal(t, ParameterType(""), actual)
}
