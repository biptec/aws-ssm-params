package app

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"

	"github.com/biptec/aws-ssm-params/internal/filter"
	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/textio"
)

func TestCompactStringsTrimsAndDropsEmptyValues(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"eu-north-1", "us-east-1, eu-central-1"}, compactStrings([]string{" eu-north-1 ", "", "  ", "us-east-1, eu-central-1"}))
}

func TestDedupeStringsPreservesFirstOccurrenceOrder(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"a", "b", "c"}, dedupeStrings([]string{"a", "b", "a", "c", "b"}))
}

func TestConfigFromCLIParsesRepeatedRegionsFiltersAndFields(t *testing.T) {
	ctx := testCLIContext(t, []string{
		"--region", "eu-north-1",
		"--region", "eu-central-1",
		"--profile", "cli-profile",
		"--keymap", "vi",
		"--filter", "name:/prod/*;region:eu*",
		"--output-field", "name",
		"--output-field", "value",
		"--map-field", "name:title",
		"--map-field", "value:text",
		"--show-column", "name",
		"--show-column", "value",
		"--sort-by", "name:asc",
		"--sort-by", "type:desc",
		"--with-decryption",
	})

	cfg, err := ConfigFromCLI(ctx)

	require.NoError(t, err)
	assert.Equal(t, "eu-north-1", cfg.Region)
	assert.Equal(t, []string{"eu-north-1", "eu-central-1"}, cfg.Regions)
	assert.Equal(t, "cli-profile", cfg.Profile)
	assert.Equal(t, "vi", cfg.Keymap)
	assert.True(t, cfg.WithDecryption)
	assert.Equal(t, []string{"path", "value"}, cfg.ShowColumns)
	assert.Equal(t, []string{"name:asc", "type:desc"}, cfg.SortColumns)
	assert.Empty(t, cfg.InventoryItems)
	assert.Equal(t, textio.Fields{"name", "value"}, cfg.Fields)
	assert.Equal(t, textio.FieldMappings{{AWSName: "name", FileName: "title"}, {AWSName: "value", FileName: "text"}}, cfg.FieldMappings)
	require.Len(t, cfg.FilterGroups, 1)
	assert.True(t, cfg.FilterGroups[0].Match(filter.Record{Name: "/prod/db", Region: "eu-north-1"}))
}

func TestConfigFromCLIUsesCommaSeparatedEnvironmentLists(t *testing.T) {
	t.Setenv("AWS_SSM_PARAMS_REGION", "eu-north-1,eu-central-1")
	t.Setenv("AWS_SSM_PARAMS_OUTPUT_FIELD", "name,value")
	t.Setenv("AWS_SSM_PARAMS_MAP_FIELD", "name:title,value:text")
	t.Setenv("AWS_SSM_PARAMS_SHOW_COLUMN", "name,value")
	t.Setenv("AWS_SSM_PARAMS_SORT_BY", "name:asc,type:desc")
	t.Setenv("AWS_SSM_PARAMS_WITH_DECRYPTION", "true")

	ctx := testCLIContext(t, nil)
	cfg, err := ConfigFromCLI(ctx)

	require.NoError(t, err)
	assert.Equal(t, "eu-north-1", cfg.Region)
	assert.Equal(t, []string{"eu-north-1", "eu-central-1"}, cfg.Regions)
	assert.True(t, cfg.WithDecryption)
	assert.Equal(t, textio.Fields{"name", "value"}, cfg.Fields)
	assert.Equal(t, textio.FieldMappings{{AWSName: "name", FileName: "title"}, {AWSName: "value", FileName: "text"}}, cfg.FieldMappings)
	assert.Equal(t, []string{"path", "value"}, cfg.ShowColumns)
	assert.Equal(t, []string{"name:asc", "type:desc"}, cfg.SortColumns)
	assert.Empty(t, cfg.InventoryItems)
}

func TestConfigFromCLIPrefersToolEnvOverNativeAWSAliases(t *testing.T) {
	t.Setenv("AWS_SSM_PARAMS_REGION", "eu-north-1,eu-central-1")
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_SSM_PARAMS_PROFILE", "tool-profile")
	t.Setenv("AWS_PROFILE", "native-profile")

	cfg, err := ConfigFromCLI(testCLIContext(t, nil))

	require.NoError(t, err)
	assert.Equal(t, "eu-north-1", cfg.Region)
	assert.Equal(t, []string{"eu-north-1", "eu-central-1"}, cfg.Regions)
	assert.Equal(t, "tool-profile", cfg.Profile)
}

func TestConfigFromCLIUsesNativeAWSAliasesWhenToolEnvIsUnset(t *testing.T) {
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_PROFILE", "native-profile")

	cfg, err := ConfigFromCLI(testCLIContext(t, nil))

	require.NoError(t, err)
	assert.Equal(t, "us-east-1", cfg.Region)
	assert.Equal(t, []string{"us-east-1"}, cfg.Regions)
	assert.Equal(t, "native-profile", cfg.Profile)
}

func TestPrepareItemsLoadsExplicitInventorySources(t *testing.T) {
	cfg := Config{
		Region: "eu-north-1",
		InventoryItems: []inventory.Item{
			{Path: "/app/from-stdin", Kind: "path-file", Source: "stdin", SecretName: "from-stdin"},
			{Path: "/app/from-stdin", Kind: "path-file", Source: "stdin", SecretName: "duplicate"},
		},
	}

	items, err := PrepareItems(context.Background(), &cfg)

	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "/app/from-stdin", items[0].Path)
	assert.Equal(t, "stdin", items[0].Source)
	assert.Equal(t, "eu-north-1", items[0].Region)
}

func TestPrepareItemsMarksExplicitInventoryWildcardForMultipleRegions(t *testing.T) {
	cfg := Config{
		Regions:        []string{"eu-north-1", "eu-central-1"},
		Region:         "eu-north-1",
		InventoryItems: []inventory.Item{{Path: "/app/shared", Kind: "path-file", Source: "stdin", SecretName: "shared"}},
	}

	items, err := PrepareItems(context.Background(), &cfg)

	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "/app/shared", items[0].Path)
	assert.Equal(t, "*", items[0].Region)
}

func TestConfigFromCLIRejectsRegionWithAllRegions(t *testing.T) {
	cfg, err := ConfigFromCLI(testCLIContext(t, []string{"--region", "eu-north-1", "--all-regions"}))

	assert.Empty(t, cfg)
	require.Error(t, err)
	assert.ErrorContains(t, err, "--region and --all-regions")
}

func TestRejectCommaSeparatedFlagArgsRejectsCLICommas(t *testing.T) {
	err := RejectCommaSeparatedFlagArgs([]string{"--region", "eu-north-1,eu-central-1", "tui"}, "region")

	require.Error(t, err)
	assert.ErrorContains(t, err, "repeat the flag")
}

func TestRejectCommaSeparatedFlagArgsIgnoresArgsAfterDoubleDash(t *testing.T) {
	err := RejectCommaSeparatedFlagArgs([]string{"--region", "eu-north-1", "--", "--region", "eu-central-1,us-east-1"}, "region")

	require.NoError(t, err)
}

func TestConfigFromCLIRejectsOutputFieldAliasSyntax(t *testing.T) {
	cfg, err := ConfigFromCLI(testCLIContext(t, []string{"--output-field", "value:text"}))

	assert.Empty(t, cfg)
	require.Error(t, err)
	assert.ErrorContains(t, err, "unsupported --output-field value")
}

func testCLIContext(t *testing.T, args []string) *CLIContext {
	t.Helper()
	cmd := &cli.Command{
		Name: "test",
		Flags: []cli.Flag{
			&cli.StringSliceFlag{Name: "region", Sources: cli.EnvVars("AWS_SSM_PARAMS_REGION", "AWS_REGION")},
			&cli.BoolFlag{Name: "all-regions", Sources: cli.EnvVars("AWS_SSM_PARAMS_ALL_REGIONS")},
			&cli.StringFlag{Name: "profile", Sources: cli.EnvVars("AWS_SSM_PARAMS_PROFILE", "AWS_PROFILE")},
			&cli.BoolFlag{Name: "no-color", Sources: cli.EnvVars("AWS_SSM_PARAMS_NO_COLOR")},
			&cli.StringFlag{Name: "keymap", Value: "emacs", Sources: cli.EnvVars("AWS_SSM_PARAMS_KEYMAP")},
			&cli.StringFlag{Name: "filters-file", Sources: cli.EnvVars("AWS_SSM_PARAMS_FILTER_FILE")},
			&cli.StringSliceFlag{Name: "filter", Sources: cli.EnvVars("AWS_SSM_PARAMS_FILTER")},
			&cli.StringSliceFlag{Name: "output-field", Sources: cli.EnvVars("AWS_SSM_PARAMS_OUTPUT_FIELD")},
			&cli.StringSliceFlag{Name: "map-field", Sources: cli.EnvVars("AWS_SSM_PARAMS_MAP_FIELD")},
			&cli.BoolFlag{Name: "with-decryption", Sources: cli.EnvVars("AWS_SSM_PARAMS_WITH_DECRYPTION")},
			&cli.StringSliceFlag{Name: "sort-by", Sources: cli.EnvVars("AWS_SSM_PARAMS_SORT_BY")},
			&cli.StringSliceFlag{Name: "show-column", Sources: cli.EnvVars("AWS_SSM_PARAMS_SHOW_COLUMN")},
			&cli.BoolFlag{Name: "no-confirm-overwrite-file"},
			&cli.BoolFlag{Name: "no-confirm-write-securestring"},
			&cli.BoolFlag{Name: "no-confirm-delete-one"},
			&cli.BoolFlag{Name: "no-confirm-delete-all"},
		},
		Action: func(context.Context, *cli.Command) error { return nil },
	}
	require.NoError(t, cmd.Run(context.Background(), append([]string{"test"}, args...)))
	return NewCLIContext(context.Background(), cmd)
}
