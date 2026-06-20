package app

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/biptec/aws-ssm-params/internal/filter"
	secretfmt "github.com/biptec/aws-ssm-params/internal/format"
	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/biptec/aws-ssm-params/internal/ui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v2"
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
		"--name", "/prod/db",
		"--name", "/prod/api",
		"--field", "name:title",
		"--field", "value:text",
		"--show-column", "name",
		"--show-column", "value",
		"--sort-column", "name:asc",
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
	assert.Equal(t, "name:asc", cfg.SortColumn)
	assert.Equal(t, []string{"/prod/db", "/prod/api"}, cfg.Names)
	assert.Equal(t, []string{"name", "value"}, cfg.Fields)
	assert.Equal(t, []secretfmt.FieldMapping{{AWSName: "name", FileName: "title"}, {AWSName: "value", FileName: "text"}}, cfg.FieldMappings)
	require.Len(t, cfg.FilterGroups, 1)
	assert.True(t, cfg.FilterGroups[0].Match(filter.Record{Name: "/prod/db", Region: "eu-north-1"}))
}

func TestConfigFromCLIUsesCommaSeparatedEnvironmentLists(t *testing.T) {
	t.Setenv("AWS_SSM_PARAMS_REGIONS", "eu-north-1,eu-central-1")
	t.Setenv("AWS_SSM_PARAMS_FIELDS", "name:title,value:text")
	t.Setenv("AWS_SSM_PARAMS_SHOW_COLUMNS", "name,value")
	t.Setenv("AWS_SSM_PARAMS_NAME", "/app/a,/app/b")
	t.Setenv("AWS_SSM_PARAMS_NAMES_FILE", "/tmp/params.txt")
	t.Setenv("AWS_SSM_PARAMS_WITH_DECRYPTION", "true")

	ctx := testCLIContext(t, nil)
	cfg, err := ConfigFromCLI(ctx)

	require.NoError(t, err)
	assert.Equal(t, "eu-north-1", cfg.Region)
	assert.Equal(t, []string{"eu-north-1", "eu-central-1"}, cfg.Regions)
	assert.True(t, cfg.WithDecryption)
	assert.Equal(t, []string{"name", "value"}, cfg.Fields)
	assert.Equal(t, []secretfmt.FieldMapping{{AWSName: "name", FileName: "title"}, {AWSName: "value", FileName: "text"}}, cfg.FieldMappings)
	assert.Equal(t, []string{"path", "value"}, cfg.ShowColumns)
	assert.Equal(t, []string{"/app/a", "/app/b"}, cfg.Names)
	assert.Equal(t, "/tmp/params.txt", cfg.NamesFile)
}

func TestConfigFromCLIUsesInventoryEnvironmentOnlyForInteractive(t *testing.T) {
	t.Setenv("AWS_SSM_PARAMS_NAME", "/app/a,/app/b")
	t.Setenv("AWS_SSM_PARAMS_NAMES_FILE", "/tmp/params.txt")

	ctx := testCLIContext(t, nil)
	ctx.Command = &cli.Command{Name: "export"}
	cfg, err := ConfigFromCLI(ctx)

	require.NoError(t, err)
	assert.Empty(t, cfg.Names)
	assert.Empty(t, cfg.NamesFile)
}

func TestPrepareItemsLoadsExplicitNamesAndNamesFile(t *testing.T) {
	pathsFile := filepath.Join(t.TempDir(), "names.txt")
	require.NoError(t, os.WriteFile(pathsFile, []byte("/app/from-file\n"), 0o600))
	ctx := testCLIContext(t, []string{"--region", "eu-north-1", "--name", "/app/inline", "--names-file", pathsFile})
	cfg, err := ConfigFromCLI(ctx)
	require.NoError(t, err)

	items, err := PrepareItems(context.Background(), &cfg)

	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "/app/from-file", items[0].Path)
	assert.Equal(t, pathsFile, items[0].Source)
	assert.Equal(t, "eu-north-1", items[0].Region)
	assert.Equal(t, "/app/inline", items[1].Path)
	assert.Equal(t, "--name", items[1].Source)
	assert.Equal(t, "eu-north-1", items[1].Region)
}

func TestPrepareItemsMarksExplicitNamesWildcardForMultipleRegions(t *testing.T) {
	ctx := testCLIContext(t, []string{"--region", "eu-north-1", "--region", "eu-central-1", "--name", "/app/shared"})
	cfg, err := ConfigFromCLI(ctx)
	require.NoError(t, err)

	items, err := PrepareItems(context.Background(), &cfg)

	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "/app/shared", items[0].Path)
	assert.Equal(t, "*", items[0].Region)
}

func TestConfigFromCLIRejectsRegionWithAllRegions(t *testing.T) {
	ctx := testCLIContext(t, []string{"--region", "eu-north-1", "--all-regions"})

	cfg, err := ConfigFromCLI(ctx)

	assert.Empty(t, cfg)
	require.Error(t, err)
	assert.ErrorContains(t, err, "--region and --all-regions")
}

func TestRejectCommaSeparatedFlagArgsRejectsCLICommas(t *testing.T) {
	err := RejectCommaSeparatedFlagArgs([]string{"--region", "eu-north-1,eu-central-1", "interactive"}, "region")

	require.Error(t, err)
	assert.ErrorContains(t, err, "repeat the flag")
}

func TestRejectCommaSeparatedFlagArgsIgnoresArgsAfterDoubleDash(t *testing.T) {
	err := RejectCommaSeparatedFlagArgs([]string{"--region", "eu-north-1", "--", "--region", "eu-central-1,us-east-1"}, "region")

	require.NoError(t, err)
}

func TestFilterRecordsByGroupsScopesImportRecords(t *testing.T) {
	groups, err := filter.ParseGroups([]string{"name:/app/a", "name:/app/c"})
	require.NoError(t, err)
	records := []secretfmt.Record{
		{Path: "/app/a", Value: "a"},
		{Path: "/app/b", Value: "b"},
		{Path: "/app/c", Value: "c"},
	}

	filtered := filterRecordsByGroups(records, groups)

	require.Len(t, filtered, 2)
	assert.Equal(t, []string{"/app/a", "/app/c"}, []string{filtered[0].Path, filtered[1].Path})
}

func TestImportDefaultValueReadsInlineOrFile(t *testing.T) {
	ctx := testImportCLIContext(t, []string{"--default-value", "inline"})
	value, err := importDefaultValue(ctx)
	require.NoError(t, err)
	assert.Equal(t, "inline", value)
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

func TestExportFieldsDefaultsToAllFields(t *testing.T) {
	t.Parallel()

	fields := exportFields(Config{})

	assert.Equal(t, []string{"name", "region", "type", "tier", "data-type", "policies", "description", "value", "date", "version", "len", "sha256", "user"}, fields)
}

func TestExportRecordFromStatusRespectsExplicitFields(t *testing.T) {
	t.Parallel()

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

func TestGetParameterFieldReturnsSelectedMetadataFields(t *testing.T) {
	t.Parallel()

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
		"value":       "secret",
	} {
		actual, err := getParameterField(context.Background(), client, "/app/key", field, "eu-north-1")
		require.NoError(t, err, field)
		assert.Equal(t, expected, actual, field)
	}
}

func testCLIContext(t *testing.T, args []string) *cli.Context {
	t.Helper()
	set := flag.NewFlagSet("test", flag.ContinueOnError)
	regions := cli.NewStringSlice()
	set.Var(regions, "region", "")
	names := cli.NewStringSlice()
	set.Var(names, "name", "")
	set.String("names-file", "", "")
	set.Bool("all-regions", false, "")
	set.String("profile", "", "")
	set.Bool("no-color", false, "")
	set.String("keymap", "", "")
	filters := cli.NewStringSlice()
	set.Var(filters, "filter", "")
	set.String("filters-file", "", "")
	fields := cli.NewStringSlice()
	set.Var(fields, "field", "")
	set.Bool("with-decryption", false, "")
	set.String("sort-column", "", "")
	showColumns := cli.NewStringSlice()
	set.Var(showColumns, "show-column", "")
	set.Bool("no-confirm-overwrite-file", false, "")
	set.Bool("no-confirm-write-securestring", false, "")
	set.Bool("no-confirm-delete-one", false, "")
	set.Bool("no-confirm-delete-all", false, "")
	require.NoError(t, set.Parse(args))
	return cli.NewContext(cli.NewApp(), set, nil)
}

func testImportCLIContext(t *testing.T, args []string) *cli.Context {
	t.Helper()
	set := flag.NewFlagSet("import", flag.ContinueOnError)
	set.Bool("default-override", false, "")
	set.String("default-value", "", "")
	set.String("default-value-file", "", "")
	set.String("default-type", "", "")
	set.String("default-tier", "", "")
	set.String("default-data-type", "", "")
	set.String("default-region", "", "")
	set.String("default-description", "", "")
	set.String("default-policies", "", "")
	set.String("default-policies-file", "", "")
	require.NoError(t, set.Parse(args))
	return cli.NewContext(cli.NewApp(), set, nil)
}

func testSetCLIContext(t *testing.T, args []string) *cli.Context {
	t.Helper()
	set := flag.NewFlagSet("put", flag.ContinueOnError)
	set.String("tier", "", "")
	set.String("data-type", "", "")
	set.String("description", "", "")
	set.String("policies", "", "")
	set.String("policies-file", "", "")
	require.NoError(t, set.Parse(args))
	return cli.NewContext(cli.NewApp(), set, nil)
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

func (f fakeSSMClient) CheckAccess(context.Context) error             { return nil }
func (f fakeSSMClient) ListRegions(context.Context) ([]string, error) { return nil, nil }
func (f fakeSSMClient) ForRegion(_ string) ssm.Client                 { return f }
func (f fakeSSMClient) DefaultRegion() string                         { return f.parameter.Region }
func (f fakeSSMClient) Get(_ context.Context, path string) (ssm.Parameter, error) {
	if f.parameter.Name == path {
		return f.parameter, nil
	}
	return ssm.Parameter{}, ssm.ErrNotFound
}

func (f fakeSSMClient) GetMany(_ context.Context, _ []string) (values map[string]ssm.Parameter, errs map[string]error) {
	return nil, nil
}

func (f fakeSSMClient) DescribeMany(_ context.Context, _ []string) map[string]ssm.Metadata {
	if f.metadata.Name == "" {
		return map[string]ssm.Metadata{}
	}
	return map[string]ssm.Metadata{f.metadata.Name: f.metadata}
}

func (f fakeSSMClient) ListParameterMetadata(context.Context) ([]ssm.Metadata, error) {
	return nil, nil
}

func (f fakeSSMClient) ListParameterMetadataWithFilters(context.Context, []ssm.ParameterFilter) ([]ssm.Metadata, error) {
	return nil, nil
}

func (f fakeSSMClient) PutParameter(_ context.Context, _, _ string, _ ssm.ParameterType) error {
	return nil
}

func (f fakeSSMClient) PutParameterWithOptions(_ context.Context, _, _ string, _ ssm.ParameterType, _ ssm.PutParameterOptions) error {
	return nil
}
func (f fakeSSMClient) DeleteMany(_ context.Context, _ []string) error { return nil }
