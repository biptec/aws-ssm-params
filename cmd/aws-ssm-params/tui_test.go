package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadTUIImportFromPipedStdin(t *testing.T) {
	oldStdin := os.Stdin
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { os.Stdin = oldStdin })

	os.Stdin = reader

	input := `[{"/app/from-stdin":{"value":"secret"}}]`
	_, err = writer.WriteString(input)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	data, useInputTTY, err := loadTUIImportFromStdin()

	require.NoError(t, err)
	assert.True(t, useInputTTY)
	assert.Equal(t, input, string(data))
}
