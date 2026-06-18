package inventory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/biptec/aws-ssm-params/internal/fileio"

	crerr "github.com/cockroachdb/errors"

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
	assert.ErrorContains(t, err, "invalid SSM name")
}

func writeTestFile(path, content string) error {
	return crerr.Wrapf(os.WriteFile(path, []byte(content), 0o600), "write test file %s", path)
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "paths.txt")
	require.NoError(t, os.WriteFile(file, []byte(content), 0o600))
	return file
}

func TestAppendPathIfMissingAppendsToEndAndAvoidsDuplicates(t *testing.T) {
	file := writeTempFile(t, "# Parameters\n/app/old # existing\n")

	appended, err := AppendPathIfMissing(file, "/app/new")
	require.NoError(t, err)
	assert.True(t, appended)
	assert.Equal(t, "# Parameters\n/app/old # existing\n/app/new\n", readTempFile(t, file))

	appended, err = AppendPathIfMissing(file, "/app/old")
	require.NoError(t, err)
	assert.False(t, appended)
	assert.Equal(t, "# Parameters\n/app/old # existing\n/app/new\n", readTempFile(t, file))
}

func TestAppendPathIfMissingAddsMissingNewlineBeforeAppending(t *testing.T) {
	file := writeTempFile(t, "/app/old")

	appended, err := AppendPathIfMissing(file, "/app/new")
	require.NoError(t, err)
	assert.True(t, appended)
	assert.Equal(t, "/app/old\n/app/new\n", readTempFile(t, file))
}

func readTempFile(t *testing.T, file string) string {
	t.Helper()
	data, err := fileio.ReadFile(file)
	require.NoError(t, err)
	return string(data)
}

func TestRemovePathsIfPresentRemovesPathsAndPreservesOtherLines(t *testing.T) {
	file := writeTempFile(t, "# tracked paths\n/app/old # remove me\n/app/keep\n/app/old\n")

	removed, err := RemovePathsIfPresent(file, []string{"/app/old"})

	require.NoError(t, err)
	assert.Equal(t, 2, removed)
	assert.Equal(t, "# tracked paths\n/app/keep\n", readTempFile(t, file))
}
