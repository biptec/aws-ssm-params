package app

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
)

func TestCompactStringsTrimsAndDropsEmptyValues(t *testing.T) {
	assert.Equal(t, []string{"eu-north-1", "us-east-1", "eu-central-1"}, compactStrings([]string{" eu-north-1 ", "", "  ", "us-east-1, eu-central-1"}))
}

func TestDedupeStringsPreservesFirstOccurrenceOrder(t *testing.T) {
	assert.Equal(t, []string{"a", "b", "c"}, dedupeStrings([]string{"a", "b", "a", "c", "b"}))
}

func TestApplyItemRegionsUsesWildcardForAllRegionsAndMultiRegion(t *testing.T) {
	items := []inventory.Item{{Path: "/a"}, {Path: "/b"}}
	applyItemRegions(items, Config{AllRegions: true, Region: "eu-north-1"})
	assert.Equal(t, "*", items[0].Region)
	assert.Equal(t, "*", items[1].Region)

	items = []inventory.Item{{Path: "/a"}}
	applyItemRegions(items, Config{Regions: []string{"eu-north-1", "us-east-1"}, Region: "eu-north-1"})
	assert.Equal(t, "*", items[0].Region)

	items = []inventory.Item{{Path: "/a"}}
	applyItemRegions(items, Config{Regions: []string{"eu-north-1"}, Region: "eu-north-1"})
	assert.Equal(t, "eu-north-1", items[0].Region)
}

func TestEnsureAllRegionsSeedRegionKeepsExistingRegion(t *testing.T) {
	cfg := Config{}
	ensureAllRegionsSeedRegion(&cfg)
	assert.Equal(t, allRegionsSeedRegion, cfg.Region)

	cfg = Config{Region: "eu-north-1"}
	ensureAllRegionsSeedRegion(&cfg)
	assert.Equal(t, "eu-north-1", cfg.Region)
}

func TestParseCommonArgFlagsSupportsTailFlags(t *testing.T) {
	args, flags, err := parseCommonArgFlags([]string{"/path", "--file", "secret.txt", "--override", "--type", "string-list"}, "", false, "")

	require.NoError(t, err)
	assert.Equal(t, []string{"/path"}, args)
	assert.Equal(t, "secret.txt", flags.file)
	assert.True(t, flags.override)
	assert.Equal(t, "string-list", flags.parameterType)

	args, flags, err = parseCommonArgFlags([]string{"/path", "-t=string"}, "", false, "")
	require.NoError(t, err)
	assert.Equal(t, []string{"/path"}, args)
	assert.Equal(t, "string", flags.parameterType)
}

func TestParseCommonArgFlagsRequiresFileValue(t *testing.T) {
	args, flags, err := parseCommonArgFlags([]string{"/path", "--file"}, "", false, "")

	require.Error(t, err)
	assert.Nil(t, args)
	assert.Empty(t, flags.file)
	assert.False(t, flags.override)
}

func TestResolveSetTypePreservesExistingTypeAndAllowsOverride(t *testing.T) {
	parameterType, err := resolveSetType("", "String")
	require.NoError(t, err)
	assert.Equal(t, "String", parameterType.String())

	parameterType, err = resolveSetType("string-list", "SecureString")
	require.NoError(t, err)
	assert.Equal(t, "StringList", parameterType.String())

	parameterType, err = resolveSetType("", "")
	require.NoError(t, err)
	assert.Equal(t, "SecureString", parameterType.String())
}

func TestResolveImportTypeUsesRecordExistingFlagThenDefault(t *testing.T) {
	parameterType, err := resolveImportType("string", "SecureString", "StringList")
	require.NoError(t, err)
	assert.Equal(t, "StringList", parameterType.String())

	parameterType, err = resolveImportType("string", "SecureString", "")
	require.NoError(t, err)
	assert.Equal(t, "SecureString", parameterType.String())

	parameterType, err = resolveImportType("string", "", "")
	require.NoError(t, err)
	assert.Equal(t, "String", parameterType.String())
}

func TestLoadItemsAllowsMissingPathsFileAndRejectsEmptyPathsFile(t *testing.T) {
	items, err := LoadItems(Config{})
	require.NoError(t, err)
	assert.Nil(t, items)

	file := filepath.Join(t.TempDir(), "paths.txt")
	require.NoError(t, os.WriteFile(file, []byte("# only comments\n"), 0o600))

	items, err = LoadItems(Config{PathsFile: file})
	require.Error(t, err)
	assert.Nil(t, items)
	assert.ErrorContains(t, err, "empty")
}

func TestPrepareImportItemsAllowsJSONWithoutPathsFile(t *testing.T) {
	cfg := Config{Region: "eu-north-1"}

	items, err := PrepareImportItems(&cfg, "json")

	require.NoError(t, err)
	assert.Nil(t, items)
	assert.Equal(t, []string{"eu-north-1"}, cfg.Regions)
}

func TestPrepareImportItemsRequiresPathsFileForDotenv(t *testing.T) {
	cfg := Config{Region: "eu-north-1"}

	items, err := PrepareImportItems(&cfg, "dotenv")

	require.Error(t, err)
	assert.Nil(t, items)
	assert.ErrorContains(t, err, "--paths-file")
}

func TestPrepareImportItemsLoadsPathsFileForDotenv(t *testing.T) {
	file := filepath.Join(t.TempDir(), "paths.txt")
	require.NoError(t, os.WriteFile(file, []byte("/app/dev/api/JWT_SECRET\n"), 0o600))
	cfg := Config{PathsFile: file, Region: "eu-north-1"}

	items, err := PrepareImportItems(&cfg, "dotenv")

	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "/app/dev/api/JWT_SECRET", items[0].Path)
}

func TestConfigFromCLIReadsGlobalEnvironmentVariables(t *testing.T) {
	t.Setenv("AWS_SSM_PARAMS_REGION", "eu-north-1,eu-central-1")
	t.Setenv("AWS_SSM_PARAMS_PROFILE", "dev-profile")
	t.Setenv("AWS_SSM_PARAMS_PATHS_FILE", "paths.txt")
	t.Setenv("AWS_SSM_PARAMS_KEYMAP", "vi")
	t.Setenv("AWS_SSM_PARAMS_COLUMNS", "region,type,value")
	t.Setenv("AWS_SSM_PARAMS_NO_COLOR", "true")
	t.Setenv("AWS_SSM_PARAMS_ALLOW_PATHS_FILE_UPDATE", "true")

	cfg, err := ConfigFromCLI(testCLIContext(t, nil))

	require.NoError(t, err)
	assert.Equal(t, "eu-north-1", cfg.Region)
	assert.Equal(t, []string{"eu-north-1", "eu-central-1"}, cfg.Regions)
	assert.Equal(t, "dev-profile", cfg.Profile)
	assert.Equal(t, "paths.txt", cfg.PathsFile)
	assert.Equal(t, "vi", cfg.Keymap)
	assert.Equal(t, []string{"region", "type", "value"}, cfg.Columns)
	assert.True(t, cfg.NoColor)
	assert.True(t, cfg.AllowPathsFileUpdate)
}

func TestConfigFromCLIFlagsOverrideEnvironmentVariables(t *testing.T) {
	t.Setenv("AWS_SSM_PARAMS_REGION", "eu-north-1")
	t.Setenv("AWS_SSM_PARAMS_PROFILE", "env-profile")
	t.Setenv("AWS_SSM_PARAMS_PATHS_FILE", "env-paths.txt")
	t.Setenv("AWS_SSM_PARAMS_KEYMAP", "vi")
	t.Setenv("AWS_SSM_PARAMS_COLUMNS", "region,type")

	ctx := testCLIContext(t, []string{
		"--region", "eu-central-1",
		"--profile", "cli-profile",
		"--paths-file", "cli-paths.txt",
		"--keymap", "emacs",
		"--columns", "date,value",
	})
	cfg, err := ConfigFromCLI(ctx)

	require.NoError(t, err)
	assert.Equal(t, "eu-central-1", cfg.Region)
	assert.Equal(t, []string{"eu-central-1"}, cfg.Regions)
	assert.Equal(t, "cli-profile", cfg.Profile)
	assert.Equal(t, "cli-paths.txt", cfg.PathsFile)
	assert.Equal(t, "emacs", cfg.Keymap)
	assert.Equal(t, []string{"date", "value"}, cfg.Columns)
}

func TestConfigFromCLIRejectsUnsupportedColumns(t *testing.T) {
	ctx := testCLIContext(t, []string{"--columns", "region,source"})

	cfg, err := ConfigFromCLI(ctx)

	assert.Empty(t, cfg)
	require.Error(t, err)
	assert.ErrorContains(t, err, "unsupported --columns value")
}

func testCLIContext(t *testing.T, args []string) *cli.Context {
	t.Helper()
	set := flag.NewFlagSet("test", flag.ContinueOnError)
	regions := cli.NewStringSlice()
	set.Var(regions, "region", "")
	set.Bool("all-regions", false, "")
	set.String("profile", "", "")
	set.Bool("no-color", false, "")
	set.String("keymap", "", "")
	set.String("paths-file", "", "")
	set.String("columns", "", "")
	set.Bool("allow-paths-file-update", false, "")
	require.NoError(t, set.Parse(args))
	return cli.NewContext(cli.NewApp(), set, nil)
}

func TestConfigFromCLIRejectsPathsFileUpdateWithoutPathsFile(t *testing.T) {
	ctx := testCLIContext(t, []string{"--allow-paths-file-update"})

	cfg, err := ConfigFromCLI(ctx)

	assert.Empty(t, cfg)
	require.Error(t, err)
	assert.ErrorContains(t, err, "--allow-paths-file-update requires --paths-file")
}
