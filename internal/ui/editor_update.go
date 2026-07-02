package ui

import tea "github.com/charmbracelet/bubbletea"

type editorUpdateComponent struct {
	model model
}

func (component editorUpdateComponent) updateEditorPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	key := msg.String()

	if m.editorButtonsFocused {
		if isPrimaryActionMsg(msg) {
			return m.saveValue(m.textArea.Value())
		}

		switch {
		case isHelpKeyMsg(msg):
			m.openPopupShortcuts(screenTextArea, popupEditor)
			return m, nil
		case isCancelKeyMsg(msg):
			return m.requestEditorBack()
		case isEnterKeyMsg(msg):
			if m.editorButtonCursor == importActionCancel {
				return m.requestEditorBack()
			}

			return m.saveValue(m.textArea.Value())
		}

		switch {
		case isForwardTabKeyString(key):
			if m.editorButtonCursor == importActionPrimary {
				m.editorButtonCursor = importActionCancel
			} else {
				m.clearEditorButtonFocus()
				m = m.focusEditField(m.editFieldOrder()[0])
			}

			return m, nil
		case isBackwardTabKeyString(key):
			if m.editorButtonCursor == importActionCancel {
				m.editorButtonCursor = importActionPrimary
			} else {
				m.clearEditorButtonFocus()
				fields := m.editFieldOrder()
				m = m.focusEditField(fields[len(fields)-1])
			}

			return m, nil
		case isLeftKeyString(key):
			m.editorButtonCursor = importActionPrimary
			return m, nil
		case isRightKeyString(key):
			m.editorButtonCursor = importActionCancel
			return m, nil
		}

		if action, ok := m.interpretNavigationKey(key); ok {
			switch action {
			case navPrevious:
				m.clearEditorButtonFocus()
			case navFirst:
				m.clearEditorButtonFocus()
				m = m.focusEditField(m.editFieldOrder()[0])
			case navLast:
				m.editorButtonCursor = importActionCancel
			case navNone, navNext, navPageUp, navPageDown:
			}

			return m, nil
		}

		return m, nil
	}

	switch {
	case isForwardTabKeyString(key):
		m.moveEditorPopupTabFocus(false)
		return m, nil
	case isBackwardTabKeyString(key):
		m.moveEditorPopupTabFocus(true)
		return m, nil
	}

	return component.updateTextArea(msg)
}

func (m *model) moveEditorPopupTabFocus(reverse bool) {
	fields := m.editFieldOrder()

	idx := indexOfEditField(fields, m.editField)
	if idx < 0 {
		idx = 0
	}

	if reverse {
		if idx == 0 {
			m.focusEditorButton(importActionCancel)
			return
		}

		*m = m.focusEditField(fields[idx-1])

		return
	}

	if idx >= len(fields)-1 {
		m.focusEditorButton(importActionPrimary)
		return
	}

	*m = m.focusEditField(fields[idx+1])
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

	if isLineNumbersKeyMsg(msg) && isExpandableEditField(m.editField) {
		m.showGutters = !m.showGutters
		return m, nil
	}

	if m.keymapStyle() == keymapVi && isEditableTextField(m.editField) {
		if isHelpKeyMsg(msg) {
			m.openEditorPopupShortcuts()
			return m, nil
		}

		if m.viInsertMode {
			if isViNormalModeKeyMsg(msg) {
				m.viInsertMode = false
				m.pendingKeySequence = ""

				return m, nil
			}
		} else {
			if action, ok := m.editorNavigationAction(key); ok {
				allowFromExpanded := action == navPrevious && isBackwardTabKeyString(key) ||
					action == navNext && isForwardTabKeyString(key)
				if updated, cmd, handled := moveFocusWithNavigation(action, allowFromExpanded); handled {
					return updated, cmd
				}
			}

			if isPrimaryActionMsg(msg) {
				resetFileConfirmation()
				return m.saveValue(m.textArea.Value())
			}

			switch {
			case isCancelKeyMsg(msg):
				return m.requestEditorBack()
			case isEnterKeyMsg(msg):
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
			case isFocusedFieldActionsKeyMsg(msg):
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

	switch {
	case isHelpKeyMsg(msg):
		m.openEditorPopupShortcuts()
		return m, nil
	case isCancelKeyMsg(msg):
		if isPrintableCancelKeyMsg(msg) && m.shouldTypePrintableQInEditField() {
			break
		}

		return m.requestEditorBack()
	}

	if action, ok := m.editorNavigationAction(key); ok {
		allowFromExpanded := action == navPrevious && isBackwardTabKeyString(key) ||
			action == navNext && isForwardTabKeyString(key)
		if updated, cmd, handled := moveFocusWithNavigation(action, allowFromExpanded); handled {
			return updated, cmd
		}
	}

	if isPrimaryActionMsg(msg) {
		resetFileConfirmation()
		return m.saveValue(m.textArea.Value())
	}

	switch {
	case isEnterKeyMsg(msg):
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
	case isFocusedFieldActionsKeyMsg(msg):
		resetFileConfirmation()
		m.openActionsPopupForFocusedField()

		return m, nil
	case bindingMatchesString(emacsTextKillLineShortcut, key):
		if isMultilineEditField(m.editField) && m.keymapStyle() != keymapEmacs {
			return m, nil
		}
	case isReservedMultilineEditKeyMsg(msg):
		if isMultilineEditField(m.editField) {
			return m, nil
		}
	case isDeleteBackwardKeyMsg(msg):
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
