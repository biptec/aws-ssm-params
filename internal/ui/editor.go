package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	crerr "github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/fileio"
	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/randomx"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	tea "github.com/charmbracelet/bubbletea"
)

type editField int

const (
	editFieldValue editField = iota
	editFieldSSMPath
	editFieldRegion
	editFieldType
	editFieldTier
	editFieldDataType
	editFieldOverwrite
	editFieldDescription
	editFieldPolicies
	editFieldFilePath
)

type editDirection int

const (
	editDirectionNext editDirection = iota
	editDirectionPrevious
)

type fileWriteConfirmation int

const (
	fileWriteConfirmationNone fileWriteConfirmation = iota
	fileWriteConfirmationSecure
	fileWriteConfirmationOverwrite
)

type actionItem struct{ label, value string }

type parameterTypeItem struct {
	hotkey      string
	label       string
	value       ssm.ParameterType
	description string
}

type parameterTierItem struct {
	hotkey      string
	label       string
	value       ssm.ParameterTier
	description string
}

type parameterDataTypeItem struct {
	hotkey      string
	label       string
	value       ssm.ParameterDataType
	description string
}

type overwriteItem struct {
	hotkey      string
	label       string
	value       bool
	description string
}

type editSnapshot struct {
	name          string
	region        string
	parameterType string
	tier          string
	dataType      string
	overwrite     bool
	newParameter  bool
	description   string
	policies      string
	value         string
}

// updateTextArea handles the unified edit form: editable SSM name, region/type selectors, file path, multiline value, and save/file operations.
func (m model) updateTextArea(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
			m.openShortcuts(screenTextArea)
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
			case "ctrl+s":
				resetFileConfirmation()
				return m.saveValue(m.textArea.Value())
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
		m.openShortcuts(screenTextArea)
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
	case "ctrl+s":
		resetFileConfirmation()
		return m.saveValue(m.textArea.Value())
	}

	if updated, handled := m.updateEmacsTextFieldKey(key); handled {
		updated.collapseExpandedFieldAfterEdit(beforeEditField, beforeExpandableValue)
		return updated, nil
	}

	var cmd tea.Cmd
	switch m.editField {
	case editFieldRegion, editFieldType, editFieldTier, editFieldDataType, editFieldOverwrite:
	case editFieldSSMPath:
		m.editPathInput, cmd = m.editPathInput.Update(msg)
	case editFieldDescription:
		m.editDescriptionArea, cmd = m.editDescriptionArea.Update(msg)
	case editFieldFilePath:
		m.editFileInput, cmd = m.editFileInput.Update(msg)
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

func (m *model) openActionsPopupForFocusedField() bool {
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

func (m model) updateValueActionsPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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

func (m model) updatePoliciesActionsPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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

func (m model) updateFileActionPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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

func (m model) updateFileWriteConfirmPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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

func (m model) updateUnsavedChangesPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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

func (m model) updateRandomValuePopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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

// startMultiline opens the selected parameter value in the multiline editor.
func (m model) startMultiline(ret screen) (tea.Model, tea.Cmd) {
	m.returnScreen = ret
	m.editRegion = m.initialEditRegion()
	m.editType = m.initialEditType()
	m.editTier = m.initialEditTier()
	m.editDataType = m.initialEditDataType()
	m.editNewParameter = false
	m.editOverwrite = !m.currentStatus().Exists
	m.expandedFields = map[editField]bool{}
	m.textArea.SetValue(m.currentStatus().Value)
	m.editPoliciesArea.SetValue(prettyPoliciesForEditor(m.currentStatus().Policies))
	m.editPathInput.SetValue(m.currentItem().Path)
	m.editPathInput.Placeholder = ""
	m.editPathInput.Blur()
	m.editDescriptionInput.SetValue(m.currentStatus().Description)
	m.editDescriptionInput.Placeholder = ""
	m.editDescriptionInput.Blur()
	m.editDescriptionArea.SetValue(m.currentStatus().Description)
	m.editDescriptionArea.Blur()
	m.editFileInput.SetValue("")
	m.editFileInput.Placeholder = ""
	m.editFileInput.Blur()
	m.editField = editFieldSSMPath
	m.editDirection = editDirectionNext
	m.viInsertMode = m.keymapStyle() != keymapVi
	m.pendingFileWrite = fileWriteConfirmationNone
	m.warningMessage = ""
	m.message = ""
	m.errMessage = ""
	m.editInitialSnapshot = m.currentEditSnapshot()
	m.screen = screenTextArea
	m = m.focusEditField(editFieldSSMPath)
	return m, nil
}

// startNewParameter opens the editor with empty fields so users can create a parameter without a names file.
func (m model) startNewParameter(ret screen) (tea.Model, tea.Cmd) {
	m.returnScreen = ret
	m.editRegion = m.initialEditRegion()
	m.editType = ssm.DefaultParameterType
	m.editTier = ssm.DefaultParameterTier
	m.editDataType = ssm.DefaultParameterDataType
	m.editNewParameter = true
	m.editOverwrite = false
	m.expandedFields = map[editField]bool{}
	m.textArea.SetValue("")
	m.editPoliciesArea.SetValue("")
	m.editPathInput.SetValue("")
	m.editPathInput.Placeholder = ""
	m.editDescriptionInput.SetValue("")
	m.editDescriptionInput.Placeholder = ""
	m.editDescriptionInput.Blur()
	m.editDescriptionArea.SetValue("")
	m.editDescriptionArea.Blur()
	m.editFileInput.SetValue("")
	m.editFileInput.Placeholder = ""
	m.editField = editFieldSSMPath
	m.editDirection = editDirectionNext
	m.viInsertMode = m.keymapStyle() != keymapVi
	m.pendingFileWrite = fileWriteConfirmationNone
	m.warningMessage = ""
	m.message = ""
	m.errMessage = ""
	m.screen = screenTextArea
	m = m.focusEditField(editFieldSSMPath)
	m.editInitialSnapshot = m.currentEditSnapshot()
	return m, nil
}

// focusEditField moves the edit-screen focus to one field and focuses/blurs the underlying input widgets.
func (m model) focusEditField(field editField) model {
	if !m.editFieldAllowed(field) || (field == editFieldPolicies && !m.shouldShowPoliciesField()) || (field == editFieldOverwrite && !m.shouldShowOverwriteField()) {
		fields := m.editFieldOrder()
		if len(fields) == 0 {
			field = editFieldSSMPath
		} else {
			field = fields[0]
		}
	}
	m.blurEditFields()
	m.editField = field
	switch field {
	case editFieldRegion, editFieldType, editFieldTier, editFieldDataType, editFieldOverwrite:
	case editFieldSSMPath:
		m.editPathInput.Focus()
	case editFieldDescription:
		m.editDescriptionArea.Focus()
	case editFieldFilePath:
		m.editFileInput.Focus()
	case editFieldPolicies:
		m.editPoliciesArea.Focus()
	case editFieldValue:
		m.textArea.Focus()

	default:
	}
	m.message = ""
	m.errMessage = ""
	return m
}

// blurEditFields removes focus from all concrete input widgets used by the edit screen.
func (m *model) blurEditFields() {
	m.textArea.Blur()
	m.editPoliciesArea.Blur()
	m.editDescriptionArea.Blur()
	m.editPathInput.Blur()
	m.editDescriptionInput.Blur()
	m.editFileInput.Blur()
}

func (m model) requestEditorBack() (tea.Model, tea.Cmd) {
	m.pendingFileWrite = fileWriteConfirmationNone
	m.warningMessage = ""
	if m.editorHasUnsavedChanges() {
		m.pushPopup(popupUnsavedChanges)
		return m, nil
	}
	m.discardEditorChanges()
	return m, nil
}

func (m *model) discardEditorChanges() {
	m.blurEditFields()
	m.pendingFileWrite = fileWriteConfirmationNone
	m.warningMessage = ""
	m.clearPopupStack()
	m.screen = m.returnScreen
}

func (m model) editorHasUnsavedChanges() bool {
	if m.editInitialSnapshot == (editSnapshot{}) {
		return false
	}
	return m.currentEditSnapshot() != m.editInitialSnapshot
}

func (m model) currentEditSnapshot() editSnapshot {
	return editSnapshot{
		name:          m.editPathInput.Value(),
		region:        m.editRegion,
		parameterType: m.normalizedEditType().String(),
		tier:          m.normalizedEditTier().String(),
		dataType:      m.normalizedEditDataType().String(),
		overwrite:     m.editOverwrite,
		newParameter:  m.editNewParameter,
		description:   m.editDescriptionArea.Value(),
		policies:      m.editPoliciesArea.Value(),
		value:         m.textArea.Value(),
	}
}

// focusNextEditField advances the edit-screen focus in the visual field order.
func (m model) focusNextEditField() (tea.Model, tea.Cmd) {
	return m.moveToEditField(m.nextEditField(), editDirectionNext)
}

// focusPreviousEditField moves the edit-screen focus backwards in the visual field order.
func (m model) focusPreviousEditField() (tea.Model, tea.Cmd) {
	return m.moveToEditField(m.previousEditField(), editDirectionPrevious)
}

// moveToEditField moves focus through all edit fields without opening selector screens automatically.
func (m model) moveToEditField(field editField, direction editDirection) (tea.Model, tea.Cmd) {
	m.editDirection = direction
	return m.focusEditField(field), nil
}

func (m model) editFieldOrder() []editField {
	candidates := []editField{editFieldSSMPath, editFieldRegion, editFieldType, editFieldTier, editFieldDataType}
	if m.shouldShowOverwriteField() {
		candidates = append(candidates, editFieldOverwrite)
	}
	candidates = append(candidates, editFieldDescription)
	if m.shouldShowPoliciesField() {
		candidates = append(candidates, editFieldPolicies)
	}
	candidates = append(candidates, editFieldValue)
	fields := make([]editField, 0, len(candidates))
	for _, field := range candidates {
		if m.editFieldAllowed(field) {
			fields = append(fields, field)
		}
	}
	if len(fields) == 0 {
		return []editField{editFieldSSMPath}
	}
	return fields
}

func (m model) hasVisibleFieldAfter(field editField) bool {
	fields := m.editFieldOrder()
	idx := indexOfEditField(fields, field)
	return idx >= 0 && idx < len(fields)-1
}

func (m model) nextEditField() editField {
	fields := m.editFieldOrder()
	idx := indexOfEditField(fields, m.editField)
	return fields[nextCursor(idx, len(fields))]
}

func (m model) previousEditField() editField {
	fields := m.editFieldOrder()
	idx := indexOfEditField(fields, m.editField)
	return fields[previousCursor(idx, len(fields))]
}

func indexOfEditField(fields []editField, field editField) int {
	for i, candidate := range fields {
		if candidate == field {
			return i
		}
	}
	return 0
}

// openRegionSelect loads all enabled AWS regions on first use, then opens the region selector.
func (m model) openRegionSelect() (tea.Model, tea.Cmd) {
	m = m.ensureRegionSelectOptions()
	regions := m.regionSelectOptions()
	if len(regions) == 0 {
		return m.focusEditField(editFieldValue), nil
	}
	m.regionCursor = indexOf(regions, m.editRegion)
	m.pushPopup(popupRegionSelect)
	return m, nil
}

// ensureRegionSelectOptions lazily asks AWS for the full enabled-region list so saving is not limited to startup regions.
func (m model) ensureRegionSelectOptions() model {
	if len(m.editRegionOptions) > 0 || m.client == nil {
		return m
	}
	regions, err := m.client.ListRegions(m.ctx)
	if err != nil {
		m.errMessage = err.Error()
		return m
	}
	if len(regions) > 0 {
		m.editRegionOptions = regions
	}
	return m
}

func (m model) regionSelectOptions() []string {
	var regions []string
	if len(m.editRegionOptions) > 0 {
		regions = append([]string(nil), m.editRegionOptions...)
	} else {
		regions = m.regionOptions()
	}
	sort.Strings(regions)
	return regions
}

func (m *model) openFileWriteConfirmation(kind fileWriteConfirmation) {
	m.pendingFileWrite = kind
	m.warningMessage = ""
	if m.activePopup == popupFileWriteConfirm {
		return
	}
	if m.activePopup != popupFileAction {
		m.activePopup = popupFileAction
	}
	m.pushNestedPopup(popupFileWriteConfirm)
}

// loadValueFromFile reads the path from the edit screen and replaces the active file-action field with that file content.
func (m model) loadValueFromFile() (tea.Model, tea.Cmd) {
	path := strings.TrimSpace(m.editFileInput.Value())
	if path == "" {
		m.errMessage = "File path is required."
		m.message = ""
		m.warningMessage = ""
		return m, nil
	}
	expandedPath, err := expandLocalPath(path)
	if err != nil {
		m.errMessage = err.Error()
		m.message = ""
		m.warningMessage = ""
		return m, nil
	}
	data, err := fileio.ReadFile(expandedPath)
	if err != nil {
		m.errMessage = err.Error()
		m.message = ""
		m.warningMessage = ""
		return m, nil
	}
	if m.fileActionField == editFieldPolicies {
		m.editPoliciesArea.SetValue(prettyPoliciesForEditor(string(data)))
		m = m.focusEditField(editFieldPolicies)
		m.message = "Loaded policies from " + path
	} else {
		m.textArea.SetValue(string(data))
		m = m.focusEditField(editFieldValue)
		m.message = "Loaded value from " + path
	}
	m.errMessage = ""
	m.warningMessage = ""
	return m, nil
}

// writeValueToFile writes the current active file-action field to the path from the edit screen.
// SecureString value writes and overwrite operations require explicit y confirmation to reduce accidental local writes.
func (m model) writeValueToFile(secureConfirmed, overwriteConfirmed bool) (tea.Model, tea.Cmd) {
	path := strings.TrimSpace(m.editFileInput.Value())
	if path == "" {
		m.errMessage = "File path is required."
		m.message = ""
		m.warningMessage = ""
		m.pendingFileWrite = fileWriteConfirmationNone
		return m, nil
	}
	expandedPath, err := expandLocalPath(path)
	if err != nil {
		m.errMessage = err.Error()
		m.message = ""
		m.warningMessage = ""
		m.pendingFileWrite = fileWriteConfirmationNone
		return m, nil
	}
	if m.fileActionField != editFieldPolicies && m.normalizedEditType() == ssm.ParameterTypeSecureString && !secureConfirmed && !m.opts.NoConfirmWriteSecureValue {
		m.errMessage = ""
		m.message = ""
		m.warningMessage = ""
		m.openFileWriteConfirmation(fileWriteConfirmationSecure)
		return m, nil
	}
	if _, err := os.Stat(expandedPath); err == nil && !overwriteConfirmed && !m.opts.NoConfirmOverwriteFile {
		m.errMessage = ""
		m.message = ""
		m.warningMessage = ""
		m.openFileWriteConfirmation(fileWriteConfirmationOverwrite)
		return m, nil
	} else if err != nil && !os.IsNotExist(err) {
		m.errMessage = err.Error()
		m.message = ""
		m.warningMessage = ""
		m.pendingFileWrite = fileWriteConfirmationNone
		return m, nil
	}
	contents := m.fileActionContents()
	if err := fileio.WriteFile(expandedPath, []byte(contents), 0o600); err != nil {
		m.errMessage = err.Error()
		m.message = ""
		m.warningMessage = ""
		m.pendingFileWrite = fileWriteConfirmationNone
		return m, nil
	}
	m.errMessage = ""
	m.warningMessage = ""
	if m.fileActionField == editFieldPolicies {
		m.message = "Wrote policies to " + path
	} else {
		m.message = "Wrote value to " + path
	}
	m.pendingFileWrite = fileWriteConfirmationNone
	return m, nil
}

func (m model) fileActionContents() string {
	if m.fileActionField == editFieldPolicies {
		return m.editPoliciesArea.Value()
	}
	return m.textArea.Value()
}

// startTypeSelect opens the type picker and remembers which editor/preview screen should be restored afterwards.
func (m model) startTypeSelect(ret screen) (tea.Model, tea.Cmd) {
	m.typeReturnScreen = ret
	m.typeCursor = indexOfParameterType(parameterTypeItems(), m.normalizedEditType())
	m.pushPopup(popupTypeSelect)
	return m, nil
}

func (m model) startTierSelect(ret screen) (tea.Model, tea.Cmd) {
	m.typeReturnScreen = ret
	m.tierCursor = indexOfParameterTier(parameterTierItems(), m.normalizedEditTier())
	m.pushPopup(popupTierSelect)
	return m, nil
}

func (m model) startDataTypeSelect(ret screen) (tea.Model, tea.Cmd) {
	m.typeReturnScreen = ret
	m.dataTypeCursor = indexOfParameterDataType(parameterDataTypeItems(), m.normalizedEditDataType())
	m.pushPopup(popupDataTypeSelect)
	return m, nil
}

func (m model) startOverwriteSelect(ret screen) (tea.Model, tea.Cmd) {
	if !m.shouldShowOverwriteField() {
		return m.focusEditField(editFieldDescription), nil
	}
	m.typeReturnScreen = ret
	m.overwriteCursor = indexOfOverwrite(overwriteItems(), m.editOverwrite)
	m.pushPopup(popupOverwriteSelect)
	return m, nil
}

// startConfirm initializes a confirmation screen for one or more items.
func (m *model) startConfirm(prompt, expected string, items []inventory.Item, ret screen) {
	m.confirmPrompt = prompt
	m.confirmExpected = expected
	m.confirmItems = items
	m.returnScreen = ret
	m.input.SetValue("")
	m.input.Placeholder = ""
	m.input.Focus()
	m.errMessage = ""
	m.pushPopup(popupConfirm)
}

func (m model) startRandomFromPopup(kind string) (tea.Model, tea.Cmd) {
	if kind == "base64-custom" {
		m.fileActionMode = "random-custom"
		m.input.SetValue("32")
		m.input.Placeholder = ""
		m.input.Focus()
		m.pushPopup(popupFileAction)
		return m, nil
	}
	return m.generateRandomValueIntoEditor(kind)
}

func (m model) generateRandomValueIntoEditor(kind string) (tea.Model, tea.Cmd) {
	value, err := m.randomValue(kind)
	if err != nil {
		m.errMessage = err.Error()
		return m, nil
	}
	m.textArea.SetValue(value)
	m.screen = screenTextArea
	m = m.focusEditField(editFieldValue)
	m.message = "Random value inserted. Press Ctrl-s to save."
	m.errMessage = ""
	m.warningMessage = ""
	m.clearPopupStack()
	return m, nil
}

func (m model) randomValue(kind string) (string, error) {
	switch kind {
	case "base64-32":
		value, err := randomx.Base64(32)
		return value, crerr.Wrap(err, "generate base64 random value")
	case "hex-32":
		value, err := randomx.Hex(32)
		return value, crerr.Wrap(err, "generate hex random value")
	case "uuid":
		value, err := randomx.UUID()
		return value, crerr.Wrap(err, "generate UUID random value")
	case "base64-custom":
		n, err := strconv.Atoi(strings.TrimSpace(m.input.Value()))
		if err != nil || n <= 0 {
			return "", errors.New("invalid byte length")
		}
		value, err := randomx.Base64(n)
		return value, crerr.Wrap(err, "generate custom base64 random value")
	default:
		return "", errors.New("unknown random value generator")
	}
}

// saveValue captures the current item/region and switches to the loading screen while the save command runs.
func (m model) saveValue(value string) (tea.Model, tea.Cmd) {
	item := m.currentItem()
	oldPath := item.Path
	if m.screen == screenTextArea {
		newPath := strings.TrimSpace(m.editPathInput.Value())
		if newPath == "" {
			m.errMessage = "Name is required."
			m.message = ""
			return m, nil
		}
		item.Path = newPath
	}
	if value == "" {
		if !m.editNewParameter && !m.editorHasUnsavedChanges() {
			m.message = "No changes to save."
			m.errMessage = ""
			m.warningMessage = ""
			return m, nil
		}
		m.errMessage = "Value is required."
		m.message = ""
		m.warningMessage = ""
		return m, nil
	}
	if strings.TrimSpace(m.editRegion) == "" {
		m.errMessage = "Region is required."
		m.message = ""
		m.warningMessage = ""
		return m, nil
	}
	if !m.normalizedEditType().IsValid() {
		m.errMessage = "Type is required."
		m.message = ""
		m.warningMessage = ""
		return m, nil
	}
	if !m.normalizedEditTier().IsValid() {
		m.errMessage = "Tier is required."
		m.message = ""
		m.warningMessage = ""
		return m, nil
	}
	if !m.normalizedEditDataType().IsValid() {
		m.errMessage = "DataType is required."
		m.message = ""
		m.warningMessage = ""
		return m, nil
	}
	item.Region = m.editRegion
	policies := ""
	policiesSet := false
	if m.shouldShowPoliciesField() {
		rawPolicies := strings.TrimSpace(m.editPoliciesArea.Value())
		policies = normalizePoliciesForAWS(rawPolicies)
		if strings.TrimSpace(policies) == "[{}]" {
			policiesSet = true
		}
		if rawPolicies == "" && strings.TrimSpace(m.currentStatus().Policies) != "" {
			policies = "[{}]"
			policiesSet = true
		}
	}
	overwrite := true
	if m.shouldShowOverwriteField() {
		overwrite = m.editOverwrite
	}
	m.busyMessage = "Saving parameter..."
	m.loadingTitle = ""
	m.loadingLines = nil
	description := strings.TrimSpace(m.editDescriptionArea.Value())
	if description == "" {
		description = strings.TrimSpace(m.editDescriptionInput.Value())
	}
	return m, saveValueCmd(m.ctx, m.client, item, oldPath, value, m.normalizedEditType(), ssm.PutParameterOptions{Description: description, Tier: m.normalizedEditTier(), DataType: m.normalizedEditDataType(), Policies: policies, PoliciesSet: policiesSet, Overwrite: overwrite}, m.opts.NamesFile, m.opts.AllowNamesFileUpdate)
}

// saveValueCmd writes one SSM parameter to Parameter Store and reloads its fresh status for the UI.
// Wildcard items must be converted to a concrete region before saving, otherwise the command returns an inline error.
func saveValueCmd(ctx context.Context, client ssm.Client, item inventory.Item, oldPath, value string, parameterType ssm.ParameterType, opts ssm.PutParameterOptions, pathsFile string, allowNamesFileUpdate bool) tea.Cmd {
	return func() tea.Msg {
		if item.Region == "*" {
			return statusUpdatedMsg{path: item.Path, oldPath: oldPath, err: fmt.Errorf("cannot save %s without a concrete AWS region", item.Path)}
		}
		regionalClient := client.ForRegion(item.Region)
		if err := regionalClient.PutParameterWithOptions(ctx, item.Path, value, parameterType, opts); err != nil {
			return statusUpdatedMsg{path: item.Path, oldPath: oldPath, err: err}
		}
		appendedToNamesFile := false
		if pathsFile != "" && allowNamesFileUpdate {
			appended, err := inventory.AppendPathIfMissing(pathsFile, item.Path)
			if err != nil {
				st := LoadStatuses(ctx, regionalClient, []inventory.Item{item}, true)[0]
				return statusUpdatedMsg{path: item.Path, oldPath: oldPath, status: st, message: "Updated " + item.Path, warning: fmt.Sprintf("Could not append %s to %s: %v", item.Path, pathsFile, err)}
			}
			if appended {
				appendedToNamesFile = true
				item.Kind = "path-file"
				item.Source = pathsFile
			}
		}
		st := LoadStatuses(ctx, regionalClient, []inventory.Item{item}, true)[0]
		message := "Updated " + item.Path
		if appendedToNamesFile {
			message += " and added it to " + pathsFile
		}
		return statusUpdatedMsg{path: item.Path, oldPath: oldPath, status: st, message: message}
	}
}

// deleteCmd groups selected items by concrete region and deletes them from SSM.
// Wildcard missing rows are skipped because they do not represent a real parameter in one AWS region.
func deleteCmd(ctx context.Context, client ssm.Client, items []inventory.Item, pathsFile string, allowNamesFileUpdate bool) tea.Cmd {
	return func() tea.Msg {
		byRegion := map[string][]string{}
		for _, item := range items {
			if item.Region == "*" {
				continue
			}
			byRegion[item.Region] = append(byRegion[item.Region], item.Path)
		}
		for region, paths := range byRegion {
			if err := client.ForRegion(region).DeleteMany(ctx, paths); err != nil {
				return deleteDoneMsg{items: items, err: err}
			}
		}

		removeRows := pathsFile == ""
		if pathsFile != "" && allowNamesFileUpdate {
			if _, err := inventory.RemovePathsIfPresent(pathsFile, itemPaths(items)); err != nil {
				return deleteDoneMsg{items: items, warning: fmt.Sprintf("Could not update %s after delete: %v", pathsFile, err)}
			}
			removeRows = true
		}
		return deleteDoneMsg{items: items, removeRows: removeRows}
	}
}

// renderMainScreen composes the selected-parameter summary and the scrollable table of visible statuses.

func expandLocalPath(path string) (string, error) {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", crerr.Wrap(err, "resolve user home directory")
		}
		return home, nil
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", crerr.Wrap(err, "resolve user home directory")
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
	}
	return path, nil
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

func (m model) fieldAllowed(field string) bool {
	if field == "name" || len(m.opts.Fields) == 0 {
		return true
	}
	for _, candidate := range m.opts.Fields {
		if candidate == field {
			return true
		}
	}
	return false
}

func (m model) editFieldAllowed(field editField) bool {
	switch field {
	case editFieldFilePath:
		return true
	case editFieldSSMPath:
		return true
	case editFieldRegion:
		return m.fieldAllowed("region")
	case editFieldType:
		return m.fieldAllowed("type")
	case editFieldTier:
		return m.fieldAllowed("tier")
	case editFieldDataType:
		return m.fieldAllowed("data-type")
	case editFieldDescription:
		return m.fieldAllowed("description")
	case editFieldPolicies:
		return m.fieldAllowed("policies")
	case editFieldValue:
		return m.fieldAllowed("value")
	case editFieldOverwrite:
		return m.fieldAllowed("value")
	default:
		return true
	}
}

// randomItems returns supported random value generator choices.
func randomItems() []actionItem {
	return []actionItem{{"base64 32 bytes", "base64-32"}, {"hex 32 bytes", "hex-32"}, {"uuid", "uuid"}, {"custom length base64", "base64-custom"}}
}

// itemPaths extracts SSM names for loading/progress displays.
func itemPaths(items []inventory.Item) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Path)
	}
	return out
}

// parameterTypeItems returns the AWS SSM parameter types exposed in the TUI.
func parameterTypeItems() []parameterTypeItem {
	return []parameterTypeItem{
		{hotkey: "e", label: ssm.ParameterTypeSecureString.String(), value: ssm.ParameterTypeSecureString, description: "encrypted value; best default for secrets"},
		{hotkey: "s", label: ssm.ParameterTypeString.String(), value: ssm.ParameterTypeString, description: "plain text scalar value"},
		{hotkey: "l", label: ssm.ParameterTypeStringList.String(), value: ssm.ParameterTypeStringList, description: "comma-separated plain text list"},
	}
}

// parameterTierItems returns the AWS SSM parameter tiers exposed in the TUI.
func parameterTierItems() []parameterTierItem {
	return []parameterTierItem{
		{hotkey: "i", label: ssm.ParameterTierIntelligentTiering.String(), value: ssm.ParameterTierIntelligentTiering, description: "AWS chooses Standard or Advanced as needed"},
		{hotkey: "s", label: ssm.ParameterTierStandard.String(), value: ssm.ParameterTierStandard, description: "default tier for most parameters"},
		{hotkey: "a", label: ssm.ParameterTierAdvanced.String(), value: ssm.ParameterTierAdvanced, description: "larger values and higher parameter limits"},
	}
}

// parameterDataTypeItems returns AWS SSM parameter data types exposed in the TUI.
func parameterDataTypeItems() []parameterDataTypeItem {
	return []parameterDataTypeItem{
		{hotkey: "t", label: ssm.ParameterDataTypeText.String(), value: ssm.ParameterDataTypeText, description: "ordinary text; AWS default"},
		{hotkey: "a", label: ssm.ParameterDataTypeEC2Image.String(), value: ssm.ParameterDataTypeEC2Image, description: "validate that the value is an AMI id"},
		{hotkey: "i", label: ssm.ParameterDataTypeSSMIntegration.String(), value: ssm.ParameterDataTypeSSMIntegration, description: "for AWS SSM service integrations"},
	}
}

// overwriteItems returns the choices for AWS SSM --overwrite.
func overwriteItems() []overwriteItem {
	return []overwriteItem{
		{hotkey: "t", label: "true", value: true, description: "update the parameter if it already exists"},
		{hotkey: "f", label: "false", value: false, description: "let AWS return an error if it already exists"},
	}
}

// initialEditType chooses the type shown when opening an editor.
// Existing parameters preserve their AWS type, while missing/new parameters default to SecureString.
func (m model) initialEditType() ssm.ParameterType {
	current := m.currentStatus().Type
	if parameterType, err := ssm.ParseParameterType(current); err == nil {
		return parameterType
	}
	return ssm.DefaultParameterType
}

// normalizedEditType returns a valid parameter type even if edit state has not been initialized yet.
func (m model) normalizedEditType() ssm.ParameterType {
	if m.editType.IsValid() {
		return m.editType
	}
	return ssm.DefaultParameterType
}

func indexOfParameterType(items []parameterTypeItem, value ssm.ParameterType) int {
	for i, item := range items {
		if item.value == value {
			return i
		}
	}
	return 0
}

func parameterTypeIndexByHotkey(items []parameterTypeItem, key string) (int, bool) {
	for i, item := range items {
		if item.hotkey == key {
			return i, true
		}
	}
	return 0, false
}

func (m model) initialEditTier() ssm.ParameterTier {
	current := m.currentStatus().Tier
	if tier, err := ssm.ParseParameterTier(current); err == nil {
		return tier
	}
	return ssm.DefaultParameterTier
}

func (m model) normalizedEditTier() ssm.ParameterTier {
	if m.editTier.IsValid() {
		return m.editTier
	}
	return ssm.DefaultParameterTier
}

func (m model) shouldShowPoliciesField() bool {
	return m.editFieldAllowed(editFieldPolicies) && m.normalizedEditTier() == ssm.ParameterTierAdvanced
}

func (m model) shouldShowOverwriteField() bool {
	return m.editFieldAllowed(editFieldOverwrite) && (m.editNewParameter || !m.currentStatus().Exists)
}

func indexOfParameterTier(items []parameterTierItem, value ssm.ParameterTier) int {
	for i, item := range items {
		if item.value == value {
			return i
		}
	}
	return 0
}

func parameterTierIndexByHotkey(items []parameterTierItem, key string) (int, bool) {
	for i, item := range items {
		if item.hotkey == key {
			return i, true
		}
	}
	return 0, false
}

func (m model) initialEditDataType() ssm.ParameterDataType {
	current := m.currentStatus().DataType
	if dataType, err := ssm.ParseParameterDataType(current); err == nil {
		return dataType
	}
	return ssm.DefaultParameterDataType
}

func (m model) normalizedEditDataType() ssm.ParameterDataType {
	if m.editDataType.IsValid() {
		return m.editDataType
	}
	return ssm.DefaultParameterDataType
}

func indexOfParameterDataType(items []parameterDataTypeItem, value ssm.ParameterDataType) int {
	for i, item := range items {
		if item.value == value {
			return i
		}
	}
	return 0
}

func parameterDataTypeIndexByHotkey(items []parameterDataTypeItem, key string) (int, bool) {
	for i, item := range items {
		if item.hotkey == key {
			return i, true
		}
	}
	return 0, false
}

func indexOfOverwrite(items []overwriteItem, value bool) int {
	for i, item := range items {
		if item.value == value {
			return i
		}
	}
	return 0
}

func overwriteIndexByHotkey(items []overwriteItem, key string) (int, bool) {
	for i, item := range items {
		if item.hotkey == key {
			return i, true
		}
	}
	return 0, false
}

// initialEditRegion chooses the default concrete region when editing a parameter.
// For wildcard rows it prefers the first configured region so saving never targets "*" accidentally.
func (m model) initialEditRegion() string {
	item := m.currentItem()
	if item.Region != "" && item.Region != "*" {
		return item.Region
	}
	regions := m.regionOptions()
	if len(regions) > 0 {
		return regions[0]
	}
	if m.opts.Region != "all regions" {
		return m.opts.Region
	}
	return ""
}

// regionOptions returns the concrete regions available for saving the current value.
func (m model) regionOptions() []string {
	if len(m.opts.Regions) > 0 {
		return append([]string(nil), m.opts.Regions...)
	}
	if m.opts.Region != "" && m.opts.Region != "all regions" && m.opts.Region != "-" {
		return []string{m.opts.Region}
	}
	return nil
}

func prettyPoliciesForEditor(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return raw
	}
	decoded = canonicalPoliciesForEditor(decoded)
	out, err := json.MarshalIndent(decoded, "", "  ")
	if err != nil {
		return raw
	}
	return string(out)
}

func normalizePoliciesForAWS(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return raw
	}
	decoded = canonicalPoliciesForEditor(decoded)
	out, err := json.Marshal(decoded)
	if err != nil {
		return raw
	}
	return string(out)
}

func canonicalPoliciesForEditor(value any) any {
	switch v := value.(type) {
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, canonicalPolicyItem(item))
		}
		return out
	default:
		return canonicalPolicyItem(v)
	}
}

func canonicalPolicyItem(value any) any {
	v, ok := value.(map[string]any)
	if !ok {
		return value
	}
	policyText, ok := v["PolicyText"]
	if !ok {
		return value
	}
	switch text := policyText.(type) {
	case string:
		var decoded any
		if err := json.Unmarshal([]byte(text), &decoded); err == nil {
			return canonicalPoliciesForEditor(decoded)
		}
	case map[string]any, []any:
		return canonicalPoliciesForEditor(text)
	}
	return value
}
