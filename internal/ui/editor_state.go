package ui

import (
	"sort"

	"github.com/biptec/aws-ssm-params/internal/ssm"
	tea "github.com/charmbracelet/bubbletea"
)

type editorStateComponent struct {
	model model
}

// startMultiline opens the selected parameter value in the multiline editor.
func (component editorStateComponent) startMultiline() (tea.Model, tea.Cmd) {
	m := component.model
	m.returnScreen = screenMain
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
func (component editorStateComponent) startNewParameter(ret screen) (tea.Model, tea.Cmd) {
	m := component.model
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
func (component editorStateComponent) focusEditField(field editField) model {
	m := component.model
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
func (component *editorStateComponent) blurEditFields() {
	m := &component.model
	m.textArea.Blur()
	m.editPoliciesArea.Blur()
	m.editDescriptionArea.Blur()
	m.editPathInput.Blur()
	m.editDescriptionInput.Blur()
	m.editFileInput.Blur()
}

func (component editorStateComponent) requestEditorBack() (tea.Model, tea.Cmd) {
	m := component.model
	m.pendingFileWrite = fileWriteConfirmationNone

	m.warningMessage = ""
	if m.editorHasUnsavedChanges() {
		m.pushPopup(popupUnsavedChanges)
		return m, nil
	}

	m.discardEditorChanges()

	return m, nil
}

func (component *editorStateComponent) discardEditorChanges() {
	m := &component.model
	m.blurEditFields()
	m.pendingFileWrite = fileWriteConfirmationNone
	m.warningMessage = ""
	m.clearPopupStack()
	m.screen = m.returnScreen
}

func (component editorStateComponent) editorHasUnsavedChanges() bool {
	m := component.model
	if m.editInitialSnapshot.isZero() {
		return false
	}

	current := m.currentEditSnapshot()

	return !current.equal(&m.editInitialSnapshot)
}

func (component editorStateComponent) currentEditSnapshot() editSnapshot {
	m := component.model

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
func (component editorStateComponent) focusNextEditField() (tea.Model, tea.Cmd) {
	m := component.model
	return m.moveToEditField(m.nextEditField())
}

// focusPreviousEditField moves the edit-screen focus backwards in the visual field order.
func (component editorStateComponent) focusPreviousEditField() (tea.Model, tea.Cmd) {
	m := component.model
	return m.moveToEditField(m.previousEditField())
}

// moveToEditField moves focus through all edit fields without opening selector screens automatically.
func (component editorStateComponent) moveToEditField(field editField) (tea.Model, tea.Cmd) {
	m := component.model

	return m.focusEditField(field), nil
}

func (component editorStateComponent) editFieldOrder() []editField {
	m := component.model

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

func (component editorStateComponent) hasVisibleFieldAfter(field editField) bool {
	m := component.model
	fields := m.editFieldOrder()
	idx := indexOfEditField(fields, field)

	return idx >= 0 && idx < len(fields)-1
}

func (component editorStateComponent) nextEditField() editField {
	m := component.model
	fields := m.editFieldOrder()
	idx := indexOfEditField(fields, m.editField)

	return fields[nextCursor(idx, len(fields))]
}

func (component editorStateComponent) previousEditField() editField {
	m := component.model
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
func (component editorStateComponent) openRegionSelect() (tea.Model, tea.Cmd) {
	m := component.model
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
func (component editorStateComponent) ensureRegionSelectOptions() model {
	m := component.model
	if len(m.editRegionOptions) > 0 || m.client == nil {
		return m
	}

	regions, err := backendFor(m).listRegions(m.contextProvider())
	if err != nil {
		m.errMessage = err.Error()
		return m
	}

	if len(regions) > 0 {
		m.editRegionOptions = regions
	}

	return m
}

func (component editorStateComponent) regionSelectOptions() []string {
	m := component.model

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
func (component editorStateComponent) startTypeSelect(ret screen) (tea.Model, tea.Cmd) {
	m := component.model
	m.typeReturnScreen = ret
	m.typeCursor = parameterTypeItems().index(m.normalizedEditType())
	m.pushPopup(popupTypeSelect)

	return m, nil
}

func (component editorStateComponent) startTierSelect(ret screen) (tea.Model, tea.Cmd) {
	m := component.model
	m.typeReturnScreen = ret
	m.tierCursor = parameterTierItems().index(m.normalizedEditTier())
	m.pushPopup(popupTierSelect)

	return m, nil
}

func (component editorStateComponent) startDataTypeSelect(ret screen) (tea.Model, tea.Cmd) {
	m := component.model
	m.typeReturnScreen = ret
	m.dataTypeCursor = parameterDataTypeItems().index(m.normalizedEditDataType())
	m.pushPopup(popupDataTypeSelect)

	return m, nil
}

func (component editorStateComponent) startOverwriteSelect(ret screen) (tea.Model, tea.Cmd) {
	m := component.model
	if !m.shouldShowOverwriteField() {
		return m.focusEditField(editFieldDescription), nil
	}

	m.typeReturnScreen = ret
	m.overwriteCursor = overwriteItems().index(m.editOverwrite)
	m.pushPopup(popupOverwriteSelect)

	return m, nil
}
