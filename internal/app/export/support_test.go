package export

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"

	"github.com/biptec/aws-ssm-params/internal/app"
	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/textio"
	"github.com/biptec/aws-ssm-params/internal/ui"
)

func TestScalarExportFieldRequiresExactlyOneField(t *testing.T) {
	ctx := testCLIContext(t, []string{"--scalar", "--output-field", "value"})
	cfg, err := app.ConfigFromCLI(ctx)
	require.NoError(t, err)

	field, err := scalarField(ctx, cfg)

	require.NoError(t, err)
	assert.Equal(t, textio.FieldValue, field)
}

func TestScalarExportFieldRejectsMissingField(t *testing.T) {
	ctx := testCLIContext(t, []string{"--scalar"})
	cfg, err := app.ConfigFromCLI(ctx)
	require.NoError(t, err)

	field, err := scalarField(ctx, cfg)

	assert.Empty(t, field)
	require.Error(t, err)
	assert.ErrorContains(t, err, "exactly one --output-field")
}

func TestValidateKeyFieldOutputFieldsRejectsExplicitCollision(t *testing.T) {
	err := validateKeyFieldOutputFields(textio.FieldName, textio.Fields{textio.FieldName, textio.FieldValue})

	require.Error(t, err)
	assert.ErrorContains(t, err, "cannot use the same field")
}

func TestValidateKeyFieldOutputFieldsAllowsImplicitAllFields(t *testing.T) {
	require.NoError(t, validateKeyFieldOutputFields(textio.FieldName, nil))
}

func TestExportFieldMappingsApplyAliasesWithoutFiltering(t *testing.T) {
	mappings := textio.FieldMappings{{AWSName: textio.FieldName, FileName: "title"}}.
		WithDefaults().
		ForFields(textio.Fields{textio.FieldName, textio.FieldValue, textio.FieldType})

	assert.Equal(t, textio.FieldMappings{
		{AWSName: textio.FieldName, FileName: "title"},
		{AWSName: textio.FieldValue, FileName: textio.FieldValue},
		{AWSName: textio.FieldType, FileName: textio.FieldType},
	}, mappings)
}

func TestExportRecordFieldsIncludesScalarAndKeyFields(t *testing.T) {
	fields := recordFields(textio.Fields{textio.FieldValue}, textio.FieldValue, textio.FieldRegion)

	assert.Equal(t, textio.Fields{textio.FieldValue, textio.FieldRegion}, fields)
}

func TestSortStatusesForExportUsesMultipleColumns(t *testing.T) {
	statuses := ui.Statuses{
		{Item: inventory.Item{Path: "/app/a", Region: "eu-north-1"}, Type: "String", Version: 10},
		{Item: inventory.Item{Path: "/app/c", Region: "eu-north-1"}, Type: "SecureString", Version: 2},
		{Item: inventory.Item{Path: "/app/b", Region: "eu-north-1"}, Type: "String", Version: 2},
	}

	parseSortRules([]string{"type:asc", "version:desc", "name:asc"}).sort(statuses)

	assert.Equal(t, []string{"/app/c", "/app/a", "/app/b"}, []string{statuses[0].Item.Path, statuses[1].Item.Path, statuses[2].Item.Path})
}

func TestIncludeValuesForSortColumnsIncludesDerivedValueFields(t *testing.T) {
	assert.True(t, parseSortRules([]string{"len:desc"}).requiresValues())
	assert.True(t, parseSortRules([]string{"sha256:asc"}).requiresValues())
	assert.True(t, parseSortRules([]string{"value:asc"}).requiresValues())
	assert.False(t, parseSortRules([]string{"name:asc", "type:desc"}).requiresValues())
}

func TestExportFieldsDefaultsToAllFields(t *testing.T) {
	t.Parallel()

	fields := fieldsForConfig(app.Config{})

	assert.Equal(t, textio.Fields{
		textio.FieldName,
		textio.FieldRegion,
		textio.FieldType,
		textio.FieldTier,
		textio.FieldDataType,
		textio.FieldPolicies,
		textio.FieldDescription,
		textio.FieldValue,
		textio.FieldDate,
		textio.FieldVersion,
		textio.FieldLen,
		textio.FieldSHA256,
		textio.FieldUser,
	}, fields)
}

func TestExportDefaultFieldsWithKeyFieldStillRequestValues(t *testing.T) {
	fields := fieldsForConfig(app.Config{})
	recordFields := recordFields(fields, "", textio.FieldName)

	assert.Contains(t, recordFields, textio.FieldName)
	assert.Contains(t, recordFields, textio.FieldValue)
	assert.True(t, recordFields.RequiresValues())
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

	record := recordFromStatus(status, textio.Fields{textio.FieldName, textio.FieldValue})

	assert.Equal(t, textio.Fields{textio.FieldName, textio.FieldValue}, record.Fields)
	assert.Equal(t, "secret", record.Value)
	assert.Empty(t, record.Region)
	assert.Empty(t, record.Type)
	assert.Empty(t, record.Description)
}

func testCLIContext(t *testing.T, args []string) *app.CLIContext {
	t.Helper()
	cmd := &cli.Command{
		Name: "export-test",
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
			&cli.StringSliceFlag{Name: "sort-by", Sources: cli.EnvVars("AWS_SSM_PARAMS_SORT_BY")},
			&cli.BoolFlag{Name: "with-decryption", Sources: cli.EnvVars("AWS_SSM_PARAMS_WITH_DECRYPTION")},
			&cli.BoolFlag{Name: "scalar"},
		},
		Action: func(context.Context, *cli.Command) error { return nil },
	}
	require.NoError(t, cmd.Run(context.Background(), append([]string{"export-test"}, args...)))
	return app.NewCLIContext(context.Background(), cmd)
}
