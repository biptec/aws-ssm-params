package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/biptec/aws-ssm-params/internal/app"
	importcmd "github.com/biptec/aws-ssm-params/internal/app/import"
	"github.com/biptec/aws-ssm-params/internal/textio"
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
		"--" + importFlagMapPath, "/app/dev:/dev",
		"--" + importFlagDryRun,
	})

	options, err := importOptionsFromCLI(context.Background(), cmd)

	require.NoError(t, err)
	assert.Equal(t, "String", options.DefaultType.String())
	assert.Equal(t, "Advanced", options.DefaultOptions.Tier.String())
	assert.Equal(t, "description", options.DefaultOptions.Description)
	assert.Equal(t, importcmd.PolicySkip, options.Policy.OnCreate)
	assert.Equal(t, importcmd.PolicyAsk, options.Policy.OnUpdate)
	assert.Equal(t, app.PathMappings{{AWSPath: "/app/dev", FilePath: "/dev"}}, options.PathMappings)
	assert.True(t, options.DryRun)
}

func TestImportOptionsReadEnvironmentFlags(t *testing.T) {
	policiesFile := filepath.Join(t.TempDir(), "policies.json")
	require.NoError(t, os.WriteFile(policiesFile, []byte(`[{"Type":"Expiration"}]`), 0o600))

	t.Setenv(envRegion, "eu-north-1")
	t.Setenv(importEnvMapField, textio.FieldName+":title,"+textio.FieldRegion+":area")
	t.Setenv(importEnvMapPath, "/app/dev:/dev")
	t.Setenv(importEnvFormat, string(textio.FormatJSON))
	t.Setenv(importEnvKeyField, textio.FieldName)
	t.Setenv(importEnvOnCreate, importPolicySkipValue)
	t.Setenv(importEnvOnUpdate, importPolicyErrorValue)
	t.Setenv(importEnvContinueOnError, "true")
	t.Setenv(importEnvSummary, "true")
	t.Setenv(importEnvDryRun, "true")
	t.Setenv(importEnvDefaultType, "secure-string")
	t.Setenv(importEnvDefaultTier, "advanced")
	t.Setenv(importEnvDefaultDataType, "text")
	t.Setenv(importEnvDefaultRegion, "eu-central-1")
	t.Setenv(importEnvDefaultDescription, "from env")
	t.Setenv(importEnvDefaultPoliciesFile, policiesFile)

	flags := append(globalFlags(), importCLICommand().Flags...)
	cmd := testParsedCommand(t, flags, nil)

	options, err := importOptionsFromCLI(context.Background(), cmd)

	require.NoError(t, err)
	assert.Equal(t, "eu-north-1", options.Region)
	assert.Equal(t, textio.FormatJSON, options.Format)
	assert.Equal(t, textio.FieldName, options.KeyField)
	assert.Equal(t, app.PathMappings{{AWSPath: "/app/dev", FilePath: "/dev"}}, options.PathMappings)
	assert.Equal(t, importcmd.PolicySkip, options.Policy.OnCreate)
	assert.Equal(t, importcmd.PolicyError, options.Policy.OnUpdate)
	assert.True(t, options.ContinueOnError)
	assert.True(t, options.Summary)
	assert.True(t, options.DryRun)
	assert.Equal(t, "SecureString", options.DefaultType.String())
	assert.Equal(t, "Advanced", options.DefaultOptions.Tier.String())
	assert.Equal(t, "text", options.DefaultOptions.DataType.String())
	assert.Equal(t, "eu-central-1", options.DefaultRegion)
	assert.Equal(t, "from env", options.DefaultOptions.Description)
	assert.Equal(t, `[{"Type":"Expiration"}]`, options.DefaultOptions.Policies)
	assert.Equal(t, textio.FieldMappings{
		{AWSName: textio.FieldName, FileName: "title"},
		{AWSName: textio.FieldRegion, FileName: "area"},
	}, options.FieldMappings)
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

func TestImportPolicyDefaultsCreateWithoutPromptAndAsksOnUpdate(t *testing.T) {
	flags := append(globalFlags(), importCLICommand().Flags...)
	cmd := testParsedCommand(t, flags, []string{
		"--" + flagRegion, "eu-north-1",
	})

	options, err := importOptionsFromCLI(context.Background(), cmd)

	require.NoError(t, err)
	assert.Equal(t, importcmd.PolicyNone, options.Policy.OnCreate)
	assert.Equal(t, importcmd.PolicyAsk, options.Policy.OnUpdate)
}

func TestImportPolicyAcceptsExplicitNone(t *testing.T) {
	flags := append(globalFlags(), importCLICommand().Flags...)
	cmd := testParsedCommand(t, flags, []string{
		"--" + flagRegion, "eu-north-1",
		"--" + importFlagOnCreate, importPolicyNoneValue,
		"--" + importFlagOnUpdate, importPolicyNoneValue,
	})

	options, err := importOptionsFromCLI(context.Background(), cmd)

	require.NoError(t, err)
	assert.Equal(t, importcmd.PolicyNone, options.Policy.OnCreate)
	assert.Equal(t, importcmd.PolicyNone, options.Policy.OnUpdate)
}
