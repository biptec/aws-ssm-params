package ssm

import (
	"context"
	"strings"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type deleteRecorder struct {
	region string
	calls  *[]string
}

func (client *deleteRecorder) CheckAccess(context.Context) error { return nil }

func (client *deleteRecorder) ListRegions(context.Context) ([]string, error) { return nil, nil }

func (client *deleteRecorder) ForRegion(region string) Client {
	return &deleteRecorder{region: region, calls: client.calls}
}

func (client *deleteRecorder) DefaultRegion() string { return client.region }

func (*deleteRecorder) GetMany(context.Context, []string) (parameters map[string]Parameter, errs map[string]error) {
	return nil, nil
}

func (*deleteRecorder) DescribeMany(context.Context, []string) map[string]Metadata { return nil }

func (*deleteRecorder) ListParameterMetadata(context.Context) ([]Metadata, error) { return nil, nil }

func (*deleteRecorder) ListParameterMetadataWithFilters(context.Context, []ParameterFilter) ([]Metadata, error) {
	return nil, nil
}

func (*deleteRecorder) PutParameterWithOptions(context.Context, string, string, ParameterType, PutParameterOptions) error {
	return nil
}

func (client *deleteRecorder) DeleteMany(ctx context.Context, paths []string) error {
	if err := ctx.Err(); err != nil {
		return errors.Wrap(err, "record fake deletion")
	}

	*client.calls = append(*client.calls, client.region+":"+strings.Join(paths, ","))

	return nil
}

func TestDeleterGroupsByRegionAndDeduplicatesTargets(t *testing.T) {
	var calls []string

	client := &deleteRecorder{calls: &calls}

	err := NewDeleter(client).Delete(context.Background(), []DeleteTarget{
		{Name: "/app/two", Region: "eu-north-1"},
		{Name: "/app/one", Region: "eu-central-1"},
		{Name: "/app/two", Region: "eu-north-1"},
	})

	require.NoError(t, err)
	assert.Equal(t, []string{
		"eu-central-1:/app/one",
		"eu-north-1:/app/two",
	}, calls)
}
