package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/biptec/aws-ssm-params/internal/filter"
	"github.com/biptec/aws-ssm-params/internal/textio"
)

func TestRecordsExpandMissingRegionsAndPreserveExplicitRegion(t *testing.T) {
	records := Records{
		{Path: "/app/shared", Fields: textio.Fields{textio.FieldName}},
		{Path: "/app/regional", Region: "ap-south-1"},
	}

	expanded, err := records.ExpandRegions([]string{"eu-north-1", "eu-central-1"})

	require.NoError(t, err)
	assert.Equal(t, Records{
		{Path: "/app/shared", Region: "eu-north-1", Fields: textio.Fields{textio.FieldName, textio.FieldRegion}},
		{Path: "/app/shared", Region: "eu-central-1", Fields: textio.Fields{textio.FieldName, textio.FieldRegion}},
		{Path: "/app/regional", Region: "ap-south-1"},
	}, expanded)
}

func TestRecordsFilterUsesImportedFields(t *testing.T) {
	groups, err := filter.ParseGroups([]string{"name:/app/*;region:eu-*;type:SecureString"})
	require.NoError(t, err)

	records := Records{
		{Path: "/app/token", Region: "eu-north-1", Type: "SecureString"},
		{Path: "/app/public", Region: "eu-north-1", Type: "String"},
		{Path: "/other/token", Region: "eu-north-1", Type: "SecureString"},
	}

	filtered := records.Filter(groups)

	require.Len(t, filtered, 1)
	assert.Equal(t, "/app/token", filtered[0].Path)
}

func TestRecordsUniqueByIdentityKeepsSameNameInDifferentRegions(t *testing.T) {
	records := Records{
		{Path: " /app/token ", Region: " eu-north-1 "},
		{Path: "/app/token", Region: "eu-north-1"},
		{Path: "/app/token", Region: "eu-central-1"},
	}

	unique := records.UniqueByIdentity()

	assert.Equal(t, Records{
		{Path: "/app/token", Region: "eu-north-1"},
		{Path: "/app/token", Region: "eu-central-1"},
	}, unique)
}
