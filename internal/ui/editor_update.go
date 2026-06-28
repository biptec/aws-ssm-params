package ui

import (
	tea "github.com/charmbracelet/bubbletea"
)

type editorUpdateComponent struct {
	model model
}

func (component editorUpdateComponent) updateEditorPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	key := msg.String()

	if m.editorButtonsFocused {
		if isPrimaryActionKey(key) {
			return m.saveValue(m.textArea.Value())
		}

		switch key {
		case "ctrl+_", "ctrl+/":
			m.openPopupShortcuts(screenTextArea, popupEditor)
			return m, nil
		case "q", "esc", "ctrl+g":
			return m.requestEditorBack()
		case "enter", "ctrl+j":
			if m.editorButtonCursor == importActionCancel {
				return m.requestEditorBack()
			}

			return m.saveValue(m.textArea.Value())
		case "tab":
			if m.editorButtonCursor == importActionPrimary {
				m.editorButtonCursor = importActionCancel
			} else {
				m.clearEditorButtonFocus()
				m = m.focusEditField(m.editFieldOrder()[0])
			}

			return m, nil
		case "shift+tab":
			if m.editorButtonCursor == importActionCancel {
				m.editorButtonCursor = importActionPrimary
			} else {
				m.clearEditorButtonFocus()
				fields := m.editFieldOrder()
				m = m.focusEditField(fields[len(fields)-1])
			}

			return m, nil
		case "left":
			m.editorButtonCursor = importActionPrimary
			return m, nil
		case "right":
			m.editorButtonCursor = importActionCancel
			return m, nil
		}

		if action, ok := m.interpretNavigationKey(key, false); ok {
			switch action {
			case navPrevious:
				m.clearEditorButtonFocus()
			case navFirst:
				m.clearEditorButtonFocus()
				m = m.focusEditField(m.editFieldOrder()[0])
			case navLast:
				m.editorButtonCursor = importActionCancel
			}

			return m, nil
		}

		return m, nil
	}

	switch key {
	case "tab":
		if m.moveEditorPopupTabFocus(false) {
			return m, nil
		}
	case "shift+tab":
		if m.moveEditorPopupTabFocus(true) {
			return m, nil
		}
	}

	return component.updateTextArea(msg)
}

func (m *model) moveEditorPopupTabFocus(reverse bool) bool {
	fields := m.editFieldOrder()
	idx := indexOfEditField(fields, m.editField)
	if idx < 0 {
		idx = 0
	}

	if reverse {
		if idx == 0 {
			m.focusEditorButton(importActionCancel)
			return true
		}

		*m = m.focusEditField(fields[idx-1])
		return true
	}

	if idx >= len(fields)-1 {
		m.focusEditorButton(importActionPrimary)
		return true
	}

	*m = m.focusEditField(fields[idx+1])
	return true
}

// updateTextArea handles the unified edit form: editable SSM name, region/type selectors, file path, multiline value, and save/file operations.
func (component editorUpdateComponent) updateTextArea(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	resetFileConfirmation := func() {
		m.pendingFileWrite = fileWriteConfirmationNone
		m.warningMessage = ""
	}

	moveFocusWithNavigation := func(action navigationAction, allowFromExpanded bool) (tea.Model, tea.Cmd, bool) {
		if !allowFromExpanded && m.isCurrentExpandableFieldExpanded() {
			return m, nil, false
		}

		switch action {
		case navPrevious:
			resetFileConfirmation()

			updated, cmd := m.focusPreviousEditField()

			return updated, cmd, true
		case navNext:
			resetFileConfirmation()

			updated, cmd := m.focusNextEditField()

			return updated, cmd, true
		case navNone, navPageUp, navPageDown, navFirst, navLast:
			return m, nil, false
		}

		return m, nil, false
	}

	key := msg.String()
	beforeEditField := m.editField

	beforeExpandableValue := ""
	if isExpandableEditField(beforeEditField) {
		beforeExpandableValue = m.expandableFieldValue(beforeEditField)
	}

	if key == "ctrl+l" && isExpandableEditField(m.editField) {
		m.showGutters = !m.showGutters
		return m, nil
	}

	if m.keymapStyle() == keymapVi && isEditableTextField(m.editField) {
		if isHelpKey(key) {
			m.openEditorPopupShortcuts()
			return m, nil
		}

		if m.viInsertMode {
			if key == "esc" {
				m.viInsertMode = false
				m.pendingKeySequence = ""

				return m, nil
			}
		} else {
			if action, ok := m.editorNavigationAction(key); ok {
				allowFromExpanded := action == navPrevious && key == "shift+tab" || action == navNext && key == "tab"
				if updated, cmd, handled := moveFocusWithNavigation(action, allowFromExpanded); handled {
					return updated, cmd
				}
			}

			if isPrimaryActionKey(key) {
				resetFileConfirmation()
				return m.saveValue(m.textArea.Value())
			}

			switch key {
			case "q", "esc", "ctrl+g":
				return m.requestEditorBack()
			case "enter", "ctrl+j":
				resetFileConfirmation()

				if m.expandCompactFieldIfNeeded() {
					return m, nil
				}

				if m.editField == editFieldRegion {
					return m.openRegionSelect()
				}

				if m.editField == editFieldType {
					return m.startTypeSelect(screenTextArea)
				}

				if m.editField == editFieldTier {
					return m.startTierSelect(screenTextArea)
				}

				if m.editField == editFieldDataType {
					return m.startDataTypeSelect(screenTextArea)
				}

				if m.editField == editFieldOverwrite {
					return m.startOverwriteSelect(screenTextArea)
				}
			case "alt+e":
				if m.openActionsPopupForFocusedField() {
					return m, nil
				}

				return m, nil
			}

			if updated, handled := m.updateViTextFieldNormal(key); handled {
				updated.collapseExpandedFieldAfterEdit(beforeEditField, beforeExpandableValue)
				return updated, nil
			}

			return m, nil
		}
	}

	switch key {
	case "ctrl+_", "ctrl+/":
		m.openEditorPopupShortcuts()
		return m, nil
	case "q", "esc", "ctrl+g":
		if key == "q" && m.shouldTypePrintableQInEditField() {
			break
		}

		return m.requestEditorBack()
	}

	if action, ok := m.editorNavigationAction(key); ok {
		allowFromExpanded := action == navPrevious && key == "shift+tab" || action == navNext && key == "tab"
		if updated, cmd, handled := moveFocusWithNavigation(action, allowFromExpanded); handled {
			return updated, cmd
		}
	}

	if isPrimaryActionKey(key) {
		resetFileConfirmation()
		return m.saveValue(m.textArea.Value())
	}

	switch key {
	case "enter", "ctrl+j":
		resetFileConfirmation()

		if m.expandCompactFieldIfNeeded() {
			return m, nil
		}

		switch m.editField {
		case editFieldValue, editFieldDescription, editFieldPolicies, editFieldFilePath:
		case editFieldSSMPath:
			return m.focusNextEditField()
		case editFieldRegion:
			return m.openRegionSelect()
		case editFieldType:
			return m.startTypeSelect(screenTextArea)
		case editFieldTier:
			return m.startTierSelect(screenTextArea)
		case editFieldDataType:
			return m.startDataTypeSelect(screenTextArea)
		case editFieldOverwrite:
			return m.startOverwriteSelect(screenTextArea)

		default:
		}
	case "alt+e":
		resetFileConfirmation()
		m.openActionsPopupForFocusedField()

		return m, nil
	case "y":
		switch m.pendingFileWrite {
		case fileWriteConfirmationNone:
		case fileWriteConfirmationSecure:
			m.pendingFileWrite = fileWriteConfirmationNone
			m.warningMessage = ""

			return m.writeValueToFile(true, false)
		case fileWriteConfirmationOverwrite:
			m.pendingFileWrite = fileWriteConfirmationNone
			m.warningMessage = ""

			return m.writeValueToFile(true, true)

		default:
		}
	case "pagedown", "pgdown", "ctrl+v":
		if !isMultilineEditField(m.editField) {
			break
		}

		resetFileConfirmation()
		m.moveActiveMultilinePage(1)

		return m, nil
	case "alt+v", "pageup", "pgup":
		if !isMultilineEditField(m.editField) {
			break
		}

		resetFileConfirmation()
		m.moveActiveMultilinePage(-1)

		return m, nil
	case "ctrl+k":
		if isMultilineEditField(m.editField) && m.keymapStyle() != keymapEmacs {
			return m, nil
		}
	case "ctrl+w", "ctrl+r":
		if isMultilineEditField(m.editField) {
			return m, nil
		}
	case "backspace", "ctrl+h":
		if isEditableTextField(m.editField) && m.activeTextDeleteBackward() {
			m.collapseExpandedFieldAfterEdit(beforeEditField, beforeExpandableValue)
			m.pendingFileWrite = fileWriteConfirmationNone
			m.warningMessage = ""
			m.message = ""
			m.errMessage = ""

			return m, nil
		}
	}

	if updated, handled := m.updateEmacsTextFieldKey(key); handled {
		updated.collapseExpandedFieldAfterEdit(beforeEditField, beforeExpandableValue)
		return updated, nil
	}

	if m.keymapStyle() == keymapVi && m.viInsertMode && isEditableTextField(m.editField) {
		switch key {
		case "alt+f":
			m.activeTextWordForward()
			m.collapseExpandedFieldAfterEdit(beforeEditField, beforeExpandableValue)
			return m, nil
		case "alt+b":
			m.activeTextWordBackward()
			m.collapseExpandedFieldAfterEdit(beforeEditField, beforeExpandableValue)
			return m, nil
		}
	}

	var cmd tea.Cmd

	switch m.editField {
	case editFieldRegion, editFieldType, editFieldTier, editFieldDataType, editFieldOverwrite:
	case editFieldSSMPath:
		cmd = m.updateTextInput(&m.editPathInput, msg)
	case editFieldDescription:
		m.editDescriptionArea, cmd = m.editDescriptionArea.Update(msg)
	case editFieldFilePath:
		cmd = m.updateTextInput(&m.editFileInput, msg)
	case editFieldPolicies:
		m.editPoliciesArea, cmd = m.editPoliciesArea.Update(msg)
	case editFieldValue:
		m.textArea, cmd = m.textArea.Update(msg)

	default:
	}

	m.collapseExpandedFieldAfterEdit(beforeEditField, beforeExpandableValue)
	m.pendingFileWrite = fileWriteConfirmationNone
	m.warningMessage = ""
	m.message = ""
	m.errMessage = ""

	return m, cmd
}
