package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBasePathResolveAndRelativizeRoundTrip(t *testing.T) {
	basePath, err := ParseBasePath("/app/prod/")
	require.NoError(t, err)

	absolute, err := basePath.Resolve("api/token")
	require.NoError(t, err)
	assert.Equal(t, "/app/prod/api/token", absolute)

	relative, err := basePath.Relativize(absolute)
	require.NoError(t, err)
	assert.Equal(t, "api/token", relative)
}

func TestBasePathPreservesAbsoluteImportedName(t *testing.T) {
	basePath, err := ParseBasePath("/app/prod")
	require.NoError(t, err)

	name, err := basePath.Resolve("/shared/token")

	require.NoError(t, err)
	assert.Equal(t, "/shared/token", name)
}

func TestBasePathRejectsSiblingPrefixOnExport(t *testing.T) {
	basePath, err := ParseBasePath("/app/prod")
	require.NoError(t, err)

	_, err = basePath.Relativize("/app/prod2/token")

	require.Error(t, err)
	assert.ErrorContains(t, err, "outside --base-path")
}

func TestBasePathRequiresAbsoluteValue(t *testing.T) {
	_, err := ParseBasePath("app/prod")

	require.Error(t, err)
	assert.ErrorContains(t, err, "--base-path must start with /")
}
