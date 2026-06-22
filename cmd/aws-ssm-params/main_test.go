package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCLIHelpShowsTUICommand(t *testing.T) {
	cliApp := newCLIApp([]string{"--help"})
	var out bytes.Buffer
	cliApp.Writer = &out

	err := cliApp.Run(context.Background(), []string{"aws-ssm-params", "--help"})

	require.NoError(t, err)
	assert.Contains(t, out.String(), "tui")
	assert.Contains(t, out.String(), "import")
	assert.Contains(t, out.String(), "export")
	assert.NotContains(t, out.String(), "interactive")
	assert.NotContains(t, out.String(), "\n   get ")
	assert.NotContains(t, out.String(), "\n   put ")
	assert.NotContains(t, out.String(), "--name")
	assert.NotContains(t, out.String(), "--names-file")
	assert.NotContains(t, out.String(), "--json-key-field")
}

func TestTUIHelpUsesShowColumnFlag(t *testing.T) {
	cliApp := newCLIApp([]string{"tui", "--help"})
	var out bytes.Buffer
	cliApp.Writer = &out

	err := cliApp.Run(context.Background(), []string{"aws-ssm-params", "tui", "--help"})

	require.NoError(t, err)
	assert.NotContains(t, out.String(), "--name")
	assert.NotContains(t, out.String(), "--names-file")
	assert.Contains(t, out.String(), "--show-column")
	assert.False(t, strings.Contains(out.String(), "--columns"))
}

func TestCLIHelpUsesSingleToolEnvironmentVariablePerFlag(t *testing.T) {
	cases := [][]string{
		{"aws-ssm-params", "--help"},
		{"aws-ssm-params", "tui", "--help"},
		{"aws-ssm-params", "export", "--help"},
		{"aws-ssm-params", "import", "--help"},
	}
	legacyEnvNames := []string{
		"AWS_SSM_PARAMS_REGIONS",
		"AWS_SSM_PARAMS_FILTERS",
		"AWS_SSM_PARAMS_FILTERS_FILE",
		"AWS_SSM_PARAMS_SHOW_COLUMNS",
		"AWS_SSM_PARAMS_OUTPUT_FIELDS",
		"AWS_SSM_PARAMS_MAP_FIELDS",
		"$NO_COLOR",
	}

	for _, args := range cases {
		cliApp := newCLIApp(args[1:])
		var out bytes.Buffer
		cliApp.Writer = &out

		err := cliApp.Run(context.Background(), args)

		require.NoError(t, err)
		for _, legacyEnvName := range legacyEnvNames {
			assert.NotContains(t, out.String(), legacyEnvName, strings.Join(args, " "))
		}
	}
}

func TestCLIHelpShowsNativeAWSRegionAndProfileAliases(t *testing.T) {
	cliApp := newCLIApp([]string{"--help"})
	var out bytes.Buffer
	cliApp.Writer = &out

	err := cliApp.Run(context.Background(), []string{"aws-ssm-params", "--help"})

	require.NoError(t, err)
	assert.Contains(t, out.String(), "AWS_SSM_PARAMS_REGION")
	assert.Contains(t, out.String(), "AWS_REGION")
	assert.Contains(t, out.String(), "AWS_SSM_PARAMS_PROFILE")
	assert.Contains(t, out.String(), "AWS_PROFILE")
	assert.NotContains(t, out.String(), "$NO_COLOR")
}

func TestInteractiveCommandIsRemoved(t *testing.T) {
	cliApp := newCLIApp([]string{"interactive", "--help"})
	var out bytes.Buffer
	cliApp.Writer = &out

	err := cliApp.Run(context.Background(), []string{"aws-ssm-params", "interactive", "--help"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "No help topic")
}

func TestGetAndPutCommandsAreRemoved(t *testing.T) {
	for _, command := range []string{"get", "put"} {
		cliApp := newCLIApp([]string{command, "--help"})

		err := cliApp.Run(context.Background(), []string{"aws-ssm-params", command, "--help"})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "No help topic")
	}
}

func TestUnknownCommandReturnsError(t *testing.T) {
	cliApp := newCLIApp([]string{"unknown"})
	var out bytes.Buffer
	cliApp.Writer = &out

	err := cliApp.Run(context.Background(), []string{"aws-ssm-params", "unknown"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command: unknown")
}

func TestCLIRejectsCommaSeparatedRegionFlag(t *testing.T) {
	cliApp := newCLIApp([]string{"--region", "eu-north-1,eu-central-1"})

	err := cliApp.Run(context.Background(), []string{"aws-ssm-params", "--region", "eu-north-1,eu-central-1"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "repeat the flag")
}

func TestTUIRejectsRemovedNameFlag(t *testing.T) {
	cliApp := newCLIApp([]string{"tui", "--name", "/app/a"})

	err := cliApp.Run(context.Background(), []string{"aws-ssm-params", "tui", "--name", "/app/a", "--help"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "flag provided but not defined")
}

func TestExportHelpDoesNotExposeInteractiveInventoryFlags(t *testing.T) {
	cliApp := newCLIApp([]string{"export", "--help"})
	var out bytes.Buffer
	cliApp.Writer = &out

	err := cliApp.Run(context.Background(), []string{"aws-ssm-params", "export", "--help"})

	require.NoError(t, err)
	assert.NotContains(t, out.String(), "--name")
	assert.NotContains(t, out.String(), "--names-file")
	assert.NotContains(t, out.String(), "--json-key-field")
	assert.Contains(t, out.String(), "--key-field")
	assert.Contains(t, out.String(), "--sort-by")
	assert.Contains(t, out.String(), "--scalar")
	assert.NotContains(t, out.String(), "--include-missing")
}

func TestImportHelpDoesNotExposeInteractiveInventoryFlags(t *testing.T) {
	cliApp := newCLIApp([]string{"import", "--help"})
	var out bytes.Buffer
	cliApp.Writer = &out

	err := cliApp.Run(context.Background(), []string{"aws-ssm-params", "import", "--help"})

	require.NoError(t, err)
	assert.NotContains(t, out.String(), "--name")
	assert.NotContains(t, out.String(), "--names-file")
	assert.NotContains(t, out.String(), "--json-key-field")
	assert.Contains(t, out.String(), "--key-field")
	assert.Contains(t, out.String(), "--root-path")
	assert.Contains(t, out.String(), "--on-create")
	assert.Contains(t, out.String(), "--on-update")
	assert.Contains(t, out.String(), "--continue-on-error")
	assert.Contains(t, out.String(), "--summary")
	assert.NotContains(t, out.String(), "--no-create")
	assert.NotContains(t, out.String(), "--no-update")
	assert.NotContains(t, out.String(), "--default-value")
	assert.NotContains(t, out.String(), "--default-value-file")
}
