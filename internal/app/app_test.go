package app

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/biptec/aws-ssm-params/internal/inventory"
)

func TestPrepareItemsLoadsExplicitInventorySources(t *testing.T) {
	cfg := Config{
		Region: "eu-north-1",
		InventoryItems: []inventory.Item{
			{Path: "/app/from-stdin", Kind: "path-file", Source: "stdin", SecretName: "from-stdin"},
			{Path: "/app/from-stdin", Kind: "path-file", Source: "stdin", SecretName: "duplicate"},
		},
	}

	items, err := PrepareItems(context.Background(), &cfg)

	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "/app/from-stdin", items[0].Path)
	assert.Equal(t, "stdin", items[0].Source)
	assert.Equal(t, "eu-north-1", items[0].Region)
}

func TestPrepareItemsMarksExplicitInventoryWildcardForMultipleRegions(t *testing.T) {
	cfg := Config{
		Regions:        []string{"eu-north-1", "eu-central-1"},
		Region:         "eu-north-1",
		InventoryItems: []inventory.Item{{Path: "/app/shared", Kind: "path-file", Source: "stdin", SecretName: "shared"}},
	}

	items, err := PrepareItems(context.Background(), &cfg)

	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "/app/shared", items[0].Path)
	assert.Equal(t, "*", items[0].Region)
}
