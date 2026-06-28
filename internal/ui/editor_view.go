package ui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
)

type editorViewComponent struct {
	model model
}

type editorExpandableFieldView struct {
	field editField
	label string
	area  *textarea.Model
}

func (component editorViewComponent) renderTextAreaScreen() string {
	m := component.model

	title, lines := component.editorFormLines(m.textAreaBodyHeight() - 2)

	return m.renderBox(title, lines, m.textAreaBodyHeight())
}

func (component editorViewComponent) renderEditorPopup() string {
	m := component.model
	title, lines := component.editorFormLines(max(1, m.popupContentLineBudget() - 2))
	lines = append(lines, "", component.editorActionButtonsLine())

	return m.renderPopupBoxMinWidth(title, lines, editorPopupMinInnerWidth())
}

func (component editorViewComponent) editorFormLines(innerHeight int) (string, []string) {
	m := component.model

	title := "Edit Parameter"
	if m.editNewParameter || !m.currentStatus().Exists {
		title = "New Parameter"
	}

	labelWidth := 11

	lines := []string{m.editTextInputFieldLine(editFieldSSMPath, "Name", &m.editPathInput, labelWidth)}
	if m.editFieldAllowed(editFieldRegion) {
		lines = append(lines, m.editFieldLine(editFieldRegion, "Region", m.editOptionValue(editFieldRegion, valueOrDash(m.editRegion)), labelWidth))
	}

	if m.editFieldAllowed(editFieldType) {
		lines = append(lines, m.editFieldLine(editFieldType, "Type", m.editOptionValue(editFieldType, m.normalizedEditType().String()), labelWidth))
	}

	if m.editFieldAllowed(editFieldTier) {
		lines = append(lines, m.editFieldLine(editFieldTier, "Tier", m.editOptionValue(editFieldTier, m.normalizedEditTier().String()), labelWidth))
	}

	if m.editFieldAllowed(editFieldDataType) {
		lines = append(lines, m.editFieldLine(editFieldDataType, "DataType", m.editOptionValue(editFieldDataType, m.normalizedEditDataType().String()), labelWidth))
	}

	if m.shouldShowOverwriteField() {
		lines = append(lines, m.editFieldLine(editFieldOverwrite, "Overwrite", m.editOptionValue(editFieldOverwrite, strconv.FormatBool(m.editOverwrite)), labelWidth))
	}

	expandableFields := component.editorExpandableFieldViews()
	expandedFields := component.expandedEditorTextareaItems(expandableFields)

	fixedLines := len(lines) + len(expandableFields) + component.editorExpandableSeparatorCount(expandableFields)
	if m.shouldShowEncryptedEditPlaceholder() {
		fixedLines++
	}

	rowLimits := formTextareaRowLimits(expandedFields, max(1, innerHeight-fixedLines))

	for _, field := range expandableFields {
		maxRows := rowLimits[int(field.field)]
		if maxRows == 0 {
			maxRows = 1
		}

		lines = append(lines, m.renderExpandableField(field.field, field.label, field.area, labelWidth, maxRows, m.hasVisibleFieldAfter(field.field))...)
	}

	if m.shouldShowEncryptedEditPlaceholder() {
		lines = append(lines, m.editFieldLine(editFieldValue, "Value", m.encryptedPlaceholder(), labelWidth))
	}

	return title, lines
}

func (component editorViewComponent) editorExpandableFieldViews() []editorExpandableFieldView {
	m := component.model
	fields := make([]editorExpandableFieldView, 0, 3)

	if m.editFieldAllowed(editFieldDescription) {
		descriptionArea := m.editDescriptionArea
		fields = append(fields, editorExpandableFieldView{field: editFieldDescription, label: "Description", area: &descriptionArea})
	}

	if m.shouldShowPoliciesField() {
		fields = append(fields, editorExpandableFieldView{field: editFieldPolicies, label: "Policies", area: &m.editPoliciesArea})
	}

	if !m.shouldShowEncryptedEditPlaceholder() && m.editFieldAllowed(editFieldValue) {
		fields = append(fields, editorExpandableFieldView{field: editFieldValue, label: "Value", area: &m.textArea})
	}

	return fields
}

func (component editorViewComponent) expandedEditorTextareaItems(fields []editorExpandableFieldView) []formTextareaLayoutItem {
	m := component.model
	items := make([]formTextareaLayoutItem, 0, len(fields))

	for _, field := range fields {
		if field.area == nil || !m.shouldRenderExpandedField(field.field, field.area, 11) {
			continue
		}

		items = append(items, formTextareaLayoutItem{
			key:          int(field.field),
			area:         field.area,
			focused:      m.editField == field.field && field.area.Focused(),
			contentWidth: component.editorTextareaContentWidth(field.area),
		})
	}

	return items
}

func (component editorViewComponent) editorExpandableSeparatorCount(fields []editorExpandableFieldView) int {
	m := component.model
	count := 0

	for _, field := range fields {
		if field.area == nil || !m.shouldRenderExpandedField(field.field, field.area, 11) {
			continue
		}

		if m.hasVisibleFieldAfter(field.field) {
			count++
		}
	}

	return count
}

func (component editorViewComponent) renderTextAreaValueLines(maxRows int) []string {
	m := component.model
	return m.renderMultilineFieldLines(editFieldValue, &m.textArea, maxRows)
}

func (component editorViewComponent) renderExpandableField(field editField, label string, area *textarea.Model, labelWidth, maxRows int, hasNext bool) []string {
	m := component.model
	if !m.shouldRenderExpandedField(field, area, labelWidth) {
		return []string{m.editFieldLine(field, label, m.singleLineAreaView(field, area, labelWidth), labelWidth)}
	}

	lines := []string{m.formStandaloneLabel(m.editFieldLabel(field, label)+":", component.editorFieldFocused(field))}

	lines = append(lines, m.renderMultilineFieldLines(field, area, maxRows)...)
	if hasNext {
		lines = append(lines, "")
	}

	return lines
}

func (component editorViewComponent) shouldRenderExpandedField(field editField, area *textarea.Model, labelWidth int) bool {
	m := component.model
	if m.expandedFields[field] {
		return true
	}

	return !m.canRenderCompactValue(area.Value(), labelWidth)
}

func (component editorViewComponent) singleLineFieldWidth(labelWidth int) int {
	m := component.model
	labelText := padMin("", labelWidth+1)

	return max(1, m.editorLineWidth()-lipgloss.Width(labelText)-3)
}

func (component editorViewComponent) singleLineAreaView(field editField, area *textarea.Model, labelWidth int) string {
	m := component.model
	focused := component.editorFieldFocused(field) && area.Focused()

	return m.formSingleLineAreaView(area, focused, labelWidth, m.editorLineWidth())
}

func (component editorViewComponent) expandableFieldValue(field editField) string {
	m := component.model

	switch field {
	case editFieldSSMPath, editFieldRegion, editFieldType, editFieldTier, editFieldDataType, editFieldOverwrite, editFieldFilePath:
		return ""
	case editFieldDescription:
		return m.editDescriptionArea.Value()
	case editFieldPolicies:
		return m.editPoliciesArea.Value()
	case editFieldValue:
		return m.textArea.Value()
	default:
		return ""
	}
}

func (component *editorViewComponent) collapseExpandedFieldAfterEdit(field editField, before string) {
	m := &component.model
	if !isExpandableEditField(field) || m.expandedFields == nil || !m.expandedFields[field] {
		return
	}

	after := m.expandableFieldValue(field)
	if after == before {
		return
	}

	if m.canRenderCompactValue(after, 11) {
		delete(m.expandedFields, field)
	}
}

func (component editorViewComponent) canRenderCompactValue(value string, labelWidth int) bool {
	m := component.model

	if strings.Contains(value, "\n") {
		return false
	}

	return lipgloss.Width(value) <= m.singleLineFieldWidth(labelWidth)
}

func (component *editorViewComponent) expandCompactFieldIfNeeded() bool {
	m := &component.model
	if !isExpandableEditField(m.editField) || m.isCurrentExpandableFieldExpanded() {
		return false
	}

	if m.expandedFields == nil {
		m.expandedFields = map[editField]bool{}
	}

	m.expandedFields[m.editField] = true
	m.insertNewlineInActiveExpandableField()
	m.focusEditField(m.editField)

	return true
}

func (component *editorViewComponent) insertNewlineInActiveExpandableField() {
	m := &component.model
	if !isExpandableEditField(m.editField) {
		return
	}

	value := []rune(m.activeTextValue())
	pos := min(max(0, m.activeTextCursorAbs()), len(value))
	value = append(value[:pos], append([]rune{'\n'}, value[pos:]...)...)
	m.setActiveTextValueAndCursor(string(value), pos+1)
}

func (component editorViewComponent) isCurrentExpandableFieldExpanded() bool {
	m := component.model
	switch m.editField {
	case editFieldSSMPath, editFieldRegion, editFieldType, editFieldTier, editFieldDataType, editFieldOverwrite, editFieldFilePath:
		return false
	case editFieldDescription:
		return m.shouldRenderExpandedField(editFieldDescription, &m.editDescriptionArea, 11)
	case editFieldPolicies:
		return m.shouldRenderExpandedField(editFieldPolicies, &m.editPoliciesArea, 11)
	case editFieldValue:
		return m.shouldRenderExpandedField(editFieldValue, &m.textArea, 11)
	default:
		return false
	}
}

func (component editorViewComponent) renderMultilineFieldLines(field editField, area *textarea.Model, maxRows int) []string {
	m := component.model
	focused := component.editorFieldFocused(field) && area.Focused()

	return m.formMultilineAreaLines(area, maxRows, component.editorTextareaContentWidth(area), focused)
}

func (component editorViewComponent) editorTextareaMaxRows(field editField) int {
	m := component.model
	innerHeight := m.textAreaBodyHeight() - 2
	if m.editorPopupActiveOrStack() {
		innerHeight = max(1, m.popupContentLineBudget()-2)
	}

	fixedLines := 1
	if m.editFieldAllowed(editFieldRegion) {
		fixedLines++
	}
	if m.editFieldAllowed(editFieldType) {
		fixedLines++
	}
	if m.editFieldAllowed(editFieldTier) {
		fixedLines++
	}
	if m.editFieldAllowed(editFieldDataType) {
		fixedLines++
	}
	if m.shouldShowOverwriteField() {
		fixedLines++
	}

	expandableFields := component.editorExpandableFieldViews()
	expandedFields := component.expandedEditorTextareaItems(expandableFields)
	fixedLines += len(expandableFields) + component.editorExpandableSeparatorCount(expandableFields)
	if m.shouldShowEncryptedEditPlaceholder() {
		fixedLines++
	}

	rowLimits := formTextareaRowLimits(expandedFields, max(1, innerHeight-fixedLines))
	if rows := rowLimits[int(field)]; rows > 0 {
		return rows
	}

	return 1
}

type multilineVisualSegment struct {
	logical int
	start   int
	end     int
}

func multilineVisualSegments(value string, width int) ([]string, []multilineVisualSegment) {
	width = max(1, width)

	logicalLines := strings.Split(value, "\n")
	if len(logicalLines) == 0 {
		logicalLines = []string{""}
	}

	segments := make([]multilineVisualSegment, 0, len(logicalLines))
	for logicalIndex, line := range logicalLines {
		runes := []rune(line)
		if len(runes) == 0 {
			segments = append(segments, multilineVisualSegment{logical: logicalIndex})
			continue
		}

		for start := 0; start < len(runes); start += width {
			segments = append(segments, multilineVisualSegment{logical: logicalIndex, start: start, end: min(len(runes), start+width)})
		}
	}

	return logicalLines, segments
}

func cursorVisualSegmentIndex(lines []string, segments []multilineVisualSegment, cursorLine, cursorOffset, width int) int {
	if len(segments) == 0 {
		return 0
	}

	cursorLine = min(max(0, cursorLine), len(lines)-1)
	lineLen := len([]rune(lines[cursorLine]))
	cursorOffset = min(max(0, cursorOffset), lineLen)
	targetStart := 0

	if lineLen > 0 {
		if cursorOffset >= lineLen {
			targetStart = ((lineLen - 1) / max(1, width)) * max(1, width)
		} else {
			targetStart = (cursorOffset / max(1, width)) * max(1, width)
		}
	}

	for i, segment := range segments {
		if segment.logical == cursorLine && segment.start == targetStart {
			return i
		}
	}

	return 0
}

func (component editorViewComponent) textAreaBodyHeight() int {
	m := component.model
	if m.height <= 0 {
		return max(8, m.height-2)
	}

	bodyHeight := m.height

	return max(8, bodyHeight)
}

func (component editorViewComponent) editFieldLine(field editField, name, renderedValue string, labelWidth int) string {
	m := component.model
	return m.formFieldLine(m.editFieldLabel(field, name), renderedValue, labelWidth, component.editorFieldFocused(field))
}

func (component editorViewComponent) editTextInputFieldLine(field editField, name string, input *textinput.Model, labelWidth int) string {
	m := component.model
	label := m.editFieldLabel(field, name)
	invalid := field == editFieldSSMPath && !parameterNameIsValid(strings.TrimSpace(input.Value()))

	return m.formTextInputFieldLineWithValidation(label, input, labelWidth, m.editorLineWidth(), invalid)
}

func (component editorViewComponent) editorLineWidth() int {
	m := component.model
	if m.editorPopupActiveOrStack() {
		return component.editorPopupLineWidth()
	}

	return m.boxInnerWidth()
}

func (component editorViewComponent) editorPopupLineWidth() int {
	m := component.model
	labelWidth := 11
	valueWidth := importMinimumValueWidth(labelWidth)

	valueWidth = max(valueWidth, lipgloss.Width(m.editPathInput.Value())+1)
	if m.editFieldAllowed(editFieldRegion) {
		valueWidth = max(valueWidth, lipgloss.Width(valueOrDash(m.editRegion))+1)
	}
	if m.editFieldAllowed(editFieldType) {
		valueWidth = max(valueWidth, lipgloss.Width(m.normalizedEditType().String())+1)
	}
	if m.editFieldAllowed(editFieldTier) {
		valueWidth = max(valueWidth, lipgloss.Width(m.normalizedEditTier().String())+1)
	}
	if m.editFieldAllowed(editFieldDataType) {
		valueWidth = max(valueWidth, lipgloss.Width(m.normalizedEditDataType().String())+1)
	}
	if m.shouldShowOverwriteField() {
		valueWidth = max(valueWidth, lipgloss.Width(strconv.FormatBool(m.editOverwrite))+1)
	}

	lineWidth := max(editorPopupMinContentLineWidth(), importInputLineWidth(labelWidth, valueWidth))
	for _, area := range component.editorPopupTextareasForWidth() {
		areaWidth := formTextareaLogicalContentWidth(area, importMinimumValueWidth(labelWidth), m.popupAvailableLineWidth())
		if m.showGutters {
			areaWidth += formTextareaGutterWidth(area)
		}

		lineWidth = max(lineWidth, areaWidth)
	}

	return min(m.popupAvailableLineWidth(), lineWidth)
}

func (component editorViewComponent) editorPopupTextareasForWidth() []*textarea.Model {
	m := component.model
	areas := []*textarea.Model{}
	if m.editFieldAllowed(editFieldDescription) {
		areas = append(areas, &m.editDescriptionArea)
	}
	if m.shouldShowPoliciesField() {
		areas = append(areas, &m.editPoliciesArea)
	}
	if !m.shouldShowEncryptedEditPlaceholder() && m.editFieldAllowed(editFieldValue) {
		areas = append(areas, &m.textArea)
	}

	return areas
}

func (component editorViewComponent) editorTextareaContentWidth(area *textarea.Model) int {
	m := component.model
	if !m.editorPopupActiveOrStack() {
		return m.multilineContentWidth()
	}

	maxWidth := component.editorLineWidth()
	if m.showGutters {
		maxWidth = max(1, maxWidth-formTextareaGutterWidth(area))
	}

	return formTextareaLogicalContentWidth(area, importMinimumValueWidth(11), maxWidth)
}

func (component editorViewComponent) editorActionButtonsLine() string {
	m := component.model
	return m.formActionButtonsLine("Save", m.activePopup == popupEditor && m.editorButtonsFocused, m.editorButtonCursor)
}

func editorPopupMinInnerWidth() int {
	return max(72, importPopupMinInnerWidth(11))
}

func editorPopupMinContentLineWidth() int {
	return max(1, editorPopupMinInnerWidth()-4)
}

func (component editorViewComponent) editFieldLabel(field editField, name string) string {
	m := component.model
	if m.keymapStyle() == keymapVi && m.viInsertMode && component.editorFieldFocused(field) && isEditableTextField(field) {
		return name + " [INSERT]"
	}

	return name
}

func isEditableTextField(field editField) bool {
	return field == editFieldSSMPath || field == editFieldDescription || field == editFieldFilePath || field == editFieldPolicies || field == editFieldValue
}

func isMultilineEditField(field editField) bool {
	return field == editFieldDescription || field == editFieldPolicies || field == editFieldValue
}

func isExpandableEditField(field editField) bool {
	return isMultilineEditField(field)
}

func (component editorViewComponent) shouldTypePrintableQInEditField() bool {
	m := component.model
	if !isEditableTextField(m.editField) {
		return false
	}

	return m.keymapStyle() == keymapEmacs || m.viInsertMode
}

func (component editorViewComponent) editOptionValue(field editField, value string) string {
	m := component.model
	return m.formOptionValue(component.editorFieldFocused(field), value)
}

func (component editorViewComponent) editorFieldFocused(field editField) bool {
	m := component.model
	if m.editorButtonsFocused || m.editField != field {
		return false
	}

	return m.activePopup == popupEditor || !m.editorPopupActiveOrStack()
}

func (component *editorViewComponent) moveActiveMultilinePage(direction int) {
	m := &component.model

	height := m.textArea.Height()
	switch m.editField {
	case editFieldValue, editFieldSSMPath, editFieldRegion, editFieldType, editFieldTier, editFieldDataType, editFieldOverwrite, editFieldFilePath:
	case editFieldDescription:
		height = m.editDescriptionArea.Height()
	case editFieldPolicies:
		height = m.editPoliciesArea.Height()

	default:
	}

	for i := 0; i < pageSize(height); i++ {
		m.moveActiveTextLine(direction)
	}
}

// textAreaFooterText includes region-switching shortcut help only when multiple concrete regions are available.
func (component editorViewComponent) textAreaFooterText() string {
	m := component.model
	valueAction := ""

	switch m.editField {
	case editFieldSSMPath, editFieldRegion, editFieldType, editFieldTier, editFieldDataType, editFieldOverwrite, editFieldFilePath:
	case editFieldValue:
		valueAction = " • alt+e value actions"
	case editFieldDescription:
		valueAction = " • alt+e description actions"
	case editFieldPolicies:
		valueAction = " • alt+e policies actions"

	default:
	}

	lineAction := ""
	if isExpandableEditField(m.editField) {
		lineAction = " • ctrl+l lines"
	}

	if m.usesViEditMode() {
		if m.viInsertMode {
			return "ctrl+/ help • " + primaryActionHelp() + " save" + lineAction + valueAction + " • esc normal"
		}

		return "ctrl+/ help • i insert • " + primaryActionHelp() + " save" + lineAction + valueAction + " • esc back"
	}

	suffix := " • esc back"

	switch m.editField {
	case editFieldValue, editFieldSSMPath, editFieldDescription, editFieldPolicies, editFieldFilePath:
		return "ctrl+/ help • " + primaryActionHelp() + " save" + lineAction + valueAction + suffix
	case editFieldRegion:
		return "ctrl+/ help • enter choose region • " + primaryActionHelp() + " save" + suffix
	case editFieldType:
		return "ctrl+/ help • enter choose type • " + primaryActionHelp() + " save" + suffix
	case editFieldTier:
		return "ctrl+/ help • enter choose tier • " + primaryActionHelp() + " save" + suffix
	case editFieldDataType:
		return "ctrl+/ help • enter choose data type • " + primaryActionHelp() + " save" + suffix
	case editFieldOverwrite:
		return "ctrl+/ help • enter choose overwrite • " + primaryActionHelp() + " save" + suffix
	default:
		return "ctrl+/ help • " + primaryActionHelp() + " save" + lineAction + valueAction + suffix
	}
}
