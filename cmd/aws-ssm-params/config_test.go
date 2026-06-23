package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"

	"github.com/biptec/aws-ssm-params/internal/filter"
)

func TestGlobalOptionsParseRepeatedRegionsAndFilters(t *testing.T) {
	cmd := testParsedCommand(t, globalFlags(), []string{
		"--" + flagRegion, "eu-north-1",
		"--" + flagRegion, "eu-central-1",
		"--" + flagProfile, "cli-profile",
		"--" + flagKeymap, "vi",
		"--" + flagFilter, "name:/prod/*;region:eu*",
	})

	options, err := globalOptionsFromCLI(context.Background(), cmd)

	require.NoError(t, err)
	assert.Equal(t, "eu-north-1", options.Region)
	assert.Equal(t, []string{"eu-north-1", "eu-central-1"}, options.Regions)
	assert.Equal(t, "cli-profile", options.Profile)
	assert.Equal(t, "vi", options.Keymap)
	require.Len(t, options.FilterGroups, 1)
	assert.True(t, options.FilterGroups[0].Match(&filter.Record{Name: "/prod/db", Region: "eu-north-1"}))
}

func TestGlobalOptionsUseCommaSeparatedEnvironmentLists(t *testing.T) {
	t.Setenv(envRegion, "eu-north-1,eu-central-1")
	cmd := testParsedCommand(t, globalFlags(), nil)

	options, err := globalOptionsFromCLI(context.Background(), cmd)

	require.NoError(t, err)
	assert.Equal(t, []string{"eu-north-1", "eu-central-1"}, options.Regions)
}

func TestGlobalOptionsPreferToolEnvironmentAliases(t *testing.T) {
	t.Setenv(envRegion, "eu-north-1,eu-central-1")
	t.Setenv(envAWSRegion, "us-east-1")
	t.Setenv(envProfile, "tool-profile")
	t.Setenv(envAWSProfile, "native-profile")
	cmd := testParsedCommand(t, globalFlags(), nil)

	options, err := globalOptionsFromCLI(context.Background(), cmd)

	require.NoError(t, err)
	assert.Equal(t, "eu-north-1", options.Region)
	assert.Equal(t, "tool-profile", options.Profile)
}

func TestGlobalOptionsRejectRegionWithAllRegions(t *testing.T) {
	cmd := testParsedCommand(t, globalFlags(), []string{
		"--" + flagRegion, "eu-north-1",
		"--" + flagAllRegions,
	})

	_, err := globalOptionsFromCLI(context.Background(), cmd)

	require.Error(t, err)
	assert.ErrorContains(t, err, "--"+flagRegion+" and --"+flagAllRegions)
}

func TestRejectCommaSeparatedFlagArgs(t *testing.T) {
	err := rejectCommaSeparatedFlagArgs(
		[]string{"--" + flagRegion, "eu-north-1,eu-central-1", tuiCommandName},
		flagRegion,
	)

	require.Error(t, err)
	assert.ErrorContains(t, err, "repeat the flag")
}

func TestRejectCommaSeparatedFlagArgsIgnoresDoubleDashTail(t *testing.T) {
	err := rejectCommaSeparatedFlagArgs(
		[]string{"--" + flagRegion, "eu-north-1", "--", "--" + flagRegion, "eu-central-1,us-east-1"},
		flagRegion,
	)

	require.NoError(t, err)
}

func testParsedCommand(t *testing.T, flags []cli.Flag, args []string) *cli.Command {
	t.Helper()

	cmd := &cli.Command{
		Name:   "test",
		Flags:  flags,
		Action: func(context.Context, *cli.Command) error { return nil },
	}
	require.NoError(t, cmd.Run(context.Background(), append([]string{"test"}, args...)))

	return cmd
}
