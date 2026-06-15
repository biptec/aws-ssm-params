package inventory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadPathsFileIgnoresCommentsDeduplicatesAndSorts(t *testing.T) {
	file := filepath.Join(t.TempDir(), "paths.txt")
	require.NoError(t, writeTestFile(file, `
# full-line comment
/app/prod/z/password # inline comment
/app/prod/a/token
/app/prod/z/password
`))

	items, err := LoadPathsFile(file)

	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "/app/prod/a/token", items[0].Path)
	assert.Equal(t, "token", items[0].SecretName)
	assert.Equal(t, "path-file", items[0].Kind)
	assert.Equal(t, "/app/prod/z/password", items[1].Path)
}

func TestLoadPathsFileRejectsRelativePaths(t *testing.T) {
	file := filepath.Join(t.TempDir(), "paths.txt")
	require.NoError(t, writeTestFile(file, "relative/path\n"))

	items, err := LoadPathsFile(file)

	require.Error(t, err)
	assert.Nil(t, items)
	assert.ErrorContains(t, err, "invalid SSM path")
}

func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}
