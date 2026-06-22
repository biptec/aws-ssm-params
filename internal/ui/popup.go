package ui

import (
	"fmt"
	"strings"

	"github.com/biptec/aws-ssm-params/internal/inventory"
	tea "github.com/charmbracelet/bubbletea"
)

type popupKind int

const (
	popupNone popupKind = iota
	popupColumns
	popupShortcuts
	popupConfirm
	popupSort
	popupRegionSelect
	popupTypeSelect
	popupTierSelect
	popupDataTypeSelect
	popupOverwriteSelect
	popupValueActions
	popupPoliciesActions
	popupFileAction
	popupFileWriteConfirm
	popupUnsavedChanges
	popupRandomValue
)

func (m *model) openShortcuts(from screen) {
	m.shortcutsFor = from
	m.shortcutsPopupFor = popupNone
	m.pushPopup(popupShortcuts)
}

func (m *model) openPopupShortcuts(from screen, popup popupKind) {
	m.shortcutsFor = from
	m.shortcutsPopupFor = popup
	m.pushPopup(popupShortcuts)
}

func (m *model) pushPopup(kind popupKind) {
	m.popupStack = nil
	m.activePopup = kind
	m.pendingKeySequence = ""
}

func (m *model) pushNestedPopup(kind popupKind) {
	m.popupStack = nil
	if m.activePopup != popupNone {
		m.popupStack = append(m.popupStack, m.activePopup)
	}
	m.activePopup = kind
	m.pendingKeySequence = ""
}

func (m *model) popPopup() {
	if len(m.popupStack) == 0 {
		m.activePopup = popupNone
		m.pendingKeySequence = ""
		return
	}
	last := len(m.popupStack) - 1
	m.activePopup = m.popupStack[last]
	m.popupStack = m.popupStack[:last]
	m.pendingKeySequence = ""
}

func (m *model) clearPopupStack() {
	m.activePopup = popupNone
	m.popupStack = nil
	m.pendingKeySequence = ""
}

func (m model) popupLayers() []popupKind {
	layers := append([]popupKind(nil), m.popupStack...)
	if m.activePopup != popupNone {
		layers = append(layers, m.activePopup)
	}
	return layers
}

func (m model) updateSortPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
func (m model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		return m, deleteCmd(m.ctx, m.client, items, m.opts.NamesFile, m.opts.AllowNamesFileUpdate)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// updateRegionSelect lets users choose the concrete AWS region for saving a wildcard/all-regions parameter.
func (m model) updateRegionSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
func (m model) updateTypeSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
func (m model) updateHelp(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc", "ctrl+g", "?":
		m.screen = m.shortcutsFor
	}
	return m, nil
}

func (m model) updateShortcutsPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc", "ctrl+g", "?":
		m.popPopup()
	}
	return m, nil
}

func (m model) updateConfirmPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		return m, deleteCmd(m.ctx, m.client, items, m.opts.NamesFile, m.opts.AllowNamesFileUpdate)
	}
	if m.confirmExpected == "" {
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) updateRegionSelectPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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

func (m model) updateTypeSelectPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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

func (m model) updateTierSelectPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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

func (m model) updateDataTypeSelectPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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

func (m model) updateOverwriteSelectPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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

func (m model) renderSortPopup() string {
	return m.renderPopupBoxWithActions("Sort By", m.sortOptionLines(), "Space toggle   D direction   Esc close")
}

func (m model) renderValueActionsPopup() string {
	items := valueActionItems()
	lines := make([]string, 0, len(items))
	for i, item := range items {
		lines = append(lines, m.singleSelectLine(item.label, i == m.valueActionCursor, i == m.valueActionCursor))
	}
	return m.renderPopupBoxWithActions("Value Actions", lines, "Enter select   Esc cancel")
}

func (m model) renderPoliciesActionsPopup() string {
	items := policiesActionItems()
	lines := make([]string, 0, len(items))
	for i, item := range items {
		lines = append(lines, m.singleSelectLine(item.label, i == m.valueActionCursor, i == m.valueActionCursor))
	}
	return m.renderPopupBoxWithActions("Policies Actions", lines, "Enter select   Esc cancel")
}

func (m model) renderFileActionPopup() string {
	title := "Load from file"
	if m.fileActionField == editFieldPolicies {
		title = "Load policies from file"
	}
	label := "File path:"
	inputWidth := 48
	switch m.fileActionMode {
	case "write":
		title = "Write to file"
		if m.fileActionField == editFieldPolicies {
			title = "Write policies to file"
		}
	case "random-custom":
		title = "Random Value"
		label = "Byte length:"
		inputWidth = 12
	}
	button := "load"
	switch m.fileActionMode {
	case "write":
		button = "write"
	case "random-custom":
		button = "generate"
	}
	lines := []string{m.popupInputLine(label, m.input, inputWidth)}
	return m.renderPopupBoxWithActions(title, lines, "Enter "+button+"   Esc cancel")
}

func (m model) renderFileWriteConfirmPopup() string {
	message := "Confirm file write?"
	switch m.pendingFileWrite {
	case fileWriteConfirmationNone:
	case fileWriteConfirmationSecure:
		message = "This is a SecureString value. Write it to a local file in plain text?"
	case fileWriteConfirmationOverwrite:
		message = "File already exists. Overwrite it?"

	default:
	}
	return m.renderPopupBoxWithActions("Confirm", []string{message}, "Enter yes   Esc cancel")
}

func (m model) renderUnsavedChangesPopup() string {
	return m.renderPopupBoxWithActions("Confirm", []string{"Unsaved changes. Discard unsaved changes?"}, "Enter discard   Esc cancel")
}

func (m model) renderRandomValuePopup() string {
	items := randomItems()
	lines := make([]string, 0, len(items))
	for i, item := range items {
		lines = append(lines, m.singleSelectLine(item.label, i == m.randomCursor, i == m.randomCursor))
	}
	return m.renderPopupBoxWithActions("Random Value", lines, "Enter select   Esc cancel")
}

func (m model) sortOptionLines() []string {
	items := m.popupSortItems()
	lines := make([]string, 0, len(items))
	if len(items) > 0 && m.sortCursor >= len(items) {
		m.sortCursor = len(items) - 1
	}
	for i, item := range items {
		_, checked := sortRuleForColumn(m.sortRules, item.column)
		lines = append(lines, m.multiSelectLine(m.sortPopupLabel(item), checked, i == m.sortCursor))
	}
	return lines
}

// renderConfirmScreen renders the destructive-action confirmation prompt and input field.
func (m model) renderConfirmScreen() string {
	confirmLines := strings.Split(m.confirmPrompt, "\n")
	lines := make([]string, 0, len(confirmLines)+2)
	for _, line := range confirmLines {
		lines = append(lines, "  "+line)
	}
	lines = append(lines, "", "  > "+m.input.View())
	return m.renderBox("Confirm", lines, m.height)
}

func (m model) renderConfirmPopup() string {
	confirmLines := strings.Split(m.confirmPrompt, "\n")
	lines := make([]string, 0, len(confirmLines)+2)
	for _, line := range confirmLines {
		if strings.TrimSpace(line) == "" {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, line)
	}
	if m.confirmExpected != "" {
		prefix := "Type " + m.value(m.confirmExpected) + " to confirm: "
		lines = append(lines, "", m.popupInputLinePlainPrefix(prefix, m.input, max(len(m.confirmExpected)+1, 18)))
	}
	return m.renderPopupBoxWithActions("Confirm", lines, "Enter confirm   Esc cancel")
}

// renderRegionSelectScreen renders the region picker used before saving wildcard/all-regions items.
func (m model) renderRegionSelectScreen() string {
	regions := m.regionSelectOptions()
	lines := make([]string, 0, 2+len(regions))
	lines = append(lines, "  "+m.muted("Choose region for saving this value:"), "")
	for i, region := range regions {
		row := region
		if i == m.regionCursor {
			row = m.selectedMarker() + m.selectedRow(row)
		} else {
			row = "  " + row
		}
		lines = append(lines, "  "+row)
	}
	return m.renderBox("Region", lines, m.height)
}

func (m model) renderRegionSelectPopup() string {
	return m.renderPopupBoxWithActions("Region", m.regionSelectLines(), "Enter select   Esc cancel")
}

func (m model) regionSelectLines() []string {
	regions := m.regionSelectOptions()
	lines := make([]string, 0, 2+len(regions))
	lines = append(lines, m.muted("Choose region for saving this value:"), "")
	for i, region := range regions {
		lines = append(lines, m.singleSelectLine(region, i == m.regionCursor, i == m.regionCursor))
	}
	return lines
}

// renderTypeSelectScreen renders the AWS SSM parameter type picker used by value editors.
func (m model) renderTypeSelectScreen() string {
	typeItems := parameterTypeItems()
	lines := make([]string, 0, 2+len(typeItems))
	lines = append(lines, "  "+m.muted("Choose how this value should be stored in AWS SSM Parameter Store:"), "")
	for i, it := range typeItems {
		row := fmt.Sprintf("%s — %s", it.label, it.description)
		if i == m.typeCursor {
			row = m.selectedMarker() + m.selectedRow(row)
		} else {
			row = "  " + row
		}
		lines = append(lines, "  "+row)
	}
	return m.renderBox("Parameter Type", lines, m.height)
}

func (m model) renderTypeSelectPopup() string {
	return m.renderPopupBoxWithActions("Parameter Type", m.typeSelectLines(), "Enter select   Esc cancel")
}

func (m model) renderTierSelectPopup() string {
	return m.renderPopupBoxWithActions("Parameter Tier", m.tierSelectLines(), "Enter select   Esc cancel")
}

func (m model) renderDataTypeSelectPopup() string {
	return m.renderPopupBoxWithActions("Data Type", m.dataTypeSelectLines(), "Enter select   Esc cancel")
}

func (m model) renderOverwriteSelectPopup() string {
	return m.renderPopupBoxWithActions("Overwrite", m.overwriteSelectLines(), "Enter select   Esc cancel")
}

func (m model) typeSelectLines() []string {
	typeItems := parameterTypeItems()
	lines := make([]string, 0, 2+len(typeItems))
	lines = append(lines, m.muted("Choose how this value should be stored in AWS SSM Parameter Store:"), "")
	for i, it := range typeItems {
		row := fmt.Sprintf("%s — %s", it.label, it.description)
		lines = append(lines, m.singleSelectLine(row, i == m.typeCursor, i == m.typeCursor))
	}
	return lines
}

func (m model) tierSelectLines() []string {
	tierItems := parameterTierItems()
	lines := make([]string, 0, 2+len(tierItems))
	lines = append(lines, m.muted("Choose the AWS SSM storage tier for this parameter:"), "")
	for i, it := range tierItems {
		row := fmt.Sprintf("%s — %s", it.label, it.description)
		lines = append(lines, m.singleSelectLine(row, i == m.tierCursor, i == m.tierCursor))
	}
	return lines
}

func (m model) dataTypeSelectLines() []string {
	dataTypeItems := parameterDataTypeItems()
	lines := make([]string, 0, 2+len(dataTypeItems))
	lines = append(lines, m.muted("Choose AWS SSM value validation data type:"), "")
	for i, it := range dataTypeItems {
		row := fmt.Sprintf("%s — %s", it.label, it.description)
		lines = append(lines, m.singleSelectLine(row, i == m.dataTypeCursor, i == m.dataTypeCursor))
	}
	return lines
}

func (m model) overwriteSelectLines() []string {
	overwriteItems := overwriteItems()
	lines := make([]string, 0, 2+len(overwriteItems))
	lines = append(lines, m.muted("Choose whether AWS SSM may overwrite an existing parameter:"), "")
	for i, it := range overwriteItems {
		row := fmt.Sprintf("%s — %s", it.label, it.description)
		lines = append(lines, m.singleSelectLine(row, i == m.overwriteCursor, i == m.overwriteCursor))
	}
	return lines
}

// renderHelpScreen renders the full shortcut reference.
