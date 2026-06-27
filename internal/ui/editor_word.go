package ui

import (
	"unicode"
)

func wordForwardIndex(value []rune, pos int) int {
	pos = min(max(0, pos), len(value))
	for pos < len(value) && textWordRune(value[pos]) {
		pos++
	}

	for pos < len(value) && !textWordRune(value[pos]) {
		pos++
	}

	return pos
}

func wordBackwardIndex(value []rune, pos int) int {
	pos = min(max(0, pos), len(value))
	for pos > 0 && !textWordRune(value[pos-1]) {
		pos--
	}

	for pos > 0 && textWordRune(value[pos-1]) {
		pos--
	}

	return pos
}

func textWordRune(r rune) bool {
	return unicode.IsLetter(r) ||
		unicode.IsDigit(r) ||
		r == '_' ||
		r == '-' ||
		r == '.'
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}

	return value
}
