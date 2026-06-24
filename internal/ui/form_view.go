package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
)

// formTextInputFieldLine renders a labelled one-line input for form-like screens.
// It is shared by the editor and import popups so field sizing, labels, and input
// cursor behavior stay consistent across the TUI.
func (m model) formTextInputFieldLine(name string, input *textinput.Model, labelWidth, innerWidth int) string {
	labelText := padMin(name+":", labelWidth+1)
	// Bubbles textinput renders the focused cursor as one visible cell in addition to
	// its configured width. Reserve that extra cell so the final styled line does not
	// overflow the box and lose ANSI styling during truncation.
	available := innerWidth - lipgloss.Width(labelText) - 2
	input.Width = max(1, available)
	// Width changes affect the textinput horizontal viewport. Re-run the
	// component's overflow calculation so a long focused value expands to the new
	// available width instead of keeping the previous, narrower viewport.
	input.SetCursor(input.Position())

	return m.fieldLine(name, input.View(), labelWidth)
}

// formOptionValue renders the value side of a selector row.
// The trailing chevron is the same focus affordance used by editor selector fields.
func (m model) formOptionValue(focused bool, value string) string {
	renderedValue := m.value(value)
	if !focused {
		return renderedValue
	}

	return renderedValue + " " + m.focusMarker("<")
}

// formSingleLineAreaView renders a multiline textarea as a compact one-line field.
// Expanded multiline editing is still owned by the editor; import popups use the
// same compact rendering so textarea fields do not invent another visual style.
func (m model) formSingleLineAreaView(area *textarea.Model, focused bool, labelWidth, innerWidth int) string {
	labelText := padMin("", labelWidth+1)
	width := max(1, innerWidth-lipgloss.Width(labelText)-3)
	value := strings.ReplaceAll(area.Value(), "\n", " ")

	if !focused {
		return m.value(truncateStyled(value, width))
	}

	_, offset := textAreaCursorLineOffset(area)

	return m.value(m.inputValueWithCursor(value, offset, width))
}

// formMultilineAreaLines renders the expanded form of a textarea.
// The editor and import defaults share this renderer so cursor placement,
// wrapping, and optional gutters behave the same wherever a multiline form
// field appears.
func (m model) formMultilineAreaLines(area *textarea.Model, maxRows, contentWidth int, focused bool) []string {
	maxRows = max(1, maxRows)
	contentWidth = max(1, contentWidth)

	logicalLines, segments := multilineVisualSegments(area.Value(), contentWidth)
	lineCount := max(1, len(logicalLines))
	lineNumberWidth := len(strconv.Itoa(lineCount))

	lineWidth := contentWidth
	if m.showGutters {
		lineWidth += lineNumberWidth + lipgloss.Width(" │ ")
	}

	cursorLine := min(max(0, area.Line()), lineCount-1)
	lineInfo := area.LineInfo()
	cursorOffset := min(max(0, lineInfo.StartColumn+lineInfo.ColumnOffset), len([]rune(logicalLines[cursorLine])))

	cursorVisual := 0
	if focused {
		cursorVisual = cursorVisualSegmentIndex(logicalLines, segments, cursorLine, cursorOffset, contentWidth)
	}

	type visualLine struct {
		text string
	}

	visual := make([]visualLine, 0, lineCount)

	for visualIndex, segment := range segments {
		runes := []rune(logicalLines[segment.logical])

		piece := ""
		if segment.start < segment.end {
			piece = string(runes[segment.start:segment.end])
		}

		ownsCursor := focused && visualIndex == cursorVisual
		if ownsCursor {
			piece = m.withCursorMarker(piece, cursorOffset-segment.start)
		}

		prefix := ""
		if m.showGutters {
			prefix = fmt.Sprintf("%*d │ ", lineNumberWidth, segment.logical+1)
			if segment.start > 0 {
				prefix = fmt.Sprintf("%*s | ", lineNumberWidth, "")
			}
		}

		if !m.showGutters {
			piece = rawLeftLinePrefix + piece
		}

		visual = append(visual, visualLine{text: prefix + piece})
	}

	start := 0

	if len(visual) > maxRows {
		if focused {
			start = min(max(0, cursorVisual-maxRows+1), len(visual)-maxRows)
		} else {
			start = len(visual) - maxRows
		}
	}

	end := min(len(visual), start+maxRows)

	lines := make([]string, 0, end-start)
	for _, line := range visual[start:end] {
		if !m.showGutters {
			lines = append(lines, rawLeftLinePrefix+padVisible(strings.TrimPrefix(line.text, rawLeftLinePrefix), contentWidth))
			continue
		}

		lines = append(lines, padVisible(line.text, lineWidth))
	}

	if focused {
		for len(lines) < maxRows {
			lines = append(lines, m.formEmptyMultilineAreaLine(contentWidth, lineWidth))
		}
	}

	return lines
}

func (m model) formEmptyMultilineAreaLine(contentWidth, lineWidth int) string {
	if !m.showGutters {
		return rawLeftLinePrefix + strings.Repeat(" ", max(1, contentWidth))
	}

	return strings.Repeat(" ", max(1, lineWidth))
}

type formTextareaLayoutItem struct {
	key          int
	area         *textarea.Model
	focused      bool
	contentWidth int
}

// formTextareaRowLimits divides a shared vertical budget between multiline
// fields. Every textarea starts at its real rendered height; when the content no
// longer fits, rows are removed gradually from the non-focused textareas first
// and from the focused textarea only if that is still not enough.
func formTextareaRowLimits(items []formTextareaLayoutItem, rowBudget int) map[int]int {
	limits := make(map[int]int, len(items))
	if len(items) == 0 {
		return limits
	}

	rowBudget = max(1, rowBudget)

	focusedIndex := -1
	actualRows := make([]int, len(items))

	for i, item := range items {
		actualRows[i] = formTextareaVisualLineCount(item.area, item.contentWidth)
		if item.focused {
			focusedIndex = i
			continue
		}
	}

	totalRows := 0
	for _, rows := range actualRows {
		totalRows += rows
	}

	if totalRows <= rowBudget {
		for i, item := range items {
			limits[item.key] = actualRows[i]
		}

		return limits
	}

	for i, item := range items {
		limits[item.key] = actualRows[i]
	}

	overflow := totalRows - rowBudget
	for overflow > 0 {
		candidateIndex := formTextareaShrinkCandidate(items, limits, focusedIndex)
		if candidateIndex < 0 {
			break
		}

		limits[items[candidateIndex].key]--
		overflow--
	}

	return limits
}

func formTextareaShrinkCandidate(items []formTextareaLayoutItem, limits map[int]int, focusedIndex int) int {
	candidateIndex := -1
	candidateRows := 1

	for i, item := range items {
		if i == focusedIndex {
			continue
		}

		rows := limits[item.key]
		if rows > candidateRows {
			candidateIndex = i
			candidateRows = rows
		}
	}

	if candidateIndex >= 0 {
		return candidateIndex
	}

	for i, item := range items {
		rows := limits[item.key]
		if rows > candidateRows {
			candidateIndex = i
			candidateRows = rows
		}
	}

	return candidateIndex
}

func formTextareaVisualLineCount(area *textarea.Model, contentWidth int) int {
	if area == nil {
		return 1
	}

	_, segments := multilineVisualSegments(area.Value(), contentWidth)

	return max(1, len(segments))
}

func formTextareaLogicalContentWidth(area *textarea.Model, minWidth, maxWidth int) int {
	if area == nil {
		return max(1, min(minWidth, maxWidth))
	}

	width := minWidth
	for _, line := range strings.Split(area.Value(), "\n") {
		width = max(width, lipgloss.Width(line)+1)
	}

	return max(1, min(width, maxWidth))
}

func formTextareaGutterWidth(area *textarea.Model) int {
	lineCount := 1
	if area != nil {
		lineCount = max(1, len(strings.Split(area.Value(), "\n")))
	}

	return len(strconv.Itoa(lineCount)) + lipgloss.Width(" │ ")
}

func (m model) multilineContentWidth() int {
	if !m.showGutters {
		return max(8, m.boxInnerWidth()-2)
	}

	lineNumberWidth := 4
	prefixWidth := lineNumberWidth + lipgloss.Width(" │ ")

	return max(8, m.boxInnerWidth()-prefixWidth-2)
}

func (m model) withCursorMarker(line string, offset int) string {
	runes := []rune(line)

	offset = min(max(0, offset), len(runes))
	if offset == len(runes) {
		if m.opts.NoColor {
			return string(runes) + "█"
		}

		return string(runes) + cursorStyle.Render(" ")
	}

	if m.opts.NoColor {
		return string(runes[:offset]) + "█" + string(runes[offset+1:])
	}

	return string(runes[:offset]) + cursorStyle.Render(string(runes[offset])) + string(runes[offset+1:])
}

// formInputValue renders an unlabelled text input value, for compact input pairs
// such as the import map-path rows.
func (m model) formInputValue(input *textinput.Model, width int) string {
	width = max(1, width)
	if input.Focused() {
		return padVisible(m.inputValueWithCursor(input.Value(), input.Position(), width), width)
	}

	value := input.Value()
	if value == "" {
		return strings.Repeat(" ", width)
	}

	return padVisible(m.value(truncateInline(value, width)), width)
}
