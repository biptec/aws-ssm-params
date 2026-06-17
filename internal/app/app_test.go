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

func TestLoadItemsAllowsMissingNamesFileAndRejectsEmptyNamesFile(t *testing.T) {
	items, err := LoadItems(Config{})
	require.NoError(t, err)
	assert.Nil(t, items)

	file := filepath.Join(t.TempDir(), "paths.txt")
	require.NoError(t, os.WriteFile(file, []byte("# only comments\n"), 0o600))

	items, err = LoadItems(Config{NamesFile: file})
	require.Error(t, err)
	assert.Nil(t, items)
	assert.ErrorContains(t, err, "empty")
}

func TestPrepareImportItemsAllowsJSONWithoutNamesFile(t *testing.T) {
	cfg := Config{Region: "eu-north-1"}

	items, err := PrepareImportItems(&cfg, "json")

	require.NoError(t, err)
	assert.Nil(t, items)
	assert.Equal(t, []string{"eu-north-1"}, cfg.Regions)
}

func TestPrepareImportItemsRequiresNamesFileForDotenv(t *testing.T) {
	cfg := Config{Region: "eu-north-1"}

	items, err := PrepareImportItems(&cfg, "dotenv")

	require.Error(t, err)
	assert.Nil(t, items)
	assert.ErrorContains(t, err, "--names-file")
}

func TestPrepareImportItemsLoadsNamesFileForDotenv(t *testing.T) {
	file := filepath.Join(t.TempDir(), "paths.txt")
	require.NoError(t, os.WriteFile(file, []byte("/app/dev/api/JWT_SECRET\n"), 0o600))
	cfg := Config{NamesFile: file, Region: "eu-north-1"}

	items, err := PrepareImportItems(&cfg, "dotenv")

	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "/app/dev/api/JWT_SECRET", items[0].Path)
}

func TestConfigFromCLIReadsGlobalEnvironmentVariables(t *testing.T) {
	t.Setenv("AWS_SSM_PARAMS_REGIONS", "eu-north-1,eu-central-1")
	t.Setenv("AWS_SSM_PARAMS_PROFILE", "dev-profile")
	t.Setenv("AWS_SSM_PARAMS_NAMES_FILE", "paths.txt")
	t.Setenv("AWS_SSM_PARAMS_KEYMAP", "vi")
	t.Setenv("AWS_SSM_PARAMS_SHOW_COLUMNS", "region,type,value")
	t.Setenv("AWS_SSM_PARAMS_NO_COLOR", "true")
	t.Setenv("AWS_SSM_PARAMS_ALLOW_NAMES_FILE_UPDATE", "true")

	cfg, err := ConfigFromCLI(testCLIContext(t, nil))

	require.NoError(t, err)
	assert.Equal(t, "eu-north-1", cfg.Region)
	assert.Equal(t, []string{"eu-north-1", "eu-central-1"}, cfg.Regions)
	assert.Equal(t, "dev-profile", cfg.Profile)
	assert.Equal(t, "paths.txt", cfg.NamesFile)
	assert.Equal(t, "vi", cfg.Keymap)
	assert.Equal(t, []string{"region", "type", "value"}, cfg.ShowColumns)
	assert.True(t, cfg.NoColor)
	assert.True(t, cfg.AllowNamesFileUpdate)
}

func TestConfigFromCLIFlagsOverrideEnvironmentVariables(t *testing.T) {
	t.Setenv("AWS_SSM_PARAMS_REGIONS", "eu-north-1")
	t.Setenv("AWS_SSM_PARAMS_PROFILE", "env-profile")
	t.Setenv("AWS_SSM_PARAMS_NAMES_FILE", "env-paths.txt")
	t.Setenv("AWS_SSM_PARAMS_KEYMAP", "vi")
	t.Setenv("AWS_SSM_PARAMS_SHOW_COLUMNS", "region,type")

	ctx := testCLIContext(t, []string{
		"--regions", "eu-central-1",
		"--profile", "cli-profile",
		"--names-file", "cli-paths.txt",
		"--keymap", "emacs",
		"--show-columns", "date,value",
	})
	cfg, err := ConfigFromCLI(ctx)

	require.NoError(t, err)
	assert.Equal(t, "eu-central-1", cfg.Region)
	assert.Equal(t, []string{"eu-central-1"}, cfg.Regions)
	assert.Equal(t, "cli-profile", cfg.Profile)
	assert.Equal(t, "cli-paths.txt", cfg.NamesFile)
	assert.Equal(t, "emacs", cfg.Keymap)
	assert.Equal(t, []string{"date", "value"}, cfg.ShowColumns)
}

func TestConfigFromCLIRejectsUnsupportedColumns(t *testing.T) {
	ctx := testCLIContext(t, []string{"--show-columns", "region,source"})

	cfg, err := ConfigFromCLI(ctx)

	assert.Empty(t, cfg)
	require.Error(t, err)
	assert.ErrorContains(t, err, "unsupported --show-columns value")
}

func testCLIContext(t *testing.T, args []string) *cli.Context {
	t.Helper()
	set := flag.NewFlagSet("test", flag.ContinueOnError)
	regions := cli.NewStringSlice()
	set.Var(regions, "regions", "")
	set.Bool("all-regions", false, "")
	set.String("profile", "", "")
	set.Bool("no-color", false, "")
	set.String("keymap", "", "")
	set.String("names-file", "", "")
	names := cli.NewStringSlice()
	set.Var(names, "names", "")
	fields := cli.NewStringSlice()
	set.Var(fields, "fields", "")
	set.Bool("without-decryption", false, "")
	set.String("sort", "", "")
	set.Bool("show-secure-values", false, "")
	set.Bool("no-confirm-overwrite-file", false, "")
	set.Bool("no-confirm-write-securestring", false, "")
	set.Bool("no-confirm-delete-one", false, "")
	set.Bool("no-confirm-delete-all", false, "")
	set.String("show-columns", "", "")
	set.Bool("allow-names-file-update", false, "")
	require.NoError(t, set.Parse(args))
	return cli.NewContext(cli.NewApp(), set, nil)
}

func TestConfigFromCLIRejectsNamesFileUpdateWithoutNamesFile(t *testing.T) {
	ctx := testCLIContext(t, []string{"--allow-names-file-update"})

	cfg, err := ConfigFromCLI(ctx)

	assert.Empty(t, cfg)
	require.Error(t, err)
	assert.ErrorContains(t, err, "--allow-names-file-update requires --names-file")
}

func TestConfigFromCLIParsesGlobalNamesAndFields(t *testing.T) {
	ctx := testCLIContext(t, []string{"--names", "/app/a,/app/b", "--fields", "type,value"})

	cfg, err := ConfigFromCLI(ctx)

	require.NoError(t, err)
	assert.Equal(t, []string{"/app/a", "/app/b"}, cfg.Names)
	assert.Equal(t, []string{"name", "type", "value"}, cfg.Fields)
}

func TestLoadItemsUnionsNamesFileAndNames(t *testing.T) {
	file := filepath.Join(t.TempDir(), "names.txt")
	require.NoError(t, os.WriteFile(file, []byte("/app/a\n/app/b\n"), 0o600))

	items, err := LoadItems(Config{NamesFile: file, Names: []string{"/app/b", "/app/c"}})

	require.NoError(t, err)
	require.Len(t, items, 3)
	assert.Equal(t, []string{"/app/a", "/app/b", "/app/c"}, []string{items[0].Path, items[1].Path, items[2].Path})
}
