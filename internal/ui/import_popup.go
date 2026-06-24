package ui

import (
	"strings"
	"time"

	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/biptec/aws-ssm-params/internal/textio"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type importState struct {
	importFilePathInput textinput.Model

	importMainCursor      int
	importKeyFieldCursor  int
	importFormatCursor    int
	importMapFieldsCursor int
	importMapPathsCursor  int
	importDefaultsCursor  int

	importFormat   string
	importKeyField string

	importMapFieldInputs []textinput.Model
	importMapPathRows    []importMapPathRow

	importDefaultRegion      string
	importDefaultType        ssm.ParameterType
	importDefaultTier        ssm.ParameterTier
	importDefaultDataType    ssm.ParameterDataType
	importDefaultPolicies    textarea.Model
	importDefaultDescription textarea.Model
	importDefaultsAnimation  importDefaultsRowAnimation
}

type importMapPathRow struct {
	awsPath  textinput.Model
	filePath textinput.Model
}

type importDefaultsRowAnimation struct {
	active bool
	id     int
	frame  int
	frames int
	from   map[int]int
	to     map[int]int
}

type importDefaultsAnimationTickMsg struct {
	id int
}

type importMainField int

const (
	importMainFieldFilePath importMainField = iota
	importMainFieldKeyField
	importMainFieldFormat
	importMainFieldMapFields
	importMainFieldMapPaths
	importMainFieldDefaults
	importMainFieldsCount
)

const (
	importFormatDotenv = "dotenv"
	importFormatJSON   = "json"
	importFormatYAML   = "yaml"
)

const (
	importMainLabelWidth     = 14
	importDefaultsLabelWidth = 11
	importEmptySummary       = "empty"
	importParentCompactWidth = importMainLabelWidth + 3 + 18
)

const (
	importDefaultsAnimationFrames   = 5
	importDefaultsAnimationInterval = 80 * time.Millisecond
)

var importFormatOptions = []string{importFormatDotenv, importFormatJSON, importFormatYAML}

var importKeyFieldOptions = []string{
	"",
	textio.FieldName,
	textio.FieldRegion,
	textio.FieldType,
	textio.FieldTier,
	textio.FieldDataType,
	textio.FieldPolicies,
	textio.FieldDescription,
	textio.FieldValue,
	textio.FieldDate,
	textio.FieldVersion,
	textio.FieldLen,
	textio.FieldSHA256,
	textio.FieldUser,
}

var importMapFieldLabels = []string{
	"Name",
	"Value",
	"Type",
	"Region",
	"DataType",
	"Date",
	"Version",
	"Tier",
	"Len",
	"SHA256",
	"User",
	"Description",
}

var importMapFieldKeys = []string{
	textio.FieldName,
	textio.FieldValue,
	textio.FieldType,
	textio.FieldRegion,
	textio.FieldDataType,
	textio.FieldDate,
	textio.FieldVersion,
	textio.FieldTier,
	textio.FieldLen,
	textio.FieldSHA256,
	textio.FieldUser,
	textio.FieldDescription,
}

func newImportState(opts *Options) importState {
	state := importState{
		importFilePathInput:      newImportTextInput(opts),
		importFormat:             importFormatDotenv,
		importDefaultRegion:      "",
		importDefaultType:        "",
		importDefaultTier:        "",
		importDefaultDataType:    "",
		importDefaultPolicies:    newImportTextArea(opts),
		importDefaultDescription: newImportTextArea(opts),
	}

	state.importMapFieldInputs = make([]textinput.Model, len(importMapFieldLabels))
	for i := range state.importMapFieldInputs {
		state.importMapFieldInputs[i] = newImportTextInput(opts)
	}

	state.importMapPathRows = []importMapPathRow{newImportMapPathRow(opts)}
	state.focusImportMain()

	return state
}

func newImportTextInput(opts *Options) textinput.Model {
	input := textinput.New()
	input.Prompt = ""
	input.CharLimit = 0
	input.Width = 80
	configureTextInputStyles(&input, opts)

	return input
}

func newImportTextArea(opts *Options) textarea.Model {
	area := textarea.New()
	area.Prompt = ""
	area.CharLimit = 0
	area.MaxHeight = 0
	area.ShowLineNumbers = false

	if opts != nil && !opts.NoColor {
		area.FocusedStyle.Base = valueStyle
		area.BlurredStyle.Base = valueStyle
		area.Cursor.Style = valueStyle
	}

	return area
}

func newImportMapPathRow(opts *Options) importMapPathRow {
	return importMapPathRow{
		awsPath:  newImportTextInput(opts),
		filePath: newImportTextInput(opts),
	}
}

func (m *model) openImportPopup() {
	state := m.importState
	if state.importFilePathInput.Width == 0 {
		state = newImportState(&m.opts)
	}

	state.importMainCursor = int(importMainFieldFilePath)
	state.focusImportMain()
	m.importState = state
	m.pushPopup(popupImportFile)
}

func (state *importState) blurImportInputs() {
	state.importFilePathInput.Blur()

	for i := range state.importMapFieldInputs {
		state.importMapFieldInputs[i].Blur()
	}

	for i := range state.importMapPathRows {
		state.importMapPathRows[i].awsPath.Blur()
		state.importMapPathRows[i].filePath.Blur()
	}

	state.importDefaultPolicies.Blur()
	state.importDefaultDescription.Blur()
}

func (state *importState) focusImportMain() {
	state.blurImportInputs()

	switch importMainField(state.importMainCursor) {
	case importMainFieldFilePath:
		state.importFilePathInput.Focus()
	case importMainFieldKeyField,
		importMainFieldFormat,
		importMainFieldMapFields,
		importMainFieldMapPaths,
		importMainFieldDefaults,
		importMainFieldsCount:
	}
}

func (state *importState) focusImportMapField() {
	state.blurImportInputs()

	if len(state.importMapFieldInputs) == 0 {
		return
	}

	state.importMapFieldsCursor = min(max(0, state.importMapFieldsCursor), len(state.importMapFieldInputs)-1)
	state.importMapFieldInputs[state.importMapFieldsCursor].Focus()
}

func (state *importState) focusImportMapPath() {
	state.blurImportInputs()
	state.normalizeMapPathRows(nil)

	if len(state.importMapPathRows) == 0 {
		return
	}

	maxCursor := len(state.importMapPathRows)*2 - 1
	state.importMapPathsCursor = min(max(0, state.importMapPathsCursor), maxCursor)

	row, side := state.importMapPathCursorPosition()
	if side == 0 {
		state.importMapPathRows[row].awsPath.Focus()
	} else {
		state.importMapPathRows[row].filePath.Focus()
	}
}

func (state *importState) focusImportDefaults() {
	state.blurImportInputs()

	switch state.importDefaultsCursor {
	case 4:
		state.importDefaultPolicies.Focus()
	case 5:
		state.importDefaultDescription.Focus()
	default:
	}
}

func (state *importState) ensureTrailingMapPathRow(opts *Options) {
	if len(state.importMapPathRows) == 0 {
		state.importMapPathRows = append(state.importMapPathRows, newImportMapPathRow(opts))
		return
	}

	last := &state.importMapPathRows[len(state.importMapPathRows)-1]
	if strings.TrimSpace(last.awsPath.Value()) == "" && strings.TrimSpace(last.filePath.Value()) == "" {
		return
	}

	state.importMapPathRows = append(state.importMapPathRows, newImportMapPathRow(opts))
}

func (state *importState) normalizeMapPathRows(opts *Options) {
	if len(state.importMapPathRows) == 0 {
		state.importMapPathRows = append(state.importMapPathRows, newImportMapPathRow(opts))
		return
	}

	for len(state.importMapPathRows) > 1 {
		last := &state.importMapPathRows[len(state.importMapPathRows)-1]

		previous := &state.importMapPathRows[len(state.importMapPathRows)-2]
		if !importMapPathRowEmpty(last) || !importMapPathRowEmpty(previous) {
			break
		}

		state.importMapPathRows = state.importMapPathRows[:len(state.importMapPathRows)-1]
	}

	state.ensureTrailingMapPathRow(opts)

	maxCursor := len(state.importMapPathRows)*2 - 1
	state.importMapPathsCursor = min(max(0, state.importMapPathsCursor), maxCursor)
}

func importMapPathRowEmpty(row *importMapPathRow) bool {
	return row == nil || strings.TrimSpace(row.awsPath.Value()) == "" && strings.TrimSpace(row.filePath.Value()) == ""
}

func (state *importState) importMapPathCursorPosition() (row, side int) {
	if len(state.importMapPathRows) == 0 {
		return 0, 0
	}

	cursor := min(max(0, state.importMapPathsCursor), len(state.importMapPathRows)*2-1)

	return cursor / 2, cursor % 2
}

func (component popupViewComponent) renderImportFilePopup() string {
	m := component.model
	innerWidth := m.importParentTextInputLineWidth(importMainLabelWidth, m.importFilePathInput.Value(), 18)
	lines := []string{
		m.importMainTextInputLine("File path", &m.importFilePathInput, innerWidth),
		"",
		m.importChoiceLine("Key field", m.importKeyFieldDisplay(), int(importMainFieldKeyField)),
		m.importChoiceLine("Format", m.importFormat, int(importMainFieldFormat)),
		m.importChoiceLine("Map fields", m.importMapFieldsSummary(), int(importMainFieldMapFields)),
		m.importChoiceLine("Map paths", m.importMapPathsSummary(), int(importMainFieldMapPaths)),
		m.importChoiceLine("Defaults", m.importDefaultsSummary(), int(importMainFieldDefaults)),
	}

	return m.renderPopupBoxWithActions("Import from file", lines, m.importFileActions())
}

func (component popupViewComponent) renderImportFormatPopup() string {
	m := component.model

	lines := make([]string, 0, len(importFormatOptions))
	for i, option := range importFormatOptions {
		lines = append(lines, m.singleSelectLine(option, i == m.importFormatCursor, i == m.importFormatCursor))
	}

	return m.renderPopupBoxWithActions("Format", lines, "Enter select   Esc cancel")
}

func (component popupViewComponent) renderImportKeyFieldPopup() string {
	m := component.model

	lines := make([]string, 0, len(importKeyFieldOptions))
	for i, option := range importKeyFieldOptions {
		label := option
		if label == "" {
			label = "none"
		}

		lines = append(lines, m.singleSelectLine(label, i == m.importKeyFieldCursor, i == m.importKeyFieldCursor))
	}

	return m.renderPopupBoxWithActions("Key field", lines, "Enter select   Esc cancel")
}

func (component popupViewComponent) renderImportMapFieldsPopup() string {
	m := component.model
	labelWidth := maxStringWidth(importMapFieldLabels)
	innerWidth := m.importMapFieldsLineWidth(labelWidth)

	lines := make([]string, 0, len(importMapFieldLabels))
	for i, label := range importMapFieldLabels {
		lines = append(lines, m.formTextInputFieldLine(label, &m.importMapFieldInputs[i], labelWidth, innerWidth))
	}

	return m.renderPopupBoxWithActions("Map fields", lines, "Enter apply   Esc cancel")
}

func (component popupViewComponent) renderImportMapPathsPopup() string {
	m := component.model
	m.normalizeMapPathRows(&m.opts)

	inputWidth := m.importMapPathInputWidth()

	lines := make([]string, 0, len(m.importMapPathRows))
	for i := range m.importMapPathRows {
		row := &m.importMapPathRows[i]
		left := m.formInputValue(&row.awsPath, inputWidth)
		right := m.formInputValue(&row.filePath, inputWidth)
		lines = append(lines, left+" : "+right)
	}

	return m.renderPopupBoxWithActions("Map paths", lines, "Enter apply   Esc cancel")
}

func (component popupViewComponent) renderImportDefaultsPopup() string {
	m := component.model
	rowLimits := m.importDefaultTextareaRowLimits()
	lines := make([]string, 0, 8)
	lines = append(
		lines,
		m.importDefaultOptionLine("Region", m.importDefaultRegionDisplay(), 0),
		m.importDefaultOptionLine("Type", m.importDefaultTypeDisplay(), 1),
		m.importDefaultOptionLine("Tier", m.importDefaultTierDisplay(), 2),
		m.importDefaultOptionLine("DataType", m.importDefaultDataTypeDisplay(), 3),
	)

	lines = append(lines, m.importDefaultAreaLines("Policies", &m.importDefaultPolicies, 4, rowLimits[4])...)
	lines = append(lines, m.importDefaultAreaLines("Description", &m.importDefaultDescription, 5, rowLimits[5])...)

	return m.renderPopupBoxWithActionsMinWidth("Defaults", lines, m.importDefaultsActions(), m.importDefaultsMinInnerWidth())
}

func (m model) importChoiceLine(label, value string, cursor int) string {
	focused := m.importMainCursor == cursor

	renderedValue := m.importMainSummaryValue(m.importSummaryValue(value, focused))
	if focused {
		renderedValue += " " + m.focusMarker("<")
	}

	return m.importMainFieldLine(label, renderedValue)
}

func (m model) importSummaryValue(value string, focused bool) string {
	valueWidth := max(1, m.importParentLineWidth()-importMainLabelWidth-2)
	if focused {
		valueWidth = max(1, valueWidth-lipgloss.Width(" <"))
	}

	return truncateInline(value, valueWidth)
}

func (m model) importMainTextInputLine(label string, input *textinput.Model, innerWidth int) string {
	labelText := padMin(label+":", importMainLabelWidth+1)
	available := innerWidth - lipgloss.Width(labelText) - 2
	input.Width = max(1, available)
	input.SetCursor(input.Position())

	return m.importMainFieldLine(label, input.View())
}

func (m model) importMainFieldLine(label, renderedValue string) string {
	labelText := padMin(label+":", importMainLabelWidth+1)

	return m.importMainLabel(labelText) + " " + renderedValue
}

func (m model) importMainLabel(value string) string {
	return m.label(value)
}

func (m model) importMainSummaryValue(value string) string {
	if m.opts.NoColor {
		return value
	}

	if !strings.Contains(value, ":") {
		return m.value(value)
	}

	parts := strings.Split(value, ";")
	out := strings.Builder{}

	for i, part := range parts {
		if i > 0 {
			out.WriteString(m.muted(";"))
		}

		left, right, ok := strings.Cut(part, ":")
		if !ok {
			out.WriteString(m.value(part))
			continue
		}

		out.WriteString(m.value(left))
		out.WriteString(m.muted(":" + right))
	}

	return out.String()
}

func (m model) importMapFieldsSummary() string {
	parts := make([]string, 0, len(m.importMapFieldInputs))
	for i := range m.importMapFieldInputs {
		value := strings.TrimSpace(m.importMapFieldInputs[i].Value())
		if value == "" {
			continue
		}

		parts = append(parts, importMapFieldKeys[i]+":"+value)
	}

	return importSummaryOrEmpty(parts)
}

func (m model) importMapPathsSummary() string {
	m.normalizeMapPathRows(&m.opts)

	parts := make([]string, 0, len(m.importMapPathRows))
	for i := range m.importMapPathRows {
		row := &m.importMapPathRows[i]
		awsPath := strings.TrimSpace(row.awsPath.Value())

		filePath := strings.TrimSpace(row.filePath.Value())
		if awsPath == "" && filePath == "" {
			continue
		}

		parts = append(parts, awsPath+":"+filePath)
	}

	return importSummaryOrEmpty(parts)
}

func (m model) importDefaultsSummary() string {
	parts := make([]string, 0, 6)
	if m.importDefaultRegion != "" {
		parts = append(parts, textio.FieldRegion+":"+m.importDefaultRegion)
	}

	if m.importDefaultType.IsValid() {
		parts = append(parts, textio.FieldType+":"+m.importDefaultType.String())
	}

	if m.importDefaultTier.IsValid() {
		parts = append(parts, textio.FieldTier+":"+m.importDefaultTier.String())
	}

	if m.importDefaultDataType.IsValid() {
		parts = append(parts, textio.FieldDataType+":"+m.importDefaultDataType.String())
	}

	if strings.TrimSpace(m.importDefaultPolicies.Value()) != "" {
		parts = append(parts, textio.FieldPolicies+":"+oneLineImportSummary(m.importDefaultPolicies.Value()))
	}

	if strings.TrimSpace(m.importDefaultDescription.Value()) != "" {
		parts = append(parts, textio.FieldDescription+":"+oneLineImportSummary(m.importDefaultDescription.Value()))
	}

	return importSummaryOrEmpty(parts)
}

func importSummaryOrEmpty(parts []string) string {
	if len(parts) == 0 {
		return importEmptySummary
	}

	return strings.Join(parts, ";")
}

func oneLineImportSummary(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func (m model) importFileActions() string {
	if importMainField(m.importMainCursor) == importMainFieldFilePath {
		return "Enter load   Esc cancel"
	}

	return "Enter open   Esc cancel"
}

func (m model) importDefaultsActions() string {
	switch {
	case m.importDefaultsCursor >= 0 && m.importDefaultsCursor <= 3:
		return "Enter select   Esc cancel"
	case m.importDefaultsCursor == 4 || m.importDefaultsCursor == 5:
		if m.importDefaultAreaExpanded(m.importFocusedDefaultArea()) {
			return "Enter newline   Esc cancel"
		}

		return "Enter expand/newline   Esc cancel"
	default:
		return "Enter apply   Esc cancel"
	}
}

func (m model) importFocusedDefaultArea() *textarea.Model {
	switch m.importDefaultsCursor {
	case 4:
		return &m.importDefaultPolicies
	case 5:
		return &m.importDefaultDescription
	default:
		return nil
	}
}

func (m model) importDefaultOptionLine(label, value string, cursor int) string {
	return m.fieldLine(label, m.formOptionValue(m.importDefaultsCursor == cursor, value), importDefaultsLabelWidth)
}

func (m model) importDefaultAreaLines(label string, area *textarea.Model, cursor, maxRows int) []string {
	focused := m.importDefaultsCursor == cursor && area.Focused()
	if m.importDefaultAreaExpanded(area) {
		lines := make([]string, 0, 6)
		lines = append(lines, m.label(label+":"))
		contentWidth := m.importDefaultTextareaContentWidth(area)

		lines = append(lines, m.formMultilineAreaLines(area, max(1, maxRows), contentWidth, focused)...)

		return lines
	}

	innerWidth := m.importAreaLineWidth(importDefaultsLabelWidth, area.Value(), 18)
	value := m.formSingleLineAreaView(area, focused, importDefaultsLabelWidth, innerWidth)

	return []string{m.fieldLine(label, value, importDefaultsLabelWidth)}
}

func (m model) importDefaultTextareaRowLimits() map[int]int {
	target := m.importDefaultTextareaTargetRowLimits()
	if !m.importDefaultsAnimation.active {
		return target
	}

	if !importDefaultRowLimitsEqual(target, m.importDefaultsAnimation.to) {
		return target
	}

	return m.importDefaultsAnimation.rowLimits(target)
}

func (m model) importDefaultTextareaTargetRowLimits() map[int]int {
	items := make([]formTextareaLayoutItem, 0, 2)
	fixedLines := 4

	m.addImportDefaultTextareaLayoutItem(&items, &fixedLines, 4, &m.importDefaultPolicies)
	m.addImportDefaultTextareaLayoutItem(&items, &fixedLines, 5, &m.importDefaultDescription)

	rowBudget := max(1, m.popupContentLineBudget()-fixedLines)

	return formTextareaRowLimits(items, rowBudget)
}

func (animation importDefaultsRowAnimation) rowLimits(target map[int]int) map[int]int {
	limits := copyImportDefaultRowLimits(target)
	if !animation.active || animation.frames <= 0 {
		return limits
	}

	for _, key := range []int{4, 5} {
		fromRows := animation.from[key]
		toRows := animation.to[key]

		switch {
		case fromRows <= 0 && toRows <= 0:
			continue
		case fromRows <= 0:
			fromRows = toRows
		case toRows <= 0:
			toRows = fromRows
		}

		limits[key] = interpolateImportDefaultRows(fromRows, toRows, animation.frame, animation.frames)
	}

	return limits
}

func interpolateImportDefaultRows(from, to, frame, frames int) int {
	frame = min(max(0, frame), max(1, frames))

	diff := to - from
	if diff >= 0 {
		return from + (diff*frame+frames/2)/frames
	}

	return from - ((-diff*frame + frames/2) / frames)
}

func importDefaultRowLimitsEqual(left, right map[int]int) bool {
	for _, key := range []int{4, 5} {
		if left[key] != right[key] {
			return false
		}
	}

	return true
}

func copyImportDefaultRowLimits(limits map[int]int) map[int]int {
	out := make(map[int]int, len(limits))
	for key, value := range limits {
		out[key] = value
	}

	return out
}

func (m model) addImportDefaultTextareaLayoutItem(items *[]formTextareaLayoutItem, fixedLines *int, cursor int, area *textarea.Model) {
	if !m.importDefaultAreaExpanded(area) {
		(*fixedLines)++
		return
	}

	(*fixedLines)++

	*items = append(*items, formTextareaLayoutItem{
		key:          cursor,
		area:         area,
		focused:      m.importDefaultsCursor == cursor && area.Focused(),
		contentWidth: m.importDefaultTextareaContentWidth(area),
	})
}

func (m model) popupContentLineBudget() int {
	if m.height <= 0 {
		return 20
	}

	return max(1, m.height-6)
}

func (m model) popupAvailableLineWidth() int {
	available := m.boxInnerWidth() - 12
	if available <= 0 {
		return 20
	}

	return max(20, available)
}

func (m model) importParentLineWidth() int {
	if m.importFilePopupIsParentLayer() {
		return importParentCompactWidth
	}

	return m.popupAvailableLineWidth()
}

func (m model) importFilePopupIsParentLayer() bool {
	if m.activePopup == popupImportFile {
		return false
	}

	for _, kind := range m.popupStack {
		if kind == popupImportFile {
			return true
		}
	}

	return false
}

func (m model) importParentTextInputLineWidth(labelWidth int, value string, minValueWidth int) int {
	valueWidth := max(minValueWidth, lipgloss.Width(value)+1)

	return min(m.importParentLineWidth(), labelWidth+3+valueWidth)
}

func (m model) importAreaLineWidth(labelWidth int, value string, minValueWidth int) int {
	value = strings.ReplaceAll(value, "\n", " ")
	valueWidth := max(minValueWidth, lipgloss.Width(value)+1)

	return min(m.popupAvailableLineWidth(), labelWidth+3+valueWidth)
}

func (m model) importMapFieldsLineWidth(labelWidth int) int {
	valueWidth := 18
	for i := range m.importMapFieldInputs {
		valueWidth = max(valueWidth, lipgloss.Width(m.importMapFieldInputs[i].Value())+1)
	}

	return min(m.popupAvailableLineWidth(), labelWidth+3+valueWidth)
}

func (m model) importMapPathInputWidth() int {
	inputWidth := 12

	for i := range m.importMapPathRows {
		row := &m.importMapPathRows[i]
		inputWidth = max(inputWidth, lipgloss.Width(row.awsPath.Value())+1)
		inputWidth = max(inputWidth, lipgloss.Width(row.filePath.Value())+1)
	}

	maxInputWidth := max(1, (m.popupAvailableLineWidth()-3)/2)

	return min(inputWidth, maxInputWidth)
}

func (m model) importDefaultTextareaContentWidth(area *textarea.Model) int {
	maxWidth := m.popupAvailableLineWidth()
	if m.showGutters {
		maxWidth = max(1, maxWidth-formTextareaGutterWidth(area))
	}

	return formTextareaLogicalContentWidth(area, 18, maxWidth)
}

func (m model) importDefaultsMinInnerWidth() int {
	minLineWidth := 0
	if m.importDefaultAreaExpanded(&m.importDefaultPolicies) {
		minLineWidth = max(minLineWidth, m.importDefaultTextareaLineWidth(&m.importDefaultPolicies))
	}

	if m.importDefaultAreaExpanded(&m.importDefaultDescription) {
		minLineWidth = max(minLineWidth, m.importDefaultTextareaLineWidth(&m.importDefaultDescription))
	}

	if minLineWidth == 0 {
		return 0
	}

	return minLineWidth + 4
}

func (m model) importDefaultTextareaLineWidth(area *textarea.Model) int {
	contentWidth := m.importDefaultTextareaContentWidth(area)
	if !m.showGutters {
		return contentWidth
	}

	return contentWidth + formTextareaGutterWidth(area)
}

func (m model) importDefaultAreaExpanded(area *textarea.Model) bool {
	return areaContainsLineBreak(area) || !m.importDefaultAreaCanRenderCompact(area)
}

func areaContainsLineBreak(area *textarea.Model) bool {
	return area != nil && strings.Contains(area.Value(), "\n")
}

func (m model) importDefaultAreaCanRenderCompact(area *textarea.Model) bool {
	if area == nil {
		return true
	}

	value := area.Value()
	if strings.Contains(value, "\n") {
		return false
	}

	labelText := padMin("", importDefaultsLabelWidth+1)
	width := max(1, m.popupAvailableLineWidth()-lipgloss.Width(labelText)-3)

	return lipgloss.Width(value) <= width
}

func importFieldCursorFromNavigation(cursor, count int, action navigationAction) (int, bool) {
	switch action {
	case navPrevious:
		return previousCursor(cursor, count), true
	case navNext:
		return nextCursor(cursor, count), true
	case navNone, navPageUp, navPageDown, navFirst, navLast:
		return cursor, false
	}

	return cursor, false
}

func (component popupUpdateComponent) updateImportFilePopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	key := msg.String()

	if action, ok := m.editorNavigationAction(key); ok {
		if cursor, moved := importFieldCursorFromNavigation(m.importMainCursor, int(importMainFieldsCount), action); moved {
			m.importMainCursor = cursor
			m.focusImportMain()

			return m, nil
		}
	}

	switch key {
	case "ctrl+_", "ctrl+/":
		m.openPopupShortcuts(screenMain, popupImportFile)
	case "q", "esc", "ctrl+g":
		m.popPopup()
	case "enter", "ctrl+j":
		switch importMainField(m.importMainCursor) {
		case importMainFieldKeyField:
			m.importKeyFieldCursor = indexOf(importKeyFieldOptions, m.importKeyField)
			m.pushNestedPopup(popupImportKeyField)
		case importMainFieldFormat:
			m.importFormatCursor = indexOf(importFormatOptions, m.importFormat)
			m.pushNestedPopup(popupImportFormat)
		case importMainFieldMapFields:
			m.importMapFieldsCursor = 0
			m.focusImportMapField()
			m.pushNestedPopup(popupImportMapFields)
		case importMainFieldMapPaths:
			m.importMapPathsCursor = 0
			m.focusImportMapPath()
			m.pushNestedPopup(popupImportMapPaths)
		case importMainFieldDefaults:
			m.importDefaultsCursor = 0
			m.focusImportDefaults()
			m.pushNestedPopup(popupImportDefaults)
		case importMainFieldFilePath, importMainFieldsCount:
			m.message = "Import loading is not implemented yet"
		}
	default:
		return m.updateImportMainInput(msg)
	}

	return m, nil
}

func (component popupUpdateComponent) updateImportFormatPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	key := msg.String()

	if (&m).handleSelectorNavigation(key, &m.importFormatCursor, len(importFormatOptions)) {
		return m, nil
	}

	switch key {
	case "ctrl+_", "ctrl+/":
		m.openPopupShortcuts(screenMain, popupImportFormat)
	case "q", "esc", "ctrl+g":
		m.popPopup()
	case "enter", "ctrl+j":
		if len(importFormatOptions) > 0 {
			m.importFormat = importFormatOptions[min(m.importFormatCursor, len(importFormatOptions)-1)]
		}

		m.popPopup()
	}

	return m, nil
}

func (component popupUpdateComponent) updateImportKeyFieldPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	key := msg.String()

	if (&m).handleSelectorNavigation(key, &m.importKeyFieldCursor, len(importKeyFieldOptions)) {
		return m, nil
	}

	switch key {
	case "ctrl+_", "ctrl+/":
		m.openPopupShortcuts(screenMain, popupImportKeyField)
	case "q", "esc", "ctrl+g":
		m.popPopup()
	case "enter", "ctrl+j":
		if len(importKeyFieldOptions) > 0 {
			m.importKeyField = importKeyFieldOptions[min(m.importKeyFieldCursor, len(importKeyFieldOptions)-1)]
		}

		m.popPopup()
	}

	return m, nil
}

func (component popupUpdateComponent) updateImportMapFieldsPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	key := msg.String()

	if action, ok := m.editorNavigationAction(key); ok {
		if cursor, moved := importFieldCursorFromNavigation(m.importMapFieldsCursor, len(m.importMapFieldInputs), action); moved {
			m.importMapFieldsCursor = cursor
			m.focusImportMapField()

			return m, nil
		}
	}

	switch key {
	case "ctrl+_", "ctrl+/":
		m.openPopupShortcuts(screenMain, popupImportMapFields)
	case "q", "esc", "ctrl+g", "enter", "ctrl+j":
		m.popPopup()
	default:
		if m.importMapFieldBackspaceMovesPrevious(key) {
			m.importMapFieldsCursor = previousCursor(m.importMapFieldsCursor, len(m.importMapFieldInputs))
			m.focusImportMapField()

			return m, nil
		}

		var cmd tea.Cmd

		m.importMapFieldInputs[m.importMapFieldsCursor], cmd = m.importMapFieldInputs[m.importMapFieldsCursor].Update(msg)

		return m, cmd
	}

	return m, nil
}

func (component popupUpdateComponent) updateImportMapPathsPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	key := msg.String()

	if action, ok := m.editorNavigationAction(key); ok {
		if cursor, moved := importFieldCursorFromNavigation(m.importMapPathsCursor, len(m.importMapPathRows)*2, action); moved {
			m.importMapPathsCursor = cursor
			m.focusImportMapPath()

			return m, nil
		}
	}

	switch key {
	case "ctrl+_", "ctrl+/":
		m.openPopupShortcuts(screenMain, popupImportMapPaths)
	case "q", "esc", "ctrl+g", "enter", "ctrl+j":
		m.popPopup()
	default:
		if m.importMapPathBackspaceMovesPrevious(key) {
			m.importMapPathsCursor = previousCursor(m.importMapPathsCursor, len(m.importMapPathRows)*2)
			m.normalizeMapPathRows(&m.opts)
			m.focusImportMapPath()

			return m, nil
		}

		row, side := m.importMapPathCursorPosition()

		var cmd tea.Cmd
		if side == 0 {
			m.importMapPathRows[row].awsPath, cmd = m.importMapPathRows[row].awsPath.Update(msg)
		} else {
			m.importMapPathRows[row].filePath, cmd = m.importMapPathRows[row].filePath.Update(msg)
		}

		m.normalizeMapPathRows(&m.opts)
		m.focusImportMapPath()

		return m, cmd
	}

	return m, nil
}

func (m model) importMapFieldBackspaceMovesPrevious(key string) bool {
	if len(m.importMapFieldInputs) == 0 {
		return false
	}

	cursor := min(max(0, m.importMapFieldsCursor), len(m.importMapFieldInputs)-1)

	return importTextInputBackspaceMovesPrevious(key, &m.importMapFieldInputs[cursor])
}

func (m model) importMapPathBackspaceMovesPrevious(key string) bool {
	if len(m.importMapPathRows) == 0 {
		return false
	}

	row, side := m.importMapPathCursorPosition()
	if row < 0 || row >= len(m.importMapPathRows) {
		return false
	}

	if side == 0 {
		return importTextInputBackspaceMovesPrevious(key, &m.importMapPathRows[row].awsPath)
	}

	return importTextInputBackspaceMovesPrevious(key, &m.importMapPathRows[row].filePath)
}

func importTextInputBackspaceMovesPrevious(key string, input *textinput.Model) bool {
	if key != "backspace" && key != "ctrl+h" {
		return false
	}

	return input != nil && input.Position() == 0
}

func (component popupUpdateComponent) updateImportDefaultsPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	key := msg.String()

	if action, ok := m.editorNavigationAction(key); ok {
		areaExpanded := m.importDefaultFocusedAreaExpanded()

		allowFieldNavigation := !areaExpanded || key == "tab" || key == "shift+tab"
		if !allowFieldNavigation {
			return m.updateImportDefaultInput(msg)
		}

		if cursor, moved := importFieldCursorFromNavigation(m.importDefaultsCursor, 6, action); moved {
			fromLimits := m.importDefaultTextareaRowLimits()
			m.importDefaultsCursor = cursor
			m.focusImportDefaults()

			return m.startImportDefaultsRowAnimation(fromLimits, m.importDefaultTextareaTargetRowLimits())
		}
	}

	switch key {
	case "ctrl+_", "ctrl+/":
		m.openPopupShortcuts(screenMain, popupImportDefaults)
	case "q", "esc", "ctrl+g":
		m.popPopup()
	case "alt+e":
		switch m.importDefaultsCursor {
		case 4:
			m.openImportDefaultActionsPopup(editFieldPolicies)
		case 5:
			m.openImportDefaultActionsPopup(editFieldDescription)
		}
	case "enter", "ctrl+j":
		switch m.importDefaultsCursor {
		case 0:
			return m.openImportDefaultRegionSelect()
		case 1:
			m.typeCursor = importParameterTypeItems().index(m.importDefaultType)
			m.pushNestedPopup(popupTypeSelect)
		case 2:
			m.tierCursor = importParameterTierItems().index(m.importDefaultTier)
			m.pushNestedPopup(popupTierSelect)
		case 3:
			m.dataTypeCursor = importParameterDataTypeItems().index(m.importDefaultDataType)
			m.pushNestedPopup(popupDataTypeSelect)
		case 4, 5:
			m.focusImportDefaults()

			return m.updateImportDefaultInput(msg)
		default:
			m.popPopup()
		}
	default:
		return m.updateImportDefaultInput(msg)
	}

	return m, nil
}

func (m model) startImportDefaultsRowAnimation(from, to map[int]int) (tea.Model, tea.Cmd) {
	if importDefaultRowLimitsEqual(from, to) {
		m.importDefaultsAnimation.active = false

		return m, nil
	}

	animationID := m.importDefaultsAnimation.id + 1
	m.importDefaultsAnimation = importDefaultsRowAnimation{
		active: true,
		id:     animationID,
		frame:  0,
		frames: importDefaultsAnimationFrames,
		from:   copyImportDefaultRowLimits(from),
		to:     copyImportDefaultRowLimits(to),
	}

	return m, tickImportDefaultsAnimation(animationID)
}

func (m model) updateImportDefaultsAnimationTick(msg importDefaultsAnimationTickMsg) (tea.Model, tea.Cmd) {
	if !m.importDefaultsAnimation.active || m.importDefaultsAnimation.id != msg.id {
		return m, nil
	}

	if m.activePopup != popupImportDefaults {
		m.importDefaultsAnimation.active = false

		return m, nil
	}

	m.importDefaultsAnimation.frame++
	if m.importDefaultsAnimation.frame >= m.importDefaultsAnimation.frames {
		m.importDefaultsAnimation.active = false

		return m, nil
	}

	return m, tickImportDefaultsAnimation(m.importDefaultsAnimation.id)
}

func tickImportDefaultsAnimation(id int) tea.Cmd {
	return tea.Tick(importDefaultsAnimationInterval, func(time.Time) tea.Msg {
		return importDefaultsAnimationTickMsg{id: id}
	})
}

func (m model) importDefaultFocusedAreaExpanded() bool {
	area := m.importFocusedDefaultArea()
	if area == nil {
		return false
	}

	return m.importDefaultAreaExpanded(area)
}

func (m model) updateImportMainInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch importMainField(m.importMainCursor) {
	case importMainFieldFilePath:
		m.importFilePathInput, cmd = m.importFilePathInput.Update(msg)
	case importMainFieldKeyField,
		importMainFieldFormat,
		importMainFieldMapFields,
		importMainFieldMapPaths,
		importMainFieldDefaults,
		importMainFieldsCount:
	}

	return m, cmd
}

func (m model) updateImportDefaultInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch m.importDefaultsCursor {
	case 4:
		m.importDefaultPolicies, cmd = m.importDefaultPolicies.Update(msg)
	case 5:
		m.importDefaultDescription, cmd = m.importDefaultDescription.Update(msg)
	default:
	}

	return m, cmd
}

func (m model) importKeyFieldDisplay() string {
	if m.importKeyField == "" {
		return "none"
	}

	return m.importKeyField
}

func (m model) importDefaultRegionDisplay() string {
	if m.importDefaultRegion == "" {
		return "none"
	}

	return m.importDefaultRegion
}

func (m model) importDefaultTypeDisplay() string {
	if !m.importDefaultType.IsValid() {
		return "none"
	}

	return m.importDefaultType.String()
}

func (m model) importDefaultTierDisplay() string {
	if !m.importDefaultTier.IsValid() {
		return "none"
	}

	return m.importDefaultTier.String()
}

func (m model) importDefaultDataTypeDisplay() string {
	if !m.importDefaultDataType.IsValid() {
		return "none"
	}

	return m.importDefaultDataType.String()
}

func (m model) openImportDefaultRegionSelect() (tea.Model, tea.Cmd) {
	m = m.ensureRegionSelectOptions()

	regions := m.importDefaultRegionOptions()
	if len(regions) == 0 {
		return m, nil
	}

	m.regionCursor = indexOf(regions, m.importDefaultRegion)
	m.pushNestedPopup(popupRegionSelect)

	return m, nil
}

func (m model) importDefaultRegionOptions() []string {
	regions := m.regionSelectOptions()

	return append([]string{""}, regions...)
}

func (m model) importSelectorActive() bool {
	return len(m.popupStack) > 0 && m.popupStack[len(m.popupStack)-1] == popupImportDefaults
}

func (m model) finishImportSelector() model {
	m.popPopup()

	if m.activePopup == popupImportDefaults {
		m.focusImportDefaults()
	}

	return m
}

func maxStringWidth(values []string) int {
	width := 0
	for _, value := range values {
		width = max(width, lipgloss.Width(value))
	}

	return width
}
