package ui

import (
	tea "github.com/charmbracelet/bubbletea"
)

type editorActionsComponent struct {
	model model
}

func (component *editorActionsComponent) openActionsPopupForFocusedField() bool {
	m := &component.model
	switch m.editField {
	case editFieldSSMPath, editFieldRegion, editFieldType, editFieldTier, editFieldDataType, editFieldOverwrite, editFieldDescription, editFieldFilePath:
		return false
	case editFieldValue:
		m.valueActionCursor = 0
		m.fileActionField = editFieldValue
		m.pushPopup(popupValueActions)
		return true
	case editFieldPolicies:
		m.valueActionCursor = 0
		m.fileActionField = editFieldPolicies
		m.pushPopup(popupPoliciesActions)
		return true
	default:
		return false
	}
}

func (component editorActionsComponent) updateValueActionsPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	items := valueActionItems()
	key := msg.String()
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
	choose := func(action string) (tea.Model, tea.Cmd) {
		switch action {
		case "clear":
			m.textArea.SetValue("")
			m.clearPopupStack()
			m.message = "Value cleared. Press Ctrl-s to save."
			return m, nil
		case "random":
			m.randomCursor = 0
			m.fileActionField = editFieldValue
			m.pushPopup(popupRandomValue)
			return m, nil
		case "load":
			m.fileActionMode = "load"
			m.fileActionField = editFieldValue
			m.input.SetValue(m.editFileInput.Value())
			m.input.Placeholder = ""
			m.input.Focus()
			m.pushPopup(popupFileAction)
			return m, nil
		case "write":
			m.fileActionMode = "write"
			m.fileActionField = editFieldValue
			m.input.SetValue(m.editFileInput.Value())
			m.input.Placeholder = ""
			m.input.Focus()
			m.pushPopup(popupFileAction)
			return m, nil
		}
		return m, nil
	}
	if action, ok := valueActionByHotkey(key); ok {
		return choose(action)
	}
	switch key {
	case "ctrl+_", "ctrl+/":
		m.openPopupShortcuts(screenTextArea, popupValueActions)
	case "q", "esc", "ctrl+g":
		m.popPopup()
	case "enter", "ctrl+j":
		if len(items) > 0 {
			return choose(items[m.valueActionCursor].value)
		}
	}
	return m, nil
}

func (component editorActionsComponent) updatePoliciesActionsPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	items := policiesActionItems()
	key := msg.String()
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
	choose := func(action string) (tea.Model, tea.Cmd) {
		switch action {
		case "clear":
			m.editPoliciesArea.SetValue("")
			m.clearPopupStack()
			m.message = "Policies cleared. Press Ctrl-s to save."
			m.focusEditField(editFieldPolicies)
			return m, nil
		case "load":
			m.fileActionMode = "load"
			m.fileActionField = editFieldPolicies
			m.input.SetValue(m.editFileInput.Value())
			m.input.Placeholder = ""
			m.input.Focus()
			m.pushPopup(popupFileAction)
			return m, nil
		case "write":
			m.fileActionMode = "write"
			m.fileActionField = editFieldPolicies
			m.input.SetValue(m.editFileInput.Value())
			m.input.Placeholder = ""
			m.input.Focus()
			m.pushPopup(popupFileAction)
			return m, nil
		}
		return m, nil
	}
	if action, ok := policiesActionByHotkey(key); ok {
		return choose(action)
	}
	switch key {
	case "ctrl+_", "ctrl+/":
		m.openPopupShortcuts(screenTextArea, popupPoliciesActions)
	case "q", "esc", "ctrl+g":
		m.popPopup()
	case "enter", "ctrl+j":
		if len(items) > 0 {
			return choose(items[m.valueActionCursor].value)
		}
	}
	return m, nil
}

func (component editorActionsComponent) updateFileActionPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	key := msg.String()
	finish := func(updated tea.Model, cmd tea.Cmd) (tea.Model, tea.Cmd) {
		if mm, ok := updated.(model); ok {
			if mm.errMessage == "" && mm.pendingFileWrite == fileWriteConfirmationNone {
				mm.clearPopupStack()
			} else if mm.activePopup == popupFileAction {
				mm.input.Focus()
			}
			return mm, cmd
		}
		return updated, cmd
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
		m.input.Blur()
		var updated tea.Model
		var cmd tea.Cmd
		switch m.fileActionMode {
		case "load":
			m.editFileInput.SetValue(m.input.Value())
			updated, cmd = m.loadValueFromFile()
		case "write":
			m.editFileInput.SetValue(m.input.Value())
			updated, cmd = m.writeValueToFile(false, false)
		case "random-custom":
			updated, cmd = m.generateRandomValueIntoEditor("base64-custom")
		default:
			updated = m
		}
		return finish(updated, cmd)
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
				mm.clearPopupStack()
			} else if mm.activePopup == popupFileAction {
				mm.input.Focus()
			}
			return mm, cmd
		}
		return updated, cmd
	}
	switch key {
	case "ctrl+_", "ctrl+/":
		m.openPopupShortcuts(screenTextArea, popupFileWriteConfirm)
		return m, nil
	case "q", "esc", "ctrl+g":
		m.pendingFileWrite = fileWriteConfirmationNone
		m.warningMessage = ""
		m.popPopup()
		if m.activePopup == popupFileAction {
			m.input.Focus()
		}
		return m, nil
	case "enter", "ctrl+j", "y":
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
	}
	return m, nil
}

func (component editorActionsComponent) updateUnsavedChangesPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	switch msg.String() {
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
	if kind, ok := randomKindByPopupHotkey(key); ok {
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
		m.popPopup()
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

func valueActionItems() []valueActionItem {
	return []valueActionItem{
		{hotkey: "c", value: "clear", label: "Clear value"},
		{hotkey: "r", value: "random", label: "Random value"},
		{hotkey: "l", value: "load", label: "Load from file"},
		{hotkey: "w", value: "write", label: "Write to file"},
	}
}

func valueActionByHotkey(key string) (string, bool) {
	for _, item := range valueActionItems() {
		if item.hotkey == key {
			return item.value, true
		}
	}
	return "", false
}

func policiesActionItems() []valueActionItem {
	return []valueActionItem{
		{hotkey: "c", value: "clear", label: "Clear policies"},
		{hotkey: "l", value: "load", label: "Load from file"},
		{hotkey: "w", value: "write", label: "Write to file"},
	}
}

func policiesActionByHotkey(key string) (string, bool) {
	for _, item := range policiesActionItems() {
		if item.hotkey == key {
			return item.value, true
		}
	}
	return "", false
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

func randomKindByPopupHotkey(key string) (string, bool) {
	for _, item := range randomItems() {
		if randomPopupHotkey(item.value) == key {
			return item.value, true
		}
	}
	return "", false
}
