package ui

type editorKeybindingsComponent struct {
	model model
}

func (component editorKeybindingsComponent) updateEmacsTextFieldKey(key string) (model, bool) {
	m := component.model
	if m.keymapStyle() != keymapEmacs || !isEditableTextField(m.editField) {
		return m, false
	}

	action, ok := newKeymap(m).emacsTextEditAction(key)
	if !ok {
		return m, false
	}

	if (action == textEditPageUp || action == textEditPageDown) && !isMultilineEditField(m.editField) {
		return m, false
	}

	m.applyTextEditAction(action)

	return m, true
}

func (component editorKeybindingsComponent) updateViTextFieldNormal(key string) (model, bool) {
	m := component.model
	if _, consumed := (&m).handlePendingEditSequence(key); consumed {
		return m, true
	}

	action, ok := newKeymap(m).viTextEditAction(key)
	if !ok {
		return m, false
	}

	if action == textEditEnterInsertMode {
		m.viInsertMode = true
		m = m.focusEditField(m.editField)

		return m, true
	}

	if action == textEditPendingStart {
		m.pendingKeySequence = firstBindingKey(viTextStartPrefixShortcut)

		return m, true
	}

	if action == textEditPendingDelete {
		m.pendingKeySequence = firstBindingKey(viTextDeletePrefixShortcut)
		return m, true
	}

	m.applyTextEditAction(action)

	return m, true
}

func (component *editorKeybindingsComponent) handlePendingEditSequence(key string) (handled, consumed bool) {
	m := &component.model
	if m.pendingKeySequence == "" {
		return false, false
	}

	pending := m.pendingKeySequence
	m.pendingKeySequence = ""

	action, handled, consumed := newKeymap(*m).resolvePendingTextEditSequence(pending, key)
	if handled {
		m.applyTextEditAction(action)

		return true, true
	}

	return false, consumed
}

func (m *model) applyTextEditAction(action textEditAction) {
	switch action {
	case textEditForwardChar:
		m.moveActiveTextCursor(1)
	case textEditBackwardChar:
		m.moveActiveTextCursor(-1)
	case textEditPreviousLine:
		m.moveActiveTextLine(-1)
	case textEditNextLine:
		m.moveActiveTextLine(1)
	case textEditLineStart:
		m.activeTextLineStart()
	case textEditLineEnd:
		m.activeTextLineEnd()
	case textEditWordForward:
		m.activeTextWordForward()
	case textEditWordBackward:
		m.activeTextWordBackward()
	case textEditTextStart:
		m.activeTextStart()
	case textEditTextEnd:
		m.activeTextEnd()
	case textEditPageUp:
		m.moveActiveMultilinePage(-1)
	case textEditPageDown:
		m.moveActiveMultilinePage(1)
	case textEditDeleteChar:
		m.activeTextDeleteChar()
	case textEditDeleteToLineEnd:
		m.activeTextDeleteToLineEnd()
	case textEditDeleteWordForward:
		m.activeTextDeleteWordForward()
	case textEditDeleteWordBackward:
		m.activeTextDeleteWordBackward()
	case textEditNone, textEditEnterInsertMode, textEditPendingStart, textEditPendingDelete:
	}
}
