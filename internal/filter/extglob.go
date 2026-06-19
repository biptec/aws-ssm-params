package filter

import (
	"fmt"
	"slices"
	"strings"
)

type globMatcher struct{ pattern string }

// Compile builds a matcher for the CLI extglob language.
func Compile(pattern string) (Matcher, error) {
	if pattern == "" {
		return nil, fmt.Errorf("empty pattern")
	}
	if err := validatePattern(pattern); err != nil {
		return nil, err
	}
	return globMatcher{pattern: pattern}, nil
}

func (matcher globMatcher) Match(value string) bool {
	for _, end := range matchPattern(matcher.pattern, 0, value, 0) {
		if end == len(value) {
			return true
		}
	}
	return false
}

func validatePattern(pattern string) error {
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '\\' {
			i++
			continue
		}
		if i+1 < len(pattern) && isExtglobOperator(pattern[i]) && pattern[i+1] == '(' {
			end, err := findGroupEnd(pattern, i+1)
			if err != nil {
				return err
			}
			i = end
		}
	}
	return nil
}

func matchPattern(pattern string, patternIndex int, value string, valueIndex int) []int {
	if patternIndex == len(pattern) {
		return []int{valueIndex}
	}
	if valueIndex > len(value) {
		return nil
	}

	if isGlobstar(pattern, patternIndex) {
		return matchRestAfterGlobstar(pattern, patternIndex+2, value, valueIndex)
	}

	char := pattern[patternIndex]
	if char == '\\' {
		return matchEscaped(pattern, patternIndex, value, valueIndex)
	}
	if char == '*' {
		return matchRestAfterSegmentStar(pattern, patternIndex+1, value, valueIndex)
	}
	if char == '?' && !isExtglobStart(pattern, patternIndex) {
		return matchSingleNonSlash(pattern, patternIndex+1, value, valueIndex)
	}
	if char == '[' {
		return matchCharacterClass(pattern, patternIndex, value, valueIndex)
	}
	if isExtglobStart(pattern, patternIndex) {
		return matchExtglob(pattern, patternIndex, value, valueIndex)
	}
	return matchLiteral(pattern, patternIndex, value, valueIndex)
}

func matchEscaped(pattern string, patternIndex int, value string, valueIndex int) []int {
	literal := byte('\\')
	nextPatternIndex := patternIndex + 1
	if nextPatternIndex < len(pattern) {
		literal = pattern[nextPatternIndex]
		nextPatternIndex++
	}
	if valueIndex >= len(value) || value[valueIndex] != literal {
		return nil
	}
	return matchPattern(pattern, nextPatternIndex, value, valueIndex+1)
}

func matchLiteral(pattern string, patternIndex int, value string, valueIndex int) []int {
	if valueIndex >= len(value) || value[valueIndex] != pattern[patternIndex] {
		return nil
	}
	return matchPattern(pattern, patternIndex+1, value, valueIndex+1)
}

func matchSingleNonSlash(pattern string, nextPatternIndex int, value string, valueIndex int) []int {
	if valueIndex >= len(value) || value[valueIndex] == '/' {
		return nil
	}
	return matchPattern(pattern, nextPatternIndex, value, valueIndex+1)
}

func matchRestAfterSegmentStar(pattern string, nextPatternIndex int, value string, valueIndex int) []int {
	segmentEnd := nextSlash(value, valueIndex)
	ends := make([]int, 0, segmentEnd-valueIndex+1)
	for end := valueIndex; end <= segmentEnd; end++ {
		ends = append(ends, matchPattern(pattern, nextPatternIndex, value, end)...)
	}
	return uniqueInts(ends)
}

func matchRestAfterGlobstar(pattern string, nextPatternIndex int, value string, valueIndex int) []int {
	ends := make([]int, 0, len(value)-valueIndex+1)
	for end := valueIndex; end <= len(value); end++ {
		ends = append(ends, matchPattern(pattern, nextPatternIndex, value, end)...)
	}
	return uniqueInts(ends)
}

func matchCharacterClass(pattern string, patternIndex int, value string, valueIndex int) []int {
	end := strings.IndexByte(pattern[patternIndex+1:], ']')
	if end < 0 {
		return matchLiteral(pattern, patternIndex, value, valueIndex)
	}
	end += patternIndex + 1
	if valueIndex >= len(value) || value[valueIndex] == '/' {
		return nil
	}
	if !classMatches(pattern[patternIndex+1:end], value[valueIndex]) {
		return nil
	}
	return matchPattern(pattern, end+1, value, valueIndex+1)
}

func classMatches(class string, value byte) bool {
	negated := false
	if strings.HasPrefix(class, "!") || strings.HasPrefix(class, "^") {
		negated = true
		class = class[1:]
	}
	matched := false
	for i := 0; i < len(class); i++ {
		if i+2 < len(class) && class[i+1] == '-' {
			if value >= class[i] && value <= class[i+2] {
				matched = true
			}
			i += 2
			continue
		}
		if value == class[i] {
			matched = true
		}
	}
	if negated {
		return !matched
	}
	return matched
}

func matchExtglob(pattern string, patternIndex int, value string, valueIndex int) []int {
	operator := pattern[patternIndex]
	openIndex := patternIndex + 1
	closeIndex, err := findGroupEnd(pattern, openIndex)
	if err != nil {
		return nil
	}
	alternatives := splitAlternatives(pattern[openIndex+1 : closeIndex])
	nextPatternIndex := closeIndex + 1
	positions := extglobPositions(operator, alternatives, value, valueIndex)
	ends := make([]int, 0, len(positions))
	for _, position := range positions {
		ends = append(ends, matchPattern(pattern, nextPatternIndex, value, position)...)
	}
	return uniqueInts(ends)
}

func extglobPositions(operator byte, alternatives []string, value string, valueIndex int) []int {
	switch operator {
	case '@':
		return matchAlternatives(alternatives, value, valueIndex)
	case '?':
		positions := make([]int, 0, len(alternatives)+1)
		positions = append(positions, valueIndex)
		positions = append(positions, matchAlternatives(alternatives, value, valueIndex)...)
		return uniqueInts(positions)
	case '+':
		return repeatAlternatives(alternatives, value, valueIndex, 1)
	case '*':
		return repeatAlternatives(alternatives, value, valueIndex, 0)
	case '!':
		return negativeAlternatives(alternatives, value, valueIndex)
	default:
		return nil
	}
}

func matchAlternatives(alternatives []string, value string, valueIndex int) []int {
	positions := make([]int, 0, len(alternatives))
	for _, alternative := range alternatives {
		positions = append(positions, matchPattern(alternative, 0, value, valueIndex)...)
	}
	return uniqueInts(positions)
}

func repeatAlternatives(alternatives []string, value string, valueIndex, minimum int) []int {
	positions := make([]int, 0, len(alternatives)+1)
	seenDepth := map[int]int{valueIndex: 0}
	queue := []repeatState{{position: valueIndex, count: 0}}
	for len(queue) > 0 {
		state := queue[0]
		queue = queue[1:]
		if state.count >= minimum {
			positions = append(positions, state.position)
		}
		for _, next := range matchAlternatives(alternatives, value, state.position) {
			if next == state.position {
				continue
			}
			if previous, ok := seenDepth[next]; ok && previous <= state.count+1 {
				continue
			}
			seenDepth[next] = state.count + 1
			queue = append(queue, repeatState{position: next, count: state.count + 1})
		}
	}
	return uniqueInts(positions)
}

type repeatState struct {
	position int
	count    int
}

func negativeAlternatives(alternatives []string, value string, valueIndex int) []int {
	limit := len(value)
	if !alternativesMayMatchSlash(alternatives) {
		limit = nextSlash(value, valueIndex)
	}
	positions := make([]int, 0, limit-valueIndex+1)
	for end := valueIndex; end <= limit; end++ {
		candidate := value[valueIndex:end]
		if !matchesAnyAlternativeFull(alternatives, candidate) {
			positions = append(positions, end)
		}
	}
	return uniqueInts(positions)
}

func alternativesMayMatchSlash(alternatives []string) bool {
	for _, alternative := range alternatives {
		if strings.Contains(alternative, "/") || strings.Contains(alternative, "**") {
			return true
		}
	}
	return false
}

func matchesAnyAlternativeFull(alternatives []string, value string) bool {
	for _, alternative := range alternatives {
		for _, end := range matchPattern(alternative, 0, value, 0) {
			if end == len(value) {
				return true
			}
		}
	}
	return false
}

func isGlobstar(pattern string, patternIndex int) bool {
	return patternIndex+1 < len(pattern) && pattern[patternIndex] == '*' && pattern[patternIndex+1] == '*'
}

func isExtglobStart(pattern string, patternIndex int) bool {
	return patternIndex+1 < len(pattern) && isExtglobOperator(pattern[patternIndex]) && pattern[patternIndex+1] == '('
}

func isExtglobOperator(value byte) bool {
	switch value {
	case '@', '?', '+', '*', '!':
		return true
	default:
		return false
	}
}

func findGroupEnd(pattern string, openIndex int) (int, error) {
	depth := 1
	for i := openIndex + 1; i < len(pattern); i++ {
		if pattern[i] == '\\' {
			i++
			continue
		}
		if i+1 < len(pattern) && isExtglobOperator(pattern[i]) && pattern[i+1] == '(' {
			depth++
			i++
			continue
		}
		switch pattern[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i, nil
			}
		}
	}
	return 0, fmt.Errorf("unclosed extglob group in %q", pattern)
}

func splitAlternatives(value string) []string {
	alternatives := []string{}
	start := 0
	depth := 0
	for i := 0; i < len(value); i++ {
		if value[i] == '\\' {
			i++
			continue
		}
		if i+1 < len(value) && isExtglobOperator(value[i]) && value[i+1] == '(' {
			depth++
			i++
			continue
		}
		switch value[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case '|':
			if depth == 0 {
				alternatives = append(alternatives, value[start:i])
				start = i + 1
			}
		}
	}
	alternatives = append(alternatives, value[start:])
	return alternatives
}

func nextSlash(value string, start int) int {
	idx := strings.IndexByte(value[start:], '/')
	if idx < 0 {
		return len(value)
	}
	return start + idx
}

func uniqueInts(values []int) []int {
	if len(values) < 2 {
		return values
	}
	slices.Sort(values)
	return slices.Compact(values)
}
