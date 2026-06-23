package importer

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/biptec/aws-ssm-params/internal/app"
	"github.com/biptec/aws-ssm-params/internal/filter"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/biptec/aws-ssm-params/internal/textio"
)

type fakeImportClient struct {
	region   string
	metas    map[string]ssm.Metadata
	putCalls int
}

func (*fakeImportClient) CheckAccess(context.Context) error { return nil }

func (*fakeImportClient) ListRegions(context.Context) ([]string, error) { return nil, nil }

func (client *fakeImportClient) ForRegion(region string) ssm.Client {
	copied := *client
	copied.region = region

	return &copied
}

func (client *fakeImportClient) DefaultRegion() string { return client.region }

func (*fakeImportClient) GetMany(context.Context, []string) (parameters map[string]ssm.Parameter, errs map[string]error) {
	return nil, nil
}

func (client *fakeImportClient) DescribeMany(ctx context.Context, paths []string) map[string]ssm.Metadata {
	metas, _ := client.DescribeManyStrict(ctx, paths)
	return metas
}

func (client *fakeImportClient) DescribeManyStrict(ctx context.Context, paths []string) (metas map[string]ssm.Metadata, errs map[string]error) {
	if err := ctx.Err(); err != nil {
		return nil, map[string]error{"": err}
	}

	metas = make(map[string]ssm.Metadata)
	errs = make(map[string]error)

	for _, path := range paths {
		if meta, ok := client.metas[path]; ok {
			metas[path] = meta
		} else {
			errs[path] = ssm.ErrNotFound
		}
	}

	return metas, errs
}

func (*fakeImportClient) ListParameterMetadata(context.Context) ([]ssm.Metadata, error) {
	return nil, nil
}

func (*fakeImportClient) ListParameterMetadataWithFilters(context.Context, []ssm.ParameterFilter) ([]ssm.Metadata, error) {
	return nil, nil
}

func (client *fakeImportClient) PutParameterWithOptions(context.Context, string, string, ssm.ParameterType, ssm.PutParameterOptions) error {
	client.putCalls++

	return nil
}

func (*fakeImportClient) DeleteMany(context.Context, []string) error { return nil }

func TestFilterRecordsByGroupsScopesImportRecords(t *testing.T) {
	groups, err := filter.ParseGroups([]string{"name:/app/a", "name:/app/c"})
	require.NoError(t, err)

	records := app.Records{
		{Path: "/app/a", Value: "a"},
		{Path: "/app/b", Value: "b"},
		{Path: "/app/c", Value: "c"},
	}

	filtered := records.Filter(groups)

	require.Len(t, filtered, 2)
	assert.Equal(t, []string{"/app/a", "/app/c"}, []string{filtered[0].Path, filtered[1].Path})
}

func TestDefaultOptionsRespectFieldScope(t *testing.T) {
	defaults := ssmPutOptionsForTest(t, "description")

	options := defaultOptionsForFields(defaults, textio.Fields{textio.FieldName, textio.FieldValue})

	assert.Empty(t, options.Tier)
	assert.Empty(t, options.DataType)
	assert.Empty(t, options.Description)
}

func TestApplyBasePathToRecordsPrefixesRelativeNames(t *testing.T) {
	records := app.Records{{Path: "DATABASE_URL", Value: "postgres://localhost/app"}}
	basePath, err := app.ParseBasePath("/app/prod/api/")
	require.NoError(t, err)

	resolved, err := records.ResolveNames(basePath)

	require.NoError(t, err)
	assert.Equal(t, "/app/prod/api/DATABASE_URL", resolved[0].Path)
}

func TestApplyBasePathToRecordsPreservesAbsoluteNames(t *testing.T) {
	records := app.Records{{Path: "/explicit/path"}}
	basePath, err := app.ParseBasePath("/app/prod")
	require.NoError(t, err)

	resolved, err := records.ResolveNames(basePath)

	require.NoError(t, err)
	assert.Equal(t, "/explicit/path", resolved[0].Path)
}

func TestApplyBasePathToRecordsRejectsRelativeNamesWithoutBase(t *testing.T) {
	_, err := (app.Records{{Path: "DATABASE_URL"}}).ResolveNames("")

	require.Error(t, err)
	assert.ErrorContains(t, err, "requires a base path")
}

func TestImportDryRunDoesNotWriteParametersOrPrompt(t *testing.T) {
	client := &fakeImportClient{metas: map[string]ssm.Metadata{}}
	options := &Options{
		Options: &app.Options{
			Region:  "eu-north-1",
			Regions: []string{"eu-north-1"},
		},
		Format: textio.FormatJSON,
		Policy: Policy{
			OnCreate: PolicyAsk,
		},
		DryRun:  true,
		Summary: true,
	}

	var output bytes.Buffer

	err := runWithDependencies(
		context.Background(),
		options,
		strings.NewReader(`[{"name":"/app/token","value":"secret"}]`),
		&output,
		dependencies{newClient: func(ssm.ClientConfig) ssm.Client { return client }},
	)

	require.NoError(t, err)
	assert.Zero(t, client.putCalls)
	assert.Contains(t, output.String(), "DRY-RUN: would create parameter /app/token in eu-north-1")
	assert.Contains(t, output.String(), "Import dry-run summary:")
}

func TestImportOptionsForDotenvRecordDoesNotClearPoliciesImplicitly(t *testing.T) {
	record := textio.Record{Path: "/app/value", Fields: textio.Fields{textio.FieldName, textio.FieldValue}, Value: "secret"}
	cloud := ssm.Metadata{Tier: ssm.ParameterTierStandard.String(), DataType: ssm.DefaultParameterDataType.String(), Policies: ""}
	defaults := ssmPutOptionsForTest(t, "")

	opts, err := (&OptionsResolver{defaults: defaults}).forRecord(&record, &cloud, true)

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
	defaults := ssmPutOptionsForTest(t, "")

	opts, err := (&OptionsResolver{defaults: defaults}).forRecord(&record, &cloud, true)

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
	defaults := ssmPutOptionsForTest(t, "default desc")

	cloud := ssm.Metadata{}
	opts, err := (&OptionsResolver{defaults: defaults}).forRecord(&record, &cloud, false)

	require.NoError(t, err)
	assert.Equal(t, "Advanced", opts.Tier.String())
	assert.Equal(t, "aws:ec2:image", opts.DataType.String())
	assert.Equal(t, "from file", opts.Description)
	assert.Equal(t, `[{"Type":"Expiration"}]`, opts.Policies)
}

func ssmPutOptionsForTest(t *testing.T, description string) ssm.PutParameterOptions {
	t.Helper()

	tier, err := ssm.ParseParameterTier("standard")
	require.NoError(t, err)
	dataType, err := ssm.ParseParameterDataType("text")
	require.NoError(t, err)

	return ssm.PutParameterOptions{Tier: tier, DataType: dataType, Description: description}
}
