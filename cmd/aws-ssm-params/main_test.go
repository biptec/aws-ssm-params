package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCLIHelpShowsInteractiveCommand(t *testing.T) {
	cliApp := newCLIApp([]string{"--help"})
	var out bytes.Buffer
	cliApp.Writer = &out

	err := cliApp.Run([]string{"aws-ssm-params", "--help"})

	require.NoError(t, err)
	assert.Contains(t, out.String(), "interactive")
	assert.Contains(t, out.String(), "tui")
	assert.NotContains(t, out.String(), "--name")
	assert.NotContains(t, out.String(), "--names-file")
}

func TestInteractiveHelpUsesShowColumnFlag(t *testing.T) {
	cliApp := newCLIApp([]string{"interactive", "--help"})
	var out bytes.Buffer
	cliApp.Writer = &out

	err := cliApp.Run([]string{"aws-ssm-params", "interactive", "--help"})

	require.NoError(t, err)
	assert.Contains(t, out.String(), "--name")
	assert.Contains(t, out.String(), "--names-file")
	assert.Contains(t, out.String(), "--show-column")
	assert.False(t, strings.Contains(out.String(), "--columns"))
}

func TestTUIAliasShowsInteractiveHelp(t *testing.T) {
	cliApp := newCLIApp([]string{"tui", "--help"})
	var out bytes.Buffer
	cliApp.Writer = &out

	err := cliApp.Run([]string{"aws-ssm-params", "tui", "--help"})

	require.NoError(t, err)
	assert.Contains(t, out.String(), "interactive")
	assert.Contains(t, out.String(), "--name")
	assert.Contains(t, out.String(), "--names-file")
	assert.Contains(t, out.String(), "--show-column")
}

func TestUnknownCommandReturnsError(t *testing.T) {
	cliApp := newCLIApp([]string{"unknown"})
	var out bytes.Buffer
	cliApp.Writer = &out

	err := cliApp.Run([]string{"aws-ssm-params", "unknown"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command: unknown")
}

func TestCLIRejectsCommaSeparatedRegionFlag(t *testing.T) {
	cliApp := newCLIApp([]string{"--region", "eu-north-1,eu-central-1", "interactive"})

	err := cliApp.Run([]string{"aws-ssm-params", "--region", "eu-north-1,eu-central-1", "interactive", "--help"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "repeat the flag")
}

func TestCLIRejectsCommaSeparatedNameFlag(t *testing.T) {
	cliApp := newCLIApp([]string{"interactive", "--name", "/app/a,/app/b"})

	err := cliApp.Run([]string{"aws-ssm-params", "interactive", "--name", "/app/a,/app/b", "--help"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "repeat the flag")
}

func TestExportHelpDoesNotExposeInteractiveInventoryFlags(t *testing.T) {
	cliApp := newCLIApp([]string{"export", "--help"})
	var out bytes.Buffer
	cliApp.Writer = &out

	err := cliApp.Run([]string{"aws-ssm-params", "export", "--help"})

	require.NoError(t, err)
	assert.NotContains(t, out.String(), "--name")
	assert.NotContains(t, out.String(), "--names-file")
}

func TestImportHelpDoesNotExposeInteractiveInventoryFlags(t *testing.T) {
	cliApp := newCLIApp([]string{"import", "--help"})
	var out bytes.Buffer
	cliApp.Writer = &out

	err := cliApp.Run([]string{"aws-ssm-params", "import", "--help"})

	require.NoError(t, err)
	assert.NotContains(t, out.String(), "--name")
	assert.NotContains(t, out.String(), "--names-file")
}
