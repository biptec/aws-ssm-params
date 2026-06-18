package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSSMClient struct {
	region         string
	regions        []string
	listRegionsErr error
	params         map[string]ssm.Parameter
	metas          map[string]ssm.Metadata
	putOpts        map[string]ssm.PutParameterOptions
	errs           map[string]error
}

func (f fakeSSMClient) CheckAccess(context.Context) error { return nil }

func (f fakeSSMClient) ListRegions(context.Context) ([]string, error) {
	if f.listRegionsErr != nil {
		return nil, f.listRegionsErr
	}
	return append([]string(nil), f.regions...), nil
}

func (f fakeSSMClient) ForRegion(region string) ssm.Client {
	f.region = region
	return f
}

func (f fakeSSMClient) DefaultRegion() string { return f.region }

func (f fakeSSMClient) Get(ctx context.Context, path string) (ssm.Parameter, error) {
	values, errs := f.GetMany(ctx, []string{path})
	if value, ok := values[path]; ok {
		return value, nil
	}
	if err, ok := errs[path]; ok {
		return ssm.Parameter{}, err
	}
	return ssm.Parameter{}, ssm.ErrNotFound
}

func (f fakeSSMClient) GetMany(_ context.Context, paths []string) (map[string]ssm.Parameter, map[string]error) {
	values := map[string]ssm.Parameter{}
	errs := map[string]error{}
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

func (f fakeSSMClient) DescribeMany(_ context.Context, paths []string) map[string]ssm.Metadata {
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

func (f fakeSSMClient) ListParameterMetadata(context.Context) ([]ssm.Metadata, error) {
	var result []ssm.Metadata
	for key, meta := range f.metas {
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

func splitItemKey(key string) (string, string) {
	for i := range key {
		if key[i] == '\x00' {
			return key[:i], key[i+1:]
		}
	}
	return "", key
}

func (f fakeSSMClient) PutParameter(ctx context.Context, path, value string, parameterType ssm.ParameterType) error {
	return f.PutParameterWithOptions(ctx, path, value, parameterType, ssm.PutParameterOptions{Overwrite: true})
}

func (f fakeSSMClient) PutParameterWithOptions(_ context.Context, path, value string, parameterType ssm.ParameterType, opts ssm.PutParameterOptions) error {
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
		if strings.TrimSpace(opts.Policies) != "" {
			meta.Policies = opts.Policies
		}
		f.metas[itemKey(f.region, path)] = meta
	}
	return nil
}
func (f fakeSSMClient) DeleteMany(_ context.Context, _ []string) error { return nil }

func TestLoadStatusesByItemRegionCombinesValuesMetadataAndMissing(t *testing.T) {
	client := fakeSSMClient{
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
	client := fakeSSMClient{
		params: map[string]ssm.Parameter{
			itemKey("eu-north-1", "/app/api/password"): {Name: "/app/api/password", Type: "SecureString", Value: "secret"},
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

func TestLoadStatusesAllRegionsReturnsInlineErrorsWhenRegionDiscoveryFails(t *testing.T) {
	client := fakeSSMClient{listRegionsErr: errors.New("regions unavailable")}
	items := []inventory.Item{{Path: "/app/api/password", Region: "*"}}

	statuses := LoadStatusesForRegions(context.Background(), client, items, true, nil)

	require.Len(t, statuses, 1)
	assert.False(t, statuses[0].Exists)
	assert.ErrorContains(t, errors.New(statuses[0].Error), "regions unavailable")
}

func TestStatusHelpers(t *testing.T) {
	status := statusFromValue(
		inventory.Item{Path: "/path", Region: "*"},
		ssm.Parameter{Region: "eu-north-1", Type: "SecureString", Value: "secret", Version: 7, Modified: "param-date"},
		ssm.Metadata{Tier: "Advanced", Description: "desc", User: "user"},
		true,
	)

	assert.Equal(t, "eu-north-1", status.Item.Region)
	assert.True(t, status.Exists)
	assert.Equal(t, 6, status.Length)
	assert.Equal(t, hashPrefix("secret"), status.SHA256Prefix)
	assert.Equal(t, "param-date", status.Modified)
	assert.Equal(t, "OK", statusLabel(status))
	assert.Equal(t, "MISS", statusLabel(Status{}))
	assert.Equal(t, "ERR", statusLabel(Status{Error: "boom"}))
	assert.Equal(t, "EMPTY", statusLabel(Status{Exists: true, Empty: true}))
}

func TestLoadStatusesWithoutItemsDiscoversParametersInSelectedRegions(t *testing.T) {
	client := fakeSSMClient{
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
	client := fakeSSMClient{
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
