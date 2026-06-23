package client

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsssm "github.com/aws/aws-sdk-go-v2/service/ssm"
	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/biptec/aws-ssm-params/internal/ssm"
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

func (f *fakeSDKSSM) GetParameters(ctx context.Context, input *awsssm.GetParametersInput, optionFns ...func(*awsssm.Options)) (*awsssm.GetParametersOutput, error) {
	_, _ = ctx, optionFns

	f.getInputs = append(f.getInputs, input)

	return f.getOutput, nil
}

func (f *fakeSDKSSM) DescribeParameters(ctx context.Context, input *awsssm.DescribeParametersInput, optionFns ...func(*awsssm.Options)) (*awsssm.DescribeParametersOutput, error) {
	_, _ = ctx, optionFns

	f.describeInputs = append(f.describeInputs, input)

	return f.describeOutput, nil
}

func (f *fakeSDKSSM) PutParameter(ctx context.Context, input *awsssm.PutParameterInput, optionFns ...func(*awsssm.Options)) (*awsssm.PutParameterOutput, error) {
	_, _ = ctx, optionFns

	f.putInputs = append(f.putInputs, input)

	return &awsssm.PutParameterOutput{}, f.putErr
}

func (f *fakeSDKSSM) DeleteParameters(ctx context.Context, input *awsssm.DeleteParametersInput, optionFns ...func(*awsssm.Options)) (*awsssm.DeleteParametersOutput, error) {
	_, _ = ctx, optionFns

	f.deleteInputs = append(f.deleteInputs, input)

	return &awsssm.DeleteParametersOutput{}, f.deleteErr
}

type fakeSDKSTS struct{ err error }

func (f fakeSDKSTS) GetCallerIdentity(ctx context.Context, input *sts.GetCallerIdentityInput, optionFns ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
	_, _, _ = ctx, input, optionFns
	return &sts.GetCallerIdentityOutput{}, f.err
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
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
	client := newClient("prod", "eu-north-1")

	assert.Equal(t, "prod", client.Profile)
	assert.Equal(t, "eu-north-1", client.Region)
	assert.True(t, client.WithDecryption)
}

func TestForRegionSharesLoadedConfigAndOverridesRegion(t *testing.T) {
	provider := aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
		return aws.Credentials{AccessKeyID: "AKIA", SecretAccessKey: "SECRET", Source: "test-provider"}, nil
	})
	cache := &sdkConfigCache{loaded: true, cfg: aws.Config{Region: "us-east-1", Credentials: provider}}
	root := &client{Profile: "prod", Region: "us-east-1", WithDecryption: true, sharedCfg: cache}

	regional, ok := root.ForRegion("eu-west-1").(*client)
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
	client := &client{Region: "eu-north-1", WithDecryption: true, getParametersFunc: api.GetParameters}

	values, errs := client.GetMany(context.Background(), []string{"/app/ok", "/app/missing"})

	require.Len(t, api.getInputs, 1)
	assert.Equal(t, []string{"/app/ok", "/app/missing"}, api.getInputs[0].Names)
	assert.True(t, aws.ToBool(api.getInputs[0].WithDecryption))
	assert.Equal(t, ssm.Parameter{Name: "/app/ok", Region: "eu-north-1", Type: "SecureString", Value: "secret", Version: 7, Modified: modified.Format(time.RFC1123)}, values["/app/ok"])
	assert.ErrorIs(t, errs["/app/missing"], ssm.ErrNotFound)
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
	client := &client{Region: "eu-north-1", describeParametersFunc: api.DescribeParameters}

	metas := client.DescribeMany(context.Background(), []string{"/app/key"})

	require.Len(t, api.describeInputs, 1)
	require.Len(t, api.describeInputs[0].ParameterFilters, 1)
	assert.Equal(t, "Name", aws.ToString(api.describeInputs[0].ParameterFilters[0].Key))
	assert.Equal(t, "Equals", aws.ToString(api.describeInputs[0].ParameterFilters[0].Option))
	assert.Equal(t, []string{"/app/key"}, api.describeInputs[0].ParameterFilters[0].Values)
	assert.Equal(t, ssm.Metadata{Name: "/app/key", Region: "eu-north-1", Type: "String", Tier: "Advanced", DataType: "text", Description: "description", User: "arn:user/dev", Modified: modified.Format(time.RFC1123)}, metas["/app/key"])
}

func TestListRegionsSortsDiscoveredRegions(t *testing.T) {
	client := &client{describeRegionsFunc: func(context.Context, *client) ([]string, error) {
		return []string{"us-east-1", "eu-north-1"}, nil
	}}

	regions, err := client.ListRegions(context.Background())

	require.NoError(t, err)
	assert.Equal(t, []string{"eu-north-1", "us-east-1"}, regions)
}

func TestDescribeAWSRegionsFiltersDisabledRegions(t *testing.T) {
	provider := aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
		return aws.Credentials{AccessKeyID: "AKIA", SecretAccessKey: "SECRET", SessionToken: "TOKEN", Source: "test-provider"}, nil
	})
	httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, "ec2.eu-north-1.amazonaws.com", req.URL.Host)

		body := `<DescribeRegionsResponse>
			<regionInfo>
				<item><regionName>us-east-1</regionName><optInStatus>opt-in-not-required</optInStatus></item>
				<item><regionName>ap-south-2</regionName><optInStatus>not-opted-in</optInStatus></item>
				<item><regionName>eu-north-1</regionName><optInStatus>opted-in</optInStatus></item>
			</regionInfo>
		</DescribeRegionsResponse>`

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})}
	cache := &sdkConfigCache{
		loaded: true,
		cfg:    aws.Config{Region: "eu-north-1", Credentials: provider, HTTPClient: httpClient},
	}
	client := &client{Region: "eu-north-1", sharedCfg: cache}

	regions, err := describeAWSRegions(context.Background(), client)

	require.NoError(t, err)
	assert.Equal(t, []string{"us-east-1", "eu-north-1"}, regions)
}

func TestPutParameterWithOptionsMapsSDKInput(t *testing.T) {
	api := &fakeSDKSSM{}
	client := &client{putParameterFunc: api.PutParameter}
	opts := ssm.PutParameterOptions{
		Description: "desc",
		Tier:        ssm.ParameterTierAdvanced,
		DataType:    ssm.ParameterDataTypeText,
		Policies:    `[{"Type":"NoChangeNotification"}]`,
		Overwrite:   true,
	}

	err := client.PutParameterWithOptions(context.Background(), "/app/key", "value", ssm.ParameterTypeString, opts)

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
	client := &client{getCallerIdentityFunc: fakeSDKSTS{err: errors.New("expired token")}.GetCallerIdentity}

	err := client.CheckAccess(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot access AWS")
	assert.Contains(t, err.Error(), "expired token")
}

func TestClientDeleteGroupsByRegionAndDeduplicatesParameters(t *testing.T) {
	api := &fakeSDKSSM{}
	client := &client{deleteParametersFunc: api.DeleteParameters}

	err := client.Delete(context.Background(), &DeleteRequest{Parameters: []DeleteParameter{
		{Name: "/app/two", Region: "eu-north-1"},
		{Name: "/app/one", Region: "eu-central-1"},
		{Name: "/app/two", Region: "eu-north-1"},
	}})

	require.NoError(t, err)
	require.Len(t, api.deleteInputs, 2)
	assert.Equal(t, []string{"/app/one"}, api.deleteInputs[0].Names)
	assert.Equal(t, []string{"/app/two"}, api.deleteInputs[1].Names)
}

func TestTraceHTTPClientUsesTraceRoundTripper(t *testing.T) {
	client := traceHTTPClient()

	require.NotNil(t, client)
	require.NotNil(t, client.Transport)
	_, traced := client.Transport.(traceRoundTripper)
	assert.True(t, traced)
}

func TestChunkStringsUsesDefaultSizeAndKeepsOrder(t *testing.T) {
	values := []string{"a", "b", "c", "d", "e"}

	assert.Equal(t, [][]string{{"a", "b"}, {"c", "d"}, {"e"}}, chunkStrings(values, 2))
	assert.Equal(t, [][]string{{"a", "b", "c", "d", "e"}}, chunkStrings(values, 0))
}
