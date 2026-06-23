package importer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

func TestDefaultOptionsRespectFieldScope(t *testing.T) {
	defaults := ssmPutOptionsForTest(t, "standard", "text", "description")

	options := defaultOptionsForFields(defaults, textio.Fields{textio.FieldName, textio.FieldValue})

	assert.Empty(t, options.Tier)
	assert.Empty(t, options.DataType)
	assert.Empty(t, options.Description)
}

func TestApplyBasePathToRecordsPrefixesRelativeNames(t *testing.T) {
	records := Records{{Path: "DATABASE_URL", Value: "postgres://localhost/app"}}
	basePath, err := app.ParseBasePath("/app/prod/api/")
	require.NoError(t, err)

	resolved, err := records.withBasePath(basePath)

	require.NoError(t, err)
	assert.Equal(t, "/app/prod/api/DATABASE_URL", resolved[0].Path)
}

func TestApplyBasePathToRecordsPreservesAbsoluteNames(t *testing.T) {
	records := Records{{Path: "/explicit/path"}}
	basePath, err := app.ParseBasePath("/app/prod")
	require.NoError(t, err)

	resolved, err := records.withBasePath(basePath)

	require.NoError(t, err)
	assert.Equal(t, "/explicit/path", resolved[0].Path)
}

func TestApplyBasePathToRecordsRejectsRelativeNamesWithoutBase(t *testing.T) {
	_, err := (Records{{Path: "DATABASE_URL"}}).withBasePath("")

	require.Error(t, err)
	assert.ErrorContains(t, err, "requires a base path")
}

func TestImportOptionsForDotenvRecordDoesNotClearPoliciesImplicitly(t *testing.T) {
	record := textio.Record{Path: "/app/value", Fields: textio.Fields{textio.FieldName, textio.FieldValue}, Value: "secret"}
	cloud := ssm.Metadata{Tier: ssm.ParameterTierStandard.String(), DataType: ssm.DefaultParameterDataType.String(), Policies: ""}
	defaults := ssmPutOptionsForTest(t, "standard", "text", "")

	opts, err := (OptionsResolver{defaults: defaults}).forRecord(record, cloud, true)

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

	opts, err := (OptionsResolver{defaults: defaults}).forRecord(record, cloud, true)

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

	opts, err := (OptionsResolver{defaults: defaults}).forRecord(record, ssm.Metadata{}, false)

	require.NoError(t, err)
	assert.Equal(t, "Advanced", opts.Tier.String())
	assert.Equal(t, "aws:ec2:image", opts.DataType.String())
	assert.Equal(t, "from file", opts.Description)
	assert.Equal(t, `[{"Type":"Expiration"}]`, opts.Policies)
}

func ssmPutOptionsForTest(t *testing.T, tierValue, dataTypeValue, description string) ssm.PutParameterOptions {
	t.Helper()
	tier, err := ssm.ParseParameterTier(tierValue)
	require.NoError(t, err)
	dataType, err := ssm.ParseParameterDataType(dataTypeValue)
	require.NoError(t, err)
	return ssm.PutParameterOptions{Tier: tier, DataType: dataType, Description: description}
}
