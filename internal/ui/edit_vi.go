package ui

import (
	"strings"
	"unicode"
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
	case "x":
		m.activeTextDeleteChar()
		return m, true
	}
	return m, false
}

func (m *model) handlePendingEditSequence(key string) (bool, bool) {
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
	case editFieldSSMPath:
		m.editPathInput.SetCursor(m.editPathInput.Position() + delta)
	case editFieldFilePath:
		m.editFileInput.SetCursor(m.editFileInput.Position() + delta)
	case editFieldValue:
		m.setTextAreaCursorAbs(m.textAreaCursorAbs() + delta)
	}
}

func (m *model) moveActiveTextLine(delta int) {
	if m.editField != editFieldValue {
		return
	}
	if delta > 0 {
		m.textArea.CursorDown()
	} else if delta < 0 {
		m.textArea.CursorUp()
	}
}

func (m *model) activeTextLineStart() {
	switch m.editField {
	case editFieldSSMPath:
		m.editPathInput.CursorStart()
	case editFieldFilePath:
		m.editFileInput.CursorStart()
	case editFieldValue:
		m.textArea.CursorStart()
	}
}

func (m *model) activeTextLineEnd() {
	switch m.editField {
	case editFieldSSMPath:
		m.editPathInput.CursorEnd()
	case editFieldFilePath:
		m.editFileInput.CursorEnd()
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
	case editFieldSSMPath:
		return m.editPathInput.Value()
	case editFieldFilePath:
		return m.editFileInput.Value()
	case editFieldValue:
		return m.textArea.Value()
	default:
		return ""
	}
}

func (m *model) activeTextCursorAbs() int {
	switch m.editField {
	case editFieldSSMPath:
		return m.editPathInput.Position()
	case editFieldFilePath:
		return m.editFileInput.Position()
	case editFieldValue:
		return m.textAreaCursorAbs()
	default:
		return 0
	}
}

func (m *model) setActiveTextCursorAbs(pos int) {
	switch m.editField {
	case editFieldSSMPath:
		m.editPathInput.SetCursor(pos)
	case editFieldFilePath:
		m.editFileInput.SetCursor(pos)
	case editFieldValue:
		m.setTextAreaCursorAbs(pos)
	}
}

func (m *model) setActiveTextValueAndCursor(value string, pos int) {
	switch m.editField {
	case editFieldSSMPath:
		m.editPathInput.SetValue(value)
		m.editPathInput.SetCursor(pos)
	case editFieldFilePath:
		m.editFileInput.SetValue(value)
		m.editFileInput.SetCursor(pos)
	case editFieldValue:
		m.textArea.SetValue(value)
		m.setTextAreaCursorAbs(pos)
	}
}

func (m *model) textAreaCursorAbs() int {
	lines := strings.Split(m.textArea.Value(), "\n")
	row := min(max(0, m.textArea.Line()), len(lines)-1)
	col := m.textArea.LineInfo().CharOffset
	abs := 0
	for i := 0; i < row; i++ {
		abs += len([]rune(lines[i])) + 1
	}
	return abs + col
}

func (m *model) setTextAreaCursorAbs(pos int) {
	value := m.textArea.Value()
	runes := []rune(value)
	pos = min(max(0, pos), len(runes))
	m.textArea.SetValue(value)
	for m.textArea.Line() > 0 {
		m.textArea.CursorUp()
	}
	m.textArea.CursorStart()
	lines := strings.Split(value, "\n")
	remaining := pos
	for row, line := range lines {
		lineLen := len([]rune(line))
		if remaining <= lineLen || row == len(lines)-1 {
			m.textArea.SetCursor(remaining)
			return
		}
		remaining -= lineLen + 1
		m.textArea.CursorDown()
	}
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
