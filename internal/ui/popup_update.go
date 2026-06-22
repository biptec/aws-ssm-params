package ui

import (
	"fmt"

	"github.com/biptec/aws-ssm-params/internal/inventory"
	tea "github.com/charmbracelet/bubbletea"
)

type popupUpdateComponent struct {
	model model
}

func (component popupUpdateComponent) updateSortPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	items := m.popupSortItems()
	key := msg.String()
	if key != "d" {
		if col, ok := m.popupSortColumnByLetterHotkey(key); ok {
			m.toggleSortColumn(col)
			return m, nil
		}
	}
	if action, ok, consumed := (&m).handlePendingNavigationSequence(key); consumed {
		if ok {
			m.sortCursor = cursorFromNavigation(m.sortCursor, len(items), action)
		}
		return m, nil
	}
	if action, ok := m.navigationAction(key); ok {
		m.sortCursor = cursorFromNavigation(m.sortCursor, len(items), action)
		return m, nil
	}
	if m.keymapStyle() == keymapVi && key == "g" {
		m.pendingKeySequence = "g"
		return m, nil
	}
	switch key {
	case "ctrl+_", "ctrl+/":
		m.openPopupShortcuts(screenMain, popupSort)
	case "q", "esc", "ctrl+g":
		m.popPopup()
	case " ", "enter", "ctrl+j":
		if len(items) > 0 {
			m.sortCursor = min(m.sortCursor, len(items)-1)
			m.toggleSortColumn(items[m.sortCursor].column)
		}
	case "d":
		if len(items) > 0 {
			m.sortCursor = min(m.sortCursor, len(items)-1)
			m.toggleSortDirection(items[m.sortCursor].column)
		}
	}
	return m, nil
}

func cursorFromNavigation(cursor, length int, action navigationAction) int {
	if length <= 0 {
		return 0
	}
	switch action {
	case navNone:
		return cursor
	case navPrevious:
		return previousCursor(cursor, length)
	case navNext:
		return nextCursor(cursor, length)
	case navFirst:
		return 0
	case navLast:
		return length - 1
	case navPageUp:
		return max(0, cursor-10)
	case navPageDown:
		return min(length-1, cursor+10)
	default:
		return cursor
	}
}

// updateConfirm verifies a typed confirmation phrase before running destructive delete operations.
func (component popupUpdateComponent) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	switch msg.String() {
	case "ctrl+_", "ctrl+/":
		m.openShortcuts(screenConfirm)
		return m, nil
	case "q", "esc", "ctrl+g":
		m.screen = m.returnScreen
		return m, nil
	case "enter":
		if m.input.Value() != m.confirmExpected {
			m.errMessage = "confirmation phrase does not match"
			return m, nil
		}
		items := append([]inventory.Item(nil), m.confirmItems...)
		m.busyMessage = fmt.Sprintf("Deleting %d parameter(s)...", len(items))
		m.loadingTitle = ""
		m.loadingLines = nil
		return m, deleteCmdWithBackend(m.ctx, backendFor(m), items, m.opts.NamesFile, m.opts.AllowNamesFileUpdate)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// updateRegionSelect lets users choose the concrete AWS region for saving a wildcard/all-regions parameter.
func (component popupUpdateComponent) updateRegionSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	regions := m.regionSelectOptions()
	if len(regions) == 0 {
		m.screen = screenTextArea
		return m, nil
	}
	key := msg.String()
	if action, ok, consumed := (&m).handlePendingNavigationSequence(key); consumed {
		if ok {
			m.regionCursor = cursorFromNavigation(m.regionCursor, len(regions), action)
		}
		return m, nil
	}
	if action, ok := m.navigationAction(key); ok {
		m.regionCursor = cursorFromNavigation(m.regionCursor, len(regions), action)
		return m, nil
	}
	if m.keymapStyle() == keymapVi && key == "g" {
		m.pendingKeySequence = "g"
		return m, nil
	}
	switch key {
	case "ctrl+_", "ctrl+/":
		m.openShortcuts(screenRegionSelect)
	case "q", "esc", "ctrl+g":
		m.screen = screenTextArea
		m = m.focusEditField(editFieldRegion)
	case "enter", "ctrl+j":
		m.editRegion = regions[m.regionCursor]
		m.screen = screenTextArea
		m = m.focusEditField(editFieldRegion)
	}
	return m, nil
}

// updateTypeSelect lets users choose which AWS SSM parameter type will be used when the current value is saved.
// Existing parameters start with their current type; missing parameters start as SecureString unless the user changes it.
func (component popupUpdateComponent) updateTypeSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	items := parameterTypeItems()
	if len(items) == 0 {
		m.screen = m.typeReturnScreen
		return m, nil
	}
	key := msg.String()
	if action, ok, consumed := (&m).handlePendingNavigationSequence(key); consumed {
		if ok {
			m.typeCursor = cursorFromNavigation(m.typeCursor, len(items), action)
		}
		return m, nil
	}
	if action, ok := m.navigationAction(key); ok {
		m.typeCursor = cursorFromNavigation(m.typeCursor, len(items), action)
		return m, nil
	}
	if m.keymapStyle() == keymapVi && key == "g" {
		m.pendingKeySequence = "g"
		return m, nil
	}
	switch key {
	case "ctrl+_", "ctrl+/":
		m.openPopupShortcuts(screenTypeSelect, popupTypeSelect)
	case "q", "esc", "ctrl+g":
		m.screen = m.typeReturnScreen
		if m.typeReturnScreen == screenTextArea {
			m = m.focusEditField(editFieldType)
		}
	case "enter", "ctrl+j":
		m.editType = items[m.typeCursor].value
		m.screen = m.typeReturnScreen
		if m.typeReturnScreen == screenTextArea {
			m = m.focusEditField(editFieldType)
		}
	}
	return m, nil
}

// updateHelp closes the legacy shortcuts screen and returns to the screen it documents.
func (component popupUpdateComponent) updateHelp(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	switch msg.String() {
	case "q", "esc", "ctrl+g", "?":
		m.screen = m.shortcutsFor
	}
	return m, nil
}

func (component popupUpdateComponent) updateShortcutsPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	switch msg.String() {
	case "q", "esc", "ctrl+g", "?":
		m.popPopup()
	}
	return m, nil
}

func (component popupUpdateComponent) updateConfirmPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	key := msg.String()
	switch key {
	case "ctrl+_", "ctrl+/":
		m.openPopupShortcuts(screenConfirm, popupConfirm)
		return m, nil
	case "q", "esc", "ctrl+g":
		m.popPopup()
		return m, nil
	case "enter":
		if m.confirmExpected != "" && m.input.Value() != m.confirmExpected {
			m.errMessage = "confirmation phrase does not match"
			return m, nil
		}
		items := append([]inventory.Item(nil), m.confirmItems...)
		m.busyMessage = fmt.Sprintf("Deleting %d parameter(s)...", len(items))
		m.loadingTitle = ""
		m.loadingLines = nil
		m.activePopup = popupNone
		m.popupStack = nil
		return m, deleteCmdWithBackend(m.ctx, backendFor(m), items, m.opts.NamesFile, m.opts.AllowNamesFileUpdate)
	}
	if m.confirmExpected == "" {
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (component popupUpdateComponent) updateRegionSelectPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	regions := m.regionSelectOptions()
	if len(regions) == 0 {
		m.popPopup()
		m = m.focusEditField(editFieldRegion)
		return m, nil
	}
	key := msg.String()
	if action, ok, consumed := (&m).handlePendingNavigationSequence(key); consumed {
		if ok {
			m.regionCursor = cursorFromNavigation(m.regionCursor, len(regions), action)
		}
		return m, nil
	}
	if action, ok := m.navigationAction(key); ok {
		m.regionCursor = cursorFromNavigation(m.regionCursor, len(regions), action)
		return m, nil
	}
	if m.keymapStyle() == keymapVi && key == "g" {
		m.pendingKeySequence = "g"
		return m, nil
	}
	switch key {
	case "ctrl+_", "ctrl+/":
		m.openPopupShortcuts(screenRegionSelect, popupRegionSelect)
	case "q", "esc", "ctrl+g":
		m.popPopup()
		m = m.focusEditField(editFieldRegion)
	case "enter", "ctrl+j":
		m.editRegion = regions[m.regionCursor]
		m.popPopup()
		m = m.focusEditField(editFieldRegion)
	}
	return m, nil
}

func (component popupUpdateComponent) updateTypeSelectPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	items := parameterTypeItems()
	if len(items) == 0 {
		m.popPopup()
		return m, nil
	}
	key := msg.String()
	if action, ok, consumed := (&m).handlePendingNavigationSequence(key); consumed {
		if ok {
			m.typeCursor = cursorFromNavigation(m.typeCursor, len(items), action)
		}
		return m, nil
	}
	if action, ok := m.navigationAction(key); ok {
		m.typeCursor = cursorFromNavigation(m.typeCursor, len(items), action)
		return m, nil
	}
	if m.keymapStyle() == keymapVi && key == "g" {
		m.pendingKeySequence = "g"
		return m, nil
	}
	if idx, ok := parameterTypeIndexByHotkey(items, key); ok {
		m.editType = items[idx].value
		m.popPopup()
		if m.typeReturnScreen == screenTextArea {
			m = m.focusEditField(editFieldType)
		}
		return m, nil
	}
	switch key {
	case "ctrl+_", "ctrl+/":
		m.openPopupShortcuts(screenTypeSelect, popupTypeSelect)
	case "q", "esc", "ctrl+g":
		m.popPopup()
		if m.typeReturnScreen == screenTextArea {
			m = m.focusEditField(editFieldType)
		}
	case "enter", "ctrl+j":
		m.editType = items[m.typeCursor].value
		m.popPopup()
		if m.typeReturnScreen == screenTextArea {
			m = m.focusEditField(editFieldType)
		}
	}
	return m, nil
}

func (component popupUpdateComponent) updateTierSelectPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	items := parameterTierItems()
	if len(items) == 0 {
		m.popPopup()
		return m, nil
	}
	key := msg.String()
	if action, ok, consumed := (&m).handlePendingNavigationSequence(key); consumed {
		if ok {
			m.tierCursor = cursorFromNavigation(m.tierCursor, len(items), action)
		}
		return m, nil
	}
	if action, ok := m.navigationAction(key); ok {
		m.tierCursor = cursorFromNavigation(m.tierCursor, len(items), action)
		return m, nil
	}
	if m.keymapStyle() == keymapVi && key == "g" {
		m.pendingKeySequence = "g"
		return m, nil
	}
	if idx, ok := parameterTierIndexByHotkey(items, key); ok {
		m.editTier = items[idx].value
		m.popPopup()
		m = m.focusEditField(editFieldTier)
		return m, nil
	}
	switch key {
	case "ctrl+_", "ctrl+/":
		m.openPopupShortcuts(screenTextArea, popupTierSelect)
	case "q", "esc", "ctrl+g":
		m.popPopup()
		m = m.focusEditField(editFieldTier)
	case "enter", "ctrl+j":
		m.editTier = items[m.tierCursor].value
		m.popPopup()
		m = m.focusEditField(editFieldTier)
	}
	return m, nil
}

func (component popupUpdateComponent) updateDataTypeSelectPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	items := parameterDataTypeItems()
	if len(items) == 0 {
		m.popPopup()
		return m, nil
	}
	key := msg.String()
	if action, ok, consumed := (&m).handlePendingNavigationSequence(key); consumed {
		if ok {
			m.dataTypeCursor = cursorFromNavigation(m.dataTypeCursor, len(items), action)
		}
		return m, nil
	}
	if action, ok := m.navigationAction(key); ok {
		m.dataTypeCursor = cursorFromNavigation(m.dataTypeCursor, len(items), action)
		return m, nil
	}
	if m.keymapStyle() == keymapVi && key == "g" {
		m.pendingKeySequence = "g"
		return m, nil
	}
	if idx, ok := parameterDataTypeIndexByHotkey(items, key); ok {
		m.editDataType = items[idx].value
		m.popPopup()
		m = m.focusEditField(editFieldDataType)
		return m, nil
	}
	switch key {
	case "ctrl+_", "ctrl+/":
		m.openPopupShortcuts(screenTextArea, popupDataTypeSelect)
	case "q", "esc", "ctrl+g":
		m.popPopup()
		m = m.focusEditField(editFieldDataType)
	case "enter", "ctrl+j":
		m.editDataType = items[m.dataTypeCursor].value
		m.popPopup()
		m = m.focusEditField(editFieldDataType)
	}
	return m, nil
}

func (component popupUpdateComponent) updateOverwriteSelectPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	items := overwriteItems()
	if len(items) == 0 {
		m.popPopup()
		return m, nil
	}
	key := msg.String()
	if action, ok, consumed := (&m).handlePendingNavigationSequence(key); consumed {
		if ok {
			m.overwriteCursor = cursorFromNavigation(m.overwriteCursor, len(items), action)
		}
		return m, nil
	}
	if action, ok := m.navigationAction(key); ok {
		m.overwriteCursor = cursorFromNavigation(m.overwriteCursor, len(items), action)
		return m, nil
	}
	if m.keymapStyle() == keymapVi && key == "g" {
		m.pendingKeySequence = "g"
		return m, nil
	}
	if idx, ok := overwriteIndexByHotkey(items, key); ok {
		m.editOverwrite = items[idx].value
		m.popPopup()
		m = m.focusEditField(editFieldOverwrite)
		return m, nil
	}
	switch key {
	case "ctrl+_", "ctrl+/":
		m.openPopupShortcuts(screenTextArea, popupOverwriteSelect)
	case "q", "esc", "ctrl+g":
		m.popPopup()
		m = m.focusEditField(editFieldOverwrite)
	case "enter", "ctrl+j":
		m.editOverwrite = items[m.overwriteCursor].value
		m.popPopup()
		m = m.focusEditField(editFieldOverwrite)
	}
	return m, nil
}
