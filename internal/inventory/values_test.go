package inventory

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScanValuesFileDiscoversAppSecretsExternalTLSAndBackups(t *testing.T) {
	file := filepath.Join(t.TempDir(), "values.yaml")
	require.NoError(t, os.WriteFile(file, []byte(`
app:
  name: demo-api
global:
  environment: prod
secrets:
  enabled: true
  provider: aws-ssm-params
  prefix: /custom
  org: org
  name: api
  keys:
    - DB_PASSWORD
    - API_KEY
components:
  web:
    enabled: true
    ingress:
      enabled: true
      tls:
        enabled: true
        mode: externalSecret
        secretName: web-tls
        externalSecret:
          certKey: /custom/prod/tls/web.example.com/tls.crt
          keyKey: /custom/prod/tls/web.example.com/tls.key
        backupToSsm:
          enabled: true
          prefix: /backup
          domain: backup.example.com
  admin:
    enabled: true
    ingress:
      enabled: true
      tls:
        enabled: true
        secretName: admin-tls
        backupToSsm:
          enabled: true
          domains:
            - admin.example.com
            - admin-alt.example.com
`), 0o600))

	items, err := scanValuesFile(file, "fallback")

	require.NoError(t, err)

	paths := itemPathsForTest(items)
	assert.Contains(t, paths, "/custom/prod/org/api/DB_PASSWORD")
	assert.Contains(t, paths, "/custom/prod/org/api/API_KEY")
	assert.Contains(t, paths, "/custom/prod/tls/web.example.com/tls.crt")
	assert.Contains(t, paths, "/custom/prod/tls/web.example.com/tls.key")
	assert.Contains(t, paths, "/backup/prod/tls/backup.example.com/tls.crt")
	assert.Contains(t, paths, "/backup/prod/tls/backup.example.com/tls.key")
	assert.Contains(t, paths, "/app-infra/prod/tls/admin.example.com/tls.crt")
	assert.Contains(t, paths, "/app-infra/prod/tls/admin-alt.example.com/tls.key")
}

func TestParseValuesUsesFallbacksForSecretEnvironment(t *testing.T) {
	values := parseValues(`
global:
  environment: stage
secrets:
  enabled: yes
  provider: aws-ssm-params
  org: org
  name: app
  keys:
    - PASSWORD
`)

	assert.Equal(t, "stage", values.GlobalEnvironment)
	assert.True(t, values.SecretsEnabled)
	assert.Equal(t, []string{"PASSWORD"}, values.SecretsKeys)
}

func TestJoinSSMNormalizesPathFragments(t *testing.T) {
	assert.Equal(t, "/app/prod/api/password", joinSSM("/app/", "/prod", "api", "password"))
	assert.Equal(t, "/app/password", joinSSM("", "/app", "", "password"))
}

func itemPathsForTest(items []Item) []string {
	paths := make([]string, 0, len(items))
	for _, item := range items {
		paths = append(paths, item.Path)
	}

	return paths
}
