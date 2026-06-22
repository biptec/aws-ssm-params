package filter

import (
	"fmt"
	"slices"
	"strings"
)

type globMatcher struct {
	pattern string
}

type matchContext struct {
	pattern string
	value   string
}

type repeatState struct {
	position int
	count    int
}

// Compile builds a matcher for the CLI extglob language.
func Compile(pattern string) (Matcher, error) {
	if pattern == "" {
		return nil, fmt.Errorf("empty pattern")
	}
	context := matchContext{pattern: pattern}
	if err := context.validatePattern(); err != nil {
		return nil, err
	}
	return globMatcher{pattern: pattern}, nil
}

func (matcher globMatcher) Match(value string) bool {
	context := matchContext{pattern: matcher.pattern, value: value}
	for _, end := range context.match(0, 0) {
		if end == len(value) {
			return true
		}
	}
	return false
}

func (context matchContext) validatePattern() error {
	for i := 0; i < len(context.pattern); i++ {
		if context.pattern[i] == '\\' {
			i++
			continue
		}
		if context.isExtglobStart(i) {
			end, err := context.findGroupEnd(i + 1)
			if err != nil {
				return err
			}
			i = end
		}
	}
	return nil
}

func (context matchContext) match(patternIndex, valueIndex int) []int {
	if patternIndex == len(context.pattern) {
		return []int{valueIndex}
	}
	if valueIndex > len(context.value) {
		return nil
	}

	if context.isGlobstar(patternIndex) {
		return context.matchRestAfterGlobstar(patternIndex+2, valueIndex)
	}

	char := context.pattern[patternIndex]
	if char == '\\' {
		return context.matchEscaped(patternIndex, valueIndex)
	}
	if char == '*' {
		return context.matchRestAfterSegmentStar(patternIndex+1, valueIndex)
	}
	if char == '?' && !context.isExtglobStart(patternIndex) {
		return context.matchSingleNonSlash(patternIndex+1, valueIndex)
	}
	if char == '[' {
		return context.matchCharacterClass(patternIndex, valueIndex)
	}
	if context.isExtglobStart(patternIndex) {
		return context.matchExtglob(patternIndex, valueIndex)
	}
	return context.matchLiteral(patternIndex, valueIndex)
}

func (context matchContext) matchEscaped(patternIndex, valueIndex int) []int {
	literal := byte('\\')
	nextPatternIndex := patternIndex + 1
	if nextPatternIndex < len(context.pattern) {
		literal = context.pattern[nextPatternIndex]
		nextPatternIndex++
	}
	if valueIndex >= len(context.value) || context.value[valueIndex] != literal {
		return nil
	}
	return context.match(nextPatternIndex, valueIndex+1)
}

func (context matchContext) matchLiteral(patternIndex, valueIndex int) []int {
	if valueIndex >= len(context.value) || context.value[valueIndex] != context.pattern[patternIndex] {
		return nil
	}
	return context.match(patternIndex+1, valueIndex+1)
}

func (context matchContext) matchSingleNonSlash(nextPatternIndex, valueIndex int) []int {
	if valueIndex >= len(context.value) || context.value[valueIndex] == '/' {
		return nil
	}
	return context.match(nextPatternIndex, valueIndex+1)
}

func (context matchContext) matchRestAfterSegmentStar(nextPatternIndex, valueIndex int) []int {
	segmentEnd := context.nextSlash(valueIndex)
	ends := make([]int, 0, segmentEnd-valueIndex+1)
	for end := valueIndex; end <= segmentEnd; end++ {
		ends = append(ends, context.match(nextPatternIndex, end)...)
	}
	return context.uniqueInts(ends)
}

func (context matchContext) matchRestAfterGlobstar(nextPatternIndex, valueIndex int) []int {
	ends := make([]int, 0, len(context.value)-valueIndex+1)
	for end := valueIndex; end <= len(context.value); end++ {
		ends = append(ends, context.match(nextPatternIndex, end)...)
	}
	return context.uniqueInts(ends)
}

func (context matchContext) matchCharacterClass(patternIndex, valueIndex int) []int {
	end := strings.IndexByte(context.pattern[patternIndex+1:], ']')
	if end < 0 {
		return context.matchLiteral(patternIndex, valueIndex)
	}
	end += patternIndex + 1
	if valueIndex >= len(context.value) || context.value[valueIndex] == '/' {
		return nil
	}
	if !context.classMatches(context.pattern[patternIndex+1:end], context.value[valueIndex]) {
		return nil
	}
	return context.match(end+1, valueIndex+1)
}

func (matchContext) classMatches(class string, value byte) bool {
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

func (context matchContext) matchExtglob(patternIndex, valueIndex int) []int {
	operator := context.pattern[patternIndex]
	openIndex := patternIndex + 1
	closeIndex, err := context.findGroupEnd(openIndex)
	if err != nil {
		return nil
	}
	alternatives := context.splitAlternatives(context.pattern[openIndex+1 : closeIndex])
	nextPatternIndex := closeIndex + 1
	positions := context.extglobPositions(operator, alternatives, valueIndex)
	ends := make([]int, 0, len(positions))
	for _, position := range positions {
		ends = append(ends, context.match(nextPatternIndex, position)...)
	}
	return context.uniqueInts(ends)
}

func (context matchContext) extglobPositions(operator byte, alternatives []string, valueIndex int) []int {
	switch operator {
	case '@':
		return context.matchAlternatives(alternatives, valueIndex)
	case '?':
		positions := make([]int, 0, len(alternatives)+1)
		positions = append(positions, valueIndex)
		positions = append(positions, context.matchAlternatives(alternatives, valueIndex)...)
		return context.uniqueInts(positions)
	case '+':
		return context.repeatAlternatives(alternatives, valueIndex, 1)
	case '*':
		return context.repeatAlternatives(alternatives, valueIndex, 0)
	case '!':
		return context.negativeAlternatives(alternatives, valueIndex)
	default:
		return nil
	}
}

func (context matchContext) matchAlternatives(alternatives []string, valueIndex int) []int {
	positions := make([]int, 0, len(alternatives))
	for _, alternative := range alternatives {
		nested := matchContext{pattern: alternative, value: context.value}
		positions = append(positions, nested.match(0, valueIndex)...)
	}
	return context.uniqueInts(positions)
}

func (context matchContext) repeatAlternatives(alternatives []string, valueIndex, minimum int) []int {
	positions := make([]int, 0, len(alternatives)+1)
	seenDepth := map[int]int{valueIndex: 0}
	queue := []repeatState{{position: valueIndex, count: 0}}
	for len(queue) > 0 {
		state := queue[0]
		queue = queue[1:]
		if state.count >= minimum {
			positions = append(positions, state.position)
		}
		for _, next := range context.matchAlternatives(alternatives, state.position) {
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
	return context.uniqueInts(positions)
}

func (context matchContext) negativeAlternatives(alternatives []string, valueIndex int) []int {
	limit := len(context.value)
	if !context.alternativesMayMatchSlash(alternatives) {
		limit = context.nextSlash(valueIndex)
	}
	positions := make([]int, 0, limit-valueIndex+1)
	for end := valueIndex; end <= limit; end++ {
		candidate := context.value[valueIndex:end]
		if !context.matchesAnyAlternativeFull(alternatives, candidate) {
			positions = append(positions, end)
		}
	}
	return context.uniqueInts(positions)
}

func (matchContext) alternativesMayMatchSlash(alternatives []string) bool {
	for _, alternative := range alternatives {
		if strings.Contains(alternative, "/") || strings.Contains(alternative, "**") {
			return true
		}
	}
	return false
}

func (matchContext) matchesAnyAlternativeFull(alternatives []string, value string) bool {
	for _, alternative := range alternatives {
		nested := matchContext{pattern: alternative, value: value}
		for _, end := range nested.match(0, 0) {
			if end == len(value) {
				return true
			}
		}
	}
	return false
}

func (context matchContext) isGlobstar(patternIndex int) bool {
	return patternIndex+1 < len(context.pattern) && context.pattern[patternIndex] == '*' && context.pattern[patternIndex+1] == '*'
}

func (context matchContext) isExtglobStart(patternIndex int) bool {
	return patternIndex+1 < len(context.pattern) && context.isExtglobOperator(context.pattern[patternIndex]) && context.pattern[patternIndex+1] == '('
}

func (matchContext) isExtglobOperator(value byte) bool {
	switch value {
	case '@', '?', '+', '*', '!':
		return true
	default:
		return false
	}
}

func (context matchContext) findGroupEnd(openIndex int) (int, error) {
	depth := 1
	for i := openIndex + 1; i < len(context.pattern); i++ {
		if context.pattern[i] == '\\' {
			i++
			continue
		}
		if context.isExtglobStart(i) {
			depth++
			i++
			continue
		}
		switch context.pattern[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i, nil
			}
		}
	}
	return 0, fmt.Errorf("unclosed extglob group in %q", context.pattern)
}

func (context matchContext) splitAlternatives(value string) []string {
	alternatives := []string{}
	start := 0
	depth := 0
	nested := matchContext{pattern: value}
	for i := 0; i < len(value); i++ {
		if value[i] == '\\' {
			i++
			continue
		}
		if nested.isExtglobStart(i) {
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

func (context matchContext) nextSlash(start int) int {
	index := strings.IndexByte(context.value[start:], '/')
	if index < 0 {
		return len(context.value)
	}
	return start + index
}

func (matchContext) uniqueInts(values []int) []int {
	if len(values) < 2 {
		return values
	}
	slices.Sort(values)
	return slices.Compact(values)
}
