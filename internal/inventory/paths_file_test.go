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

func TestPathsFileLoadIgnoresCommentsDeduplicatesAndSorts(t *testing.T) {
	file := filepath.Join(t.TempDir(), "paths.txt")
	require.NoError(t, writeTestFile(file, `
# full-line comment
/app/prod/z/password # inline comment
/app/prod/a/token
/app/prod/z/password
`))

	items, err := (PathsFile{Path: file}).Load()

	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "/app/prod/a/token", items[0].Path)
	assert.Equal(t, "token", items[0].SecretName)
	assert.Equal(t, "path-file", items[0].Kind)
	assert.Equal(t, "/app/prod/z/password", items[1].Path)
}

func TestPathsFileLoadRejectsRelativePaths(t *testing.T) {
	file := filepath.Join(t.TempDir(), "paths.txt")
	require.NoError(t, writeTestFile(file, "relative/path\n"))

	items, err := (PathsFile{Path: file}).Load()

	require.Error(t, err)
	assert.Nil(t, items)
	assert.ErrorContains(t, err, "invalid SSM name")
}

func TestPathFileLinePathNormalizesAllSupportedLineForms(t *testing.T) {
	tests := map[string]string{
		"empty":          "",
		"comment":        "  # comment\r\n",
		"path":           " /app/token \r\n",
		"inline comment": "/app/token # managed",
	}

	assert.Equal(t, "", pathFileLinePath(tests["empty"]))
	assert.Equal(t, "", pathFileLinePath(tests["comment"]))
	assert.Equal(t, "/app/token", pathFileLinePath(tests["path"]))
	assert.Equal(t, "/app/token", pathFileLinePath(tests["inline comment"]))
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

func TestPathsFileAppendAddsToEndAndAvoidsDuplicates(t *testing.T) {
	file := writeTempFile(t, "# Parameters\n/app/old # existing\n")

	appended, err := (PathsFile{Path: file}).Append("/app/new")
	require.NoError(t, err)
	assert.True(t, appended)
	assert.Equal(t, "# Parameters\n/app/old # existing\n/app/new\n", readTempFile(t, file))

	appended, err = (PathsFile{Path: file}).Append("/app/old")
	require.NoError(t, err)
	assert.False(t, appended)
	assert.Equal(t, "# Parameters\n/app/old # existing\n/app/new\n", readTempFile(t, file))
}

func TestPathsFileAppendAddsMissingNewlineBeforeAppending(t *testing.T) {
	file := writeTempFile(t, "/app/old")

	appended, err := (PathsFile{Path: file}).Append("/app/new")
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

func TestPathsFileRemoveDeletesPathsAndPreservesOtherLines(t *testing.T) {
	file := writeTempFile(t, "# tracked paths\n/app/old # remove me\n/app/keep\n/app/old\n")

	removed, err := (PathsFile{Path: file}).Remove([]string{"/app/old"})

	require.NoError(t, err)
	assert.Equal(t, 2, removed)
	assert.Equal(t, "# tracked paths\n/app/keep\n", readTempFile(t, file))
}
