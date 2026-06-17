package app

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	secretfmt "github.com/biptec/aws-ssm-params/internal/format"
	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/biptec/aws-ssm-params/internal/ui"
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
	set.String("regions", "", "")
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

func TestConfigFromCLIParsesCommaSeparatedRegionsFromSingleFlag(t *testing.T) {
	ctx := testCLIContext(t, []string{"--regions", "eu-north-1,eu-central-1"})

	cfg, err := ConfigFromCLI(ctx)

	require.NoError(t, err)
	assert.Equal(t, "eu-north-1", cfg.Region)
	assert.Equal(t, []string{"eu-north-1", "eu-central-1"}, cfg.Regions)
}

func TestConfigFromCLIRejectsRegionsWithAllRegions(t *testing.T) {
	ctx := testCLIContext(t, []string{"--regions", "eu-north-1", "--all-regions"})

	cfg, err := ConfigFromCLI(ctx)

	assert.Empty(t, cfg)
	require.Error(t, err)
	assert.ErrorContains(t, err, "--regions and --all-regions cannot be used together")
}

func TestRejectRepeatedFlagArgsAllowsCommaSeparatedSingleUse(t *testing.T) {
	err := RejectRepeatedFlagArgs([]string{"--regions", "eu-north-1,eu-central-1", "interactive"}, "regions")

	require.NoError(t, err)
}

func TestRejectRepeatedFlagArgsRejectsRepeatedLongFlagForms(t *testing.T) {
	err := RejectRepeatedFlagArgs([]string{"--regions", "eu-north-1", "--regions=eu-central-1", "interactive"}, "regions")

	require.Error(t, err)
	assert.ErrorContains(t, err, "--regions can only be specified once")
}

func TestRejectRepeatedFlagArgsIgnoresArgsAfterDoubleDash(t *testing.T) {
	err := RejectRepeatedFlagArgs([]string{"--regions", "eu-north-1", "--", "--regions", "eu-central-1"}, "regions")

	require.NoError(t, err)
}

func TestFieldAllowedTreatsMissingFieldsAsAllAndNameAsInternal(t *testing.T) {
	assert.True(t, fieldAllowed(nil, "value"))
	assert.True(t, fieldAllowed([]string{"value"}, "name"))
	assert.True(t, fieldAllowed([]string{"name", "value"}, "value"))
	assert.False(t, fieldAllowed([]string{"name", "value"}, "region"))
}

func TestFilterRecordsByNamesScopesImportRecords(t *testing.T) {
	records := []secretfmt.Record{
		{Path: "/app/a", Value: "a"},
		{Path: "/app/b", Value: "b"},
		{Path: "/app/c", Value: "c"},
	}

	filtered := filterRecordsByNames(records, []string{"/app/c", "/app/a"})

	require.Len(t, filtered, 2)
	assert.Equal(t, []string{"/app/a", "/app/c"}, []string{filtered[0].Path, filtered[1].Path})
}

func TestCombinedFilterNamesUsesNamesAndNamesFileItems(t *testing.T) {
	items := []inventory.Item{{Path: "/app/b"}, {Path: "/app/c"}}
	cfg := Config{Names: []string{"/app/a", "/app/b"}}

	names := combinedFilterNames(cfg, items)

	assert.Equal(t, []string{"/app/a", "/app/b", "/app/c"}, names)
}

func TestImportDefaultOptionsDropsDescriptionOutsideFieldsScope(t *testing.T) {
	ctx := testImportCLIContext(t, []string{"--default-description", "desc"})

	opts, err := importDefaultOptions(ctx, Config{Fields: []string{"name", "value"}})

	require.NoError(t, err)
	assert.Empty(t, opts.Description)
}

func TestSetOptionsRejectsMetadataFlagsOutsideFieldsScope(t *testing.T) {
	ctx := testSetCLIContext(t, []string{"--tier", "advanced"})

	opts, err := setOptions(ctx, Config{Fields: []string{"name", "value"}}, true)

	assert.Empty(t, opts)
	require.Error(t, err)
	assert.ErrorContains(t, err, `--tier requires field "tier"`)
}

func testImportCLIContext(t *testing.T, args []string) *cli.Context {
	t.Helper()
	set := flag.NewFlagSet("import", flag.ContinueOnError)
	set.Bool("default-override", false, "")
	set.String("default-type", "", "")
	set.String("default-tier", "", "")
	set.String("default-data-type", "", "")
	set.String("default-region", "", "")
	set.String("default-description", "", "")
	require.NoError(t, set.Parse(args))
	return cli.NewContext(cli.NewApp(), set, nil)
}

func testSetCLIContext(t *testing.T, args []string) *cli.Context {
	t.Helper()
	set := flag.NewFlagSet("set", flag.ContinueOnError)
	set.String("tier", "", "")
	set.String("data-type", "", "")
	set.String("description", "", "")
	set.String("policies", "", "")
	set.String("policies-file", "", "")
	require.NoError(t, set.Parse(args))
	return cli.NewContext(cli.NewApp(), set, nil)
}

func TestExportFieldsDefaultsToAllFields(t *testing.T) {
	fields := exportFields(Config{})

	assert.Equal(t, []string{"name", "region", "type", "tier", "data-type", "policies", "description", "value", "date", "version", "len", "sha256", "user"}, fields)
}

func TestExportRecordFromStatusIncludesAllFieldsWhenFieldsOmitted(t *testing.T) {
	status := ui.Status{
		Item:         inventory.Item{Path: "/app/prod/api/key", Region: "eu-north-1"},
		Exists:       true,
		Type:         "SecureString",
		Tier:         "Advanced",
		DataType:     "text",
		Policies:     `[{"Type":"Expiration"}]`,
		Description:  "API key",
		Value:        "secret",
		Modified:     "2026-06-17T00:00:00Z",
		Version:      7,
		Length:       6,
		SHA256Prefix: "2bb80d53",
		User:         "arn:aws:iam::123:user/dev",
	}

	record := exportRecordFromStatus(status, exportFields(Config{}))

	assert.Equal(t, []string{"name", "region", "type", "tier", "data-type", "policies", "description", "value", "date", "version", "len", "sha256", "user"}, record.Fields)
	assert.Equal(t, "eu-north-1", record.Region)
	assert.Equal(t, "SecureString", record.Type)
	assert.Equal(t, "Advanced", record.Tier)
	assert.Equal(t, "text", record.DataType)
	assert.Equal(t, `[{"Type":"Expiration"}]`, record.Policies)
	assert.Equal(t, "API key", record.Description)
	assert.Equal(t, "secret", record.Value)
	assert.Equal(t, "2026-06-17T00:00:00Z", record.Date)
	assert.Equal(t, int64(7), record.Version)
	assert.Equal(t, 6, record.Len)
	assert.Equal(t, "2bb80d53", record.SHA256)
	assert.Equal(t, "arn:aws:iam::123:user/dev", record.User)
}

func TestExportRecordFromStatusRespectsExplicitFields(t *testing.T) {
	status := ui.Status{
		Item:        inventory.Item{Path: "/app/prod/api/key", Region: "eu-north-1"},
		Exists:      true,
		Type:        "SecureString",
		Value:       "secret",
		Description: "API key",
	}

	record := exportRecordFromStatus(status, []string{"name", "value"})

	assert.Equal(t, []string{"name", "value"}, record.Fields)
	assert.Equal(t, "secret", record.Value)
	assert.Empty(t, record.Region)
	assert.Empty(t, record.Type)
	assert.Empty(t, record.Description)
}

func TestParseGetArgFlagsDefaultsToValueAndSupportsTailField(t *testing.T) {
	args, flags, err := parseGetArgFlags([]string{"/app/key", "--field", "type", "--file=out.txt"}, "", "")

	require.NoError(t, err)
	assert.Equal(t, []string{"/app/key"}, args)
	assert.Equal(t, "type", flags.field)
	assert.Equal(t, "out.txt", flags.file)
}

func TestParseGetArgFlagsRejectsMultipleFields(t *testing.T) {
	args, flags, err := parseGetArgFlags([]string{"/app/key"}, "", "type,value")

	require.Error(t, err)
	assert.Nil(t, args)
	assert.Empty(t, flags.field)
}

func TestImportOptionsForRecordUsesRecordMetadataWhenAllowed(t *testing.T) {
	record := secretfmt.Record{
		Fields:      []string{"name", "tier", "data-type", "description", "policies"},
		Tier:        "Advanced",
		DataType:    "aws:ec2:image",
		Description: "from file",
		Policies:    `[{"Type":"Expiration"}]`,
	}
	defaults := ssmPutOptionsForTest(t, "standard", "text", "default desc")

	opts, err := importOptionsForRecord(record, defaults, Config{})

	require.NoError(t, err)
	assert.Equal(t, "Advanced", opts.Tier.String())
	assert.Equal(t, "aws:ec2:image", opts.DataType.String())
	assert.Equal(t, "from file", opts.Description)
	assert.Equal(t, `[{"Type":"Expiration"}]`, opts.Policies)
}

func TestImportOptionsForRecordIgnoresMetadataOutsideFieldsScope(t *testing.T) {
	record := secretfmt.Record{
		Fields:      []string{"name", "tier", "data-type", "description", "policies"},
		Tier:        "Advanced",
		DataType:    "aws:ec2:image",
		Description: "from file",
		Policies:    `[{"Type":"Expiration"}]`,
	}
	defaults := ssmPutOptionsForTest(t, "standard", "text", "default desc")

	opts, err := importOptionsForRecord(record, defaults, Config{Fields: []string{"name", "value"}})

	require.NoError(t, err)
	assert.Equal(t, "Standard", opts.Tier.String())
	assert.Equal(t, "text", opts.DataType.String())
	assert.Equal(t, "default desc", opts.Description)
	assert.Empty(t, opts.Policies)
}

func TestGetParameterFieldReturnsSelectedValueFields(t *testing.T) {
	client := fakeSSMClient{
		parameter: ssm.Parameter{Name: "/app/key", Region: "eu-north-1", Type: "SecureString", Value: "secret", Version: 7, Modified: "2026-06-17T00:00:00Z"},
	}

	value, err := getParameterField(client, "/app/key", "value", "eu-north-1")
	require.NoError(t, err)
	assert.Equal(t, "secret", value)

	version, err := getParameterField(client, "/app/key", "version", "eu-north-1")
	require.NoError(t, err)
	assert.Equal(t, "7", version)

	length, err := getParameterField(client, "/app/key", "len", "eu-north-1")
	require.NoError(t, err)
	assert.Equal(t, "6", length)
}

func TestGetParameterFieldReturnsSelectedMetadataFields(t *testing.T) {
	client := fakeSSMClient{
		parameter: ssm.Parameter{Name: "/app/key", Region: "eu-north-1", Type: "SecureString", Value: "secret", Version: 7, Modified: "2026-06-17T00:00:00Z"},
		metadata:  ssm.Metadata{Name: "/app/key", Region: "eu-north-1", Type: "SecureString", Tier: "Advanced", DataType: "text", Policies: `[{"Type":"Expiration"}]`, Description: "API key", User: "arn:user/dev", Modified: "2026-06-18T00:00:00Z"},
	}

	for field, expected := range map[string]string{
		"name":        "/app/key",
		"region":      "eu-north-1",
		"type":        "SecureString",
		"tier":        "Advanced",
		"data-type":   "text",
		"policies":    `[{"Type":"Expiration"}]`,
		"description": "API key",
		"date":        "2026-06-18T00:00:00Z",
		"user":        "arn:user/dev",
	} {
		actual, err := getParameterField(client, "/app/key", field, "eu-north-1")
		require.NoError(t, err, field)
		assert.Equal(t, expected, actual, field)
	}
}

func ssmPutOptionsForTest(t *testing.T, tierValue, dataTypeValue, description string) ssm.PutParameterOptions {
	t.Helper()
	tier, err := ssm.ParseParameterTier(tierValue)
	require.NoError(t, err)
	dataType, err := ssm.ParseParameterDataType(dataTypeValue)
	require.NoError(t, err)
	return ssm.PutParameterOptions{Tier: tier, DataType: dataType, Description: description}
}

type fakeSSMClient struct {
	parameter ssm.Parameter
	metadata  ssm.Metadata
}

func (f fakeSSMClient) CheckAccess() error                 { return nil }
func (f fakeSSMClient) ListRegions() ([]string, error)     { return nil, nil }
func (f fakeSSMClient) ForRegion(region string) ssm.Client { return f }
func (f fakeSSMClient) DefaultRegion() string              { return f.parameter.Region }
func (f fakeSSMClient) Get(path string) (ssm.Parameter, error) {
	if f.parameter.Name == path {
		return f.parameter, nil
	}
	return ssm.Parameter{}, ssm.ErrNotFound
}
func (f fakeSSMClient) GetMany(paths []string) (map[string]ssm.Parameter, map[string]error) {
	return nil, nil
}
func (f fakeSSMClient) DescribeMany(paths []string) map[string]ssm.Metadata {
	if f.metadata.Name == "" {
		return map[string]ssm.Metadata{}
	}
	return map[string]ssm.Metadata{f.metadata.Name: f.metadata}
}
func (f fakeSSMClient) ListParameterMetadata() ([]ssm.Metadata, error) { return nil, nil }
func (f fakeSSMClient) PutParameter(path, value string, parameterType ssm.ParameterType) error {
	return nil
}
func (f fakeSSMClient) PutParameterWithOptions(path, value string, parameterType ssm.ParameterType, opts ssm.PutParameterOptions) error {
	return nil
}
func (f fakeSSMClient) DeleteMany(paths []string) error { return nil }
