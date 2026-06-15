package randomx

import (
	"encoding/base64"
	"encoding/hex"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBase64ReturnsRequestedRandomByteLength(t *testing.T) {
	value, err := Base64(16)

	require.NoError(t, err)
	decoded, err := base64.StdEncoding.DecodeString(value)
	require.NoError(t, err)
	assert.Len(t, decoded, 16)
}

func TestHexReturnsRequestedRandomByteLength(t *testing.T) {
	value, err := Hex(16)

	require.NoError(t, err)
	decoded, err := hex.DecodeString(value)
	require.NoError(t, err)
	assert.Len(t, decoded, 16)
}

func TestUUIDReturnsVersion4UUID(t *testing.T) {
	value, err := UUID()

	require.NoError(t, err)
	assert.Regexp(t, regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`), value)
}
