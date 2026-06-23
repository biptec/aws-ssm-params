package export

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/biptec/aws-ssm-params/internal/app"
	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/textio"
	"github.com/biptec/aws-ssm-params/internal/ui"
)

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

	fields := fieldsForOptions(nil)

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
	fields := fieldsForOptions(nil)
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

func TestRecordsMakeNamesRelativeToBasePath(t *testing.T) {
	basePath, err := app.ParseBasePath("/app/prod")
	require.NoError(t, err)
	r := runner{
		basePath:     basePath,
		recordFields: textio.Fields{textio.FieldName, textio.FieldValue},
	}
	statuses := ui.Statuses{{
		Item:   inventory.Item{Path: "/app/prod/api/token"},
		Exists: true,
		Value:  "secret",
	}}

	records, err := r.records(statuses)

	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "api/token", records[0].Path)
}

func TestRecordsPreserveAbsoluteNamesWithoutBasePath(t *testing.T) {
	r := runner{recordFields: textio.Fields{textio.FieldName}}
	statuses := ui.Statuses{{
		Item:   inventory.Item{Path: "/app/prod/api/token"},
		Exists: true,
	}}

	records, err := r.records(statuses)

	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "/app/prod/api/token", records[0].Path)
}

func TestRecordsRejectNamesOutsideBasePath(t *testing.T) {
	basePath, err := app.ParseBasePath("/app/prod")
	require.NoError(t, err)
	r := runner{
		basePath:     basePath,
		recordFields: textio.Fields{textio.FieldName},
	}
	statuses := ui.Statuses{{
		Item:   inventory.Item{Path: "/app/prod2/token"},
		Exists: true,
	}}

	_, err = r.records(statuses)

	require.Error(t, err)
	assert.ErrorContains(t, err, "outside base path")
}
