package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
)

type editorCursorComponent struct {
	model model
}

func (component *editorCursorComponent) moveActiveTextCursor(delta int) {
	m := &component.model
	switch m.editField {
	case editFieldRegion, editFieldType, editFieldTier, editFieldDataType, editFieldOverwrite:
	case editFieldSSMPath:
		m.editPathInput.SetCursor(m.editPathInput.Position() + delta)
	case editFieldFilePath:
		m.editFileInput.SetCursor(m.editFileInput.Position() + delta)
	case editFieldDescription:
		m.setDescriptionAreaCursorAbs(m.descriptionAreaCursorAbs() + delta)
	case editFieldPolicies:
		m.setPoliciesAreaCursorAbs(m.policiesAreaCursorAbs() + delta)
	case editFieldValue:
		m.setTextAreaCursorAbs(m.textAreaCursorAbs() + delta)
	}
}

func (component *editorCursorComponent) moveActiveTextLine(delta int) {
	m := &component.model
	if !isMultilineEditField(m.editField) {
		return
	}
	if delta == 0 {
		return
	}
	step := 1
	if delta < 0 {
		step = -1
	}
	for i := 0; i < absInt(delta); i++ {
		m.moveActiveWrappedLine(step)
	}
}

func (component *editorCursorComponent) moveActiveWrappedLine(delta int) {
	m := &component.model
	value := m.activeTextValue()
	width := m.multilineContentWidth()
	lines, segments := multilineVisualSegments(value, width)
	if len(segments) == 0 {
		return
	}
	line, offset := m.activeTextCursorLineOffset()
	currentVisual := cursorVisualSegmentIndex(lines, segments, line, offset, width)
	currentSegment := segments[currentVisual]
	visualColumn := max(0, offset-currentSegment.start)
	targetVisual := min(max(0, currentVisual+delta), len(segments)-1)
	targetSegment := segments[targetVisual]
	targetWidth := targetSegment.end - targetSegment.start
	newOffset := targetSegment.start + min(visualColumn, targetWidth)
	m.setActiveTextCursorAbs(multilineAbsPosition(lines, targetSegment.logical, newOffset))
}

func (component *editorCursorComponent) activeTextCursorLineOffset() (line, offset int) {
	m := &component.model
	switch m.editField {
	case editFieldSSMPath, editFieldRegion, editFieldType, editFieldTier, editFieldDataType, editFieldOverwrite, editFieldFilePath:
		return 0, 0
	case editFieldDescription:
		return textAreaCursorLineOffset(m.editDescriptionArea)
	case editFieldPolicies:
		return textAreaCursorLineOffset(m.editPoliciesArea)
	case editFieldValue:
		return textAreaCursorLineOffset(m.textArea)
	default:
		return 0, 0
	}
}

func textAreaCursorLineOffset(area interface {
	Value() string
	Line() int
	LineInfo() textarea.LineInfo
},
) (line, offset int) {
	lines := strings.Split(area.Value(), "\n")
	line = min(max(0, area.Line()), len(lines)-1)
	lineInfo := area.LineInfo()
	// Bubbles textarea exposes CharOffset/ColumnOffset relative to the current
	// soft-wrapped visual row. Add StartColumn to recover the logical offset
	// inside the underlying line so our custom renderer and wrapped navigation
	// can keep the cursor on the correct visual continuation row.
	offset = min(max(0, lineInfo.StartColumn+lineInfo.ColumnOffset), len([]rune(lines[line])))
	return line, offset
}

func multilineAbsPosition(lines []string, line, offset int) int {
	line = min(max(0, line), len(lines)-1)
	offset = min(max(0, offset), len([]rune(lines[line])))
	abs := 0
	for i := 0; i < line; i++ {
		abs += len([]rune(lines[i])) + 1
	}
	return abs + offset
}

func (component *editorCursorComponent) activeTextLineStart() {
	m := &component.model
	switch m.editField {
	case editFieldRegion, editFieldType, editFieldTier, editFieldDataType, editFieldOverwrite:
	case editFieldSSMPath:
		m.editPathInput.CursorStart()
	case editFieldFilePath:
		m.editFileInput.CursorStart()
	case editFieldDescription:
		m.editDescriptionArea.CursorStart()
	case editFieldPolicies:
		m.editPoliciesArea.CursorStart()
	case editFieldValue:
		m.textArea.CursorStart()
	}
}

func (component *editorCursorComponent) activeTextLineEnd() {
	m := &component.model
	switch m.editField {
	case editFieldRegion, editFieldType, editFieldTier, editFieldDataType, editFieldOverwrite:
	case editFieldSSMPath:
		m.editPathInput.CursorEnd()
	case editFieldFilePath:
		m.editFileInput.CursorEnd()
	case editFieldDescription:
		m.editDescriptionArea.CursorEnd()
	case editFieldPolicies:
		m.editPoliciesArea.CursorEnd()
	case editFieldValue:
		m.textArea.CursorEnd()
	}
}

func (component *editorCursorComponent) activeTextStart() {
	m := &component.model
	m.setActiveTextCursorAbs(0)
}

func (component *editorCursorComponent) activeTextEnd() {
	m := &component.model
	m.setActiveTextCursorAbs(len([]rune(m.activeTextValue())))
}

func (component *editorCursorComponent) activeTextWordForward() {
	m := &component.model
	value := []rune(m.activeTextValue())
	m.setActiveTextCursorAbs(wordForwardIndex(value, m.activeTextCursorAbs()))
}

func (component *editorCursorComponent) activeTextWordBackward() {
	m := &component.model
	value := []rune(m.activeTextValue())
	m.setActiveTextCursorAbs(wordBackwardIndex(value, m.activeTextCursorAbs()))
}

func (component *editorCursorComponent) activeTextDeleteChar() {
	m := &component.model
	value := []rune(m.activeTextValue())
	pos := m.activeTextCursorAbs()
	if pos < 0 || pos >= len(value) {
		return
	}
	value = append(value[:pos], value[pos+1:]...)
	m.setActiveTextValueAndCursor(string(value), pos)
}

func (component *editorCursorComponent) activeTextDeleteToLineEnd() {
	m := &component.model
	value := []rune(m.activeTextValue())
	pos := m.activeTextCursorAbs()
	if pos < 0 || pos > len(value) {
		return
	}
	end := pos
	for end < len(value) && value[end] != '\n' {
		end++
	}
	if end == pos && end < len(value) && value[end] == '\n' {
		end++
	}
	if end == pos {
		return
	}
	value = append(value[:pos], value[end:]...)
	m.setActiveTextValueAndCursor(string(value), pos)
}

func (component *editorCursorComponent) activeTextDeleteWordForward() {
	m := &component.model
	value := []rune(m.activeTextValue())
	pos := m.activeTextCursorAbs()
	end := wordForwardIndex(value, pos)
	if end <= pos && pos < len(value) {
		end = pos + 1
	}
	if pos < 0 || pos >= len(value) || end > len(value) {
		return
	}
	value = append(value[:pos], value[end:]...)
	m.setActiveTextValueAndCursor(string(value), pos)
}

func (component *editorCursorComponent) activeTextDeleteWordBackward() {
	m := &component.model
	value := []rune(m.activeTextValue())
	pos := m.activeTextCursorAbs()
	start := wordBackwardIndex(value, pos)
	if start < 0 || start >= pos || pos > len(value) {
		return
	}
	value = append(value[:start], value[pos:]...)
	m.setActiveTextValueAndCursor(string(value), start)
}

func (component *editorCursorComponent) activeTextValue() string {
	m := &component.model
	switch m.editField {
	case editFieldRegion, editFieldType, editFieldTier, editFieldDataType, editFieldOverwrite:
		return ""
	case editFieldSSMPath:
		return m.editPathInput.Value()
	case editFieldFilePath:
		return m.editFileInput.Value()
	case editFieldDescription:
		return m.editDescriptionArea.Value()
	case editFieldPolicies:
		return m.editPoliciesArea.Value()
	case editFieldValue:
		return m.textArea.Value()
	default:
		return ""
	}
}

func (component *editorCursorComponent) activeTextCursorAbs() int {
	m := &component.model
	switch m.editField {
	case editFieldRegion, editFieldType, editFieldTier, editFieldDataType, editFieldOverwrite:
		return 0
	case editFieldSSMPath:
		return m.editPathInput.Position()
	case editFieldFilePath:
		return m.editFileInput.Position()
	case editFieldDescription:
		return m.descriptionAreaCursorAbs()
	case editFieldPolicies:
		return m.policiesAreaCursorAbs()
	case editFieldValue:
		return m.textAreaCursorAbs()
	default:
		return 0
	}
}

func (component *editorCursorComponent) setActiveTextCursorAbs(pos int) {
	m := &component.model
	switch m.editField {
	case editFieldRegion, editFieldType, editFieldTier, editFieldDataType, editFieldOverwrite:
	case editFieldSSMPath:
		m.editPathInput.SetCursor(pos)
	case editFieldFilePath:
		m.editFileInput.SetCursor(pos)
	case editFieldDescription:
		m.setDescriptionAreaCursorAbs(pos)
	case editFieldPolicies:
		m.setPoliciesAreaCursorAbs(pos)
	case editFieldValue:
		m.setTextAreaCursorAbs(pos)
	}
}

func (component *editorCursorComponent) setActiveTextValueAndCursor(value string, pos int) {
	m := &component.model
	switch m.editField {
	case editFieldRegion, editFieldType, editFieldTier, editFieldDataType, editFieldOverwrite:
	case editFieldSSMPath:
		m.editPathInput.SetValue(value)
		m.editPathInput.SetCursor(pos)
	case editFieldFilePath:
		m.editFileInput.SetValue(value)
		m.editFileInput.SetCursor(pos)
	case editFieldDescription:
		m.editDescriptionArea.SetValue(value)
		m.setDescriptionAreaCursorAbs(pos)
	case editFieldPolicies:
		m.editPoliciesArea.SetValue(value)
		m.setPoliciesAreaCursorAbs(pos)
	case editFieldValue:
		m.textArea.SetValue(value)
		m.setTextAreaCursorAbs(pos)
	}
}

func (component *editorCursorComponent) textAreaCursorAbs() int {
	m := &component.model
	return textAreaCursorAbs(m.textArea)
}

func (component *editorCursorComponent) descriptionAreaCursorAbs() int {
	m := &component.model
	return textAreaCursorAbs(m.editDescriptionArea)
}

func (component *editorCursorComponent) policiesAreaCursorAbs() int {
	m := &component.model
	return textAreaCursorAbs(m.editPoliciesArea)
}

func textAreaCursorAbs(area interface {
	Value() string
	Line() int
	LineInfo() textarea.LineInfo
},
) int {
	lines := strings.Split(area.Value(), "\n")
	row := min(max(0, area.Line()), len(lines)-1)
	lineInfo := area.LineInfo()
	col := lineInfo.StartColumn + lineInfo.ColumnOffset
	abs := 0
	for i := 0; i < row; i++ {
		abs += len([]rune(lines[i])) + 1
	}
	return abs + col
}

func (component *editorCursorComponent) setTextAreaCursorAbs(pos int) {
	m := &component.model
	setTextAreaAbsPosition(&m.textArea, pos)
}

func (component *editorCursorComponent) setDescriptionAreaCursorAbs(pos int) {
	m := &component.model
	setTextAreaAbsPosition(&m.editDescriptionArea, pos)
}

func (component *editorCursorComponent) setPoliciesAreaCursorAbs(pos int) {
	m := &component.model
	setTextAreaAbsPosition(&m.editPoliciesArea, pos)
}

func setTextAreaAbsPosition(area *textarea.Model, pos int) {
	value := area.Value()
	runes := []rune(value)
	pos = min(max(0, pos), len(runes))
	lines := strings.Split(value, "\n")
	targetRow := 0
	targetOffset := pos
	for row, line := range lines {
		lineLen := len([]rune(line))
		if targetOffset <= lineLen || row == len(lines)-1 {
			targetRow = row
			targetOffset = min(targetOffset, lineLen)
			break
		}
		targetOffset -= lineLen + 1
	}

	area.SetValue(value)
	for area.Line() > 0 {
		area.CursorUp()
	}
	area.CursorStart()
	for area.Line() < targetRow {
		previousLine := area.Line()
		previousInfo := area.LineInfo()
		area.CursorDown()
		if area.Line() == previousLine && area.LineInfo() == previousInfo {
			break
		}
	}
	area.SetCursor(targetOffset)
}
