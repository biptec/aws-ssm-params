package format

import (
	"bytes"
	"strings"
	"testing"

	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExportDotenvWritesSSMCommentsAndQuotedValues(t *testing.T) {
	var out bytes.Buffer
	records := []Record{
		{Path: "/app/prod/api/password", Alias: "PASSWORD", Value: "secret with spaces"},
		{Path: "/app/prod/api/multiline", Alias: "MULTILINE", Value: "line1\nline2"},
	}

	err := ExportDotenv(&out, records)

	require.NoError(t, err)
	assert.Contains(t, out.String(), "# ssm: /app/prod/api/password\nPASSWORD=\"secret with spaces\"")
	assert.Contains(t, out.String(), "# ssm: /app/prod/api/multiline\nMULTILINE=\"line1\\nline2\"")
}

func TestExportDotenvPreservesParameterTypeMetadata(t *testing.T) {
	var out bytes.Buffer
	records := []Record{{Path: "/app/prod/api/log-level", Alias: "LOG_LEVEL", Value: "debug", Type: "String"}}

	err := ExportDotenv(&out, records)

	require.NoError(t, err)
	assert.Contains(t, out.String(), "# ssm: /app/prod/api/log-level\n# type: String\nLOG_LEVEL=\"debug\"")
}

func TestImportDotenvUsesExplicitSSMCommentBeforeAliasResolution(t *testing.T) {
	input := strings.NewReader("# ssm: /explicit/path\nANY_ALIAS='secret value'\n")

	records, err := ImportDotenv(input, nil)

	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, Record{Path: "/explicit/path", Alias: "ANY_ALIAS", Value: "secret value"}, records[0])
}

func TestImportDotenvPreservesTypeComment(t *testing.T) {
	input := strings.NewReader("# ssm: /app/prod/api/log-level\n# type: String\nLOG_LEVEL=debug\n")

	records, err := ImportDotenv(input, nil)

	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "String", records[0].Type)
	assert.Equal(t, "debug", records[0].Value)
}

func TestImportDotenvResolvesAliasFromInventory(t *testing.T) {
	items := []inventory.Item{{Path: "/app/prod/api/password", Kind: "app-secret"}}

	records, err := ImportDotenv(strings.NewReader("PASSWORD=\"secret\"\n"), items)

	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "/app/prod/api/password", records[0].Path)
	assert.Equal(t, "secret", records[0].Value)
}

func TestImportDotenvRejectsAmbiguousAliases(t *testing.T) {
	items := []inventory.Item{
		{Path: "/app/prod/api/password", Kind: "app-secret"},
		{Path: "/app/prod/worker/password", Kind: "app-secret"},
	}

	records, err := ImportDotenv(strings.NewReader("PASSWORD=secret\n"), items)

	require.Error(t, err)
	assert.Nil(t, records)
	assert.ErrorContains(t, err, "ambiguous")
}

func TestImportJSONSortsRecordsByPath(t *testing.T) {
	records, err := ImportJSON(strings.NewReader(`{"/z/path":"last","/a/path":"first"}`))

	require.NoError(t, err)
	require.Len(t, records, 2)
	assert.Equal(t, "/a/path", records[0].Path)
	assert.Equal(t, "first", records[0].Value)
	assert.Equal(t, "/z/path", records[1].Path)
}

func TestImportJSONSupportsTypedRecordObjects(t *testing.T) {
	records, err := ImportJSON(strings.NewReader(`{"/app/prod/api/log-level":{"type":"String","value":"debug"}}`))

	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "/app/prod/api/log-level", records[0].Path)
	assert.Equal(t, "String", records[0].Type)
	assert.Equal(t, "debug", records[0].Value)
}

func TestExportJSONUsesTypedShapeWhenTypeMetadataExists(t *testing.T) {
	var out bytes.Buffer
	records := []Record{{Path: "/app/prod/api/log-level", Value: "debug", Type: "String"}}

	err := ExportJSON(&out, records)

	require.NoError(t, err)
	assert.Contains(t, out.String(), `"/app/prod/api/log-level": {`)
	assert.Contains(t, out.String(), `"type": "String"`)
	assert.Contains(t, out.String(), `"value": "debug"`)
}

func TestAliasForPathHandlesKnownSecretTypes(t *testing.T) {
	assert.Equal(t, "PASSWORD", AliasForPath("/app/prod/api/password", inventory.Item{Kind: "app-secret"}))
	assert.Equal(t, "GHCR_TOKEN", AliasForPath("/app-infra/prod/ghcr/token", inventory.Item{}))
	assert.Equal(t, "FLUX_GITHUB_TOKEN", AliasForPath("/flux/github/token", inventory.Item{}))
	assert.Equal(t, "TLS_EXAMPLE_COM_CRT", AliasForPath("/app-infra/prod/tls/example.com/tls.crt", inventory.Item{}))
	assert.Equal(t, "TLS_EXAMPLE_COM_KEY", AliasForPath("/app-infra/prod/tls/example.com/tls.key", inventory.Item{}))
}
