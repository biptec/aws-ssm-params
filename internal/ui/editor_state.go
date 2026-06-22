package ui

import (
	"sort"

	"github.com/biptec/aws-ssm-params/internal/ssm"
	tea "github.com/charmbracelet/bubbletea"
)

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
