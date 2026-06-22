package ssm

import (
	"context"
	"testing"
	"time"

	"github.com/cockroachdb/errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsssm "github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSDKSSM struct {
	getInputs      []*awsssm.GetParametersInput
	describeInputs []*awsssm.DescribeParametersInput
	putInputs      []*awsssm.PutParameterInput
	deleteInputs   []*awsssm.DeleteParametersInput
	getOutput      *awsssm.GetParametersOutput
	describeOutput *awsssm.DescribeParametersOutput
	putErr         error
	deleteErr      error
}

func (f *fakeSDKSSM) GetParameters(_ context.Context, input *awsssm.GetParametersInput, _ ...func(*awsssm.Options)) (*awsssm.GetParametersOutput, error) {
	f.getInputs = append(f.getInputs, input)
	return f.getOutput, nil
}

func (f *fakeSDKSSM) DescribeParameters(_ context.Context, input *awsssm.DescribeParametersInput, _ ...func(*awsssm.Options)) (*awsssm.DescribeParametersOutput, error) {
	f.describeInputs = append(f.describeInputs, input)
	return f.describeOutput, nil
}

func (f *fakeSDKSSM) PutParameter(_ context.Context, input *awsssm.PutParameterInput, _ ...func(*awsssm.Options)) (*awsssm.PutParameterOutput, error) {
	f.putInputs = append(f.putInputs, input)
	return &awsssm.PutParameterOutput{}, f.putErr
}

func (f *fakeSDKSSM) DeleteParameters(_ context.Context, input *awsssm.DeleteParametersInput, _ ...func(*awsssm.Options)) (*awsssm.DeleteParametersOutput, error) {
	f.deleteInputs = append(f.deleteInputs, input)
	return &awsssm.DeleteParametersOutput{}, f.deleteErr
}

type fakeRegionAPI struct {
	regions []awsRegion
	err     error
}

func (f fakeRegionAPI) DescribeRegions(context.Context, *AWSClient) ([]awsRegion, error) {
	return f.regions, f.err
}

type fakeSDKSTS struct{ err error }

func (f fakeSDKSTS) GetCallerIdentity(_ context.Context, _ *sts.GetCallerIdentityInput, _ ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
	return &sts.GetCallerIdentityOutput{}, f.err
}

func TestFormatPoliciesUsesInlinePolicyText(t *testing.T) {
	got := formatPolicies([]ssmtypes.ParameterInlinePolicy{
		{
			PolicyText:   aws.String(`{"Type":"Expiration","Version":"1.0","Attributes":{"Timestamp":"2026-01-01T00:00:00Z"}}`),
			PolicyType:   aws.String("Expiration"),
			PolicyStatus: aws.String("Pending"),
		},
	})

	assert.JSONEq(t, `[{"Type":"Expiration","Version":"1.0","Attributes":{"Timestamp":"2026-01-01T00:00:00Z"}}]`, got)
	assert.NotContains(t, got, "PolicyText")
	assert.NotContains(t, got, "PolicyType")
	assert.NotContains(t, got, "PolicyStatus")
}

func TestFormatPoliciesFlattensInlinePolicyTextArray(t *testing.T) {
	got := formatPolicies([]ssmtypes.ParameterInlinePolicy{
		{PolicyText: aws.String(`[{"Type":"Expiration","Version":"1.0"},{"Type":"NoChangeNotification","Version":"1.0"}]`)},
	})

	assert.JSONEq(t, `[{"Type":"Expiration","Version":"1.0"},{"Type":"NoChangeNotification","Version":"1.0"}]`, got)
}

func TestNewAWSClientStoresProfileRegionAndDecryption(t *testing.T) {
	client := NewAWSClient("prod", "eu-north-1")

	assert.Equal(t, "prod", client.Profile)
	assert.Equal(t, "eu-north-1", client.Region)
	assert.True(t, client.WithDecryption)
}

func TestForRegionSharesLoadedConfigAndOverridesRegion(t *testing.T) {
	provider := aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
		return aws.Credentials{AccessKeyID: "AKIA", SecretAccessKey: "SECRET", Source: "test-provider"}, nil
	})
	cache := &awsConfigCache{loaded: true, cfg: aws.Config{Region: "us-east-1", Credentials: provider}}
	client := &AWSClient{Profile: "prod", Region: "us-east-1", WithDecryption: true, sharedCfg: cache}

	regional, ok := client.ForRegion("eu-west-1").(*AWSClient)
	require.True(t, ok)
	assert.Same(t, cache, regional.sharedCfg)

	cfg, err := regional.sdkConfig(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "eu-west-1", cfg.Region)
	creds, err := cfg.Credentials.Retrieve(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "test-provider", creds.Source)
}

func TestGetManyMapsValuesAndInvalidParameters(t *testing.T) {
	modified := time.Date(2026, 6, 18, 8, 0, 0, 0, time.UTC)
	api := &fakeSDKSSM{getOutput: &awsssm.GetParametersOutput{
		Parameters: []ssmtypes.Parameter{{
			Name:             aws.String("/app/ok"),
			Type:             ssmtypes.ParameterTypeSecureString,
			Value:            aws.String("secret"),
			Version:          7,
			LastModifiedDate: &modified,
		}},
		InvalidParameters: []string{"/app/missing"},
	}}
	client := &AWSClient{Region: "eu-north-1", WithDecryption: true, ssmClient: api}

	values, errs := client.GetMany(context.Background(), []string{"/app/ok", "/app/missing"})

	require.Len(t, api.getInputs, 1)
	assert.Equal(t, []string{"/app/ok", "/app/missing"}, api.getInputs[0].Names)
	assert.True(t, aws.ToBool(api.getInputs[0].WithDecryption))
	assert.Equal(t, Parameter{Name: "/app/ok", Region: "eu-north-1", Type: "SecureString", Value: "secret", Version: 7, Modified: modified.Format(time.RFC1123)}, values["/app/ok"])
	assert.ErrorIs(t, errs["/app/missing"], ErrNotFound)
}

func TestDescribeManyMapsMetadata(t *testing.T) {
	modified := time.Date(2026, 6, 18, 8, 0, 0, 0, time.UTC)
	api := &fakeSDKSSM{describeOutput: &awsssm.DescribeParametersOutput{Parameters: []ssmtypes.ParameterMetadata{{
		Name:             aws.String("/app/key"),
		Type:             ssmtypes.ParameterTypeString,
		Tier:             ssmtypes.ParameterTierAdvanced,
		DataType:         aws.String("text"),
		Description:      aws.String("description"),
		LastModifiedUser: aws.String("arn:user/dev"),
		LastModifiedDate: &modified,
	}}}}
	client := &AWSClient{Region: "eu-north-1", ssmClient: api}

	metas := client.DescribeMany(context.Background(), []string{"/app/key"})

	require.Len(t, api.describeInputs, 1)
	require.Len(t, api.describeInputs[0].ParameterFilters, 1)
	assert.Equal(t, "Name", aws.ToString(api.describeInputs[0].ParameterFilters[0].Key))
	assert.Equal(t, "Equals", aws.ToString(api.describeInputs[0].ParameterFilters[0].Option))
	assert.Equal(t, []string{"/app/key"}, api.describeInputs[0].ParameterFilters[0].Values)
	assert.Equal(t, Metadata{Name: "/app/key", Region: "eu-north-1", Type: "String", Tier: "Advanced", DataType: "text", Description: "description", User: "arn:user/dev", Modified: modified.Format(time.RFC1123)}, metas["/app/key"])
}

func TestListRegionsFiltersDisabledRegionsAndSorts(t *testing.T) {
	client := &AWSClient{regionClient: fakeRegionAPI{regions: []awsRegion{
		{Name: "us-east-1", OptInStatus: "opt-in-not-required"},
		{Name: "ap-south-2", OptInStatus: "not-opted-in"},
		{Name: "eu-north-1", OptInStatus: "opted-in"},
	}}}

	regions, err := client.ListRegions(context.Background())

	require.NoError(t, err)
	assert.Equal(t, []string{"eu-north-1", "us-east-1"}, regions)
}

func TestPutParameterWithOptionsMapsSDKInput(t *testing.T) {
	api := &fakeSDKSSM{}
	client := &AWSClient{ssmClient: api}
	opts := PutParameterOptions{
		Description: "desc",
		Tier:        ParameterTierAdvanced,
		DataType:    ParameterDataTypeText,
		Policies:    `[{"Type":"NoChangeNotification"}]`,
		Overwrite:   true,
	}

	err := client.PutParameterWithOptions(context.Background(), "/app/key", "value", ParameterTypeString, opts)

	require.NoError(t, err)
	require.Len(t, api.putInputs, 1)
	input := api.putInputs[0]
	assert.Equal(t, "/app/key", aws.ToString(input.Name))
	assert.Equal(t, "value", aws.ToString(input.Value))
	assert.Equal(t, ssmtypes.ParameterTypeString, input.Type)
	assert.Equal(t, ssmtypes.ParameterTierAdvanced, input.Tier)
	assert.Equal(t, "text", aws.ToString(input.DataType))
	assert.Equal(t, "desc", aws.ToString(input.Description))
	assert.Equal(t, opts.Policies, aws.ToString(input.Policies))
	assert.True(t, aws.ToBool(input.Overwrite))
}

func TestCheckAccessWrapsCredentialErrors(t *testing.T) {
	client := &AWSClient{stsClient: fakeSDKSTS{err: errors.New("expired token")}}

	err := client.CheckAccess(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot access AWS")
	assert.Contains(t, err.Error(), "expired token")
}

func TestTraceHTTPClientUsesTraceRoundTripper(t *testing.T) {
	client := traceHTTPClient()

	require.NotNil(t, client)
	require.NotNil(t, client.Transport)
	_, traced := client.Transport.(traceRoundTripper)
	assert.True(t, traced)
}

func TestFormatModifiedDateHandlesAWSDateShapes(t *testing.T) {
	unix := float64(1717243200)
	assert.Equal(t, time.Unix(int64(unix), 0).Format(time.RFC1123), formatModifiedDate(unix))
	assert.Equal(t, "", formatModifiedDate(float64(0)))
	assert.Equal(t, time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC).Format(time.RFC1123), formatModifiedDate("2024-06-01T12:00:00Z"))
	assert.Equal(t, "custom-date", formatModifiedDate("custom-date"))
	assert.Equal(t, "42", formatModifiedDate(42))
}

func TestChunkStringsUsesDefaultSizeAndKeepsOrder(t *testing.T) {
	values := []string{"a", "b", "c", "d", "e"}

	assert.Equal(t, [][]string{{"a", "b"}, {"c", "d"}, {"e"}}, chunkStrings(values, 2))
	assert.Equal(t, [][]string{{"a", "b", "c", "d", "e"}}, chunkStrings(values, 0))
}

func TestParseParameterTypeNormalizesSupportedAliases(t *testing.T) {
	cases := map[string]ParameterType{
		"":              ParameterTypeSecureString,
		"secure-string": ParameterTypeSecureString,
		"SecureString":  ParameterTypeSecureString,
		"string":        ParameterTypeString,
		"string-list":   ParameterTypeStringList,
		"StringList":    ParameterTypeStringList,
	}
	for input, expected := range cases {
		actual, err := ParseParameterType(input)
		assert.NoError(t, err)
		assert.Equal(t, expected, actual)
	}
}

func TestParseParameterTierNormalizesSupportedAliases(t *testing.T) {
	cases := map[string]ParameterTier{
		"":                    ParameterTierIntelligentTiering,
		"intelligent-tiering": ParameterTierIntelligentTiering,
		"IntelligentTiering":  ParameterTierIntelligentTiering,
		"standard":            ParameterTierStandard,
		"Advanced":            ParameterTierAdvanced,
	}
	for input, expected := range cases {
		actual, err := ParseParameterTier(input)
		assert.NoError(t, err)
		assert.Equal(t, expected, actual)
	}
}

func TestParseParameterTierRejectsUnsupportedValues(t *testing.T) {
	actual, err := ParseParameterTier("basic")

	assert.Error(t, err)
	assert.Equal(t, ParameterTier(""), actual)
}

func TestParseParameterTypeRejectsUnsupportedValues(t *testing.T) {
	actual, err := ParseParameterType("binary")

	assert.Error(t, err)
	assert.Equal(t, ParameterType(""), actual)
}
