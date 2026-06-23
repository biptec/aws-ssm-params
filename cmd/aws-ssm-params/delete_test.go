package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/biptec/aws-ssm-params/internal/textio"
)

func TestDeleteOptionsParseMappingsAndSafetyFlags(t *testing.T) {
	flags := append(globalFlags(), deleteCLICommand().Flags...)
	cmd := testParsedCommand(t, flags, []string{
		"--" + flagRegion, "eu-north-1",
		"--" + deleteFlagFormat, "json",
		"--" + deleteFlagKeyField, textio.FieldName,
		"--" + deleteFlagMapField, textio.FieldName + ":title",
		"--" + deleteFlagMapField, textio.FieldRegion + ":area",
		"--" + deleteFlagBasePath, "/base",
		"--" + deleteFlagNoConfirm,
		"--" + deleteFlagDryRun,
	})

	options, err := deleteOptionsFromCLI(context.Background(), cmd)

	require.NoError(t, err)
	assert.Equal(t, textio.FormatJSON, options.Format)
	assert.Equal(t, textio.FieldName, options.KeyField)
	assert.Equal(t, textio.FieldMappings{
		{AWSName: textio.FieldName, FileName: "title"},
		{AWSName: textio.FieldRegion, FileName: "area"},
	}, options.FieldMappings)
	assert.Equal(t, "/base", string(options.BasePath))
	assert.True(t, options.NoConfirm)
	assert.True(t, options.DryRun)
}

func TestDeleteRejectsUnsupportedMappedField(t *testing.T) {
	flags := append(globalFlags(), deleteCLICommand().Flags...)
	cmd := testParsedCommand(t, flags, []string{
		"--" + deleteFlagMapField, textio.FieldValue + ":secret",
	})

	_, err := deleteOptionsFromCLI(context.Background(), cmd)

	require.Error(t, err)
	assert.ErrorContains(t, err, "supports only name and region")
}

func TestDeleteRejectsUnsupportedKeyField(t *testing.T) {
	flags := append(globalFlags(), deleteCLICommand().Flags...)
	cmd := testParsedCommand(t, flags, []string{
		"--" + deleteFlagKeyField, textio.FieldValue,
	})

	_, err := deleteOptionsFromCLI(context.Background(), cmd)

	require.Error(t, err)
	assert.ErrorContains(t, err, "use name or region")
}
