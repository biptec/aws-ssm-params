// Package natural compares human-readable strings containing numeric segments.
package natural

import (
	"strings"
	"unicode"
)

// Compare compares strings case-insensitively while treating digit runs as numbers.
func Compare(left, right string) int {
	leftRunes := []rune(strings.ToLower(strings.TrimSpace(left)))
	rightRunes := []rune(strings.ToLower(strings.TrimSpace(right)))
	i, j := 0, 0
	for i < len(leftRunes) && j < len(rightRunes) {
		if unicode.IsDigit(leftRunes[i]) && unicode.IsDigit(rightRunes[j]) {
			li, rj := i, j
			for i < len(leftRunes) && unicode.IsDigit(leftRunes[i]) {
				i++
			}
			for j < len(rightRunes) && unicode.IsDigit(rightRunes[j]) {
				j++
			}
			if cmp := compareDigitRuns(leftRunes[li:i], rightRunes[rj:j]); cmp != 0 {
				return cmp
			}
			continue
		}
		if leftRunes[i] < rightRunes[j] {
			return -1
		}
		if leftRunes[i] > rightRunes[j] {
			return 1
		}
		i++
		j++
	}
	if len(leftRunes)-i < len(rightRunes)-j {
		return -1
	}
	if len(leftRunes)-i > len(rightRunes)-j {
		return 1
	}
	return 0
}

func compareDigitRuns(left, right []rune) int {
	leftTrimmed := trimLeadingZeroes(left)
	rightTrimmed := trimLeadingZeroes(right)
	if len(leftTrimmed) < len(rightTrimmed) {
		return -1
	}
	if len(leftTrimmed) > len(rightTrimmed) {
		return 1
	}
	for i := range leftTrimmed {
		if leftTrimmed[i] < rightTrimmed[i] {
			return -1
		}
		if leftTrimmed[i] > rightTrimmed[i] {
			return 1
		}
	}
	if len(left) < len(right) {
		return -1
	}
	if len(left) > len(right) {
		return 1
	}
	return 0
}

func trimLeadingZeroes(value []rune) []rune {
	index := 0
	for index < len(value)-1 && value[index] == '0' {
		index++
	}
	return value[index:]
}
