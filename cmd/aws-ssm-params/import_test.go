package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	importcmd "github.com/biptec/aws-ssm-params/internal/app/import"
)

func TestImportOptionsParseDefaultsAndPolicies(t *testing.T) {
	flags := append(globalFlags(), importCLICommand().Flags...)
	cmd := testParsedCommand(t, flags, []string{
		"--" + flagRegion, "eu-north-1",
		"--" + importFlagDefaultType, "string",
		"--" + importFlagDefaultTier, "advanced",
		"--" + importFlagDefaultDescription, "description",
		"--" + importFlagOnCreate, "skip",
		"--" + importFlagOnUpdate, "ask",
	})

	options, err := importOptionsFromCLI(context.Background(), cmd)

	require.NoError(t, err)
	assert.Equal(t, "String", options.DefaultType.String())
	assert.Equal(t, "Advanced", options.DefaultOptions.Tier.String())
	assert.Equal(t, "description", options.DefaultOptions.Description)
	assert.Equal(t, importcmd.PolicySkip, options.Policy.OnCreate)
	assert.Equal(t, importcmd.PolicyAsk, options.Policy.OnUpdate)
}

func TestImportRejectsUnsupportedPolicy(t *testing.T) {
	flags := append(globalFlags(), importCLICommand().Flags...)
	cmd := testParsedCommand(t, flags, []string{
		"--" + flagRegion, "eu-north-1",
		"--" + importFlagOnCreate, "apply",
	})

	_, err := importOptionsFromCLI(context.Background(), cmd)

	require.Error(t, err)
	assert.ErrorContains(t, err, "unsupported --"+importFlagOnCreate)
}
