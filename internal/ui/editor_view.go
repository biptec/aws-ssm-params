package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
)

type editorViewComponent struct {
	model model
}

func (component editorViewComponent) renderTextAreaScreen() string {
	m := component.model
	title := "Edit Parameter"
	if m.editNewParameter || !m.currentStatus().Exists {
		title = "New Parameter"
	}
	labelWidth := 11
	lines := []string{m.editTextInputFieldLine(editFieldSSMPath, "Name", m.editPathInput, labelWidth)}
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

	preferredHeight := m.textAreaBodyHeight()
	innerHeight := max(1, preferredHeight-2)
	remaining := max(1, innerHeight-len(lines))

	if m.editFieldAllowed(editFieldDescription) {
		descriptionArea := m.editDescriptionArea
		if descriptionArea.Value() == "" && m.editDescriptionInput.Value() != "" {
			descriptionArea.SetValue(m.editDescriptionInput.Value())
		}
		descriptionLines := m.renderExpandableField(editFieldDescription, "Description", descriptionArea, labelWidth, max(1, remaining-1), m.hasVisibleFieldAfter(editFieldDescription))
		lines = append(lines, descriptionLines...)
		remaining = max(1, innerHeight-len(lines))
	}

	if m.shouldShowPoliciesField() {
		policyLines := m.renderExpandableField(editFieldPolicies, "Policies", m.editPoliciesArea, labelWidth, max(1, remaining-1), m.hasVisibleFieldAfter(editFieldPolicies))
		lines = append(lines, policyLines...)
		remaining = max(1, innerHeight-len(lines))
	}

	if m.shouldShowEncryptedEditPlaceholder() {
		lines = append(lines, m.editFieldLine(editFieldValue, "Value", m.encryptedPlaceholder(), labelWidth))
	} else if m.editFieldAllowed(editFieldValue) {
		valueLines := m.renderExpandableField(editFieldValue, "Value", m.textArea, labelWidth, max(1, remaining-1), false)
		lines = append(lines, valueLines...)
	}

	return m.renderBox(title, lines, preferredHeight)
}

func (component editorViewComponent) renderTextAreaValueLines(maxRows int) []string {
	m := component.model
	return m.renderMultilineFieldLines(editFieldValue, m.textArea, maxRows)
}

func (component editorViewComponent) renderExpandableField(field editField, label string, area textarea.Model, labelWidth, maxRows int, hasNext bool) []string {
	m := component.model
	if !m.shouldRenderExpandedField(field, area, labelWidth) {
		return []string{m.editFieldLine(field, label, m.singleLineAreaView(field, area, labelWidth), labelWidth)}
	}
	lines := []string{m.label(m.editFieldLabel(field, label) + ":")}
	lines = append(lines, m.renderMultilineFieldLines(field, area, maxRows)...)
	if hasNext {
		lines = append(lines, "")
	}
	return lines
}

func (component editorViewComponent) shouldRenderExpandedField(field editField, area textarea.Model, labelWidth int) bool {
	m := component.model
	if m.expandedFields[field] {
		return true
	}
	return !m.canRenderCompactValue(area.Value(), labelWidth)
}

func (component editorViewComponent) singleLineFieldWidth(labelWidth int) int {
	m := component.model
	labelText := padMin("", labelWidth+1)
	return max(1, m.boxInnerWidth()-lipgloss.Width(labelText)-3)
}

func (component editorViewComponent) singleLineAreaView(field editField, area textarea.Model, labelWidth int) string {
	m := component.model
	width := m.singleLineFieldWidth(labelWidth)
	value := strings.ReplaceAll(area.Value(), "\n", " ")
	focused := m.editField == field && area.Focused()
	if !focused {
		return m.value(truncateStyled(value, width))
	}
	_, offset := textAreaCursorLineOffset(area)
	return m.value(m.inputValueWithCursor(value, offset, width))
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
		return m.shouldRenderExpandedField(editFieldDescription, m.editDescriptionArea, 11)
	case editFieldPolicies:
		return m.shouldRenderExpandedField(editFieldPolicies, m.editPoliciesArea, 11)
	case editFieldValue:
		return m.shouldRenderExpandedField(editFieldValue, m.textArea, 11)
	default:
		return false
	}
}

func (component editorViewComponent) renderMultilineFieldLines(field editField, area textarea.Model, maxRows int) []string {
	m := component.model
	maxRows = max(1, maxRows)
	wrapWidth := m.multilineContentWidth()
	logicalLines, segments := multilineVisualSegments(area.Value(), wrapWidth)
	lineCount := max(1, len(logicalLines))
	lineNumberWidth := len(strconv.Itoa(lineCount))
	cursorLine := min(max(0, area.Line()), lineCount-1)
	lineInfo := area.LineInfo()
	cursorOffset := min(max(0, lineInfo.StartColumn+lineInfo.ColumnOffset), len([]rune(logicalLines[cursorLine])))
	focused := m.editField == field && area.Focused()
	cursorVisual := 0
	if focused {
		cursorVisual = cursorVisualSegmentIndex(logicalLines, segments, cursorLine, cursorOffset, wrapWidth)
	}

	type visualLine struct {
		text        string
		cursorOwner bool
	}
	visual := make([]visualLine, 0, lineCount)
	for visualIndex, segment := range segments {
		runes := []rune(logicalLines[segment.logical])
		piece := ""
		if segment.start < segment.end {
			piece = string(runes[segment.start:segment.end])
		}
		ownsCursor := focused && visualIndex == cursorVisual
		if ownsCursor {
			piece = m.withCursorMarker(piece, cursorOffset-segment.start)
		}
		prefix := ""
		if m.showGutters {
			prefix = fmt.Sprintf("%*d │ ", lineNumberWidth, segment.logical+1)
			if segment.start > 0 {
				prefix = fmt.Sprintf("%*s | ", lineNumberWidth, "")
			}
		}
		if !m.showGutters {
			piece = rawLeftLinePrefix + piece
		}
		visual = append(visual, visualLine{text: prefix + piece, cursorOwner: ownsCursor})
	}

	start := 0
	if len(visual) > maxRows {
		if focused {
			start = min(max(0, cursorVisual-maxRows+1), len(visual)-maxRows)
		}
	}
	end := min(len(visual), start+maxRows)
	lines := make([]string, 0, end-start)
	for _, line := range visual[start:end] {
		lines = append(lines, line.text)
	}
	return lines
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

func (component editorViewComponent) multilineContentWidth() int {
	m := component.model
	if !m.showGutters {
		return max(8, m.boxInnerWidth()-2)
	}
	lineNumberWidth := 4
	prefixWidth := lineNumberWidth + lipgloss.Width(" │ ")
	return max(8, m.boxInnerWidth()-prefixWidth-2)
}

func (component editorViewComponent) withCursorMarker(line string, offset int) string {
	m := component.model
	runes := []rune(line)
	offset = min(max(0, offset), len(runes))
	if offset == len(runes) {
		if m.opts.NoColor {
			return string(runes) + "█"
		}
		return string(runes) + cursorStyle.Render(" ")
	}
	if m.opts.NoColor {
		return string(runes[:offset]) + "█" + string(runes[offset+1:])
	}
	return string(runes[:offset]) + cursorStyle.Render(string(runes[offset])) + string(runes[offset+1:])
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
	return m.fieldLine(m.editFieldLabel(field, name), renderedValue, labelWidth)
}

func (component editorViewComponent) editTextInputFieldLine(field editField, name string, input textinput.Model, labelWidth int) string {
	m := component.model
	label := m.editFieldLabel(field, name)
	labelText := padMin(label+":", labelWidth+1)
	// Bubbles textinput renders the focused cursor as one visible cell in addition to
	// its configured width. Reserve that extra cell so the final styled line does not
	// overflow the box and lose ANSI styling during truncation.
	available := m.boxInnerWidth() - lipgloss.Width(labelText) - 2
	input.Width = max(1, available)
	return m.fieldLine(label, input.View(), labelWidth)
}

func (component editorViewComponent) editFieldLabel(field editField, name string) string {
	m := component.model
	if m.keymapStyle() == keymapVi && m.viInsertMode && m.editField == field && isEditableTextField(field) {
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
	if m.editField == field {
		value += " <"
	}
	return m.value(value)
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
	case editFieldSSMPath, editFieldRegion, editFieldType, editFieldTier, editFieldDataType, editFieldOverwrite, editFieldDescription, editFieldFilePath:
	case editFieldValue:
		valueAction = " • alt+e value actions"
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
			return "ctrl+/ help • ctrl+s save" + lineAction + valueAction + " • esc normal"
		}
		return "ctrl+/ help • i insert • ctrl+s save" + lineAction + valueAction + " • esc back"
	}
	suffix := " • esc back"
	switch m.editField {
	case editFieldValue, editFieldSSMPath, editFieldDescription, editFieldPolicies, editFieldFilePath:
		return "ctrl+/ help • ctrl+s save" + lineAction + valueAction + suffix
	case editFieldRegion:
		return "ctrl+/ help • enter choose region • ctrl+s save" + suffix
	case editFieldType:
		return "ctrl+/ help • enter choose type • ctrl+s save" + suffix
	case editFieldTier:
		return "ctrl+/ help • enter choose tier • ctrl+s save" + suffix
	case editFieldDataType:
		return "ctrl+/ help • enter choose data type • ctrl+s save" + suffix
	case editFieldOverwrite:
		return "ctrl+/ help • enter choose overwrite • ctrl+s save" + suffix
	default:
		return "ctrl+/ help • ctrl+s save" + lineAction + valueAction + suffix
	}
}
