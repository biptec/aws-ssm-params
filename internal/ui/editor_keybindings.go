package ui

type editorKeybindingsComponent struct {
	model model
}

func (component editorKeybindingsComponent) usesViEditMode() bool {
	m := component.model
	return m.keymapStyle() == keymapVi && isEditableTextField(m.editField)
}

func (component editorKeybindingsComponent) updateEmacsTextFieldKey(key string) (model, bool) {
	m := component.model
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

func (component editorKeybindingsComponent) updateViTextFieldNormal(key string) (model, bool) {
	m := component.model
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

func (component *editorKeybindingsComponent) handlePendingEditSequence(key string) (handled, consumed bool) {
	m := &component.model
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
