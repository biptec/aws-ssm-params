package tui

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadInteractiveInventoryFromPipedStdin(t *testing.T) {
	oldStdin := os.Stdin
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { os.Stdin = oldStdin })
	os.Stdin = reader

	_, err = writer.WriteString("# comment\n/app/from-stdin\n/app/second # inline comment\n")
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	items, useTTYInput, err := loadInventoryFromStdin()

	require.NoError(t, err)
	assert.True(t, useTTYInput)
	require.Len(t, items, 2)
	assert.Equal(t, "/app/from-stdin", items[0].Path)
	assert.Equal(t, "stdin", items[0].Source)
	assert.Equal(t, "/app/second", items[1].Path)
}
