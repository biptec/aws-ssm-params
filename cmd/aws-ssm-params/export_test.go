package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/biptec/aws-ssm-params/internal/textio"
)

func TestExportOptionsParseFieldsMappingsAndSort(t *testing.T) {
	flags := append(globalFlags(), exportCLICommand().Flags...)
	cmd := testParsedCommand(t, flags, []string{
		"--" + flagRegion, "eu-north-1",
		"--" + exportFlagOutputField, textio.FieldName,
		"--" + exportFlagOutputField, textio.FieldValue,
		"--" + exportFlagMapField, textio.FieldName + ":title",
		"--" + exportFlagSortBy, textio.FieldName + ":asc",
		"--" + exportFlagWithDecryption,
	})

	options, err := exportOptionsFromCLI(context.Background(), cmd)

	require.NoError(t, err)
	assert.Equal(t, textio.Fields{textio.FieldName, textio.FieldValue}, options.Fields)
	assert.Equal(t, textio.FieldMappings{{AWSName: textio.FieldName, FileName: "title"}}, options.FieldMappings)
	assert.Equal(t, []string{textio.FieldName + ":asc"}, options.SortColumns)
	assert.True(t, options.Config.WithDecryption)
}

func TestExportScalarRequiresExactlyOneOutputField(t *testing.T) {
	flags := append(globalFlags(), exportCLICommand().Flags...)
	cmd := testParsedCommand(t, flags, []string{"--" + exportFlagScalar})

	_, err := exportOptionsFromCLI(context.Background(), cmd)

	require.Error(t, err)
	assert.ErrorContains(t, err, "--"+exportFlagOutputField)
}

func TestExportRejectsKeyAndOutputFieldCollision(t *testing.T) {
	flags := append(globalFlags(), exportCLICommand().Flags...)
	cmd := testParsedCommand(t, flags, []string{
		"--" + exportFlagOutputField, textio.FieldName,
		"--" + exportFlagKeyField, textio.FieldName,
	})

	_, err := exportOptionsFromCLI(context.Background(), cmd)

	require.Error(t, err)
	assert.ErrorContains(t, err, "cannot use the same field")
}

func TestExportRejectsOutputFieldAliasSyntax(t *testing.T) {
	flags := append(globalFlags(), exportCLICommand().Flags...)
	cmd := testParsedCommand(t, flags, []string{
		"--" + exportFlagOutputField, textio.FieldValue + ":text",
	})

	_, err := exportOptionsFromCLI(context.Background(), cmd)

	require.Error(t, err)
	assert.ErrorContains(t, err, "unsupported --"+exportFlagOutputField)
}
