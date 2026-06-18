// Package ui implements the interactive terminal user interface.
package ui

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/textarea"
)

func (m model) usesViEditMode() bool {
	return m.keymapStyle() == keymapVi && isEditableTextField(m.editField)
}

func (m model) updateEmacsTextFieldKey(key string) (model, bool) {
	if m.keymapStyle() != keymapEmacs || !isEditableTextField(m.editField) {
		return m, false
	}
	switch key {
	case "ctrl+f", "right":
		m.moveActiveTextCursor(1)
		return m, true
	case "ctrl+b", "left":
		m.moveActiveTextCursor(-1)
		return m, true
	case "ctrl+p", "up":
		m.moveActiveTextLine(-1)
		return m, true
	case "ctrl+n", "down":
		m.moveActiveTextLine(1)
		return m, true
	case "ctrl+a", "home":
		m.activeTextLineStart()
		return m, true
	case "ctrl+e", "end":
		m.activeTextLineEnd()
		return m, true
	case "alt+f":
		m.activeTextWordForward()
		return m, true
	case "alt+b":
		m.activeTextWordBackward()
		return m, true
	case "alt+<":
		m.activeTextStart()
		return m, true
	case "alt+>":
		m.activeTextEnd()
		return m, true
	case "ctrl+d":
		m.activeTextDeleteChar()
		return m, true
	case "ctrl+k":
		m.activeTextDeleteToLineEnd()
		return m, true
	case "alt+d":
		m.activeTextDeleteWordForward()
		return m, true
	case "alt+backspace":
		m.activeTextDeleteWordBackward()
		return m, true
	}
	return m, false
}

func (m model) updateViTextFieldNormal(key string) (model, bool) {
	if _, consumed := (&m).handlePendingEditSequence(key); consumed {
		return m, true
	}
	switch key {
	case "i":
		m.viInsertMode = true
		m = m.focusEditField(m.editField)
		return m, true
	case "h", "left":
		m.moveActiveTextCursor(-1)
		return m, true
	case "l", "right":
		m.moveActiveTextCursor(1)
		return m, true
	case "j", "down":
		m.moveActiveTextLine(1)
		return m, true
	case "pagedown", "pgdown", "ctrl+f":
		m.moveActiveMultilinePage(1)
		return m, true
	case "pageup", "pgup", "ctrl+b":
		m.moveActiveMultilinePage(-1)
		return m, true
	case "k", "up":
		m.moveActiveTextLine(-1)
		return m, true
	case "0", "home":
		m.activeTextLineStart()
		return m, true
	case "$", "end":
		m.activeTextLineEnd()
		return m, true
	case "w":
		m.activeTextWordForward()
		return m, true
	case "b":
		m.activeTextWordBackward()
		return m, true
	case "G":
		m.activeTextEnd()
		return m, true
	case "g":
		m.pendingKeySequence = "g"
		return m, true
	case "d":
		m.pendingKeySequence = "d"
		return m, true
	case "D":
		m.activeTextDeleteToLineEnd()
		return m, true
	case "x":
		m.activeTextDeleteChar()
		return m, true
	}
	return m, false
}

func (m *model) handlePendingEditSequence(key string) (handled, consumed bool) {
	if m.pendingKeySequence == "" {
		return false, false
	}
	pending := m.pendingKeySequence
	m.pendingKeySequence = ""
	switch pending + key {
	case "gg":
		m.activeTextStart()
		return true, true
	case "dw":
		m.activeTextDeleteWordForward()
		return true, true
	case "db":
		m.activeTextDeleteWordBackward()
		return true, true
	default:
		return false, true
	}
}

func (m *model) moveActiveTextCursor(delta int) {
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

func (m *model) moveActiveTextLine(delta int) {
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

func (m *model) moveActiveWrappedLine(delta int) {
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

func (m *model) activeTextCursorLineOffset() (line, offset int) {
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

func (m *model) activeTextLineStart() {
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

func (m *model) activeTextLineEnd() {
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

func (m *model) activeTextStart() { m.setActiveTextCursorAbs(0) }
func (m *model) activeTextEnd()   { m.setActiveTextCursorAbs(len([]rune(m.activeTextValue()))) }

func (m *model) activeTextWordForward() {
	value := []rune(m.activeTextValue())
	m.setActiveTextCursorAbs(wordForwardIndex(value, m.activeTextCursorAbs()))
}

func (m *model) activeTextWordBackward() {
	value := []rune(m.activeTextValue())
	m.setActiveTextCursorAbs(wordBackwardIndex(value, m.activeTextCursorAbs()))
}

func (m *model) activeTextDeleteChar() {
	value := []rune(m.activeTextValue())
	pos := m.activeTextCursorAbs()
	if pos < 0 || pos >= len(value) {
		return
	}
	value = append(value[:pos], value[pos+1:]...)
	m.setActiveTextValueAndCursor(string(value), pos)
}

func (m *model) activeTextDeleteToLineEnd() {
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

func (m *model) activeTextDeleteWordForward() {
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

func (m *model) activeTextDeleteWordBackward() {
	value := []rune(m.activeTextValue())
	pos := m.activeTextCursorAbs()
	start := wordBackwardIndex(value, pos)
	if start < 0 || start >= pos || pos > len(value) {
		return
	}
	value = append(value[:start], value[pos:]...)
	m.setActiveTextValueAndCursor(string(value), start)
}

func (m *model) activeTextValue() string {
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

func (m *model) activeTextCursorAbs() int {
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

func (m *model) setActiveTextCursorAbs(pos int) {
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

func (m *model) setActiveTextValueAndCursor(value string, pos int) {
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

func (m *model) textAreaCursorAbs() int {
	return textAreaCursorAbs(m.textArea)
}

func (m *model) descriptionAreaCursorAbs() int {
	return textAreaCursorAbs(m.editDescriptionArea)
}

func (m *model) policiesAreaCursorAbs() int {
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

func (m *model) setTextAreaCursorAbs(pos int) {
	setTextAreaAbsPosition(&m.textArea, pos)
}

func (m *model) setDescriptionAreaCursorAbs(pos int) {
	setTextAreaAbsPosition(&m.editDescriptionArea, pos)
}

func (m *model) setPoliciesAreaCursorAbs(pos int) {
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
