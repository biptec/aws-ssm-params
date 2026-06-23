package ui

import (
	"unicode"
)

func wordForwardIndex(value []rune, pos int) int {
	pos = min(max(0, pos), len(value))
	for pos < len(value) && !unicode.IsSpace(value[pos]) {
		pos++
	}

	for pos < len(value) && unicode.IsSpace(value[pos]) {
		pos++
	}

	return pos
}

func wordBackwardIndex(value []rune, pos int) int {
	pos = min(max(0, pos), len(value))
	for pos > 0 && unicode.IsSpace(value[pos-1]) {
		pos--
	}

	for pos > 0 && !unicode.IsSpace(value[pos-1]) {
		pos--
	}

	return pos
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}

	return value
}
