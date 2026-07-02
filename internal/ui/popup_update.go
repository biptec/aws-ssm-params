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
	if (&m).navigateSortPopupButtons(key) {
		return m, nil
	}

	if m.sortButtonsFocused {
		if isPrimaryActionMsg(msg) {
			m.closeSortPopup()
			return m, nil
		}

		switch {
		case isHelpKeyMsg(msg):
			m.openPopupShortcuts(screenMain, popupSort)
		case isCloseKeyMsg(msg):
			m.closeSortPopup()
		case isEnterKeyMsg(msg):
			m.closeSortPopup()
		}

		return m, nil
	}

	if !bindingMatchesString(sortDirectionShortcut, key) {
		if col, ok := m.popupSortColumnByLetterHotkey(key); ok {
			m.toggleSortColumn(col)
			return m, nil
		}
	}

	if (&m).handleSelectorNavigation(key, &m.sortCursor, len(items)) {
		return m, nil
	}

	switch {
	case isHelpKeyMsg(msg):
		m.openPopupShortcuts(screenMain, popupSort)
	case isCloseKeyMsg(msg):
		m.closeSortPopup()
	case isToggleKeyMsg(msg) || isEnterKeyMsg(msg):
		if len(items) > 0 {
			m.sortCursor = min(m.sortCursor, len(items)-1)
			m.toggleSortColumn(items[m.sortCursor].column)
		}
	default:
		if bindingMatchesString(sortDirectionShortcut, key) && len(items) > 0 {
			m.sortCursor = min(m.sortCursor, len(items)-1)
			m.toggleSortDirection(items[m.sortCursor].column)
		}
	}

	if isPrimaryActionMsg(msg) {
		m.closeSortPopup()
	}

	return m, nil
}

func (m *model) navigateSortPopupButtons(key string) bool {
	if isTabNavigationKeyString(key) {
		m.sortButtonsFocused = !m.sortButtonsFocused

		return true
	}

	if !m.sortButtonsFocused {
		return false
	}

	if isLeftKeyString(key) || isRightKeyString(key) {
		return true
	}

	return false
}

func (m *model) closeSortPopup() {
	m.popPopup()
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

func (m *model) handleSelectorNavigation(key string, cursor *int, length int) bool {
	if action, ok, consumed := m.handlePendingNavigationSequence(key); consumed {
		if ok {
			*cursor = cursorFromNavigation(*cursor, length, action)
		}

		return true
	}

	if action, ok := m.navigationAction(key); ok {
		*cursor = cursorFromNavigation(*cursor, length, action)
		return true
	}

	if m.startViFirstNavigationSequence(key) {
		return true
	}

	return false
}

// updateConfirm verifies a typed confirmation phrase before running destructive delete operations.
func (component popupUpdateComponent) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model

	switch {
	case isHelpKeyMsg(msg):
		m.openShortcuts(screenConfirm)
		return m, nil
	case isBackKeyMsg(msg):
		m.screen = m.returnScreen
		return m, nil
	case isEnterKeyMsg(msg):
		if m.input.Value() != m.confirmExpected {
			m.errMessage = "confirmation phrase does not match"
			return m, nil
		}

		component.model = m

		return component.finishConfirmAction()
	}

	cmd := m.updateTextInput(&m.input, msg)

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
	if (&m).handleSelectorNavigation(key, &m.regionCursor, len(regions)) {
		return m, nil
	}

	switch {
	case isHelpKeyMsg(msg):
		m.openShortcuts(screenRegionSelect)
	case isBackKeyMsg(msg):
		m.screen = screenTextArea
		m = m.focusEditField(editFieldRegion)
	case isEnterKeyMsg(msg):
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
	if (&m).handleSelectorNavigation(key, &m.typeCursor, len(items)) {
		return m, nil
	}

	switch {
	case isHelpKeyMsg(msg):
		m.openPopupShortcuts(screenTypeSelect, popupTypeSelect)
	case isBackKeyMsg(msg):
		m.screen = m.typeReturnScreen
		if m.typeReturnScreen == screenTextArea {
			m = m.focusEditField(editFieldType)
		}
	case isEnterKeyMsg(msg):
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

	if isShortcutPopupCloseKeyMsg(msg) {
		m.screen = m.shortcutsFor
	}

	return m, nil
}

func (component popupUpdateComponent) updateShortcutsPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model

	if isShortcutPopupCloseKeyMsg(msg) {
		m.popPopup()
	}

	return m, nil
}

func (component popupUpdateComponent) updateConfirmPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model

	key := msg.String()
	if isPrimaryActionMsg(msg) {
		component.model = m
		return component.finishConfirmAction()
	}

	switch {
	case isHelpKeyMsg(msg):
		m.openPopupShortcuts(screenConfirm, popupConfirm)
		return m, nil
	case isCancelKeyMsg(msg):
		m.popPopup()
		return m, nil
	}

	if (&m).navigateConfirmButtons(key) {
		return m, nil
	}

	if importEnterKey(key) {
		if m.confirmFocus == confirmFocusCancelButton {
			m.popPopup()
			return m, nil
		}

		if m.confirmFocus >= 0 {
			m.toggleConfirmStateFilter(m.confirmFocus)
			return m, nil
		}

		component.model = m

		return component.finishConfirmAction()
	}

	if isToggleKeyMsg(msg) && m.confirmFocus >= 0 {
		m.toggleConfirmStateFilter(m.confirmFocus)
		return m, nil
	}

	return m, nil
}

func (m *model) openQuitConfirmPopup() {
	m.message = ""
	m.warningMessage = ""
	m.errMessage = ""
	m.pendingQuit = true
	m.pendingQuitKey = ""
	m.confirmFocus = confirmFocusPrimaryButton
	m.confirmButtonCursor = importActionPrimary

	if m.activePopup == popupNone {
		m.pushPopup(popupQuitConfirm)
		return
	}

	m.pushNestedPopup(popupQuitConfirm)
}

func (component popupUpdateComponent) updateQuitConfirmPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	key := msg.String()

	confirm := func() (tea.Model, tea.Cmd) {
		m.pendingQuit = false
		m.pendingQuitKey = ""

		return m, tea.Quit
	}

	cancel := func() (tea.Model, tea.Cmd) {
		m.pendingQuit = false
		m.pendingQuitKey = ""
		m.popPopup()

		return m, nil
	}

	if isPrimaryActionMsg(msg) {
		return confirm()
	}

	switch {
	case isHelpKeyMsg(msg):
		m.openPopupShortcuts(screenMain, popupQuitConfirm)
		return m, nil
	case isCancelKeyMsg(msg):
		return cancel()
	}

	if (&m).navigateConfirmButtons(key) {
		return m, nil
	}

	if isEnterKeyMsg(msg) {
		if m.confirmFocus == confirmFocusCancelButton {
			return cancel()
		}

		return confirm()
	}

	return m, nil
}

func (m *model) navigateConfirmButtons(key string) bool {
	if action, ok, consumed := m.handlePendingNavigationSequence(key); consumed {
		if ok {
			m.applyConfirmButtonNavigation(action)
		}

		return true
	}

	if action, ok := m.navigationAction(key); ok {
		m.applyConfirmButtonNavigation(action)
		return true
	}

	if m.startViFirstNavigationSequence(key) {
		return true
	}

	return false
}

func (m *model) applyConfirmButtonNavigation(action navigationAction) {
	focusCount := len(m.confirmStateFilterOrder) + 2
	if focusCount <= 2 {
		switch action {
		case navPrevious, navNext, navPageUp, navPageDown:
			if m.confirmFocus == confirmFocusPrimaryButton {
				m.confirmFocus = confirmFocusCancelButton
			} else {
				m.confirmFocus = confirmFocusPrimaryButton
			}
		case navFirst:
			m.confirmFocus = confirmFocusPrimaryButton
		case navLast:
			m.confirmFocus = confirmFocusCancelButton
		case navNone:
		}

		m.syncConfirmButtonCursor()

		return
	}

	current := m.confirmFocusIndex()

	switch action {
	case navPrevious, navPageUp:
		current = (current - 1 + focusCount) % focusCount
	case navNext, navPageDown:
		current = (current + 1) % focusCount
	case navFirst:
		current = 0
	case navLast:
		current = focusCount - 1
	case navNone:
	}

	m.confirmFocus = m.confirmFocusFromIndex(current)
	m.syncConfirmButtonCursor()
}

func (m *model) confirmFocusIndex() int {
	switch m.confirmFocus {
	case confirmFocusPrimaryButton:
		return 0
	case confirmFocusCancelButton:
		return 1
	default:
		if m.confirmFocus >= 0 {
			return m.confirmFocus + 2
		}
	}

	return 0
}

func (m *model) confirmFocusFromIndex(index int) int {
	switch index {
	case 0:
		return confirmFocusPrimaryButton
	case 1:
		return confirmFocusCancelButton
	default:
		return index - 2
	}
}

func (m *model) syncConfirmButtonCursor() {
	if m.confirmFocus == confirmFocusCancelButton {
		m.confirmButtonCursor = importActionCancel
		return
	}

	m.confirmButtonCursor = importActionPrimary
}

func (m *model) toggleConfirmStateFilter(index int) {
	if index < 0 || index >= len(m.confirmStateFilterOrder) {
		return
	}

	state := m.confirmStateFilterOrder[index]
	m.confirmStateFilterSelected[state] = !m.confirmStateFilterSelected[state]
}

func (component popupUpdateComponent) finishConfirmAction() (tea.Model, tea.Cmd) {
	m := component.model

	switch m.confirmAction {
	case confirmActionPush:
		indexes := m.confirmStatusIndexes
		if len(m.confirmStateFilterOrder) > 0 {
			indexes = m.dirtyStatusIndexesByState(indexes, m.confirmStateFilterSelected)
		}

		if len(indexes) == 0 {
			m.message = "No selected local changes to push."
			m.errMessage = ""
			m.warningMessage = ""
			m.clearPopupStack()
			m.screen = m.returnScreen

			return m, nil
		}

		statuses := m.dirtyStatuses(indexes)
		m.busyMessage = fmt.Sprintf("Pushing %d local change(s)...", len(statuses))
		m.loadingTitle = ""
		m.clearPopupStack()

		return m, pushLocalChangesCmdWithBackend(m.contextProvider(), backendFor(m), statuses, m.opts.NamesFile, m.opts.AllowNamesFileUpdate)
	case confirmActionDelete:
		if m.opts.ApplyImmediately {
			items := append(inventory.Items(nil), m.confirmItems...)
			m.busyMessage = fmt.Sprintf("Deleting %d parameter(s)...", len(items))
			m.loadingTitle = ""
			m.clearPopupStack()

			return m, deleteCmdWithBackend(m.contextProvider(), backendFor(m), items, m.opts.NamesFile, m.opts.AllowNamesFileUpdate)
		}

		changed := m.applyLocalDeleteItems(m.confirmItems)

		m.message = fmt.Sprintf("Marked %d parameter(s) for deletion. Press p to push.", changed)
		if changed == 0 {
			m.message = "No parameters marked for deletion."
		}

		m.errMessage = ""
		m.warningMessage = ""
		m.clearPopupStack()
		m.screen = m.returnScreen
		m.ensureSelection()

		return m, nil
	}

	return m, nil
}

func (component popupUpdateComponent) updateRegionSelectPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	importSelector := m.importSelectorActive()
	editorSelector := m.editorSelectorActive()

	regions := m.regionSelectOptions()
	if importSelector {
		regions = m.importDefaultRegionOptions()
	}

	if len(regions) == 0 {
		switch {
		case importSelector:
			m = m.finishImportSelector()
		case editorSelector:
			m = m.finishEditorSelector()
		default:
			m.popPopup()
			m = m.focusEditField(editFieldRegion)
		}

		return m, nil
	}

	key := msg.String()
	if importSelector && importPrimaryActionKey(key) {
		m.importDefaultRegion = regions[min(m.regionCursor, len(regions)-1)]
		m = m.finishImportSelector()

		return m, nil
	}

	if importSelector && m.importButtonsFocused && importEnterKey(key) {
		if m.importButtonCursor == importActionPrimary {
			m.importDefaultRegion = regions[min(m.regionCursor, len(regions)-1)]
		}

		m = m.finishImportSelector()

		return m, nil
	}

	if editorSelector && importPrimaryActionKey(key) {
		m.editRegion = regions[min(m.regionCursor, len(regions)-1)]
		m = m.finishEditorSelector()

		return m, nil
	}

	if editorSelector && m.editorButtonsFocused && importEnterKey(key) {
		if m.editorButtonCursor == importActionPrimary {
			m.editRegion = regions[min(m.regionCursor, len(regions)-1)]
		}

		m = m.finishEditorSelector()

		return m, nil
	}

	if importSelector && (&m).navigateImportSelectorButtons(key) {
		return m, nil
	}

	if editorSelector && (&m).navigateEditorPopupButtons(key) {
		return m, nil
	}

	if (&m).handleSelectorNavigation(key, &m.regionCursor, len(regions)) {
		return m, nil
	}

	switch {
	case isHelpKeyMsg(msg):
		m.openPopupShortcuts(screenRegionSelect, popupRegionSelect)
	case isCancelKeyMsg(msg):
		switch {
		case importSelector:
			m = m.finishImportSelector()
		case editorSelector:
			m = m.finishEditorSelector()
		default:
			m.popPopup()
			m = m.focusEditField(editFieldRegion)
		}
	case isEnterKeyMsg(msg):
		switch {
		case importSelector:
			m.importDefaultRegion = regions[m.regionCursor]
			m = m.finishImportSelector()
		case editorSelector:
			m.editRegion = regions[m.regionCursor]
			m = m.finishEditorSelector()
		default:
			m.editRegion = regions[m.regionCursor]
			m.popPopup()
			m = m.focusEditField(editFieldRegion)
		}
	}

	return m, nil
}

func (component popupUpdateComponent) updateTypeSelectPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	importSelector := m.importSelectorActive()
	editorSelector := m.editorSelectorActive()

	items := parameterTypeItems()
	if importSelector {
		items = importParameterTypeItems()
	}

	if len(items) == 0 {
		m.popPopup()
		return m, nil
	}

	key := msg.String()
	if importSelector && importPrimaryActionKey(key) {
		m.importDefaultType = items[min(m.typeCursor, len(items)-1)].value
		m = m.finishImportSelector()

		return m, nil
	}

	if importSelector && m.importButtonsFocused && importEnterKey(key) {
		if m.importButtonCursor == importActionPrimary {
			m.importDefaultType = items[min(m.typeCursor, len(items)-1)].value
		}

		m = m.finishImportSelector()

		return m, nil
	}

	if importSelector && (&m).navigateImportSelectorButtons(key) {
		return m, nil
	}

	if editorSelector && importPrimaryActionKey(key) {
		m.editType = items[min(m.typeCursor, len(items)-1)].value
		m = m.finishEditorSelector()

		return m, nil
	}

	if editorSelector && m.editorButtonsFocused && importEnterKey(key) {
		if m.editorButtonCursor == importActionPrimary {
			m.editType = items[min(m.typeCursor, len(items)-1)].value
		}

		m = m.finishEditorSelector()

		return m, nil
	}

	if editorSelector && (&m).navigateEditorPopupButtons(key) {
		return m, nil
	}

	if (&m).handleSelectorNavigation(key, &m.typeCursor, len(items)) {
		return m, nil
	}

	if idx, ok := items.indexByHotkey(key); ok {
		switch {
		case importSelector:
			m.importDefaultType = items[idx].value
			m = m.finishImportSelector()
		case editorSelector:
			m.editType = items[idx].value
			m = m.finishEditorSelector()
		default:
			m.editType = items[idx].value
			m.popPopup()

			if m.typeReturnScreen == screenTextArea {
				m = m.focusEditField(editFieldType)
			}
		}

		return m, nil
	}

	switch {
	case isHelpKeyMsg(msg):
		m.openPopupShortcuts(screenTypeSelect, popupTypeSelect)
	case isCancelKeyMsg(msg):
		switch {
		case importSelector:
			m = m.finishImportSelector()
		case editorSelector:
			m = m.finishEditorSelector()
		default:
			m.popPopup()

			if m.typeReturnScreen == screenTextArea {
				m = m.focusEditField(editFieldType)
			}
		}
	case isEnterKeyMsg(msg):
		switch {
		case importSelector:
			m.importDefaultType = items[m.typeCursor].value
			m = m.finishImportSelector()
		case editorSelector:
			m.editType = items[m.typeCursor].value
			m = m.finishEditorSelector()
		default:
			m.editType = items[m.typeCursor].value
			m.popPopup()

			if m.typeReturnScreen == screenTextArea {
				m = m.focusEditField(editFieldType)
			}
		}
	}

	return m, nil
}

func (component popupUpdateComponent) updateTierSelectPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	importSelector := m.importSelectorActive()
	editorSelector := m.editorSelectorActive()

	items := parameterTierItems()
	if importSelector {
		items = importParameterTierItems()
	}

	if len(items) == 0 {
		m.popPopup()
		return m, nil
	}

	key := msg.String()
	if importSelector && importPrimaryActionKey(key) {
		m.importDefaultTier = items[min(m.tierCursor, len(items)-1)].value
		m = m.finishImportSelector()

		return m, nil
	}

	if importSelector && m.importButtonsFocused && importEnterKey(key) {
		if m.importButtonCursor == importActionPrimary {
			m.importDefaultTier = items[min(m.tierCursor, len(items)-1)].value
		}

		m = m.finishImportSelector()

		return m, nil
	}

	if importSelector && (&m).navigateImportSelectorButtons(key) {
		return m, nil
	}

	if editorSelector && importPrimaryActionKey(key) {
		m.editTier = items[min(m.tierCursor, len(items)-1)].value
		m = m.finishEditorSelector()

		return m, nil
	}

	if editorSelector && m.editorButtonsFocused && importEnterKey(key) {
		if m.editorButtonCursor == importActionPrimary {
			m.editTier = items[min(m.tierCursor, len(items)-1)].value
		}

		m = m.finishEditorSelector()

		return m, nil
	}

	if editorSelector && (&m).navigateEditorPopupButtons(key) {
		return m, nil
	}

	if (&m).handleSelectorNavigation(key, &m.tierCursor, len(items)) {
		return m, nil
	}

	if idx, ok := items.indexByHotkey(key); ok {
		switch {
		case importSelector:
			m.importDefaultTier = items[idx].value
			m = m.finishImportSelector()
		case editorSelector:
			m.editTier = items[idx].value
			m = m.finishEditorSelector()
		default:
			m.editTier = items[idx].value
			m.popPopup()
			m = m.focusEditField(editFieldTier)
		}

		return m, nil
	}

	switch {
	case isHelpKeyMsg(msg):
		m.openPopupShortcuts(screenTextArea, popupTierSelect)
	case isCancelKeyMsg(msg):
		switch {
		case importSelector:
			m = m.finishImportSelector()
		case editorSelector:
			m = m.finishEditorSelector()
		default:
			m.popPopup()
			m = m.focusEditField(editFieldTier)
		}
	case isEnterKeyMsg(msg):
		switch {
		case importSelector:
			m.importDefaultTier = items[m.tierCursor].value
			m = m.finishImportSelector()
		case editorSelector:
			m.editTier = items[m.tierCursor].value
			m = m.finishEditorSelector()
		default:
			m.editTier = items[m.tierCursor].value
			m.popPopup()
			m = m.focusEditField(editFieldTier)
		}
	}

	return m, nil
}

func (component popupUpdateComponent) updateDataTypeSelectPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	importSelector := m.importSelectorActive()
	editorSelector := m.editorSelectorActive()

	items := parameterDataTypeItems()
	if importSelector {
		items = importParameterDataTypeItems()
	}

	if len(items) == 0 {
		m.popPopup()
		return m, nil
	}

	key := msg.String()
	if importSelector && importPrimaryActionKey(key) {
		m.importDefaultDataType = items[min(m.dataTypeCursor, len(items)-1)].value
		m = m.finishImportSelector()

		return m, nil
	}

	if importSelector && m.importButtonsFocused && importEnterKey(key) {
		if m.importButtonCursor == importActionPrimary {
			m.importDefaultDataType = items[min(m.dataTypeCursor, len(items)-1)].value
		}

		m = m.finishImportSelector()

		return m, nil
	}

	if importSelector && (&m).navigateImportSelectorButtons(key) {
		return m, nil
	}

	if editorSelector && importPrimaryActionKey(key) {
		m.editDataType = items[min(m.dataTypeCursor, len(items)-1)].value
		m = m.finishEditorSelector()

		return m, nil
	}

	if editorSelector && m.editorButtonsFocused && importEnterKey(key) {
		if m.editorButtonCursor == importActionPrimary {
			m.editDataType = items[min(m.dataTypeCursor, len(items)-1)].value
		}

		m = m.finishEditorSelector()

		return m, nil
	}

	if editorSelector && (&m).navigateEditorPopupButtons(key) {
		return m, nil
	}

	if (&m).handleSelectorNavigation(key, &m.dataTypeCursor, len(items)) {
		return m, nil
	}

	if idx, ok := items.indexByHotkey(key); ok {
		switch {
		case importSelector:
			m.importDefaultDataType = items[idx].value
			m = m.finishImportSelector()
		case editorSelector:
			m.editDataType = items[idx].value
			m = m.finishEditorSelector()
		default:
			m.editDataType = items[idx].value
			m.popPopup()
			m = m.focusEditField(editFieldDataType)
		}

		return m, nil
	}

	switch {
	case isHelpKeyMsg(msg):
		m.openPopupShortcuts(screenTextArea, popupDataTypeSelect)
	case isCancelKeyMsg(msg):
		switch {
		case importSelector:
			m = m.finishImportSelector()
		case editorSelector:
			m = m.finishEditorSelector()
		default:
			m.popPopup()
			m = m.focusEditField(editFieldDataType)
		}
	case isEnterKeyMsg(msg):
		switch {
		case importSelector:
			m.importDefaultDataType = items[m.dataTypeCursor].value
			m = m.finishImportSelector()
		case editorSelector:
			m.editDataType = items[m.dataTypeCursor].value
			m = m.finishEditorSelector()
		default:
			m.editDataType = items[m.dataTypeCursor].value
			m.popPopup()
			m = m.focusEditField(editFieldDataType)
		}
	}

	return m, nil
}

func (component popupUpdateComponent) updateOverwriteSelectPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	editorSelector := m.editorSelectorActive()

	items := overwriteItems()
	if len(items) == 0 {
		m.popPopup()
		return m, nil
	}

	key := msg.String()
	if editorSelector && importPrimaryActionKey(key) {
		m.editOverwrite = items[min(m.overwriteCursor, len(items)-1)].value
		m = m.finishEditorSelector()

		return m, nil
	}

	if editorSelector && m.editorButtonsFocused && importEnterKey(key) {
		if m.editorButtonCursor == importActionPrimary {
			m.editOverwrite = items[min(m.overwriteCursor, len(items)-1)].value
		}

		m = m.finishEditorSelector()

		return m, nil
	}

	if editorSelector && (&m).navigateEditorPopupButtons(key) {
		return m, nil
	}

	if (&m).handleSelectorNavigation(key, &m.overwriteCursor, len(items)) {
		return m, nil
	}

	if idx, ok := items.indexByHotkey(key); ok {
		m.editOverwrite = items[idx].value

		switch {
		case editorSelector:
			m = m.finishEditorSelector()
		default:
			m.popPopup()
			m = m.focusEditField(editFieldOverwrite)
		}

		return m, nil
	}

	switch {
	case isHelpKeyMsg(msg):
		m.openPopupShortcuts(screenTextArea, popupOverwriteSelect)
	case isCancelKeyMsg(msg):
		switch {
		case editorSelector:
			m = m.finishEditorSelector()
		default:
			m.popPopup()
			m = m.focusEditField(editFieldOverwrite)
		}
	case isEnterKeyMsg(msg):
		m.editOverwrite = items[m.overwriteCursor].value

		switch {
		case editorSelector:
			m = m.finishEditorSelector()
		default:
			m.popPopup()
			m = m.focusEditField(editFieldOverwrite)
		}
	}

	return m, nil
}
