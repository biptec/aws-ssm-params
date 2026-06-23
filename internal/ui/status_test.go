package ui

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/filter"
	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteLoadProgress(t *testing.T) {
	var output bytes.Buffer

	progress := writeLoadProgress(&output)

	progress(2, 3, "eu-north-1", []inventory.Item{{Path: "/app/one"}, {Path: "/app/two"}})

	assert.Equal(t, "Loading parameters 2/3 from eu-north-1 region...\n/app/one\n/app/two\n", output.String())
}

type fakeSSMClient struct {
	region              string
	regions             []string
	listRegionsErr      error
	params              map[string]ssm.Parameter
	metas               map[string]ssm.Metadata
	putOpts             map[string]ssm.PutParameterOptions
	errs                map[string]error
	describeManyCalls   *int
	getManyCalls        *int
	metadataFilterCalls *int
	metadataFilters     *[][]ssm.ParameterFilter
}

func (f *fakeSSMClient) CheckAccess(context.Context) error { return nil }

func (f *fakeSSMClient) ListRegions(context.Context) ([]string, error) {
	if f.listRegionsErr != nil {
		return nil, f.listRegionsErr
	}

	return append([]string(nil), f.regions...), nil
}

func (f *fakeSSMClient) ForRegion(region string) ssm.Client {
	regional := *f
	regional.region = region

	return &regional
}

func (f *fakeSSMClient) DefaultRegion() string { return f.region }

func (f *fakeSSMClient) GetMany(ctx context.Context, paths []string) (values map[string]ssm.Parameter, errs map[string]error) {
	_ = ctx

	if f.getManyCalls != nil {
		*f.getManyCalls++
	}

	values = map[string]ssm.Parameter{}
	errs = map[string]error{}

	for _, path := range paths {
		key := itemKey(f.region, path)
		if err, ok := f.errs[key]; ok {
			errs[path] = err
			continue
		}

		if value, ok := f.params[key]; ok {
			if value.Region == "" {
				value.Region = f.region
			}

			values[path] = value

			continue
		}

		errs[path] = ssm.ErrNotFound
	}

	return values, errs
}

func (f *fakeSSMClient) DescribeMany(ctx context.Context, paths []string) map[string]ssm.Metadata {
	_ = ctx

	if f.describeManyCalls != nil {
		*f.describeManyCalls++
	}

	result := map[string]ssm.Metadata{}

	for _, path := range paths {
		key := itemKey(f.region, path)
		if meta, ok := f.metas[key]; ok {
			if meta.Region == "" {
				meta.Region = f.region
			}

			result[path] = meta
		}
	}

	return result
}

func (f *fakeSSMClient) ListParameterMetadata(context.Context) ([]ssm.Metadata, error) {
	var result []ssm.Metadata

	for key := range f.metas {
		meta := f.metas[key]

		region, _ := splitItemKey(key)
		if region != f.region {
			continue
		}

		if meta.Region == "" {
			meta.Region = f.region
		}

		result = append(result, meta)
	}

	return result, nil
}

func splitItemKey(key string) (region, path string) {
	for i := range key {
		if key[i] == '\x00' {
			return key[:i], key[i+1:]
		}
	}

	return "", key
}

func (f *fakeSSMClient) ListParameterMetadataWithFilters(ctx context.Context, filters []ssm.ParameterFilter) ([]ssm.Metadata, error) {
	if f.metadataFilterCalls != nil {
		*f.metadataFilterCalls++
	}

	if f.metadataFilters != nil {
		copied := make([]ssm.ParameterFilter, 0, len(filters))
		for _, filter := range filters {
			copied = append(copied, ssm.ParameterFilter{Key: filter.Key, Option: filter.Option, Values: append([]string(nil), filter.Values...)})
		}

		*f.metadataFilters = append(*f.metadataFilters, copied)
	}

	return f.ListParameterMetadata(ctx)
}

func (f *fakeSSMClient) PutParameterWithOptions(ctx context.Context, path, value string, parameterType ssm.ParameterType, opts ssm.PutParameterOptions) error {
	_ = ctx

	if f.putOpts != nil {
		f.putOpts[itemKey(f.region, path)] = opts
	}

	if f.params != nil {
		f.params[itemKey(f.region, path)] = ssm.Parameter{Name: path, Region: f.region, Type: parameterType.String(), Value: value}
	}

	if f.metas != nil && (strings.TrimSpace(opts.Description) != "" || strings.TrimSpace(opts.Policies) != "" || opts.Tier.IsValid() || opts.DataType.IsValid()) {
		meta := f.metas[itemKey(f.region, path)]
		meta.Name = path
		meta.Region = f.region

		meta.Type = parameterType.String()
		if strings.TrimSpace(opts.Description) != "" {
			meta.Description = opts.Description
		}

		if opts.Tier.IsValid() {
			meta.Tier = opts.Tier.String()
		}

		if opts.DataType.IsValid() {
			meta.DataType = opts.DataType.String()
		}

		if opts.PoliciesSet || strings.TrimSpace(opts.Policies) != "" {
			meta.Policies = opts.Policies
		}

		f.metas[itemKey(f.region, path)] = meta
	}

	return nil
}

func (f *fakeSSMClient) DeleteMany(ctx context.Context, paths []string) error {
	_, _ = ctx, paths
	return nil
}

func TestLoadStatusesByItemRegionCombinesValuesMetadataAndMissing(t *testing.T) {
	client := &fakeSSMClient{
		params: map[string]ssm.Parameter{
			itemKey("eu-north-1", "/app/api/password"): {Name: "/app/api/password", Type: "SecureString", Value: "secret", Version: 3},
		},
		metas: map[string]ssm.Metadata{
			itemKey("eu-north-1", "/app/api/password"): {Tier: "Standard", Description: "API password", User: "tester", Modified: "meta-date"},
		},
	}
	items := []inventory.Item{
		{Path: "/app/api/password", Region: "eu-north-1"},
		{Path: "/app/api/missing", Region: "eu-north-1"},
	}

	statuses := LoadStatusesForRegions(context.Background(), client, items, true, nil)

	require.Len(t, statuses, 2)
	assert.True(t, statuses[0].Exists)
	assert.Equal(t, "secret", statuses[0].Value)
	assert.Equal(t, "Standard", statuses[0].Tier)
	assert.Equal(t, "meta-date", statuses[0].Modified)
	assert.False(t, statuses[1].Exists)
	assert.Empty(t, statuses[1].Error)
}

func TestLoadStatusesRegionsExpandsWildcardItemsAcrossRegions(t *testing.T) {
	client := &fakeSSMClient{
		params: map[string]ssm.Parameter{
			itemKey("eu-north-1", "/app/api/password"): {Name: "/app/api/password", Type: "SecureString", Value: "secret"},
		},
		metas: map[string]ssm.Metadata{
			itemKey("eu-north-1", "/app/api/password"): {Name: "/app/api/password", Type: "SecureString"},
		},
	}
	items := []inventory.Item{{Path: "/app/api/password", Region: "*"}, {Path: "/app/api/missing", Region: "*"}}

	statuses := LoadStatusesForRegions(context.Background(), client, items, false, []string{"eu-north-1", "us-east-1"})

	require.Len(t, statuses, 2)
	assert.True(t, statuses[0].Exists)
	assert.Equal(t, "eu-north-1", statuses[0].Item.Region)
	assert.Empty(t, statuses[0].Value, "includeValues=false should hide secret values")
	assert.False(t, statuses[1].Exists)
	assert.Equal(t, "*", statuses[1].Item.Region)
}

func TestLoadStatusesBatchForRegionsStreamEmitsWildcardRegionMatches(t *testing.T) {
	client := &fakeSSMClient{
		params: map[string]ssm.Parameter{
			itemKey("eu-north-1", "/app/api/password"): {Name: "/app/api/password", Type: "SecureString", Value: "secret"},
		},
		metas: map[string]ssm.Metadata{
			itemKey("eu-north-1", "/app/api/password"): {Name: "/app/api/password", Type: "SecureString"},
		},
	}
	items := []inventory.Item{{Path: "/app/api/password", Region: "*"}}

	var batches []Statuses

	statuses := LoadStatusesBatchForRegionsStream(context.Background(), client, items, false, []string{"eu-north-1", "us-east-1"}, nil, func(batch Statuses) {
		batches = append(batches, batch)
	})

	require.Len(t, statuses, 1)
	require.NotEmpty(t, batches)
	assert.Equal(t, "/app/api/password", batches[0][0].Item.Path)
	assert.Equal(t, "eu-north-1", batches[0][0].Item.Region)
}

func TestLoadStatusesRegionsAvoidsValueLookupsWhenValuesAreHidden(t *testing.T) {
	getManyCalls := 0
	client := &fakeSSMClient{
		getManyCalls: &getManyCalls,
		metas: map[string]ssm.Metadata{
			itemKey("eu-north-1", "/app/api/password"): {Name: "/app/api/password", Type: "SecureString"},
		},
	}
	items := []inventory.Item{{Path: "/app/api/password", Region: "*"}}

	statuses := LoadStatusesForRegions(context.Background(), client, items, false, []string{"eu-north-1", "us-east-1"})

	require.Len(t, statuses, 1)
	assert.True(t, statuses[0].Exists)
	assert.Zero(t, getManyCalls)
}

func TestLoadStatusesAllRegionsReturnsInlineErrorsWhenRegionDiscoveryFails(t *testing.T) {
	client := &fakeSSMClient{listRegionsErr: errors.New("regions unavailable")}
	items := []inventory.Item{{Path: "/app/api/password", Region: "*"}}

	statuses := LoadStatusesForRegions(context.Background(), client, items, true, nil)

	require.Len(t, statuses, 1)
	assert.False(t, statuses[0].Exists)
	assert.ErrorContains(t, errors.New(statuses[0].Error), "regions unavailable")
}

func TestExactNameFiltersAreBatchedAwayFromDescribeScans(t *testing.T) {
	groups := make([]filter.Group, 0, 32)

	for i := 0; i < 30; i++ {
		group, err := filter.ParseGroup(fmt.Sprintf("/app/exact/%02d", i))
		require.NoError(t, err)

		groups = append(groups, group)
	}

	for _, value := range []string{"/app/wild-a/*", "/app/wild-b/*"} {
		group, err := filter.ParseGroup(value)
		require.NoError(t, err)

		groups = append(groups, group)
	}

	params := map[string]ssm.Parameter{}
	metas := map[string]ssm.Metadata{}

	for i := 0; i < 30; i++ {
		name := fmt.Sprintf("/app/exact/%02d", i)
		params[itemKey("eu-north-1", name)] = ssm.Parameter{Name: name, Region: "eu-north-1", Type: "String", Value: "value"}
		metas[itemKey("eu-north-1", name)] = ssm.Metadata{Name: name, Region: "eu-north-1", Type: "String"}
	}

	for _, name := range []string{"/app/wild-a/one", "/app/wild-b/two"} {
		metas[itemKey("eu-north-1", name)] = ssm.Metadata{Name: name, Region: "eu-north-1", Type: "String"}
	}

	describeManyCalls := 0
	metadataFilterCalls := 0
	client := &fakeSSMClient{region: "eu-north-1", params: params, metas: metas, describeManyCalls: &describeManyCalls, metadataFilterCalls: &metadataFilterCalls}

	statuses := LoadFilteredStatusesBatchForRegions(context.Background(), client, groups, false, []string{"eu-north-1"}, nil)

	require.Len(t, statuses, 32)
	assert.Equal(t, 3, describeManyCalls)
	assert.Equal(t, 1, metadataFilterCalls)
}

func TestSingleConditionPrefilterGroupsAreMergedByKeyAndOption(t *testing.T) {
	groups := make([]filter.Group, 0, 3)

	for _, value := range []string{"/app-infra/prod**", "/app-infra/stage**"} {
		group, err := filter.ParseGroup(value)
		require.NoError(t, err)

		groups = append(groups, group)
	}

	separateGroup, err := filter.ParseGroup("type:SecureString")
	require.NoError(t, err)

	groups = append(groups, separateGroup)

	metas := map[string]ssm.Metadata{
		itemKey("eu-north-1", "/app-infra/prod/api"):    {Name: "/app-infra/prod/api", Region: "eu-north-1", Type: "String"},
		itemKey("eu-north-1", "/app-infra/stage/api"):   {Name: "/app-infra/stage/api", Region: "eu-north-1", Type: "String"},
		itemKey("eu-north-1", "/app-infra/prod-secret"): {Name: "/app-infra/prod-secret", Region: "eu-north-1", Type: "SecureString"},
	}
	metadataFilterCalls := 0

	var metadataFilters [][]ssm.ParameterFilter

	client := &fakeSSMClient{region: "eu-north-1", metas: metas, metadataFilterCalls: &metadataFilterCalls, metadataFilters: &metadataFilters}

	statuses := LoadFilteredStatusesBatchForRegions(context.Background(), client, groups, false, []string{"eu-north-1"}, nil)

	require.Len(t, statuses, 3)
	assert.Equal(t, 2, metadataFilterCalls)
	require.Len(t, metadataFilters, 2)
	assert.Equal(t, []ssm.ParameterFilter{{Key: "Name", Option: "BeginsWith", Values: []string{"/app-infra/prod", "/app-infra/stage"}}}, metadataFilters[0])
	assert.Equal(t, []ssm.ParameterFilter{{Key: "Type", Option: "Equals", Values: []string{"SecureString"}}}, metadataFilters[1])
}

func TestStatusHelpers(t *testing.T) {
	status := statusFromValue(
		&inventory.Item{Path: "/path", Region: "*"},
		&ssm.Parameter{Region: "eu-north-1", Type: "SecureString", Value: "secret", Version: 7, Modified: "param-date"},
		&ssm.Metadata{Tier: "Advanced", Description: "desc", User: "user"},
		true,
	)

	assert.Equal(t, "eu-north-1", status.Item.Region)
	assert.True(t, status.Exists)
	assert.Equal(t, 6, status.Length)
	assert.Equal(t, hashPrefix("secret"), status.SHA256Prefix)
	assert.Equal(t, "param-date", status.Modified)
	assert.Equal(t, "OK", status.Label())
	assert.Equal(t, "MISS", (&Status{}).Label())
	assert.Equal(t, "ERR", (&Status{Error: "boom"}).Label())
	assert.Equal(t, "EMPTY", (&Status{Exists: true, Empty: true}).Label())
	assert.Equal(t, "MISSING", (&Status{}).DisplayLabel())
	assert.Equal(t, "ERROR", (&Status{Error: "boom"}).DisplayLabel())
	assert.Equal(t, "eu-north-1", (&Status{Item: inventory.Item{Region: "eu-north-1"}}).RegionLabel("us-east-1"))
	assert.Equal(t, "-", (&Status{Item: inventory.Item{Region: "*"}}).RegionLabel("eu-north-1"))
	assert.Equal(t, "us-east-1", (&Status{}).RegionLabel("us-east-1"))
	assert.True(t, (&Status{Type: ssm.ParameterTypeSecureString.String()}).HasSensitiveValue())
	assert.False(t, (&Status{Type: ssm.ParameterTypeString.String()}).HasSensitiveValue())
}

func TestSecureStringWithoutIncludedValueIsNotEmpty(t *testing.T) {
	status := statusFromValue(
		&inventory.Item{Path: "/secret", Region: "eu-north-1"},
		&ssm.Parameter{Type: ssm.ParameterTypeSecureString.String(), Value: "encrypted-ciphertext"},
		&ssm.Metadata{},
		false,
	)

	assert.True(t, status.Exists)
	assert.Empty(t, status.Value)
	assert.False(t, status.Empty)
	assert.Equal(t, "OK", status.Label())
}

func TestHiddenSecureStringValueIsNotEmpty(t *testing.T) {
	status := statusFromValue(
		&inventory.Item{Path: "/secret", Region: "eu-north-1"},
		&ssm.Parameter{Type: ssm.ParameterTypeSecureString.String(), ValueHidden: true},
		&ssm.Metadata{},
		true,
	)

	assert.True(t, status.Exists)
	assert.Empty(t, status.Value)
	assert.False(t, status.Empty)
	assert.Equal(t, "OK", status.Label())
}

func TestStatusesFilterKeepsMatchingStatuses(t *testing.T) {
	groups, err := filter.ParseGroups([]string{"name:/app/a"})
	require.NoError(t, err)

	statuses := []Status{
		{Item: inventory.Item{Path: "/app/a"}, Exists: true},
		{Item: inventory.Item{Path: "/app/b"}, Exists: true},
	}

	filtered := Statuses(statuses).Filter(groups)

	require.Len(t, filtered, 1)
	assert.Equal(t, "/app/a", filtered[0].Item.Path)
}

func TestLoadStatusesWithoutItemsDiscoversParametersInSelectedRegions(t *testing.T) {
	client := &fakeSSMClient{
		params: map[string]ssm.Parameter{
			itemKey("eu-north-1", "/app/a"): {Name: "/app/a", Region: "eu-north-1", Type: "String", Value: "one", Version: 1},
			itemKey("us-east-1", "/app/b"):  {Name: "/app/b", Region: "us-east-1", Type: "SecureString", Value: "two", Version: 2},
		},
		metas: map[string]ssm.Metadata{
			itemKey("eu-north-1", "/app/a"): {Name: "/app/a", Region: "eu-north-1", Type: "String", Tier: "Standard"},
			itemKey("us-east-1", "/app/b"):  {Name: "/app/b", Region: "us-east-1", Type: "SecureString", Tier: "Standard"},
		},
	}

	statuses := LoadStatusesForRegions(context.Background(), client, nil, true, []string{"eu-north-1", "us-east-1"})

	require.Len(t, statuses, 2)
	assert.Equal(t, "eu-north-1", statuses[0].Item.Region)
	assert.Equal(t, "/app/a", statuses[0].Item.Path)
	assert.Equal(t, "one", statuses[0].Value)
	assert.Equal(t, "us-east-1", statuses[1].Item.Region)
	assert.Equal(t, "/app/b", statuses[1].Item.Path)
	assert.Equal(t, "two", statuses[1].Value)
}

func TestLoadStatusesWithoutItemsKeepsMetadataWhenValuesAreNotIncluded(t *testing.T) {
	client := &fakeSSMClient{
		region: "eu-north-1",
		metas: map[string]ssm.Metadata{
			itemKey("eu-north-1", "/app/meta-only"): {Name: "/app/meta-only", Type: "String", Tier: "Standard", Description: "desc"},
		},
	}

	statuses := LoadStatusesForRegions(context.Background(), client, nil, false, nil)

	require.Len(t, statuses, 1)
	assert.True(t, statuses[0].Exists)
	assert.Equal(t, "/app/meta-only", statuses[0].Item.Path)
	assert.Equal(t, "eu-north-1", statuses[0].Item.Region)
	assert.Equal(t, "String", statuses[0].Type)
	assert.Equal(t, "Standard", statuses[0].Tier)
	assert.Equal(t, "desc", statuses[0].Description)
	assert.Empty(t, statuses[0].Value)
}
