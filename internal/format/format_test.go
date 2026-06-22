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
	assert.Equal(t, Record{Path: "/explicit/path", Alias: "ANY_ALIAS", Fields: []string{"name", "value"}, Value: "secret value"}, records[0])
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

func TestImportDotenvUsesUnresolvedKeysAsRelativeNames(t *testing.T) {
	input := strings.NewReader("DATABASE_URL=postgres://localhost/app\n")

	records, err := ImportDotenv(input, nil)

	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "DATABASE_URL", records[0].Path)
	assert.Equal(t, "DATABASE_URL", records[0].Alias)
	assert.Equal(t, "postgres://localhost/app", records[0].Value)
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

func TestImportJSONSupportsFullRecordObjects(t *testing.T) {
	input := strings.NewReader(`{
		"/app/prod/api/key": {
			"region": "eu-north-1",
			"type": "SecureString",
			"tier": "Advanced",
			"dataType": "text",
			"policies": "[{\"Type\":\"Expiration\"}]",
			"description": "API key",
			"value": "secret",
			"date": "2026-06-17T00:00:00Z",
			"version": 7,
			"len": 6,
			"sha256": "2bb80d53",
			"user": "arn:aws:iam::123:user/dev"
		}
	}`)

	records, err := ImportJSON(input)

	require.NoError(t, err)
	require.Len(t, records, 1)
	record := records[0]
	assert.Equal(t, []string{"name", "region", "type", "tier", "data-type", "policies", "description", "value", "date", "version", "len", "sha256", "user"}, record.Fields)
	assert.Equal(t, "eu-north-1", record.Region)
	assert.Equal(t, "SecureString", record.Type)
	assert.Equal(t, "Advanced", record.Tier)
	assert.Equal(t, "text", record.DataType)
	assert.Equal(t, `[{"Type":"Expiration"}]`, record.Policies)
	assert.Equal(t, "API key", record.Description)
	assert.Equal(t, "secret", record.Value)
	assert.Equal(t, "2026-06-17T00:00:00Z", record.Date)
	assert.Equal(t, int64(7), record.Version)
	assert.Equal(t, 6, record.Len)
	assert.Equal(t, "2bb80d53", record.SHA256)
	assert.Equal(t, "arn:aws:iam::123:user/dev", record.User)
}

func TestExportJSONMappedKeepsExplicitEmptyValueField(t *testing.T) {
	var out bytes.Buffer
	records := []Record{{Path: "/app/prod/api/key", Fields: []string{"value"}, Value: ""}}

	err := ExportJSONMapped(&out, records, []FieldMapping{{AWSName: "value", FileName: "value"}}, "")

	require.NoError(t, err)
	assert.JSONEq(t, `[{"value":""}]`, out.String())
}

func TestExportJSONMappedKeepsKeyedExplicitEmptyValueField(t *testing.T) {
	var out bytes.Buffer
	records := []Record{{Path: "/app/prod/api/key", Fields: []string{"name", "value"}, Value: ""}}

	err := ExportJSONMapped(&out, records, []FieldMapping{{AWSName: "value", FileName: "value"}}, "name")

	require.NoError(t, err)
	assert.JSONEq(t, `{"/app/prod/api/key":{"value":""}}`, out.String())
}

func TestExportYAMLUsesArrayShapeAndMappings(t *testing.T) {
	var out bytes.Buffer
	records := []Record{{Path: "/app/prod/api/key", Fields: []string{"name", "type", "value"}, Type: "SecureString", Value: "secret"}}

	err := ExportYAMLMapped(&out, records, []FieldMapping{{AWSName: "name", FileName: "path"}, {AWSName: "type", FileName: "kind"}, {AWSName: "value", FileName: "secret"}}, "")

	require.NoError(t, err)
	yaml := out.String()
	assert.Contains(t, yaml, "- kind: SecureString")
	assert.Contains(t, yaml, "  path: /app/prod/api/key")
	assert.Contains(t, yaml, "  secret: secret")
}

func TestExportYAMLUsesKeyedShape(t *testing.T) {
	var out bytes.Buffer
	records := []Record{{Path: "/app/prod/api/key", Fields: []string{"name", "value"}, Value: "secret"}}

	err := ExportYAMLMapped(&out, records, nil, "name")

	require.NoError(t, err)
	yaml := out.String()
	assert.Contains(t, yaml, "/app/prod/api/key:")
	assert.Contains(t, yaml, "  value: secret")
	assert.False(t, strings.Contains(yaml, "name:"))
}

func TestImportYAMLReadsArrayRecords(t *testing.T) {
	input := strings.NewReader(`
- name: /app/prod/api/key
  type: SecureString
  value: secret
  version: 7
`)

	records, err := ImportYAMLMapped(input, nil, "")

	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "/app/prod/api/key", records[0].Path)
	assert.Equal(t, "SecureString", records[0].Type)
	assert.Equal(t, "secret", records[0].Value)
	assert.Equal(t, int64(7), records[0].Version)
}

func TestImportYAMLReadsKeyedRecords(t *testing.T) {
	input := strings.NewReader(`
/app/prod/api/key:
  type: SecureString
  value: secret
`)

	records, err := ImportYAMLMapped(input, nil, "name")

	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "/app/prod/api/key", records[0].Path)
	assert.Equal(t, "SecureString", records[0].Type)
	assert.Equal(t, "secret", records[0].Value)
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

func TestExportJSONIncludesRequestedMetadataFields(t *testing.T) {
	var out bytes.Buffer
	records := []Record{{
		Path:        "/app/prod/api/key",
		Fields:      []string{"name", "region", "type", "tier", "data-type", "description", "value", "date", "version", "len", "sha256", "user"},
		Region:      "eu-north-1",
		Type:        "SecureString",
		Tier:        "Advanced",
		DataType:    "text",
		Description: "API key",
		Value:       "secret",
		Date:        "2026-06-17T00:00:00Z",
		Version:     7,
		Len:         6,
		SHA256:      "2bb80d53",
		User:        "arn:aws:iam::123:user/dev",
	}}

	err := ExportJSON(&out, records)

	require.NoError(t, err)
	json := out.String()
	assert.Contains(t, json, `"/app/prod/api/key": {`)
	assert.Contains(t, json, `"region": "eu-north-1"`)
	assert.Contains(t, json, `"type": "SecureString"`)
	assert.Contains(t, json, `"tier": "Advanced"`)
	assert.Contains(t, json, `"dataType": "text"`)
	assert.Contains(t, json, `"description": "API key"`)
	assert.Contains(t, json, `"value": "secret"`)
	assert.Contains(t, json, `"date": "2026-06-17T00:00:00Z"`)
	assert.Contains(t, json, `"version": 7`)
	assert.Contains(t, json, `"len": 6`)
	assert.Contains(t, json, `"sha256": "2bb80d53"`)
	assert.Contains(t, json, `"user": "arn:aws:iam::123:user/dev"`)
}

func TestExportJSONUsesObjectShapeForValueOnlyFields(t *testing.T) {
	var out bytes.Buffer
	records := []Record{{Path: "/app/prod/api/key", Fields: []string{"name", "value"}, Value: "secret", Type: "SecureString"}}

	err := ExportJSON(&out, records)

	require.NoError(t, err)
	assert.Equal(t, "{\n  \"/app/prod/api/key\": {\n    \"value\": \"secret\"\n  }\n}\n", out.String())
}

func TestExportJSONCanExportMetadataWithoutValue(t *testing.T) {
	var out bytes.Buffer
	records := []Record{{Path: "/app/prod/api/key", Fields: []string{"name", "type", "date"}, Type: "String", Value: "secret", Date: "2026-06-17T00:00:00Z"}}

	err := ExportJSON(&out, records)

	require.NoError(t, err)
	json := out.String()
	assert.Contains(t, json, `"type": "String"`)
	assert.Contains(t, json, `"date": "2026-06-17T00:00:00Z"`)
	assert.False(t, strings.Contains(json, `"value"`))
}

func TestExportScalarLinesWritesOneValuePerLine(t *testing.T) {
	records := []Record{{Path: "/app/a", Value: "secret-a"}, {Path: "/app/b", Value: "secret-b"}}
	var out bytes.Buffer

	err := ExportScalarLines(&out, records, "value")

	require.NoError(t, err)
	assert.Equal(t, "secret-a\nsecret-b\n", out.String())
}

func TestExportJSONScalarWritesArray(t *testing.T) {
	records := []Record{{Path: "/app/a", Value: "secret-a"}, {Path: "/app/b", Value: "secret-b"}}
	var out bytes.Buffer

	err := ExportJSONScalar(&out, records, "value", "")

	require.NoError(t, err)
	assert.JSONEq(t, `["secret-a","secret-b"]`, out.String())
}

func TestExportJSONScalarWritesKeyedMap(t *testing.T) {
	records := []Record{{Path: "/app/a", Value: "secret-a"}, {Path: "/app/b", Value: "secret-b"}}
	var out bytes.Buffer

	err := ExportJSONScalar(&out, records, "value", "name")

	require.NoError(t, err)
	assert.JSONEq(t, `{"/app/a":"secret-a","/app/b":"secret-b"}`, out.String())
}

func TestExportYAMLScalarWritesKeyedMap(t *testing.T) {
	records := []Record{{Path: "/app/a", Value: "secret-a"}, {Path: "/app/b", Value: "secret-b"}}
	var out bytes.Buffer

	err := ExportYAMLScalar(&out, records, "value", "name")

	require.NoError(t, err)
	assert.Contains(t, out.String(), "/app/a: secret-a")
	assert.Contains(t, out.String(), "/app/b: secret-b")
}
