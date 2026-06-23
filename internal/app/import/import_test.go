package importer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"

	"github.com/biptec/aws-ssm-params/internal/app"
	"github.com/biptec/aws-ssm-params/internal/filter"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/biptec/aws-ssm-params/internal/textio"
)

func TestFilterRecordsByGroupsScopesImportRecords(t *testing.T) {
	groups, err := filter.ParseGroups([]string{"name:/app/a", "name:/app/c"})
	require.NoError(t, err)
	records := Records{
		{Path: "/app/a", Value: "a"},
		{Path: "/app/b", Value: "b"},
		{Path: "/app/c", Value: "c"},
	}

	filtered := records.filter(groups)

	require.Len(t, filtered, 2)
	assert.Equal(t, []string{"/app/a", "/app/c"}, []string{filtered[0].Path, filtered[1].Path})
}

func TestImportDefaultOptionsDropsDescriptionOutsideFieldsScope(t *testing.T) {
	ctx := testCLIContext(t, []string{"--default-description", "desc"})

	opts, err := defaultOptions(ctx, app.Config{Fields: textio.Fields{textio.FieldName, textio.FieldValue}})

	require.NoError(t, err)
	assert.Empty(t, opts.Description)
}

func TestApplyRootPathToRecordsPrefixesRelativeNames(t *testing.T) {
	records := Records{{Path: "DATABASE_URL", Value: "postgres://localhost/app"}}

	resolved, err := records.withRootPath("/app/prod/api/")

	require.NoError(t, err)
	assert.Equal(t, "/app/prod/api/DATABASE_URL", resolved[0].Path)
}

func TestApplyRootPathToRecordsPreservesAbsoluteNames(t *testing.T) {
	records := Records{{Path: "/explicit/path"}}

	resolved, err := records.withRootPath("/app/prod")

	require.NoError(t, err)
	assert.Equal(t, "/explicit/path", resolved[0].Path)
}

func TestApplyRootPathToRecordsRejectsRelativeNamesWithoutRoot(t *testing.T) {
	_, err := (Records{{Path: "DATABASE_URL"}}).withRootPath("")

	require.Error(t, err)
	assert.ErrorContains(t, err, "--root-path")
}

func TestApplyRootPathToRecordsRejectsRelativeRootPath(t *testing.T) {
	_, err := (Records{{Path: "DATABASE_URL"}}).withRootPath("app/prod")

	require.Error(t, err)
	assert.ErrorContains(t, err, "must start with /")
}

func TestWritePolicyDefaultsToWrite(t *testing.T) {
	policy, err := parseWritePolicy(testCLIContext(t, nil))

	require.NoError(t, err)
	assert.Equal(t, writePolicyDefault, policy.OnCreate)
	assert.Equal(t, writePolicyDefault, policy.OnUpdate)
}

func TestWritePolicyParsesSkipErrorAsk(t *testing.T) {
	policy, err := parseWritePolicy(testCLIContext(t, []string{"--on-create", "skip", "--on-update", "ask"}))

	require.NoError(t, err)
	assert.Equal(t, writePolicySkip, policy.OnCreate)
	assert.Equal(t, writePolicyAsk, policy.OnUpdate)
}

func TestWritePolicyRejectsUnsupportedValue(t *testing.T) {
	policy, err := parseWritePolicy(testCLIContext(t, []string{"--on-create", "apply"}))

	assert.Empty(t, policy)
	require.Error(t, err)
	assert.ErrorContains(t, err, "unsupported --on-create value")
}

func TestImportOptionsForDotenvRecordDoesNotClearPoliciesImplicitly(t *testing.T) {
	record := textio.Record{Path: "/app/value", Fields: textio.Fields{textio.FieldName, textio.FieldValue}, Value: "secret"}
	cloud := ssm.Metadata{Tier: ssm.ParameterTierStandard.String(), DataType: ssm.DefaultParameterDataType.String(), Policies: ""}
	defaults := ssmPutOptionsForTest(t, "standard", "text", "")

	opts, err := (OptionsResolver{defaults: defaults, cfg: app.Config{}}).forRecord(record, cloud, true)

	require.NoError(t, err)
	assert.Empty(t, opts.Policies)
}

func TestImportOptionsForExplicitEmptyPoliciesClearsPolicies(t *testing.T) {
	record := textio.Record{
		Path:     "/app/value",
		Fields:   textio.Fields{textio.FieldName, textio.FieldValue, textio.FieldPolicies},
		Value:    "secret",
		Policies: "",
	}
	cloud := ssm.Metadata{
		Tier:     ssm.ParameterTierAdvanced.String(),
		DataType: ssm.DefaultParameterDataType.String(),
		Policies: `[{"Type":"Expiration"}]`,
	}
	defaults := ssmPutOptionsForTest(t, "standard", "text", "")

	opts, err := (OptionsResolver{defaults: defaults, cfg: app.Config{}}).forRecord(record, cloud, true)

	require.NoError(t, err)
	assert.Equal(t, "[{}]", opts.Policies)
	assert.True(t, opts.PoliciesSet)
}

func TestImportOptionsForRecordUsesRecordMetadataWhenAllowed(t *testing.T) {
	record := textio.Record{
		Fields: textio.Fields{
			textio.FieldName,
			textio.FieldTier,
			textio.FieldDataType,
			textio.FieldDescription,
			textio.FieldPolicies,
		},
		Tier:        "Advanced",
		DataType:    "aws:ec2:image",
		Description: "from file",
		Policies:    `[{"Type":"Expiration"}]`,
	}
	defaults := ssmPutOptionsForTest(t, "standard", "text", "default desc")

	opts, err := (OptionsResolver{defaults: defaults, cfg: app.Config{}}).forRecord(record, ssm.Metadata{}, false)

	require.NoError(t, err)
	assert.Equal(t, "Advanced", opts.Tier.String())
	assert.Equal(t, "aws:ec2:image", opts.DataType.String())
	assert.Equal(t, "from file", opts.Description)
	assert.Equal(t, `[{"Type":"Expiration"}]`, opts.Policies)
}

func testCLIContext(t *testing.T, args []string) *app.CLIContext {
	t.Helper()
	cmd := &cli.Command{
		Name: "import-test",
		Flags: []cli.Flag{
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
		},
		Action: func(context.Context, *cli.Command) error { return nil },
	}
	require.NoError(t, cmd.Run(context.Background(), append([]string{"import-test"}, args...)))
	return app.NewCLIContext(context.Background(), cmd)
}

func ssmPutOptionsForTest(t *testing.T, tierValue, dataTypeValue, description string) ssm.PutParameterOptions {
	t.Helper()
	tier, err := ssm.ParseParameterTier(tierValue)
	require.NoError(t, err)
	dataType, err := ssm.ParseParameterDataType(dataTypeValue)
	require.NoError(t, err)
	return ssm.PutParameterOptions{Tier: tier, DataType: dataType, Description: description}
}
