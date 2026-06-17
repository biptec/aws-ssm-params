package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCLIHelpShowsTUIAliasAndShowColumnsFlag(t *testing.T) {
	cliApp := newCLIApp([]string{"--help"})
	var out bytes.Buffer
	cliApp.Writer = &out

	err := cliApp.Run([]string{"aws-ssm-params", "--help"})

	require.NoError(t, err)
	assert.Contains(t, out.String(), "interactive, tui")
}

func TestInteractiveHelpUsesShowColumnsFlag(t *testing.T) {
	cliApp := newCLIApp([]string{"interactive", "--help"})
	var out bytes.Buffer
	cliApp.Writer = &out

	err := cliApp.Run([]string{"aws-ssm-params", "interactive", "--help"})

	require.NoError(t, err)
	assert.Contains(t, out.String(), "--show-columns")
	assert.False(t, strings.Contains(out.String(), "--columns"))
}

func TestTUIAliasUsesInteractiveCommandHelp(t *testing.T) {
	cliApp := newCLIApp([]string{"tui", "--help"})
	var out bytes.Buffer
	cliApp.Writer = &out

	err := cliApp.Run([]string{"aws-ssm-params", "tui", "--help"})

	require.NoError(t, err)
	assert.Contains(t, out.String(), "Open the interactive TUI")
	assert.Contains(t, out.String(), "--show-columns")
}

func TestCLIRejectsRepeatedRegionsFlag(t *testing.T) {
	cliApp := newCLIApp([]string{"--regions", "eu-north-1", "--regions=eu-central-1", "interactive"})

	err := cliApp.Run([]string{"aws-ssm-params", "--regions", "eu-north-1", "--regions=eu-central-1", "interactive", "--help"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "--regions can only be specified once")
}
