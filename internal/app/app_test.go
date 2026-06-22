package app

import (
	"context"
	"os"
	"testing"

	"github.com/biptec/aws-ssm-params/internal/filter"
	secretfmt "github.com/biptec/aws-ssm-params/internal/format"
	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/biptec/aws-ssm-params/internal/ui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
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
	assert.Equal(t, []string{"name", "value"}, cfg.Fields)
	assert.Equal(t, []secretfmt.FieldMapping{{AWSName: "name", FileName: "title"}, {AWSName: "value", FileName: "text"}}, cfg.FieldMappings)
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
	assert.Equal(t, []string{"name", "value"}, cfg.Fields)
	assert.Equal(t, []secretfmt.FieldMapping{{AWSName: "name", FileName: "title"}, {AWSName: "value", FileName: "text"}}, cfg.FieldMappings)
	assert.Equal(t, []string{"path", "value"}, cfg.ShowColumns)
	assert.Equal(t, []string{"name:asc", "type:desc"}, cfg.SortColumns)
	assert.Empty(t, cfg.InventoryItems)
}

func TestConfigFromCLIPrefersToolEnvOverNativeAWSAliases(t *testing.T) {
	t.Setenv("AWS_SSM_PARAMS_REGION", "eu-north-1,eu-central-1")
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_SSM_PARAMS_PROFILE", "tool-profile")
	t.Setenv("AWS_PROFILE", "native-profile")

	ctx := testCLIContext(t, nil)
	cfg, err := ConfigFromCLI(ctx)

	require.NoError(t, err)
	assert.Equal(t, "eu-north-1", cfg.Region)
	assert.Equal(t, []string{"eu-north-1", "eu-central-1"}, cfg.Regions)
	assert.Equal(t, "tool-profile", cfg.Profile)
}

func TestConfigFromCLIUsesNativeAWSAliasesWhenToolEnvIsUnset(t *testing.T) {
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_PROFILE", "native-profile")

	ctx := testCLIContext(t, nil)
	cfg, err := ConfigFromCLI(ctx)

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

func TestLoadInteractiveInventoryFromPipedStdin(t *testing.T) {
	oldStdin := os.Stdin
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { os.Stdin = oldStdin })
	os.Stdin = reader

	_, err = writer.WriteString("# comment\n/app/from-stdin\n/app/second # inline comment\n")
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	items, useTTYInput, err := loadInteractiveInventoryFromStdin()

	require.NoError(t, err)
	assert.True(t, useTTYInput)
	require.Len(t, items, 2)
	assert.Equal(t, "/app/from-stdin", items[0].Path)
	assert.Equal(t, "stdin", items[0].Source)
	assert.Equal(t, "/app/second", items[1].Path)
}

func TestConfigFromCLIRejectsRegionWithAllRegions(t *testing.T) {
	ctx := testCLIContext(t, []string{"--region", "eu-north-1", "--all-regions"})

	cfg, err := ConfigFromCLI(ctx)

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

func TestImportDefaultOptionsDropsDescriptionOutsideFieldsScope(t *testing.T) {
	ctx := testImportCLIContext(t, []string{"--default-description", "desc"})

	opts, err := importDefaultOptions(ctx, Config{Fields: []string{"name", "value"}})

	require.NoError(t, err)
	assert.Empty(t, opts.Description)
}

func TestApplyRootPathToRecordsPrefixesRelativeNames(t *testing.T) {
	records := []secretfmt.Record{{Path: "DATABASE_URL", Alias: "DATABASE_URL", Value: "postgres://localhost/app"}}

	resolved, err := applyRootPathToRecords(records, "/app/prod/api/")

	require.NoError(t, err)
	records = resolved
	assert.Equal(t, "/app/prod/api/DATABASE_URL", records[0].Path)
	assert.Equal(t, "DATABASE_URL", records[0].Alias)
}

func TestApplyRootPathToRecordsPreservesAbsoluteNames(t *testing.T) {
	records := []secretfmt.Record{{Path: "/explicit/path", Alias: "EXPLICIT_PATH"}}

	resolved, err := applyRootPathToRecords(records, "/app/prod")

	require.NoError(t, err)
	records = resolved
	assert.Equal(t, "/explicit/path", records[0].Path)
	assert.Equal(t, "EXPLICIT_PATH", records[0].Alias)
}

func TestApplyRootPathToRecordsRejectsRelativeNamesWithoutRoot(t *testing.T) {
	records := []secretfmt.Record{{Path: "DATABASE_URL"}}

	_, err := applyRootPathToRecords(records, "")

	require.Error(t, err)
	assert.ErrorContains(t, err, "--root-path")
}

func TestApplyRootPathToRecordsRejectsRelativeRootPath(t *testing.T) {
	records := []secretfmt.Record{{Path: "DATABASE_URL"}}

	_, err := applyRootPathToRecords(records, "app/prod")

	require.Error(t, err)
	assert.ErrorContains(t, err, "must start with /")
}

func TestWritePolicyDefaultsToWrite(t *testing.T) {
	policy, err := parseWritePolicy(testImportCLIContext(t, nil))

	require.NoError(t, err)
	assert.Equal(t, writePolicyDefault, policy.OnCreate)
	assert.Equal(t, writePolicyDefault, policy.OnUpdate)
}

func TestWritePolicyParsesSkipErrorAsk(t *testing.T) {
	policy, err := parseWritePolicy(testImportCLIContext(t, []string{"--on-create", "skip", "--on-update", "ask"}))

	require.NoError(t, err)
	assert.Equal(t, writePolicySkip, policy.OnCreate)
	assert.Equal(t, writePolicyAsk, policy.OnUpdate)
}

func TestWritePolicyRejectsUnsupportedValue(t *testing.T) {
	policy, err := parseWritePolicy(testImportCLIContext(t, []string{"--on-create", "apply"}))

	assert.Empty(t, policy)
	require.Error(t, err)
	assert.ErrorContains(t, err, "unsupported --on-create value")
}

func TestScalarExportFieldRequiresExactlyOneField(t *testing.T) {
	ctx := testCLIContext(t, []string{"--scalar", "--output-field", "value"})
	cfg, err := ConfigFromCLI(ctx)
	require.NoError(t, err)

	field, err := scalarExportField(ctx, cfg)

	require.NoError(t, err)
	assert.Equal(t, "value", field)
}

func TestScalarExportFieldRejectsMissingField(t *testing.T) {
	ctx := testCLIContext(t, []string{"--scalar"})
	cfg, err := ConfigFromCLI(ctx)
	require.NoError(t, err)

	field, err := scalarExportField(ctx, cfg)

	assert.Empty(t, field)
	require.Error(t, err)
	assert.ErrorContains(t, err, "exactly one --output-field")
}

func TestConfigFromCLIRejectsOutputFieldAliasSyntax(t *testing.T) {
	ctx := testCLIContext(t, []string{"--output-field", "value:text"})

	cfg, err := ConfigFromCLI(ctx)

	assert.Empty(t, cfg)
	require.Error(t, err)
	assert.ErrorContains(t, err, "unsupported --output-field value")
}

func TestValidateKeyFieldOutputFieldsRejectsExplicitCollision(t *testing.T) {
	err := validateKeyFieldOutputFields("name", []string{"name", "value"})

	require.Error(t, err)
	assert.ErrorContains(t, err, "cannot use the same field")
}

func TestValidateKeyFieldOutputFieldsAllowsImplicitAllFields(t *testing.T) {
	err := validateKeyFieldOutputFields("name", nil)

	require.NoError(t, err)
}

func TestExportFieldMappingsApplyAliasesWithoutFiltering(t *testing.T) {
	mappings := exportFieldMappings([]string{"name", "value", "type"}, []secretfmt.FieldMapping{{AWSName: "name", FileName: "title"}})

	assert.Equal(t, []secretfmt.FieldMapping{
		{AWSName: "name", FileName: "title"},
		{AWSName: "value", FileName: "value"},
		{AWSName: "type", FileName: "type"},
	}, mappings)
}

func TestExportRecordFieldsIncludesScalarAndKeyFields(t *testing.T) {
	fields := exportRecordFields([]string{"value"}, "value", "region")

	assert.Equal(t, []string{"value", "region"}, fields)
}

func TestSortStatusesForExportUsesMultipleColumns(t *testing.T) {
	statuses := []ui.Status{
		{Item: inventory.Item{Path: "/app/a", Region: "eu-north-1"}, Type: "String", Version: 10},
		{Item: inventory.Item{Path: "/app/c", Region: "eu-north-1"}, Type: "SecureString", Version: 2},
		{Item: inventory.Item{Path: "/app/b", Region: "eu-north-1"}, Type: "String", Version: 2},
	}

	sortStatusesForExport(statuses, []string{"type:asc", "version:desc", "name:asc"})

	assert.Equal(t, []string{"/app/c", "/app/a", "/app/b"}, []string{statuses[0].Item.Path, statuses[1].Item.Path, statuses[2].Item.Path})
}

func TestIncludeValuesForSortColumnsIncludesDerivedValueFields(t *testing.T) {
	assert.True(t, includeValuesForSortColumns([]string{"len:desc"}))
	assert.True(t, includeValuesForSortColumns([]string{"sha256:asc"}))
	assert.True(t, includeValuesForSortColumns([]string{"value:asc"}))
	assert.False(t, includeValuesForSortColumns([]string{"name:asc", "type:desc"}))
}

func TestExportFieldsDefaultsToAllFields(t *testing.T) {
	t.Parallel()

	fields := exportFields(Config{})

	assert.Equal(t, []string{"name", "region", "type", "tier", "data-type", "policies", "description", "value", "date", "version", "len", "sha256", "user"}, fields)
}

func TestExportDefaultFieldsWithKeyFieldStillRequestValues(t *testing.T) {
	fields := exportFields(Config{})
	recordFields := exportRecordFields(fields, "", "name")

	assert.Contains(t, recordFields, "name")
	assert.Contains(t, recordFields, "value")
	assert.True(t, includeValuesForFields(recordFields))
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

func TestImportOptionsForDotenvRecordDoesNotClearPoliciesImplicitly(t *testing.T) {
	record := secretfmt.Record{Path: "/app/value", Fields: []string{"name", "value"}, Value: "secret"}
	cloud := ssm.Metadata{Tier: ssm.ParameterTierStandard.String(), DataType: ssm.DefaultParameterDataType.String(), Policies: ""}
	defaults := ssmPutOptionsForTest(t, "standard", "text", "")

	opts, err := importOptionsForRecord(record, cloud, true, defaults, Config{})

	require.NoError(t, err)
	assert.Empty(t, opts.Policies)
}

func TestImportOptionsForExplicitEmptyPoliciesClearsPolicies(t *testing.T) {
	record := secretfmt.Record{Path: "/app/value", Fields: []string{"name", "value", "policies"}, Value: "secret", Policies: ""}
	cloud := ssm.Metadata{Tier: ssm.ParameterTierAdvanced.String(), DataType: ssm.DefaultParameterDataType.String(), Policies: `[{"Type":"Expiration"}]`}
	defaults := ssmPutOptionsForTest(t, "standard", "text", "")

	opts, err := importOptionsForRecord(record, cloud, true, defaults, Config{})

	require.NoError(t, err)
	assert.Equal(t, "[{}]", opts.Policies)
	assert.True(t, opts.PoliciesSet)
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

	opts, err := importOptionsForRecord(record, ssm.Metadata{}, false, defaults, Config{})

	require.NoError(t, err)
	assert.Equal(t, "Advanced", opts.Tier.String())
	assert.Equal(t, "aws:ec2:image", opts.DataType.String())
	assert.Equal(t, "from file", opts.Description)
	assert.Equal(t, `[{"Type":"Expiration"}]`, opts.Policies)
}

func testCLIContext(t *testing.T, args []string) *CLIContext {
	t.Helper()
	return testCommandContext(t, []cli.Flag{
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
		&cli.BoolFlag{Name: "scalar"},
		&cli.StringSliceFlag{Name: "sort-by", Sources: cli.EnvVars("AWS_SSM_PARAMS_SORT_BY")},
		&cli.StringSliceFlag{Name: "show-column", Sources: cli.EnvVars("AWS_SSM_PARAMS_SHOW_COLUMN")},
		&cli.BoolFlag{Name: "no-confirm-overwrite-file"},
		&cli.BoolFlag{Name: "no-confirm-write-securestring"},
		&cli.BoolFlag{Name: "no-confirm-delete-one"},
		&cli.BoolFlag{Name: "no-confirm-delete-all"},
	}, args)
}

func testImportCLIContext(t *testing.T, args []string) *CLIContext {
	t.Helper()
	return testCommandContext(t, []cli.Flag{
		&cli.StringSliceFlag{Name: "map-field"},
		&cli.StringFlag{Name: "root-path"},
		&cli.StringFlag{Name: "on-create"},
		&cli.StringFlag{Name: "on-update"},
		&cli.BoolFlag{Name: "continue-on-error"},
		&cli.BoolFlag{Name: "summary"},
		&cli.StringFlag{Name: "default-type"},
		&cli.StringFlag{Name: "default-tier"},
		&cli.StringFlag{Name: "default-data-type"},
		&cli.StringFlag{Name: "default-region"},
		&cli.StringFlag{Name: "default-description"},
		&cli.StringFlag{Name: "default-policies"},
		&cli.StringFlag{Name: "default-policies-file"},
	}, args)
}

func testCommandContext(t *testing.T, flags []cli.Flag, args []string) *CLIContext {
	t.Helper()
	cmd := &cli.Command{
		Name:  "test",
		Flags: flags,
		Action: func(context.Context, *cli.Command) error {
			return nil
		},
	}
	require.NoError(t, cmd.Run(context.Background(), append([]string{"test"}, args...)))
	return NewCLIContext(context.Background(), cmd)
}

func ssmPutOptionsForTest(t *testing.T, tierValue, dataTypeValue, description string) ssm.PutParameterOptions {
	t.Helper()
	tier, err := ssm.ParseParameterTier(tierValue)
	require.NoError(t, err)
	dataType, err := ssm.ParseParameterDataType(dataTypeValue)
	require.NoError(t, err)
	return ssm.PutParameterOptions{Tier: tier, DataType: dataType, Description: description}
}
