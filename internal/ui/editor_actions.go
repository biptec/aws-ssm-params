package ui

import (
	tea "github.com/charmbracelet/bubbletea"
)

type actionItems []actionItem

type editorActionsComponent struct {
	model model
}

func (component *editorActionsComponent) openActionsPopupForFocusedField() bool {
	m := &component.model
	switch m.editField {
	case editFieldSSMPath, editFieldRegion, editFieldType, editFieldTier, editFieldDataType, editFieldOverwrite, editFieldFilePath:
		return false
	case editFieldValue:
		m.valueActionCursor = 0
		m.fileActionField = editFieldValue
		m.pushEditorChildPopup(popupValueActions)

		return true
	case editFieldDescription:
		m.valueActionCursor = 0
		m.fileActionField = editFieldDescription
		m.pushEditorChildPopup(popupDescriptionActions)

		return true
	case editFieldPolicies:
		m.valueActionCursor = 0
		m.fileActionField = editFieldPolicies
		m.pushEditorChildPopup(popupPoliciesActions)

		return true
	default:
		return false
	}
}

func (component editorActionsComponent) updateValueActionsPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	items := valueActions()

	key := msg.String()
	choose := func(action string) (tea.Model, tea.Cmd) {
		switch action {
		case "clear":
			m.textArea.SetValue("")
			m.returnToEditorPopup()
			m.message = "Value cleared. Press Ctrl-s to save."

			return m, nil
		case "random":
			m.randomCursor = 0
			m.fileActionField = editFieldValue
			m.pushEditorChildPopup(popupRandomValue)

			return m, nil
		case "load":
			m.fileActionMode = "load"
			m.fileActionField = editFieldValue
			m.input.SetValue(m.editFileInput.Value())
			m.input.Placeholder = ""
			m.input.Focus()
			m.pushEditorChildPopup(popupFileAction)

			return m, nil
		case "write":
			m.fileActionMode = "write"
			m.fileActionField = editFieldValue
			m.input.SetValue(m.editFileInput.Value())
			m.input.Placeholder = ""
			m.input.Focus()
			m.pushEditorChildPopup(popupFileAction)

			return m, nil
		}

		return m, nil
	}

	if m.editorPopupActiveOrStack() {
		if importPrimaryActionKey(key) {
			return choose(items[min(m.valueActionCursor, len(items)-1)].value)
		}

		if m.editorButtonsFocused && importEnterKey(key) {
			if m.editorButtonCursor == importActionCancel {
				m.returnToEditorPopup()
				return m, nil
			}

			return choose(items[min(m.valueActionCursor, len(items)-1)].value)
		}

		if (&m).navigateEditorPopupButtons(key) {
			return m, nil
		}
	}

	if action, ok, consumed := (&m).handlePendingNavigationSequence(key); consumed {
		if ok {
			m.valueActionCursor = cursorFromNavigation(m.valueActionCursor, len(items), action)
		}

		return m, nil
	}

	if action, ok := m.navigationAction(key); ok {
		m.valueActionCursor = cursorFromNavigation(m.valueActionCursor, len(items), action)
		return m, nil
	}

	if m.keymapStyle() == keymapVi && key == "g" {
		m.pendingKeySequence = "g"
		return m, nil
	}
	if action, ok := items.valueByHotkey(key); ok {
		return choose(action)
	}

	switch key {
	case "ctrl+_", "ctrl+/":
		m.openPopupShortcuts(screenTextArea, popupValueActions)
	case "q", "esc", "ctrl+g":
		if m.editorPopupActiveOrStack() {
			m.returnToEditorPopup()
		} else {
			m.popPopup()
		}
	case "enter", "ctrl+j":
		if len(items) > 0 {
			return choose(items[m.valueActionCursor].value)
		}
	}

	return m, nil
}

func (component editorActionsComponent) updatePoliciesActionsPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	return component.updateTextActionsPopup(msg, editFieldPolicies, policiesActions())
}

func (component editorActionsComponent) updateDescriptionActionsPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	return component.updateTextActionsPopup(msg, editFieldDescription, descriptionActions())
}

func (component editorActionsComponent) updateTextActionsPopup(msg tea.KeyMsg, field editField, items valueActionItems) (tea.Model, tea.Cmd) {
	m := component.model

	key := msg.String()
	choose := func(action string) (tea.Model, tea.Cmd) {
		switch action {
		case "clear":
			m.clearTextActionField(field)

			return m, nil
		case "load":
			m.fileActionMode = "load"
			m.fileActionField = field
			m.input.SetValue(m.initialTextActionFilePath())
			m.input.Placeholder = ""
			m.input.Focus()
			m.pushNestedPopup(popupFileAction)

			return m, nil
		case "write":
			m.fileActionMode = "write"
			m.fileActionField = field
			m.input.SetValue(m.initialTextActionFilePath())
			m.input.Placeholder = ""
			m.input.Focus()
			m.pushNestedPopup(popupFileAction)

			return m, nil
		}

		return m, nil
	}

	if m.editorPopupActiveOrStack() {
		if importPrimaryActionKey(key) {
			return choose(items[min(m.valueActionCursor, len(items)-1)].value)
		}

		if m.editorButtonsFocused && importEnterKey(key) {
			if m.editorButtonCursor == importActionCancel {
				m.returnToEditorPopup()
				return m, nil
			}

			return choose(items[min(m.valueActionCursor, len(items)-1)].value)
		}

		if (&m).navigateEditorPopupButtons(key) {
			return m, nil
		}
	}

	if action, ok, consumed := (&m).handlePendingNavigationSequence(key); consumed {
		if ok {
			m.valueActionCursor = cursorFromNavigation(m.valueActionCursor, len(items), action)
		}

		return m, nil
	}

	if action, ok := m.navigationAction(key); ok {
		m.valueActionCursor = cursorFromNavigation(m.valueActionCursor, len(items), action)
		return m, nil
	}

	if m.keymapStyle() == keymapVi && key == "g" {
		m.pendingKeySequence = "g"
		return m, nil
	}
	if action, ok := items.valueByHotkey(key); ok {
		return choose(action)
	}

	switch key {
	case "ctrl+_", "ctrl+/":
		if field == editFieldDescription {
			m.openPopupShortcuts(screenTextArea, popupDescriptionActions)
			break
		}

			m.openPopupShortcuts(screenTextArea, popupPoliciesActions)
	case "q", "esc", "ctrl+g":
		if m.editorPopupActiveOrStack() {
			m.returnToEditorPopup()
		} else {
			m.popPopup()
		}
	case "enter", "ctrl+j":
		if len(items) > 0 {
			return choose(items[m.valueActionCursor].value)
		}
	}

	return m, nil
}

func (m *model) openImportDefaultActionsPopup(field editField) {
	m.valueActionCursor = 0
	m.fileActionField = field

	switch field {
	case editFieldPolicies:
		m.pushNestedPopup(popupPoliciesActions)
	case editFieldDescription:
		m.pushNestedPopup(popupDescriptionActions)
	case editFieldValue,
		editFieldSSMPath,
		editFieldRegion,
		editFieldType,
		editFieldTier,
		editFieldDataType,
		editFieldOverwrite,
		editFieldFilePath:
	}
}

func (m *model) clearTextActionField(field editField) {
	switch {
	case m.importDefaultsInPopupStack():
		switch field {
		case editFieldPolicies:
			m.importDefaultPolicies.SetValue("")
			m.importDefaultsCursor = 4
			m.message = "Policies cleared."
		case editFieldDescription:
			m.importDefaultDescription.SetValue("")
			m.importDefaultsCursor = 5
			m.message = "Description cleared."
		case editFieldValue,
			editFieldSSMPath,
			editFieldRegion,
			editFieldType,
			editFieldTier,
			editFieldDataType,
			editFieldOverwrite,
			editFieldFilePath:
		}

		m.returnToImportDefaultsPopup()
	default:
		switch field {
		case editFieldPolicies:
			m.editPoliciesArea.SetValue("")
			m.message = "Policies cleared. Press Ctrl-s to save."
		case editFieldDescription:
			m.editDescriptionArea.SetValue("")
			m.message = "Description cleared. Press Ctrl-s to save."
		case editFieldValue,
			editFieldSSMPath,
			editFieldRegion,
			editFieldType,
			editFieldTier,
			editFieldDataType,
			editFieldOverwrite,
			editFieldFilePath:
			return
		}

		m.returnToEditorPopup()
		*m = m.focusEditField(field)
	}
}

func (m model) initialTextActionFilePath() string {
	if m.importDefaultsInPopupStack() {
		return ""
	}

	return m.editFileInput.Value()
}

func (m model) importDefaultsInPopupStack() bool {
	if m.activePopup == popupImportDefaults {
		return true
	}

	for _, kind := range m.popupStack {
		if kind == popupImportDefaults {
			return true
		}
	}

	return false
}

func (m model) fileActionUsesButtons() bool {
	return m.editorPopupActiveOrStack() || m.importDefaultsInPopupStack()
}

func (m *model) returnToImportDefaultsPopup() {
	for m.activePopup != popupNone && m.activePopup != popupImportDefaults {
		m.popPopup()
	}

	if m.activePopup == popupImportDefaults {
		m.focusImportDefaults()
	}
}

func (component editorActionsComponent) updateFileActionPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	key := msg.String()
	if m.fileActionUsesButtons() && !m.editorButtonsFocused && importEnterKey(key) && m.fileActionMode != "random-custom" {
		return m, (&m).openPopupFileActionPicker()
	}

	finish := func(updated tea.Model, cmd tea.Cmd) (tea.Model, tea.Cmd) {
		if mm, ok := updated.(model); ok {
			if mm.errMessage == "" && mm.pendingFileWrite == fileWriteConfirmationNone {
				if mm.importDefaultsInPopupStack() {
					mm.returnToImportDefaultsPopup()
				} else if mm.editorPopupActiveOrStack() {
					mm.returnToEditorPopup()
				} else {
					mm.clearPopupStack()
				}
			} else if mm.activePopup == popupFileAction {
				mm.input.Focus()
			}

			return mm, cmd
		}

		return updated, cmd
	}
	runPrimary := func() (tea.Model, tea.Cmd) {
		m.input.Blur()

		var (
			updated tea.Model
			cmd     tea.Cmd
		)

		switch m.fileActionMode {
		case "load":
			if !m.importDefaultsInPopupStack() {
				m.editFileInput.SetValue(m.input.Value())
			}

			updated, cmd = m.loadValueFromFile()
		case "write":
			if !m.importDefaultsInPopupStack() {
				m.editFileInput.SetValue(m.input.Value())
			}

			updated, cmd = m.writeValueToFile(false, false)
		case "random-custom":
			updated, cmd = m.generateRandomValueIntoEditor("base64-custom")
		default:
			updated = m
		}

		return finish(updated, cmd)
	}

	if m.fileActionUsesButtons() {
		if importPrimaryActionKey(key) {
			return runPrimary()
		}

		if m.editorButtonsFocused && importEnterKey(key) {
			if m.editorButtonCursor == importActionCancel {
				m.input.Blur()
				m.pendingFileWrite = fileWriteConfirmationNone
				m.warningMessage = ""
				m.popPopup()
				m.editorButtonsFocused = false
				if m.activePopup == popupEditor {
					m = m.focusEditField(m.editField)
				}

				return m, nil
			}

			return runPrimary()
		}

		if (&m).navigateEditorPopupButtons(key) {
			if m.editorButtonsFocused {
				m.input.Blur()
			} else {
				m.input.Focus()
			}

			return m, nil
		}
	}

	switch key {
	case "ctrl+_", "ctrl+/":
		m.openPopupShortcuts(screenTextArea, popupFileAction)
		return m, nil
	case "q", "esc", "ctrl+g":
		m.input.Blur()
		m.pendingFileWrite = fileWriteConfirmationNone
		m.warningMessage = ""
		m.popPopup()
		m.editorButtonsFocused = false
		if m.activePopup == popupEditor {
			m = m.focusEditField(m.editField)
		}

		return m, nil
	case "y":
		if m.fileActionMode != "write" {
			break
		}

		switch m.pendingFileWrite {
		case fileWriteConfirmationNone:
		case fileWriteConfirmationSecure:
			m.pendingFileWrite = fileWriteConfirmationNone
			m.warningMessage = ""

			return finish(m.writeValueToFile(true, false))
		case fileWriteConfirmationOverwrite:
			m.pendingFileWrite = fileWriteConfirmationNone
			m.warningMessage = ""

			return finish(m.writeValueToFile(true, true))

		default:
		}
	case "enter", "ctrl+j":
		return runPrimary()
	}

	var cmd tea.Cmd

	m.input, cmd = m.input.Update(msg)

	return m, cmd
}

func (component editorActionsComponent) updateFileWriteConfirmPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	key := msg.String()
	finish := func(updated tea.Model, cmd tea.Cmd) (tea.Model, tea.Cmd) {
		if mm, ok := updated.(model); ok {
			if mm.errMessage == "" && mm.pendingFileWrite == fileWriteConfirmationNone {
				if mm.importDefaultsInPopupStack() {
					mm.returnToImportDefaultsPopup()
				} else if mm.editorPopupActiveOrStack() {
					mm.returnToEditorPopup()
				} else {
					mm.clearPopupStack()
				}
			} else if mm.activePopup == popupFileAction {
				mm.input.Focus()
			}

			return mm, cmd
		}

		return updated, cmd
	}
	confirm := func() (tea.Model, tea.Cmd) {
		switch m.pendingFileWrite {
		case fileWriteConfirmationNone:
		case fileWriteConfirmationSecure:
			m.pendingFileWrite = fileWriteConfirmationNone
			m.popPopup()

			return finish(m.writeValueToFile(true, false))
		case fileWriteConfirmationOverwrite:
			m.pendingFileWrite = fileWriteConfirmationNone
			m.popPopup()

			return finish(m.writeValueToFile(true, true))
		default:
			m.popPopup()
		}

		return m, nil
	}

	if m.editorPopupActiveOrStack() {
		if importPrimaryActionKey(key) {
			return confirm()
		}

		if m.editorButtonsFocused && importEnterKey(key) {
			if m.editorButtonCursor == importActionCancel {
				m.pendingFileWrite = fileWriteConfirmationNone
				m.warningMessage = ""
				m.popPopup()
				m.editorButtonsFocused = false

				if m.activePopup == popupFileAction {
					m.input.Focus()
				} else if m.activePopup == popupEditor {
					m = m.focusEditField(m.editField)
				}

				return m, nil
			}

			return confirm()
		}

		if (&m).navigateEditorPopupButtons(key) {
			return m, nil
		}
	}

	switch key {
	case "ctrl+_", "ctrl+/":
		m.openPopupShortcuts(screenTextArea, popupFileWriteConfirm)
		return m, nil
	case "q", "esc", "ctrl+g":
		m.pendingFileWrite = fileWriteConfirmationNone
		m.warningMessage = ""
		m.popPopup()
		m.editorButtonsFocused = false

		if m.activePopup == popupFileAction {
			m.input.Focus()
		} else if m.activePopup == popupEditor {
			m = m.focusEditField(m.editField)
		}

		return m, nil
	case "enter", "ctrl+j", "y":
		return confirm()
	}

	return m, nil
}

func (component editorActionsComponent) updateUnsavedChangesPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	key := msg.String()

	if importPrimaryActionKey(key) {
		m.discardEditorChanges()
		return m, nil
	}

	if m.editorButtonsFocused && importEnterKey(key) {
		if m.editorButtonCursor == importActionPrimary {
			m.discardEditorChanges()
			return m, nil
		}

		m.popPopup()
		m = m.focusEditField(m.editField)
		return m, nil
	}

	if (&m).navigateEditorPopupButtons(key) {
		return m, nil
	}

	switch key {
	case "ctrl+_", "ctrl/", "ctrl+/":
		m.openPopupShortcuts(screenTextArea, popupUnsavedChanges)
	case "q", "esc", "ctrl+g":
		m.popPopup()
		m = m.focusEditField(m.editField)
	case "enter", "ctrl+j", "y":
		m.discardEditorChanges()
	}

	return m, nil
}

func (component editorActionsComponent) updateRandomValuePopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	items := randomItems()

	key := msg.String()
	if m.editorPopupActiveOrStack() {
		if importPrimaryActionKey(key) {
			return m.startRandomFromPopup(items[min(m.randomCursor, len(items)-1)].value)
		}

		if m.editorButtonsFocused && importEnterKey(key) {
			if m.editorButtonCursor == importActionCancel {
				m.returnToEditorPopup()
				return m, nil
			}

			return m.startRandomFromPopup(items[min(m.randomCursor, len(items)-1)].value)
		}

		if (&m).navigateEditorPopupButtons(key) {
			return m, nil
		}
	}

	if kind, ok := items.randomKindByHotkey(key); ok {
		return m.startRandomFromPopup(kind)
	}

	if action, ok, consumed := (&m).handlePendingNavigationSequence(key); consumed {
		if ok {
			m.randomCursor = cursorFromNavigation(m.randomCursor, len(items), action)
		}

		return m, nil
	}

	if action, ok := m.navigationAction(key); ok {
		m.randomCursor = cursorFromNavigation(m.randomCursor, len(items), action)
		return m, nil
	}

	if m.keymapStyle() == keymapVi && key == "g" {
		m.pendingKeySequence = "g"
		return m, nil
	}

	switch key {
	case "ctrl+_", "ctrl+/":
		m.openPopupShortcuts(screenTextArea, popupRandomValue)
	case "q", "esc", "ctrl+g":
		if m.editorPopupActiveOrStack() {
			m.returnToEditorPopup()
		} else {
			m.popPopup()
		}
	case "enter", "ctrl+j":
		if len(items) > 0 {
			return m.startRandomFromPopup(items[m.randomCursor].value)
		}
	}

	return m, nil
}

type valueActionItem struct {
	hotkey string
	value  string
	label  string
}

type valueActionItems []valueActionItem

func (items valueActionItems) valueByHotkey(key string) (string, bool) {
	for _, item := range items {
		if item.hotkey == key {
			return item.value, true
		}
	}

	return "", false
}

func valueActions() valueActionItems {
	return valueActionItems{
		{hotkey: "c", value: "clear", label: "Clear value"},
		{hotkey: "r", value: "random", label: "Random value"},
		{hotkey: "l", value: "load", label: "Load from file"},
		{hotkey: "w", value: "write", label: "Write to file"},
	}
}

func policiesActions() valueActionItems {
	return valueActionItems{
		{hotkey: "c", value: "clear", label: "Clear policies"},
		{hotkey: "l", value: "load", label: "Load from file"},
		{hotkey: "w", value: "write", label: "Write to file"},
	}
}

func descriptionActions() valueActionItems {
	return valueActionItems{
		{hotkey: "c", value: "clear", label: "Clear description"},
		{hotkey: "l", value: "load", label: "Load from file"},
		{hotkey: "w", value: "write", label: "Write to file"},
	}
}

func randomPopupHotkey(kind string) string {
	switch kind {
	case "base64-32":
		return "b"
	case "hex-32":
		return "x"
	case "uuid":
		return "u"
	case "base64-custom":
		return "c"
	default:
		return ""
	}
}

func (items actionItems) randomKindByHotkey(key string) (string, bool) {
	for _, item := range items {
		if randomPopupHotkey(item.value) == key {
			return item.value, true
		}
	}

	return "", false
}
