package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPathMappingsMapAWSToFileUsingLongestPrefix(t *testing.T) {
	mappings := PathMappings{
		{AWSPath: "/app", FilePath: "/file"},
		{AWSPath: "/app/dev", FilePath: "/dev"},
	}

	assert.Equal(t, "/dev/token", mappings.ToFile("/app/dev/token"))
}

func TestPathMappingsMapFileToAWSUsingLongestPrefix(t *testing.T) {
	mappings := PathMappings{
		{AWSPath: "/app", FilePath: "/file"},
		{AWSPath: "/app/dev", FilePath: "/dev"},
	}

	assert.Equal(t, "/app/dev/token", mappings.ToAWS("/dev/token"))
}

func TestPathMappingsUsePlainPrefixMatchingWithoutPathBoundaryChecks(t *testing.T) {
	mappings := PathMappings{{AWSPath: "/app/dev", FilePath: "/"}}

	assert.Equal(t, "/elopment/token", mappings.ToFile("/app/development/token"))
}

func TestPathMappingsPreserveUnmatchedPaths(t *testing.T) {
	mappings := PathMappings{{AWSPath: "/app/dev/", FilePath: "/"}}

	assert.Equal(t, "/app/development/token", mappings.ToFile("/app/development/token"))
}

func TestPathMappingsCanStripMatchedPrefix(t *testing.T) {
	mappings := PathMappings{{AWSPath: "/app/test/", FilePath: ""}}

	assert.Equal(t, "foo", mappings.ToFile("/app/test/foo"))
	assert.Equal(t, "/app/test/foo", mappings.ToAWS("foo"))
}

func TestParsePathMappingsAllowsEmptyFilePrefix(t *testing.T) {
	mappings, err := ParsePathMappings([]string{"/app/test/:"})

	require.NoError(t, err)
	assert.Equal(t, PathMappings{{AWSPath: "/app/test/", FilePath: ""}}, mappings)
}

func TestParsePathMappingsRejectsMissingSeparator(t *testing.T) {
	_, err := ParsePathMappings([]string{"/app/test"})

	require.Error(t, err)
	assert.ErrorContains(t, err, "aws_path:file_path")
}
