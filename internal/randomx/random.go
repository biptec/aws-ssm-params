// Package randomx generates secret-safe random values for parameter editing.
package randomx

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"github.com/cockroachdb/errors"
)

// Base64 returns a cryptographically random base64 string based on the requested byte count.
func Base64(bytes int) (string, error) {
	data, err := randomBytes(bytes)
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(data), nil
}

// Hex returns a cryptographically random lowercase hexadecimal string based on the requested byte count.
func Hex(bytes int) (string, error) {
	data, err := randomBytes(bytes)
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(data), nil
}

// UUID generates a random RFC 4122 version 4 UUID.
// The random bytes are adjusted to set the version and variant bits before formatting.
func UUID() (string, error) {
	data, err := randomBytes(16)
	if err != nil {
		return "", err
	}

	data[6] = (data[6] & 0x0f) | 0x40
	data[8] = (data[8] & 0x3f) | 0x80

	return fmt.Sprintf("%x-%x-%x-%x-%x", data[0:4], data[4:6], data[6:8], data[8:10], data[10:]), nil
}

// randomBytes reads n bytes from crypto/rand for secret-safe random material.
func randomBytes(n int) ([]byte, error) {
	data := make([]byte, n)

	_, err := rand.Read(data)
	if err != nil {
		return nil, errors.Wrap(err, "read random bytes")
	}

	return data, nil
}
