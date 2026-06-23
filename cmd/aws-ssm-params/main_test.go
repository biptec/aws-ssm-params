package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	removedCommandInteractive = "interactive"
	removedCommandGet         = "get"
	removedCommandPut         = "put"
	unknownCommandName        = "unknown"
	removedFlagName           = "name"

	legacyEnvRegions      = envVarPrefix + "REGIONS"
	legacyEnvFilters      = envVarPrefix + "FILTERS"
	legacyEnvFiltersFile  = envVarPrefix + "FILTERS_FILE"
	legacyEnvShowColumns  = envVarPrefix + "SHOW_COLUMNS"
	legacyEnvOutputFields = envVarPrefix + "OUTPUT_FIELDS"
	legacyEnvMapFields    = envVarPrefix + "MAP_FIELDS"
)

func TestCLIHelpShowsTUICommand(t *testing.T) {
	cliApp := newCLIApp([]string{"--help"})

	var out bytes.Buffer

	cliApp.Writer = &out

	err := cliApp.Run(context.Background(), []string{appName, "--help"})

	require.NoError(t, err)
	assert.Contains(t, out.String(), tuiCommandName)
	assert.Contains(t, out.String(), importCommandName)
	assert.Contains(t, out.String(), exportCommandName)
	assert.Contains(t, out.String(), deleteCommandName)
	assert.NotContains(t, out.String(), "--filters-file")
	assert.NotContains(t, out.String(), "AWS_SSM_PARAMS_FILTER_FILE")
	assert.NotContains(t, out.String(), removedCommandInteractive)
	assert.NotContains(t, out.String(), "\n   get ")
	assert.NotContains(t, out.String(), "\n   put ")
	assert.NotContains(t, out.String(), "--name")
	assert.NotContains(t, out.String(), "--names-file")
	assert.NotContains(t, out.String(), "--json-key-field")
}

func TestTUIHelpUsesShowColumnFlag(t *testing.T) {
	cliApp := newCLIApp([]string{tuiCommandName, "--help"})

	var out bytes.Buffer

	cliApp.Writer = &out

	err := cliApp.Run(context.Background(), []string{appName, tuiCommandName, "--help"})

	require.NoError(t, err)
	assert.NotContains(t, out.String(), "--name")
	assert.NotContains(t, out.String(), "--names-file")
	assert.Contains(t, out.String(), "--"+tuiFlagShowColumn)
	assert.False(t, strings.Contains(out.String(), "--columns"))
}

func TestCLIHelpUsesSingleToolEnvironmentVariablePerFlag(t *testing.T) {
	cases := [][]string{
		{appName, "--help"},
		{appName, tuiCommandName, "--help"},
		{appName, exportCommandName, "--help"},
		{appName, importCommandName, "--help"},
		{appName, deleteCommandName, "--help"},
	}
	legacyEnvNames := []string{
		legacyEnvRegions,
		legacyEnvFilters,
		legacyEnvFiltersFile,
		legacyEnvShowColumns,
		legacyEnvOutputFields,
		legacyEnvMapFields,
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

	err := cliApp.Run(context.Background(), []string{appName, "--help"})

	require.NoError(t, err)
	assert.Contains(t, out.String(), envRegion)
	assert.Contains(t, out.String(), envAWSRegion)
	assert.Contains(t, out.String(), envProfile)
	assert.Contains(t, out.String(), envAWSProfile)
	assert.NotContains(t, out.String(), "$NO_COLOR")
}

func TestInteractiveCommandIsRemoved(t *testing.T) {
	cliApp := newCLIApp([]string{removedCommandInteractive, "--help"})

	var out bytes.Buffer

	cliApp.Writer = &out

	err := cliApp.Run(context.Background(), []string{appName, removedCommandInteractive, "--help"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "No help topic")
}

func TestGetAndPutCommandsAreRemoved(t *testing.T) {
	for _, command := range []string{removedCommandGet, removedCommandPut} {
		cliApp := newCLIApp([]string{command, "--help"})

		err := cliApp.Run(context.Background(), []string{appName, command, "--help"})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "No help topic")
	}
}

func TestUnknownCommandReturnsError(t *testing.T) {
	cliApp := newCLIApp([]string{unknownCommandName})

	var out bytes.Buffer

	cliApp.Writer = &out

	err := cliApp.Run(context.Background(), []string{appName, unknownCommandName})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command: "+unknownCommandName)
}

func TestCLIRejectsCommaSeparatedRegionFlag(t *testing.T) {
	cliApp := newCLIApp([]string{"--" + flagRegion, "eu-north-1,eu-central-1"})

	err := cliApp.Run(context.Background(), []string{appName, "--" + flagRegion, "eu-north-1,eu-central-1"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "repeat the flag")
}

func TestTUIRejectsRemovedNameFlag(t *testing.T) {
	cliApp := newCLIApp([]string{tuiCommandName, "--" + removedFlagName, "/app/a"})

	err := cliApp.Run(context.Background(), []string{appName, tuiCommandName, "--" + removedFlagName, "/app/a", "--help"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "flag provided but not defined")
}

func TestExportHelpDoesNotExposeInteractiveInventoryFlags(t *testing.T) {
	cliApp := newCLIApp([]string{exportCommandName, "--help"})

	var out bytes.Buffer

	cliApp.Writer = &out

	err := cliApp.Run(context.Background(), []string{appName, exportCommandName, "--help"})

	require.NoError(t, err)
	assert.NotContains(t, out.String(), "--name")
	assert.NotContains(t, out.String(), "--names-file")
	assert.NotContains(t, out.String(), "--json-key-field")
	assert.Contains(t, out.String(), "--"+exportFlagKeyField)
	assert.Contains(t, out.String(), "--"+exportFlagSortBy)
	assert.Contains(t, out.String(), "--"+exportFlagScalar)
	assert.Contains(t, out.String(), "--"+exportFlagMapPath)
	assert.NotContains(t, out.String(), "--base-path")
	assert.NotContains(t, out.String(), "--include-missing")
}

func TestImportHelpDoesNotExposeInteractiveInventoryFlags(t *testing.T) {
	cliApp := newCLIApp([]string{importCommandName, "--help"})

	var out bytes.Buffer

	cliApp.Writer = &out

	err := cliApp.Run(context.Background(), []string{appName, importCommandName, "--help"})

	require.NoError(t, err)
	assert.NotContains(t, out.String(), "--name")
	assert.NotContains(t, out.String(), "--names-file")
	assert.NotContains(t, out.String(), "--json-key-field")
	assert.Contains(t, out.String(), "--"+importFlagKeyField)
	assert.Contains(t, out.String(), "--"+importFlagMapPath)
	assert.NotContains(t, out.String(), "--base-path")
	assert.Contains(t, out.String(), "--"+importFlagOnCreate)
	assert.Contains(t, out.String(), "--"+importFlagOnUpdate)
	assert.Contains(t, out.String(), "--"+importFlagContinueOnError)
	assert.Contains(t, out.String(), "--"+importFlagSummary)
	assert.Contains(t, out.String(), "--"+importFlagDryRun)
	assert.NotContains(t, out.String(), "--no-create")
	assert.NotContains(t, out.String(), "--no-update")
	assert.NotContains(t, out.String(), "--default-value")
	assert.NotContains(t, out.String(), "--default-value-file")
}

func TestDeleteHelpShowsInputAndSafetyFlags(t *testing.T) {
	cliApp := newCLIApp([]string{deleteCommandName, "--help"})

	var out bytes.Buffer

	cliApp.Writer = &out

	err := cliApp.Run(context.Background(), []string{appName, deleteCommandName, "--help"})

	require.NoError(t, err)
	assert.Contains(t, out.String(), "--"+deleteFlagFormat)
	assert.Contains(t, out.String(), "--"+deleteFlagKeyField)
	assert.Contains(t, out.String(), "--"+deleteFlagMapField)
	assert.Contains(t, out.String(), "--"+deleteFlagMapPath)
	assert.NotContains(t, out.String(), "--base-path")
	assert.Contains(t, out.String(), "--"+deleteFlagNoConfirm)
	assert.Contains(t, out.String(), "--"+deleteFlagDryRun)
	assert.NotContains(t, out.String(), "--ask-confirm")
}
