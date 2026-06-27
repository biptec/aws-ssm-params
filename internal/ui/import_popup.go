package ui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/biptec/aws-ssm-params/internal/app"
	"github.com/biptec/aws-ssm-params/internal/app/exportplan"
	importer "github.com/biptec/aws-ssm-params/internal/app/import"
	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/biptec/aws-ssm-params/internal/textio"
	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type importState struct {
	importFilePathInput            textinput.Model
	importFilePicker               filepicker.Model
	importFilePickerTarget         filePickerTarget
	importFilePickerCurrentFocused bool
	importFilePickerParentFocused  bool
	importFilePickerButtonsFocused bool
	importFilePickerTargetName     string
	importFilePickerMinInnerWidth  int

	importMainCursor      int
	importKeyFieldCursor  int
	importFormatCursor    int
	importMapFieldsCursor int
	importMapPathsCursor  int
	importDefaultsCursor  int
	importFromStdin       bool
	importButtonsFocused  bool
	importButtonCursor    int

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

	importMapFieldBackup []string
	importMapPathBackup  [][2]string
	importDefaultsBackup importDefaultsSnapshot

	exportFilePathInput textinput.Model
	exportMainCursor    int
	exportFormatCursor  int
	exportKeyFieldCursor int
	exportOutputCursor   int
	exportMapFieldsCursor int
	exportMapPathsCursor int

	exportFormat   string
	exportKeyField string
	exportScalar   bool
	exportScalarField int

	exportOutputFields  []bool
	exportMapFieldInputs []textinput.Model
	exportMapPathRows    []importMapPathRow

	exportOutputBackup      []bool
	exportScalarBackup      bool
	exportScalarFieldBackup int
	exportMapFieldBackup    []string
	exportMapPathBackup     [][2]string
}

type filePickerTarget int

const (
	filePickerTargetImportFile filePickerTarget = iota
	filePickerTargetPopupFileAction
	filePickerTargetExportFile
)

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

type importDefaultsSnapshot struct {
	region      string
	typ         ssm.ParameterType
	tier        ssm.ParameterTier
	dataType    ssm.ParameterDataType
	policies    string
	description string
}

type importDefaultsAnimationTickMsg struct {
	id int
}

type importMainField int

const (
	importMainFieldFilePath importMainField = iota
	importMainFieldFormat
	importMainFieldKeyField
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
	importMainLabelWidth     = 10
	exportMainLabelWidth     = 13
	importDefaultsLabelWidth = 11
	importParentCompactWidth = 28
)

const (
	importDefaultsAnimationFrames   = 5
	importDefaultsAnimationInterval = 80 * time.Millisecond
)

const (
	importActionPrimary = iota
	importActionCancel
	importActionCount
)

const (
	importFilePickerCurrentEntry = "."
	importFilePickerParentEntry  = ".."
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

type exportMainField int

const (
	exportMainFieldFilePath exportMainField = iota
	exportMainFieldFormat
	exportMainFieldKeyField
	exportMainFieldOutputFields
	exportMainFieldMapFields
	exportMainFieldMapPaths
	exportMainFieldsCount
)

func newImportState(opts *Options) importState {
	state := importState{
		importFilePathInput:      newImportTextInput(opts),
		importFilePicker:         newImportFilePicker(opts),
		importFormat:             importFormatDotenv,
		importDefaultRegion:      "",
		importDefaultType:        "",
		importDefaultTier:        "",
		importDefaultDataType:    "",
		importDefaultPolicies:    newImportTextArea(opts),
		importDefaultDescription: newImportTextArea(opts),
	}
	state.importFilePathInput.SetValue("./")

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

func newImportFilePicker(opts *Options) filepicker.Model {
	picker := filepicker.New()
	picker.ShowHidden = true
	picker.AutoHeight = false
	picker.Height = 12
	picker.Cursor = ">"
	picker.KeyMap.Back = key.NewBinding(key.WithKeys("left", "backspace"), key.WithHelp("left", "parent"))
	picker.KeyMap.GoToTop = key.NewBinding(key.WithKeys("home"), key.WithHelp("home", "first"))
	picker.KeyMap.GoToLast = key.NewBinding(key.WithKeys("end"), key.WithHelp("end", "last"))
	picker.KeyMap.Down = key.NewBinding(key.WithKeys("down"), key.WithHelp("down", "next"))
	picker.KeyMap.Up = key.NewBinding(key.WithKeys("up"), key.WithHelp("up", "previous"))
	picker.KeyMap.PageUp = key.NewBinding(key.WithKeys("pgup", "pageup"), key.WithHelp("pgup", "page up"))
	picker.KeyMap.PageDown = key.NewBinding(key.WithKeys("pgdown", "pagedown"), key.WithHelp("pgdown", "page down"))
	picker.KeyMap.Open = key.NewBinding(key.WithKeys("right", "enter"), key.WithHelp("right", "open"))
	picker.KeyMap.Select = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select"))

	if opts != nil && !opts.NoColor {
		picker.Styles.Cursor = lipgloss.NewStyle().Foreground(selectedFg)
		picker.Styles.Selected = selectedRowStyle
		picker.Styles.Directory = labelStyle
		picker.Styles.File = valueStyle
		picker.Styles.Symlink = valueStyle
		picker.Styles.Permission = mutedStyle
		picker.Styles.FileSize = mutedStyle.Width(7).Align(lipgloss.Right)
		picker.Styles.DisabledFile = mutedStyle
		picker.Styles.DisabledSelected = mutedStyle
		picker.Styles.DisabledCursor = mutedStyle
		picker.Styles.EmptyDirectory = mutedStyle.PaddingLeft(2).SetString("No files found.")
	}

	return picker
}

func importDefaultDirectory() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}

	path, err := filepath.Abs(wd)
	if err != nil {
		return wd
	}

	return path
}

func importCompactDirectoryPath(path string, segments int) string {
	path = strings.TrimRight(path, string(os.PathSeparator))
	if path == "" || path == string(os.PathSeparator) {
		return path
	}

	parts := strings.Split(filepath.ToSlash(path), "/")
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}

	if len(kept) <= segments {
		return path
	}

	return "../" + strings.Join(kept[len(kept)-segments:], "/")
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

	state.importFromStdin = false
	state.importMainCursor = int(importMainFieldFilePath)
	state.focusImportMain()
	m.importState = state
	m.pushPopup(popupImportFile)
}

func (m *model) openImportStdinPopup() {
	state := m.importState
	if state.importFilePathInput.Width == 0 {
		state = newImportState(&m.opts)
	}

	state.importFromStdin = true
	state.importFormat = detectedImportFormatFromBytes(m.opts.ImportStdin)
	state.importMainCursor = int(importMainFieldFormat)
	state.focusImportMain()
	m.importState = state
	m.importStdinOpened = true
	m.pushPopup(popupImportFile)
}

func (m *model) openExportPopup() {
	m.ensureExportState()
	m.exportMainCursor = int(exportMainFieldFilePath)
	m.focusExportMain()
	m.pushPopup(popupExportFile)
}

func (m *model) ensureExportState() {
	if m.exportFilePathInput.Width != 0 {
		return
	}

	m.exportFilePathInput = newImportTextInput(&m.opts)
	m.exportFilePathInput.SetValue("./")
	m.exportFormat = importFormatDotenv
	m.exportScalarField = indexOf(importMapFieldKeys, textio.FieldValue)

	m.exportOutputFields = make([]bool, len(importMapFieldLabels))
	m.exportMapFieldInputs = make([]textinput.Model, len(importMapFieldLabels))
	for i := range importMapFieldLabels {
		m.exportOutputFields[i] = true
		m.exportMapFieldInputs[i] = newImportTextInput(&m.opts)
	}

	m.exportMapPathRows = []importMapPathRow{newImportMapPathRow(&m.opts)}
}

func (state *importState) blurImportInputs() {
	state.importFilePathInput.Blur()
	state.exportFilePathInput.Blur()

	for i := range state.importMapFieldInputs {
		state.importMapFieldInputs[i].Blur()
	}

	for i := range state.exportMapFieldInputs {
		state.exportMapFieldInputs[i].Blur()
	}

	for i := range state.importMapPathRows {
		state.importMapPathRows[i].awsPath.Blur()
		state.importMapPathRows[i].filePath.Blur()
	}

	for i := range state.exportMapPathRows {
		state.exportMapPathRows[i].awsPath.Blur()
		state.exportMapPathRows[i].filePath.Blur()
	}

	state.importDefaultPolicies.Blur()
	state.importDefaultDescription.Blur()
}

func (state *importState) focusImportMain() {
	state.importButtonsFocused = false
	state.blurImportInputs()
	state.normalizeImportMainCursor()

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

func (state *importState) normalizeImportMainCursor() {
	first := int(importMainFieldFilePath)
	if state.importFromStdin {
		first = int(importMainFieldFormat)
	}

	state.importMainCursor = min(max(first, state.importMainCursor), int(importMainFieldsCount)-1)
}

func (state *importState) focusImportMapField() {
	state.importButtonsFocused = false
	state.blurImportInputs()

	if len(state.importMapFieldInputs) == 0 {
		return
	}

	state.importMapFieldsCursor = min(max(0, state.importMapFieldsCursor), len(state.importMapFieldInputs)-1)
	state.importMapFieldInputs[state.importMapFieldsCursor].Focus()
}

func (state *importState) focusImportMapPath() {
	state.importButtonsFocused = false
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
	state.importButtonsFocused = false
	state.blurImportInputs()

	switch state.importDefaultsCursor {
	case 4:
		state.importDefaultPolicies.Focus()
	case 5:
		state.importDefaultDescription.Focus()
	default:
	}
}

func (state *importState) focusImportButton(cursor int) {
	state.blurImportInputs()
	state.importButtonsFocused = true
	state.importButtonCursor = min(max(0, cursor), importActionCount-1)
}

func (state *importState) focusExportMain() {
	state.importButtonsFocused = false
	state.blurImportInputs()

	if exportMainField(state.exportMainCursor) == exportMainFieldFilePath {
		state.exportFilePathInput.Focus()
	}
}

func (state *importState) focusExportOutputField() {
	state.importButtonsFocused = false
	state.blurImportInputs()
	state.exportOutputCursor = min(max(0, state.exportOutputCursor), len(importMapFieldLabels))
}

func (state *importState) focusExportMapField() {
	state.importButtonsFocused = false
	state.blurImportInputs()
	if len(state.exportMapFieldInputs) == 0 {
		return
	}

	state.exportMapFieldsCursor = min(max(0, state.exportMapFieldsCursor), len(state.exportMapFieldInputs)-1)
	state.exportMapFieldInputs[state.exportMapFieldsCursor].Focus()
}

func (state *importState) focusExportMapPath() {
	state.importButtonsFocused = false
	state.blurImportInputs()
	state.normalizeExportMapPathRows(nil)
	if len(state.exportMapPathRows) == 0 {
		return
	}

	maxCursor := len(state.exportMapPathRows)*2 - 1
	state.exportMapPathsCursor = min(max(0, state.exportMapPathsCursor), maxCursor)
	row, side := state.exportMapPathCursorPosition()
	if side == 0 {
		state.exportMapPathRows[row].awsPath.Focus()
	} else {
		state.exportMapPathRows[row].filePath.Focus()
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

func (state *importState) normalizeExportMapPathRows(opts *Options) {
	if len(state.exportMapPathRows) == 0 {
		state.exportMapPathRows = append(state.exportMapPathRows, newImportMapPathRow(opts))
		return
	}

	for len(state.exportMapPathRows) > 1 {
		last := &state.exportMapPathRows[len(state.exportMapPathRows)-1]
		previous := &state.exportMapPathRows[len(state.exportMapPathRows)-2]
		if !importMapPathRowEmpty(last) || !importMapPathRowEmpty(previous) {
			break
		}

		state.exportMapPathRows = state.exportMapPathRows[:len(state.exportMapPathRows)-1]
	}

	last := &state.exportMapPathRows[len(state.exportMapPathRows)-1]
	if strings.TrimSpace(last.awsPath.Value()) != "" || strings.TrimSpace(last.filePath.Value()) != "" {
		state.exportMapPathRows = append(state.exportMapPathRows, newImportMapPathRow(opts))
	}

	maxCursor := len(state.exportMapPathRows)*2 - 1
	state.exportMapPathsCursor = min(max(0, state.exportMapPathsCursor), maxCursor)
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

func (state *importState) exportMapPathCursorPosition() (row, side int) {
	if len(state.exportMapPathRows) == 0 {
		return 0, 0
	}

	cursor := min(max(0, state.exportMapPathsCursor), len(state.exportMapPathRows)*2-1)

	return cursor / 2, cursor % 2
}

func (component popupViewComponent) renderImportFilePopup() string {
	m := component.model
	title := "Import from file"
	lines := []string{}
	if m.importFromStdin {
		title = "Import from stdin"
	} else {
		innerWidth := m.importParentTextInputLineWidth(importMainLabelWidth, m.importFilePathInput.Value(), importMinimumValueWidth(importMainLabelWidth))
		lines = append(lines, m.importMainTextInputLine("File path", &m.importFilePathInput, innerWidth), "")
	}

	lines = append(
		lines,
		m.importChoiceLine("Format", m.importFormatDisplay(), int(importMainFieldFormat)),
		m.importChoiceLine("Key field", m.importKeyFieldDisplay(), int(importMainFieldKeyField)),
	)

	previousSummaryLines := 0
	lines = m.appendImportSummarySection(lines, "Map fields", m.importParentMapFieldSummaryPairs(), int(importMainFieldMapFields), &previousSummaryLines)
	lines = m.appendImportSummarySection(lines, "Map paths", m.importParentMapPathSummaryPairs(), int(importMainFieldMapPaths), &previousSummaryLines)
	lines = m.appendImportSummarySection(lines, "Defaults", m.importParentDefaultSummaryPairs(), int(importMainFieldDefaults), &previousSummaryLines)
	lines = append(lines, "", m.importActionButtonsLineFocused("Load", m.activePopup == popupImportFile && m.importButtonsFocused))

	return m.renderPopupBoxMinWidth(title, lines, importPopupMinInnerWidth(importMainLabelWidth))
}

func (component popupViewComponent) renderImportFormatPopup() string {
	m := component.model

	lines := make([]string, 0, len(importFormatOptions))
	for i, option := range importFormatOptions {
		focused := !m.importButtonsFocused && i == m.importFormatCursor
		lines = append(lines, m.singleSelectLine(importFormatLabel(option), i == m.importFormatCursor, focused))
	}

	lines = append(lines, "", m.importActionButtonsLine("Select"))

	return m.renderPopupBox("Format", lines)
}

func (component popupViewComponent) renderImportKeyFieldPopup() string {
	m := component.model

	lines := make([]string, 0, len(importKeyFieldOptions))
	for i, option := range importKeyFieldOptions {
		focused := !m.importButtonsFocused && i == m.importKeyFieldCursor
		lines = append(lines, m.singleSelectLine(m.importSelectorLabel(importFieldLabel(option)), i == m.importKeyFieldCursor, focused))
	}

	lines = append(lines, "", m.importActionButtonsLine("Select"))

	return m.renderPopupBox("Key field", lines)
}

func (component popupViewComponent) renderImportMapFieldsPopup() string {
	m := component.model
	labelWidth := maxStringWidth(importMapFieldLabels)
	innerWidth := m.importMapFieldsLineWidth(labelWidth)

	lines := make([]string, 0, len(importMapFieldLabels))
	for i, label := range importMapFieldLabels {
		line := m.formTextInputFieldLine(label, &m.importMapFieldInputs[i], labelWidth, innerWidth)
		lines = append(lines, padVisible(line, importBaseLineWidth(labelWidth)))
	}

	lines = append(lines, "", m.importActionButtonsLineFocused("Apply", m.activePopup == popupImportMapFields && m.importButtonsFocused))

	return m.renderPopupBoxMinWidth("Map fields", lines, importPopupMinInnerWidth(labelWidth))
}

func (component popupViewComponent) renderImportMapPathsPopup() string {
	m := component.model
	m.normalizeMapPathRows(&m.opts)

	leftInputWidth, rightInputWidth := m.importMapPathInputWidths()

	lines := make([]string, 0, len(m.importMapPathRows))
	focusedRow, _ := m.importMapPathCursorPosition()
	for i := range m.importMapPathRows {
		row := &m.importMapPathRows[i]
		rowHasValue := !importMapPathRowEmpty(row)
		left := m.formInputValueWithPlaceholder(&row.awsPath, leftInputWidth, !rowHasValue)
		right := m.formInputValueWithPlaceholder(&row.filePath, rightInputWidth, !rowHasValue)
		lines = append(lines, m.formFocusPrefix(!m.importButtonsFocused && i == focusedRow)+left+" : "+right)
	}

	lines = append(lines, "", m.importActionButtonsLine("Apply"))

	return m.renderPopupBoxMinWidth("Map paths", lines, m.importMapPathsMinInnerWidth(leftInputWidth, rightInputWidth))
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
	lines = append(lines, "", m.importActionButtonsLine("Apply"))

	return m.renderPopupBoxWithActionsMinWidth("Defaults", lines, "", max(importPopupMinInnerWidth(importDefaultsLabelWidth), m.importDefaultsMinInnerWidth()))
}

func (component popupViewComponent) renderImportFilePickerPopup() string {
	m := component.model
	picker := m.importFilePicker
	picker.Height = m.importFilePickerHeight()
	if m.importFilePickerCurrentFocused || m.importFilePickerParentFocused || m.importFilePickerButtonsFocused {
		picker.Cursor = " "
		picker.Styles.Selected = valueStyle
		picker.Styles.DisabledSelected = mutedStyle
	}

	directory := m.value(importCompactDirectoryPath(picker.CurrentDirectory, 3))
	lines := []string{m.label("Directory: ") + directory, ""}
	lines = append(lines, m.importFilePickerCurrentLine())
	lines = append(lines, m.importFilePickerParentLine())
	lines = append(lines, strings.Split(strings.TrimRight(picker.View(), "\n"), "\n")...)
	lines = append(lines, "", m.importFilePickerActionButtonsLine())

	minInnerWidth := m.importFilePickerMinInnerWidth
	if minInnerWidth == 0 {
		minInnerWidth = importFilePickerStableMinInnerWidth(picker)
	}

	return m.renderPopupBoxMinWidth("Open Path", lines, minInnerWidth)
}

func (component popupViewComponent) renderExportFilePopup() string {
	m := component.model
	m.ensureExportState()

	innerWidth := m.importParentTextInputLineWidth(exportMainLabelWidth, m.exportFilePathInput.Value(), importMinimumValueWidth(exportMainLabelWidth))
	lines := []string{
		m.exportMainTextInputLine("File path", &m.exportFilePathInput, innerWidth),
		"",
		m.exportChoiceLine("Format", importFormatLabel(m.exportFormat), int(exportMainFieldFormat)),
		m.exportChoiceLine("Key field", importFieldLabel(m.exportKeyField), int(exportMainFieldKeyField)),
	}

	previousSummaryLines := 0
	lines = m.appendExportSummarySection(lines, "Output fields", m.exportOutputSummaryPairs(), int(exportMainFieldOutputFields), &previousSummaryLines)
	lines = m.appendExportSummarySection(lines, "Map fields", m.exportMapFieldSummaryPairs(), int(exportMainFieldMapFields), &previousSummaryLines)
	lines = m.appendExportSummarySection(lines, "Map paths", m.exportMapPathSummaryPairs(), int(exportMainFieldMapPaths), &previousSummaryLines)
	lines = append(lines, "", m.importActionButtonsLineFocused("Export", m.activePopup == popupExportFile && m.importButtonsFocused))

	return m.renderPopupBoxMinWidth("Export to file", lines, importPopupMinInnerWidth(exportMainLabelWidth))
}

func (component popupViewComponent) renderExportOverwriteConfirmPopup() string {
	m := component.model
	lines := []string{
		"File already exists. Overwrite it?",
		"",
		m.importActionButtonsLineFocused("Overwrite", true),
	}

	return m.renderPopupBox("Confirm", lines)
}

func (component popupViewComponent) renderExportFormatPopup() string {
	m := component.model

	lines := make([]string, 0, len(importFormatOptions))
	for i, option := range importFormatOptions {
		focused := !m.importButtonsFocused && i == m.exportFormatCursor
		lines = append(lines, m.singleSelectLine(importFormatLabel(option), i == m.exportFormatCursor, focused))
	}

	lines = append(lines, "", m.importActionButtonsLine("Select"))

	return m.renderPopupBox("Format", lines)
}

func (component popupViewComponent) renderExportKeyFieldPopup() string {
	m := component.model

	lines := make([]string, 0, len(importKeyFieldOptions))
	for i, option := range importKeyFieldOptions {
		focused := !m.importButtonsFocused && i == m.exportKeyFieldCursor
		lines = append(lines, m.singleSelectLine(m.importSelectorLabel(importFieldLabel(option)), i == m.exportKeyFieldCursor, focused))
	}

	lines = append(lines, "", m.importActionButtonsLine("Select"))

	return m.renderPopupBox("Key field", lines)
}

func (component popupViewComponent) renderExportOutputFieldsPopup() string {
	m := component.model
	labelWidth := max(maxStringWidth(importMapFieldLabels), lipgloss.Width("Scalar"))

	lines := make([]string, 0, len(importMapFieldLabels)+3)
	lines = append(lines, m.exportOutputFieldLine("Scalar", m.exportScalarBox(), 0, labelWidth), "")
	for i, label := range importMapFieldLabels {
		lines = append(lines, m.exportOutputFieldLine(label, m.exportOutputFieldMark(i), i+1, labelWidth))
	}

	lines = append(lines, "", m.importActionButtonsLineFocused("Apply", m.activePopup == popupExportOutputFields && m.importButtonsFocused))

	return m.renderPopupBoxMinWidth("Output fields", lines, importPopupMinInnerWidth(labelWidth))
}

func (m model) exportOutputFieldLine(label, value string, cursor, labelWidth int) string {
	focused := m.activePopup == popupExportOutputFields && !m.importButtonsFocused && m.exportOutputCursor == cursor
	line := m.formFocusPrefix(focused) + m.importMainLabel(padMin(label+":", labelWidth+1)) + " " + m.value(value)

	return padVisible(line, importBaseLineWidth(labelWidth))
}

func (m model) exportScalarBox() string {
	if m.exportScalar {
		return "[x]"
	}

	return "[ ]"
}

func (m model) exportOutputFieldMark(index int) string {
	if m.exportScalar {
		if index == m.exportScalarField {
			return "(*)"
		}

		return "( )"
	}

	if index >= 0 && index < len(m.exportOutputFields) && m.exportOutputFields[index] {
		return "[x]"
	}

	return "[ ]"
}

func (component popupViewComponent) renderExportMapFieldsPopup() string {
	m := component.model
	labelWidth := maxStringWidth(importMapFieldLabels)
	innerWidth := m.exportFieldLineWidth(labelWidth, m.exportMapFieldInputs)

	lines := make([]string, 0, len(importMapFieldLabels))
	for i, label := range importMapFieldLabels {
		line := m.formTextInputFieldLine(label, &m.exportMapFieldInputs[i], labelWidth, innerWidth)
		lines = append(lines, padVisible(line, importBaseLineWidth(labelWidth)))
	}

	lines = append(lines, "", m.importActionButtonsLineFocused("Apply", m.activePopup == popupExportMapFields && m.importButtonsFocused))

	return m.renderPopupBoxMinWidth("Map fields", lines, importPopupMinInnerWidth(labelWidth))
}

func (component popupViewComponent) renderExportMapPathsPopup() string {
	m := component.model
	m.normalizeExportMapPathRows(&m.opts)

	leftInputWidth, rightInputWidth := m.exportMapPathInputWidths()
	lines := make([]string, 0, len(m.exportMapPathRows))
	focusedRow, _ := m.exportMapPathCursorPosition()
	for i := range m.exportMapPathRows {
		row := &m.exportMapPathRows[i]
		rowHasValue := !importMapPathRowEmpty(row)
		left := m.formInputValueWithPlaceholder(&row.awsPath, leftInputWidth, !rowHasValue)
		right := m.formInputValueWithPlaceholder(&row.filePath, rightInputWidth, !rowHasValue)
		lines = append(lines, m.formFocusPrefix(!m.importButtonsFocused && i == focusedRow)+left+" : "+right)
	}

	lines = append(lines, "", m.importActionButtonsLine("Apply"))

	return m.renderPopupBoxMinWidth("Map paths", lines, m.importMapPathsMinInnerWidth(leftInputWidth, rightInputWidth))
}

func (m model) exportChoiceLine(label, value string, cursor int) string {
	focused := m.activePopup == popupExportFile && !m.importButtonsFocused && m.exportMainCursor == cursor
	renderedValue := m.importMainSimpleValue(m.importSummaryValue(value))
	if value == "" {
		renderedValue = m.muted(nonePlaceholderText)
	}

	return m.exportMainFieldLine(label, renderedValue, focused)
}

func (m model) exportCheckboxLine(label string, checked bool, cursor int) string {
	value := "[ ]"
	if checked {
		value = "[x]"
	}

	focused := m.activePopup == popupExportFile && !m.importButtonsFocused && m.exportMainCursor == cursor
	return m.exportMainFieldLine(label, m.value(value), focused)
}

func (m model) exportMainTextInputLine(label string, input *textinput.Model, innerWidth int) string {
	labelText := padMin(label+":", exportMainLabelWidth+1)
	focused := m.activePopup == popupExportFile && !m.importButtonsFocused && input.Focused()
	available := innerWidth - lipgloss.Width(m.formFocusPrefix(focused)) - lipgloss.Width(labelText) - 2
	input.Width = max(1, available)
	input.SetCursor(input.Position())

	renderedValue := input.View()
	if !focused {
		value := input.Value()
		if value == "" {
			renderedValue = m.muted(nonePlaceholderText)
		} else {
			renderedValue = m.importMainSimpleValue(truncateInline(value, max(1, available)))
		}
	}

	return m.exportMainFieldLineWithWidth(label, renderedValue, focused, innerWidth)
}

func (m model) exportMainFieldLine(label, renderedValue string, focused bool) string {
	return m.exportMainFieldLineWithWidth(label, renderedValue, focused, importBaseLineWidth(exportMainLabelWidth))
}

func (m model) exportMainFieldLineWithWidth(label, renderedValue string, focused bool, lineWidth int) string {
	labelText := strings.Repeat(" ", exportMainLabelWidth+1)
	if label != "" {
		labelText = padMin(label+":", exportMainLabelWidth+1)
	}

	line := m.formFocusPrefix(focused && !m.importButtonsFocused) + m.importMainLabel(labelText) + " " + renderedValue

	return padVisible(line, max(importBaseLineWidth(exportMainLabelWidth), lineWidth))
}

func (m model) importFilePickerCurrentLine() string {
	if m.importFilePickerCurrentFocused && !m.importFilePickerButtonsFocused {
		return m.focusMarker("> ") + m.value(importFilePickerCurrentEntry)
	}

	return "  " + m.value(importFilePickerCurrentEntry)
}

func (m model) importFilePickerParentLine() string {
	if m.importFilePickerParentFocused && !m.importFilePickerButtonsFocused {
		return m.focusMarker("> ") + m.value(importFilePickerParentEntry)
	}

	return "  " + m.value(importFilePickerParentEntry)
}

func (m model) importFilePickerActionButtonsLine() string {
	return m.importActionButton("Choose", m.importFilePickerButtonsFocused && m.importButtonCursor == importActionPrimary) +
		m.muted("   ") +
		m.importActionButton("Cancel", m.importFilePickerButtonsFocused && m.importButtonCursor == importActionCancel)
}

func importFilePickerStableMinInnerWidth(picker filepicker.Model) int {
	const popupHorizontalPadding = 4

	directoryWidth := lipgloss.Width("Directory: " + importCompactDirectoryPath(picker.CurrentDirectory, 3))
	currentWidth := lipgloss.Width("  " + importFilePickerCurrentEntry)
	parentWidth := lipgloss.Width("  " + importFilePickerParentEntry)
	actionsWidth := lipgloss.Width("Choose   Cancel")

	return max(directoryWidth, currentWidth, parentWidth, actionsWidth, importFilePickerMaxEntryLineWidth(picker)) + popupHorizontalPadding
}

func importFilePickerMaxEntryLineWidth(picker filepicker.Model) int {
	entries := importFilePickerSortedEntries(picker.CurrentDirectory, picker.ShowHidden)
	maxWidth := 0
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		name := entry.Name()
		if info.Mode()&os.ModeSymlink != 0 {
			if path, err := filepath.EvalSymlinks(filepath.Join(picker.CurrentDirectory, name)); err == nil {
				name += " → " + path
			}
		}

		width := lipgloss.Width(picker.Cursor)
		if picker.ShowPermissions {
			width += lipgloss.Width(" " + info.Mode().String())
		}

		if picker.ShowSize {
			width += max(7, picker.Styles.FileSize.GetWidth())
		}

		width += lipgloss.Width(" " + name)
		maxWidth = max(maxWidth, width)
	}

	return maxWidth
}

func (m model) importChoiceLine(label, value string, cursor int) string {
	focused := m.importFilePopupFocused() && m.importMainCursor == cursor
	renderedValue := m.importMainSimpleValue(m.importSummaryValue(value))
	if value == "" {
		renderedValue = m.muted(nonePlaceholderText)
	}

	return m.importMainFieldLine(label, renderedValue, focused)
}

func (m model) importSummaryValue(value string) string {
	valueWidth := max(1, m.importParentValueWidth())

	return truncateInline(value, valueWidth)
}

func (m model) importMainTextInputLine(label string, input *textinput.Model, innerWidth int) string {
	labelText := padMin(label+":", importMainLabelWidth+1)
	focused := m.importFilePopupFocused() && input.Focused()
	available := innerWidth - lipgloss.Width(m.formFocusPrefix(focused)) - lipgloss.Width(labelText) - 2
	input.Width = max(1, available)
	input.SetCursor(input.Position())

	renderedValue := input.View()
	if !focused {
		value := input.Value()
		if value == "" {
			renderedValue = m.muted(nonePlaceholderText)
		} else {
			renderedValue = m.importMainSimpleValue(truncateInline(value, max(1, available)))
		}
	}

	return m.importMainFieldLineWithWidth(label, renderedValue, focused, innerWidth)
}

func (m model) importMainFieldLine(label, renderedValue string, focused bool) string {
	return m.importMainFieldLineWithWidth(label, renderedValue, focused, importBaseLineWidth(importMainLabelWidth))
}

func (m model) importMainFieldLineWithWidth(label, renderedValue string, focused bool, lineWidth int) string {
	labelText := strings.Repeat(" ", importMainLabelWidth+1)
	if label != "" {
		labelText = padMin(label+":", importMainLabelWidth+1)
	}

	line := m.formFocusPrefix(focused && !m.importButtonsFocused) + m.importMainLabel(labelText) + " " + renderedValue

	return padVisible(line, max(importBaseLineWidth(importMainLabelWidth), lineWidth))
}

func (m model) importMainLabel(value string) string {
	return m.label(value)
}

func (m model) importMainSimpleValue(value string) string {
	return m.value(value)
}

func (m model) importSelectorLabel(value string) string {
	if value == "" {
		return m.muted(nonePlaceholderText)
	}

	return value
}

type importSummaryPair struct {
	key   string
	value string
}

func (m model) appendImportSummarySection(lines []string, label string, pairs []importSummaryPair, cursor int, previousSummaryLines *int) []string {
	section := m.importSummaryLines(label, pairs, cursor)
	if previousSummaryLines != nil && *previousSummaryLines > 1 {
		lines = append(lines, "")
	}

	lines = append(lines, section...)
	if previousSummaryLines != nil {
		*previousSummaryLines = len(section)
	}

	return lines
}

func (m model) importSummaryLines(label string, pairs []importSummaryPair, cursor int) []string {
	focused := m.importFilePopupFocused() && m.importMainCursor == cursor
	if len(pairs) == 0 {
		return []string{m.importMainFieldLine(label, m.muted(nonePlaceholderText), focused)}
	}

	valueWidth := m.importParentValueWidth()
	lines := make([]string, 0, len(pairs))

	for i, pair := range pairs {
		lineLabel := ""
		lineFocused := false
		if i == 0 {
			lineLabel = label
			lineFocused = focused
		}

		lines = append(lines, m.importMainFieldLine(lineLabel, m.importSummaryPairValue(pair, valueWidth), lineFocused))
	}

	return lines
}

func (m model) importFilePopupFocused() bool {
	return m.activePopup == popupImportFile && !m.importButtonsFocused
}

func (m model) importSummaryPairValue(pair importSummaryPair, valueWidth int) string {
	if pair.key == "" {
		return m.importMainSimpleValue(truncateInline(pair.value, max(1, valueWidth)))
	}

	key := pair.key + ": "
	value := truncateInline(pair.value, max(1, valueWidth-lipgloss.Width(key)))
	if m.opts.NoColor {
		return key + value
	}

	return m.value(key) + m.muted(value)
}

func (m model) importMapFieldSummaryPairs() []importSummaryPair {
	parts := make([]importSummaryPair, 0, len(m.importMapFieldInputs))
	for i := range m.importMapFieldInputs {
		value := strings.TrimSpace(m.importMapFieldInputs[i].Value())
		if value == "" {
			continue
		}

		parts = append(parts, importSummaryPair{key: importFieldLabel(importMapFieldKeys[i]), value: value})
	}

	return parts
}

func (m model) appendExportSummarySection(lines []string, label string, pairs []importSummaryPair, cursor int, previousSummaryLines *int) []string {
	section := m.exportSummaryLines(label, pairs, cursor)
	if previousSummaryLines != nil && *previousSummaryLines > 1 {
		lines = append(lines, "")
	}

	lines = append(lines, section...)
	if previousSummaryLines != nil {
		*previousSummaryLines = len(section)
	}

	return lines
}

func (m model) exportSummaryLines(label string, pairs []importSummaryPair, cursor int) []string {
	focused := m.activePopup == popupExportFile && !m.importButtonsFocused && m.exportMainCursor == cursor
	if len(pairs) == 0 {
		return []string{m.exportMainFieldLine(label, m.muted(nonePlaceholderText), focused)}
	}

	valueWidth := m.exportParentValueWidth()
	lines := make([]string, 0, len(pairs))
	for i, pair := range pairs {
		lineLabel := ""
		lineFocused := false
		if i == 0 {
			lineLabel = label
			lineFocused = focused
		}

		lines = append(lines, m.exportMainFieldLine(lineLabel, m.importSummaryPairValue(pair, valueWidth), lineFocused))
	}

	return lines
}

func (m model) exportOutputSummaryPairs() []importSummaryPair {
	if m.exportScalar {
		return []importSummaryPair{{key: "Scalar", value: importFieldLabel(m.exportScalarFieldName())}}
	}

	if m.exportAllFieldsSelected() {
		return []importSummaryPair{{value: "all"}}
	}

	parts := make([]importSummaryPair, 0, len(importMapFieldLabels))
	for i := range importMapFieldLabels {
		if i < len(m.exportOutputFields) && m.exportOutputFields[i] {
			parts = append(parts, importSummaryPair{value: importFieldLabel(importMapFieldKeys[i])})
		}
	}

	return parts
}

func (m model) exportMapFieldSummaryPairs() []importSummaryPair {
	return exportFieldSummaryPairs(m.exportMapFieldInputs)
}

func exportFieldSummaryPairs(inputs []textinput.Model) []importSummaryPair {
	parts := make([]importSummaryPair, 0, len(inputs))
	for i := range inputs {
		value := strings.TrimSpace(inputs[i].Value())
		if value == "" || i >= len(importMapFieldKeys) {
			continue
		}

		parts = append(parts, importSummaryPair{key: importFieldLabel(importMapFieldKeys[i]), value: value})
	}

	return parts
}

func (m model) exportMapPathSummaryPairs() []importSummaryPair {
	m.normalizeExportMapPathRows(&m.opts)

	parts := make([]importSummaryPair, 0, len(m.exportMapPathRows))
	for i := range m.exportMapPathRows {
		row := &m.exportMapPathRows[i]
		awsPath := strings.TrimSpace(row.awsPath.Value())
		filePath := strings.TrimSpace(row.filePath.Value())
		if awsPath == "" && filePath == "" {
			continue
		}

		parts = append(parts, importSummaryPair{key: awsPath, value: filePath})
	}

	return parts
}

func (m model) exportParentValueWidth() int {
	return max(1, m.popupAvailableLineWidth()-exportMainLabelWidth-6)
}

func (m model) exportFieldLineWidth(labelWidth int, inputs []textinput.Model) int {
	width := importBaseLineWidth(labelWidth)
	for i := range inputs {
		width = max(width, importBaseLineWidth(labelWidth)+lipgloss.Width(inputs[i].Value()))
	}

	return min(width, m.popupAvailableLineWidth())
}

func (m model) exportMapPathInputWidths() (left, right int) {
	left, right = 12, 12
	for i := range m.exportMapPathRows {
		left = max(left, lipgloss.Width(m.exportMapPathRows[i].awsPath.Value()))
		right = max(right, lipgloss.Width(m.exportMapPathRows[i].filePath.Value()))
	}

	available := max(8, m.popupAvailableLineWidth()-6)
	if left+right > available {
		half := available / 2
		left = min(left, half)
		right = min(right, available-left)
	}

	return max(1, left), max(1, right)
}

func (m model) importParentMapFieldSummaryPairs() []importSummaryPair {
	if !m.importPopupActiveOrStack(popupImportMapFields) {
		return m.importMapFieldSummaryPairs()
	}

	parts := make([]importSummaryPair, 0, len(m.importMapFieldBackup))
	for i, value := range m.importMapFieldBackup {
		value = strings.TrimSpace(value)
		if value == "" || i >= len(importMapFieldKeys) {
			continue
		}

		parts = append(parts, importSummaryPair{key: importFieldLabel(importMapFieldKeys[i]), value: value})
	}

	return parts
}

func (m model) importMapPathSummaryPairs() []importSummaryPair {
	m.normalizeMapPathRows(&m.opts)

	parts := make([]importSummaryPair, 0, len(m.importMapPathRows))
	for i := range m.importMapPathRows {
		row := &m.importMapPathRows[i]
		awsPath := strings.TrimSpace(row.awsPath.Value())

		filePath := strings.TrimSpace(row.filePath.Value())
		if awsPath == "" && filePath == "" {
			continue
		}

		parts = append(parts, importSummaryPair{key: awsPath, value: filePath})
	}

	return parts
}

func (m model) importParentMapPathSummaryPairs() []importSummaryPair {
	if !m.importPopupActiveOrStack(popupImportMapPaths) {
		return m.importMapPathSummaryPairs()
	}

	parts := make([]importSummaryPair, 0, len(m.importMapPathBackup))
	for _, pair := range m.importMapPathBackup {
		awsPath := strings.TrimSpace(pair[0])
		filePath := strings.TrimSpace(pair[1])
		if awsPath == "" && filePath == "" {
			continue
		}

		parts = append(parts, importSummaryPair{key: awsPath, value: filePath})
	}

	return parts
}

func (m model) importDefaultSummaryPairs() []importSummaryPair {
	parts := make([]importSummaryPair, 0, 6)
	if m.importDefaultRegion != "" {
		parts = append(parts, importSummaryPair{key: textio.FieldRegion, value: m.importDefaultRegion})
	}

	if m.importDefaultType.IsValid() {
		parts = append(parts, importSummaryPair{key: textio.FieldType, value: m.importDefaultType.String()})
	}

	if m.importDefaultTier.IsValid() {
		parts = append(parts, importSummaryPair{key: textio.FieldTier, value: m.importDefaultTier.String()})
	}

	if m.importDefaultDataType.IsValid() {
		parts = append(parts, importSummaryPair{key: textio.FieldDataType, value: m.importDefaultDataType.String()})
	}

	if strings.TrimSpace(m.importDefaultPolicies.Value()) != "" {
		parts = append(parts, importSummaryPair{key: textio.FieldPolicies, value: oneLineImportSummary(m.importDefaultPolicies.Value())})
	}

	if strings.TrimSpace(m.importDefaultDescription.Value()) != "" {
		parts = append(parts, importSummaryPair{key: textio.FieldDescription, value: oneLineImportSummary(m.importDefaultDescription.Value())})
	}

	return parts
}

func (m model) importParentDefaultSummaryPairs() []importSummaryPair {
	if !m.importPopupActiveOrStack(popupImportDefaults) {
		return m.importDefaultSummaryPairs()
	}

	return importDefaultSnapshotSummaryPairs(m.importDefaultsBackup)
}

func importDefaultSnapshotSummaryPairs(snapshot importDefaultsSnapshot) []importSummaryPair {
	parts := make([]importSummaryPair, 0, 6)
	if snapshot.region != "" {
		parts = append(parts, importSummaryPair{key: textio.FieldRegion, value: snapshot.region})
	}

	if snapshot.typ.IsValid() {
		parts = append(parts, importSummaryPair{key: textio.FieldType, value: snapshot.typ.String()})
	}

	if snapshot.tier.IsValid() {
		parts = append(parts, importSummaryPair{key: textio.FieldTier, value: snapshot.tier.String()})
	}

	if snapshot.dataType.IsValid() {
		parts = append(parts, importSummaryPair{key: textio.FieldDataType, value: snapshot.dataType.String()})
	}

	if strings.TrimSpace(snapshot.policies) != "" {
		parts = append(parts, importSummaryPair{key: textio.FieldPolicies, value: oneLineImportSummary(snapshot.policies)})
	}

	if strings.TrimSpace(snapshot.description) != "" {
		parts = append(parts, importSummaryPair{key: textio.FieldDescription, value: oneLineImportSummary(snapshot.description)})
	}

	return parts
}

func (m model) importPopupActiveOrStack(kind popupKind) bool {
	if m.activePopup == kind {
		return true
	}

	for _, candidate := range m.popupStack {
		if candidate == kind {
			return true
		}
	}

	return false
}

func oneLineImportSummary(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func (m model) importActionButtonsLine(primary string) string {
	return m.importActionButtonsLineFocused(primary, m.importButtonsFocused)
}

func (m model) importActionButtonsLineFocused(primary string, focused bool) string {
	return m.formActionButtonsLine(primary, focused, m.importButtonCursor)
}

func (m model) importActionButton(label string, focused bool) string {
	return m.formActionButton(label, focused)
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
	line := m.formFieldLine(label, m.formOptionValue(m.importDefaultsCursor == cursor, value), importDefaultsLabelWidth, m.importDefaultsCursor == cursor && !m.importButtonsFocused)

	return padVisible(line, importBaseLineWidth(importDefaultsLabelWidth))
}

func (m model) importDefaultAreaLines(label string, area *textarea.Model, cursor, maxRows int) []string {
	focused := m.importDefaultsCursor == cursor && area.Focused()
	if m.importDefaultAreaExpanded(area) {
		lines := make([]string, 0, 6)
		lines = append(lines, m.formStandaloneLabel(label+":", focused && !m.importButtonsFocused))
		contentWidth := m.importDefaultTextareaContentWidth(area)

		lines = append(lines, m.formMultilineAreaLines(area, max(1, maxRows), contentWidth, focused)...)

		return lines
	}

	innerWidth := m.importAreaLineWidth(importDefaultsLabelWidth, area.Value(), importMinimumValueWidth(importDefaultsLabelWidth))
	value := m.formSingleLineAreaView(area, focused, importDefaultsLabelWidth, innerWidth)

	line := m.formFieldLine(label, value, importDefaultsLabelWidth, focused && !m.importButtonsFocused)

	return []string{padVisible(line, importBaseLineWidth(importDefaultsLabelWidth))}
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

func (m model) importParentValueWidth() int {
	return max(1, m.importParentLineWidth()-lipgloss.Width(m.formFocusPrefix(false))-importMainLabelWidth-2)
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

	return min(m.importParentLineWidth(), importInputLineWidth(labelWidth, valueWidth))
}

func (m model) importAreaLineWidth(labelWidth int, value string, minValueWidth int) int {
	value = strings.ReplaceAll(value, "\n", " ")
	valueWidth := max(minValueWidth, lipgloss.Width(value)+1)

	return min(m.popupAvailableLineWidth(), importInputLineWidth(labelWidth, valueWidth))
}

func (m model) importMapFieldsLineWidth(labelWidth int) int {
	valueWidth := 0
	for i := range m.importMapFieldInputs {
		valueWidth = max(valueWidth, lipgloss.Width(m.importMapFieldInputs[i].Value())+1)
	}

	return min(m.popupAvailableLineWidth(), importInputLineWidth(labelWidth, valueWidth))
}

func (m model) importMapPathInputWidths() (leftWidth, rightWidth int) {
	leftWidth = importMinimumValueWidth(0)
	rightWidth = importMinimumValueWidth(0)

	for i := range m.importMapPathRows {
		row := &m.importMapPathRows[i]
		leftWidth = max(leftWidth, lipgloss.Width(row.awsPath.Value())+1)
		rightWidth = max(rightWidth, lipgloss.Width(row.filePath.Value())+1)
	}

	maxLineWidth := max(1, m.popupAvailableLineWidth()-lipgloss.Width(m.formFocusPrefix(false))-lipgloss.Width(" : "))
	if leftWidth+rightWidth > maxLineWidth {
		overflow := leftWidth + rightWidth - maxLineWidth
		if rightWidth >= leftWidth {
			rightWidth = max(1, rightWidth-overflow)
		} else {
			leftWidth = max(1, leftWidth-overflow)
		}
	}

	return leftWidth, rightWidth
}

func (m model) importDefaultTextareaContentWidth(area *textarea.Model) int {
	maxWidth := m.popupAvailableLineWidth()
	if m.showGutters {
		maxWidth = max(1, maxWidth-formTextareaGutterWidth(area))
	}

	return formTextareaLogicalContentWidth(area, importMinimumValueWidth(importDefaultsLabelWidth), maxWidth)
}

func (m model) importFilePickerHeight() int {
	return max(6, min(18, m.popupContentLineBudget()-4))
}

func importMinimumValueWidth(labelWidth int) int {
	return importFieldValueStart(labelWidth)
}

func importInputLineWidth(labelWidth, valueWidth int) int {
	return max(importCenteredLineWidth(labelWidth), importFieldValueStart(labelWidth)+max(0, valueWidth)+1)
}

func importCenteredLineWidth(labelWidth int) int {
	return importFieldValueStart(labelWidth) * 2
}

func importFieldValueStart(labelWidth int) int {
	return labelWidth + 4
}

func importPopupMinInnerWidth(labelWidth int) int {
	return importBaseLineWidth(labelWidth) + 4
}

func importBaseLineWidth(labelWidth int) int {
	return importInputLineWidth(labelWidth, importMinimumValueWidth(labelWidth))
}

func (m model) importMapPathsMinInnerWidth(leftWidth, rightWidth int) int {
	rowWidth := lipgloss.Width(m.formFocusPrefix(false)) + leftWidth + lipgloss.Width(" : ") + rightWidth

	return rowWidth + 4
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

func importEnterKey(key string) bool {
	return key == "enter" || key == "ctrl+j"
}

func importPrimaryActionKey(key string) bool {
	return key == "ctrl+m"
}

func importCancelKey(key string) bool {
	return key == "q" || key == "esc" || key == "ctrl+g"
}

func (m *model) navigateImportSelectorButtons(key string) bool {
	switch key {
	case "tab":
		if m.importButtonsFocused {
			if m.importButtonCursor == importActionPrimary {
				m.importButtonCursor = importActionCancel
			} else {
				m.clearImportButtonFocus()
			}

			return true
		}

		m.focusImportButton(importActionPrimary)
		return true
	case "shift+tab":
		if m.importButtonsFocused {
			if m.importButtonCursor == importActionCancel {
				m.importButtonCursor = importActionPrimary
			} else {
				m.clearImportButtonFocus()
			}

			return true
		}

		m.focusImportButton(importActionCancel)
		return true
	case "left":
		if m.importButtonsFocused {
			m.importButtonCursor = importActionPrimary
			return true
		}
	case "right":
		if m.importButtonsFocused {
			m.importButtonCursor = importActionCancel
			return true
		}
	}

	return false
}

func (m *model) moveImportMainTabFocus(reverse bool) {
	count := int(importMainFieldsCount)
	if count == 0 {
		return
	}
	first := int(importMainFieldFilePath)
	if m.importFromStdin {
		first = int(importMainFieldFormat)
	}

	if m.importButtonsFocused {
		if reverse {
			if m.importButtonCursor == importActionCancel {
				m.importButtonCursor = importActionPrimary
				return
			}

			m.importMainCursor = count - 1
			m.focusImportMain()
			return
		}

		if m.importButtonCursor == importActionPrimary {
			m.importButtonCursor = importActionCancel
			return
		}

		m.importMainCursor = first
		m.focusImportMain()
		return
	}

	if reverse {
		if m.importMainCursor <= first {
			m.focusImportButton(importActionCancel)
			return
		}

		m.importMainCursor--
		m.focusImportMain()
		return
	}

	if m.importMainCursor >= count-1 {
		m.focusImportButton(importActionPrimary)
		return
	}

	m.importMainCursor++
	m.focusImportMain()
}

func (m *model) moveExportMainTabFocus(reverse bool) {
	if reverse {
		if m.importButtonsFocused {
			if m.importButtonCursor == importActionCancel {
				m.importButtonCursor = importActionPrimary
				return
			}

			m.exportMainCursor = int(exportMainFieldsCount) - 1
			m.focusExportMain()
			return
		} else if m.exportMainCursor == 0 {
			m.focusImportButton(importActionCancel)
		} else {
			m.exportMainCursor--
		}
	} else {
		if m.importButtonsFocused {
			if m.importButtonCursor == importActionPrimary {
				m.importButtonCursor = importActionCancel
				return
			}

			m.exportMainCursor = 0
			m.focusExportMain()
			return
		} else if m.exportMainCursor >= int(exportMainFieldsCount)-1 {
			m.focusImportButton(importActionPrimary)
		} else {
			m.exportMainCursor++
		}
	}

	if !m.importButtonsFocused {
		m.focusExportMain()
	}
}

func (m *model) moveExportFieldTabFocus(reverse bool, cursor *int, count int, focus func()) {
	if reverse {
		if m.importButtonsFocused {
			m.importButtonsFocused = false
			*cursor = count - 1
		} else if *cursor == 0 {
			m.focusImportButton(importActionCancel)
		} else {
			*cursor--
		}
	} else {
		if m.importButtonsFocused {
			m.importButtonCursor = nextCursor(m.importButtonCursor, importActionCount)
		} else if *cursor >= count-1 {
			m.focusImportButton(importActionPrimary)
		} else {
			*cursor++
		}
	}

	if !m.importButtonsFocused {
		focus()
	}
}

func (m *model) moveExportOutputFieldsTabFocus(reverse bool) {
	count := len(importMapFieldLabels) + 1
	if reverse {
		if m.importButtonsFocused {
			if m.importButtonCursor == importActionCancel {
				m.importButtonCursor = importActionPrimary
				return
			}

			m.exportOutputCursor = count - 1
			m.focusExportOutputField()
			return
		}

		if m.exportOutputCursor == 0 {
			m.focusImportButton(importActionCancel)
			return
		}

		m.exportOutputCursor--
		m.focusExportOutputField()
		return
	}

	if m.importButtonsFocused {
		if m.importButtonCursor == importActionPrimary {
			m.importButtonCursor = importActionCancel
			return
		}

		m.exportOutputCursor = 0
		m.focusExportOutputField()
		return
	}

	if m.exportOutputCursor >= count-1 {
		m.focusImportButton(importActionPrimary)
		return
	}

	m.exportOutputCursor++
	m.focusExportOutputField()
}

func (m *model) toggleExportOutputField() {
	if m.exportOutputCursor == 0 {
		m.exportScalar = !m.exportScalar
		if m.exportScalarField < 0 || m.exportScalarField >= len(importMapFieldKeys) {
			m.exportScalarField = indexOf(importMapFieldKeys, textio.FieldValue)
		}

		return
	}

	index := m.exportOutputCursor - 1
	if index < 0 || index >= len(importMapFieldKeys) {
		return
	}

	if m.exportScalar {
		m.exportScalarField = index
		return
	}

	if index < len(m.exportOutputFields) {
		if m.exportOutputFields[index] && m.exportSelectedOutputFieldCount() <= 1 {
			return
		}

		m.exportOutputFields[index] = !m.exportOutputFields[index]
	}
}

func (m model) exportSelectedOutputFieldCount() int {
	count := 0
	for _, selected := range m.exportOutputFields {
		if selected {
			count++
		}
	}

	return count
}

func (m *model) moveImportMapFieldsTabFocus(reverse bool) {
	count := len(m.importMapFieldInputs)
	if count == 0 {
		return
	}

	if m.importButtonsFocused {
		if reverse {
			if m.importButtonCursor == importActionCancel {
				m.importButtonCursor = importActionPrimary
				return
			}

			m.importMapFieldsCursor = count - 1
			m.focusImportMapField()
			return
		}

		if m.importButtonCursor == importActionPrimary {
			m.importButtonCursor = importActionCancel
			return
		}

		m.importMapFieldsCursor = 0
		m.focusImportMapField()
		return
	}

	if reverse {
		if m.importMapFieldsCursor <= 0 {
			m.focusImportButton(importActionCancel)
			return
		}

		m.importMapFieldsCursor--
		m.focusImportMapField()
		return
	}

	if m.importMapFieldsCursor >= count-1 {
		m.focusImportButton(importActionPrimary)
		return
	}

	m.importMapFieldsCursor++
	m.focusImportMapField()
}

func (m *model) moveImportMapPathsTabFocus(reverse bool) {
	m.normalizeMapPathRows(&m.opts)
	count := len(m.importMapPathRows)
	if count == 0 {
		return
	}

	if m.importButtonsFocused {
		if reverse {
			if m.importButtonCursor == importActionCancel {
				m.importButtonCursor = importActionPrimary
				return
			}

			m.importMapPathsCursor = (count - 1) * 2
			m.focusImportMapPath()
			return
		}

		if m.importButtonCursor == importActionPrimary {
			m.importButtonCursor = importActionCancel
			return
		}

		m.importMapPathsCursor = 0
		m.focusImportMapPath()
		return
	}

	row, _ := m.importMapPathCursorPosition()
	if reverse {
		if row <= 0 {
			m.focusImportButton(importActionCancel)
			return
		}

		m.importMapPathsCursor = (row - 1) * 2
		m.focusImportMapPath()
		return
	}

	if row >= count-1 {
		m.focusImportButton(importActionPrimary)
		return
	}

	m.importMapPathsCursor = (row + 1) * 2
	m.focusImportMapPath()
}

func (m *model) moveImportDefaultsTabFocus(reverse bool) {
	const count = 6

	if m.importButtonsFocused {
		if reverse {
			if m.importButtonCursor == importActionCancel {
				m.importButtonCursor = importActionPrimary
				return
			}

			m.importDefaultsCursor = count - 1
			m.focusImportDefaults()
			return
		}

		if m.importButtonCursor == importActionPrimary {
			m.importButtonCursor = importActionCancel
			return
		}

		m.importDefaultsCursor = 0
		m.focusImportDefaults()
		return
	}

	if reverse {
		if m.importDefaultsCursor <= 0 {
			m.focusImportButton(importActionCancel)
			return
		}

		m.importDefaultsCursor--
		m.focusImportDefaults()
		return
	}

	if m.importDefaultsCursor >= count-1 {
		m.focusImportButton(importActionPrimary)
		return
	}

	m.importDefaultsCursor++
	m.focusImportDefaults()
}

func (m *model) clearImportButtonFocus() {
	m.importButtonsFocused = false
}

func (m model) snapshotImportMapFields() []string {
	values := make([]string, len(m.importMapFieldInputs))
	for i := range m.importMapFieldInputs {
		values[i] = m.importMapFieldInputs[i].Value()
	}

	return values
}

func (m *model) restoreImportMapFields(values []string) {
	for i := range m.importMapFieldInputs {
		value := ""
		if i < len(values) {
			value = values[i]
		}

		m.importMapFieldInputs[i].SetValue(value)
		m.importMapFieldInputs[i].SetCursor(len([]rune(value)))
	}
}

func (m model) snapshotImportMapPaths() [][2]string {
	values := make([][2]string, len(m.importMapPathRows))
	for i := range m.importMapPathRows {
		values[i] = [2]string{m.importMapPathRows[i].awsPath.Value(), m.importMapPathRows[i].filePath.Value()}
	}

	return values
}

func (m *model) restoreImportMapPaths(values [][2]string) {
	if len(values) == 0 {
		values = [][2]string{{"", ""}}
	}

	m.importMapPathRows = make([]importMapPathRow, len(values))
	for i, value := range values {
		m.importMapPathRows[i] = newImportMapPathRow(&m.opts)
		m.importMapPathRows[i].awsPath.SetValue(value[0])
		m.importMapPathRows[i].awsPath.SetCursor(len([]rune(value[0])))
		m.importMapPathRows[i].filePath.SetValue(value[1])
		m.importMapPathRows[i].filePath.SetCursor(len([]rune(value[1])))
	}

	m.normalizeMapPathRows(&m.opts)
}

func snapshotTextInputs(inputs []textinput.Model) []string {
	values := make([]string, len(inputs))
	for i := range inputs {
		values[i] = inputs[i].Value()
	}

	return values
}

func restoreTextInputs(inputs []textinput.Model, values []string) {
	for i := range inputs {
		value := ""
		if i < len(values) {
			value = values[i]
		}

		inputs[i].SetValue(value)
		inputs[i].SetCursor(len([]rune(value)))
	}
}

func snapshotMapPathRows(rows []importMapPathRow) [][2]string {
	values := make([][2]string, len(rows))
	for i := range rows {
		values[i] = [2]string{rows[i].awsPath.Value(), rows[i].filePath.Value()}
	}

	return values
}

func restoreMapPathRows(rows *[]importMapPathRow, values [][2]string, opts *Options) {
	if len(values) == 0 {
		values = [][2]string{{"", ""}}
	}

	*rows = make([]importMapPathRow, len(values))
	for i, value := range values {
		(*rows)[i] = newImportMapPathRow(opts)
		(*rows)[i].awsPath.SetValue(value[0])
		(*rows)[i].awsPath.SetCursor(len([]rune(value[0])))
		(*rows)[i].filePath.SetValue(value[1])
		(*rows)[i].filePath.SetCursor(len([]rune(value[1])))
	}
}

func (m *model) closeExportChildPopup() {
	m.popPopup()
	if m.activePopup == popupExportFile {
		m.focusExportMain()
	}
}

func (m *model) cancelExportOutputFieldsPopup() {
	m.exportOutputFields = append([]bool(nil), m.exportOutputBackup...)
	m.exportScalar = m.exportScalarBackup
	m.exportScalarField = m.exportScalarFieldBackup
	m.closeExportChildPopup()
}

func (m *model) cancelExportMapFieldsPopup() {
	restoreTextInputs(m.exportMapFieldInputs, m.exportMapFieldBackup)
	m.closeExportChildPopup()
}

func (m *model) cancelExportMapPathsPopup() {
	restoreMapPathRows(&m.exportMapPathRows, m.exportMapPathBackup, &m.opts)
	m.normalizeExportMapPathRows(&m.opts)
	m.closeExportChildPopup()
}

func (m model) snapshotImportDefaults() importDefaultsSnapshot {
	return importDefaultsSnapshot{
		region:      m.importDefaultRegion,
		typ:         m.importDefaultType,
		tier:        m.importDefaultTier,
		dataType:    m.importDefaultDataType,
		policies:    m.importDefaultPolicies.Value(),
		description: m.importDefaultDescription.Value(),
	}
}

func (m *model) restoreImportDefaults(snapshot importDefaultsSnapshot) {
	m.importDefaultRegion = snapshot.region
	m.importDefaultType = snapshot.typ
	m.importDefaultTier = snapshot.tier
	m.importDefaultDataType = snapshot.dataType
	m.importDefaultPolicies.SetValue(snapshot.policies)
	m.importDefaultDescription.SetValue(snapshot.description)
}

func (m *model) closeImportChildPopup() {
	m.popPopup()
	if m.activePopup == popupImportFile {
		m.focusImportMain()
	}
}

func (m *model) closeFilePickerPopup() {
	m.popPopup()

	switch m.activePopup {
	case popupImportFile:
		m.focusImportMain()
	case popupExportFile:
		m.focusExportMain()
	case popupFileAction:
		m.editorButtonsFocused = false
		m.input.Focus()
	}
}

func (m *model) openImportFilePickerPopup() tea.Cmd {
	picker := newImportFilePicker(&m.opts)
	path := m.importFilePathInput.Value()
	picker.CurrentDirectory = importFilePickerStartDirectory(path)
	picker.Height = m.importFilePickerHeight()

	m.importFilePicker = picker
	m.importFilePickerTarget = filePickerTargetImportFile
	m.importFilePickerCurrentFocused = false
	m.importFilePickerParentFocused = false
	m.importFilePickerButtonsFocused = false
	m.importFilePickerTargetName = importFilePickerTargetName(path)
	m.importFilePickerMinInnerWidth = importFilePickerStableMinInnerWidth(picker)
	m.importButtonsFocused = false
	m.pushNestedPopup(popupImportFilePicker)

	return m.importFilePicker.Init()
}

func (m *model) openPopupFileActionPicker() tea.Cmd {
	picker := newImportFilePicker(&m.opts)
	path := m.input.Value()
	picker.CurrentDirectory = importFilePickerStartDirectory(path)
	picker.Height = m.importFilePickerHeight()

	m.importFilePicker = picker
	m.importFilePickerTarget = filePickerTargetPopupFileAction
	m.importFilePickerCurrentFocused = false
	m.importFilePickerParentFocused = false
	m.importFilePickerButtonsFocused = false
	m.importFilePickerTargetName = importFilePickerTargetName(path)
	m.importFilePickerMinInnerWidth = importFilePickerStableMinInnerWidth(picker)
	m.editorButtonsFocused = false
	m.input.Blur()
	m.pushNestedPopup(popupImportFilePicker)

	return m.importFilePicker.Init()
}

func (m *model) openExportFilePickerPopup() tea.Cmd {
	m.ensureExportState()
	picker := newImportFilePicker(&m.opts)
	path := m.exportFilePathInput.Value()
	picker.CurrentDirectory = importFilePickerStartDirectory(path)
	picker.Height = m.importFilePickerHeight()

	m.importFilePicker = picker
	m.importFilePickerTarget = filePickerTargetExportFile
	m.importFilePickerCurrentFocused = false
	m.importFilePickerParentFocused = false
	m.importFilePickerButtonsFocused = false
	m.importFilePickerTargetName = importFilePickerTargetName(path)
	m.importFilePickerMinInnerWidth = importFilePickerStableMinInnerWidth(picker)
	m.importButtonsFocused = false
	m.pushNestedPopup(popupImportFilePicker)

	return m.importFilePicker.Init()
}

func (m *model) cancelImportMapFieldsPopup() {
	m.restoreImportMapFields(m.importMapFieldBackup)
	m.closeImportChildPopup()
}

func (m *model) cancelImportMapPathsPopup() {
	m.restoreImportMapPaths(m.importMapPathBackup)
	m.closeImportChildPopup()
}

func (m *model) cancelImportDefaultsPopup() {
	m.restoreImportDefaults(m.importDefaultsBackup)
	m.closeImportChildPopup()
}

func detectedImportFormatFromPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".env":
		return importFormatDotenv
	case ".json":
		return importFormatJSON
	case ".yaml", ".yml":
		return importFormatYAML
	default:
		return ""
	}
}

func detectedImportFormatFromBytes(data []byte) string {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return importFormatDotenv
	}

	if json.Valid(trimmed) {
		return importFormatJSON
	}

	firstLine := strings.TrimSpace(strings.SplitN(string(trimmed), "\n", 2)[0])
	if strings.HasPrefix(firstLine, "---") ||
		strings.HasPrefix(firstLine, "- ") ||
		strings.Contains(firstLine, ":") && !strings.Contains(firstLine, "=") {
		return importFormatYAML
	}

	return importFormatDotenv
}

func importFilePickerStartDirectory(path string) string {
	path = importExpandUserPath(strings.TrimSpace(path))
	if path == "" {
		return importDefaultDirectory()
	}

	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return importAbsolutePath(path)
	}

	dir := filepath.Dir(path)
	if dir == "" {
		return importDefaultDirectory()
	}

	return importAbsolutePath(dir)
}

func importFilePickerTargetName(path string) string {
	path = importExpandUserPath(strings.TrimSpace(path))
	if path == "" {
		return ""
	}

	absolute := importAbsolutePath(path)
	if info, err := os.Stat(absolute); err == nil && info.IsDir() {
		return ""
	}

	name := filepath.Base(absolute)
	if name == "." || name == string(os.PathSeparator) {
		return ""
	}

	return name
}

func importExpandUserPath(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}

		if path == "~" {
			return home
		}

		return filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}

	return path
}

func importAbsolutePath(path string) string {
	path = importExpandUserPath(path)
	if path == "" {
		return importDefaultDirectory()
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}

	return abs
}

func importShortestDisplayPath(path string) string {
	absolute := importAbsolutePath(path)
	cwd, err := os.Getwd()
	if err != nil {
		return absolute
	}

	relative, err := filepath.Rel(cwd, absolute)
	if err != nil {
		return absolute
	}
	if relative == "." {
		return "."
	}

	parentPrefix := ".." + string(os.PathSeparator)
	currentPrefix := "." + string(os.PathSeparator)
	if relative != ".." && !strings.HasPrefix(relative, parentPrefix) && !strings.HasPrefix(relative, currentPrefix) {
		relative = "." + string(os.PathSeparator) + relative
	}

	if len([]rune(relative)) <= len([]rune(absolute)) {
		return relative
	}

	return absolute
}

func importFormatHotkeyIndex(key string) (int, bool) {
	switch key {
	case "d":
		return indexOf(importFormatOptions, importFormatDotenv), true
	case "j":
		return indexOf(importFormatOptions, importFormatJSON), true
	case "y":
		return indexOf(importFormatOptions, importFormatYAML), true
	default:
		return 0, false
	}
}

func (component popupUpdateComponent) updateImportFilePopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	key := msg.String()

	if importPrimaryActionKey(key) {
		return m.loadImportFileIntoList()
	}

	if m.importButtonsFocused && importEnterKey(key) {
		if m.importButtonCursor == importActionCancel {
			m.popPopup()
		} else {
			return m.loadImportFileIntoList()
		}

		return m, nil
	}

	if key == "tab" || key == "shift+tab" {
		m.moveImportMainTabFocus(key == "shift+tab")
		return m, nil
	}

	if (&m).navigateImportSelectorButtons(key) {
		if !m.importButtonsFocused {
			m.focusImportMain()
		}

		return m, nil
	}

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
			m.importButtonsFocused = false
			m.pushNestedPopup(popupImportKeyField)
		case importMainFieldFormat:
			m.importFormatCursor = indexOf(importFormatOptions, m.importFormat)
			m.importButtonsFocused = false
			m.pushNestedPopup(popupImportFormat)
		case importMainFieldMapFields:
			m.importMapFieldBackup = m.snapshotImportMapFields()
			m.importMapFieldsCursor = 0
			m.focusImportMapField()
			m.pushNestedPopup(popupImportMapFields)
		case importMainFieldMapPaths:
			m.importMapPathBackup = m.snapshotImportMapPaths()
			m.importMapPathsCursor = 0
			m.focusImportMapPath()
			m.pushNestedPopup(popupImportMapPaths)
		case importMainFieldDefaults:
			m.importDefaultsBackup = m.snapshotImportDefaults()
			m.importDefaultsCursor = 0
			m.focusImportDefaults()
			m.pushNestedPopup(popupImportDefaults)
		case importMainFieldFilePath, importMainFieldsCount:
			if m.importFromStdin {
				return m, nil
			}

			cmd := (&m).openImportFilePickerPopup()
			return m, cmd
		}
	default:
		return m.updateImportMainInput(msg)
	}

	return m, nil
}

func (component popupUpdateComponent) updateExportFilePopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	m.ensureExportState()
	key := msg.String()

	if importPrimaryActionKey(key) {
		return m.exportVisibleToFile()
	}

	if m.importButtonsFocused && importEnterKey(key) {
		if m.importButtonCursor == importActionCancel {
			m.popPopup()
		} else {
			return m.exportVisibleToFile()
		}

		return m, nil
	}

	if key == "tab" || key == "shift+tab" {
		m.moveExportMainTabFocus(key == "shift+tab")
		return m, nil
	}

	if (&m).navigateImportSelectorButtons(key) {
		if !m.importButtonsFocused {
			m.focusExportMain()
		}

		return m, nil
	}

	if action, ok := m.editorNavigationAction(key); ok {
		if cursor, moved := importFieldCursorFromNavigation(m.exportMainCursor, int(exportMainFieldsCount), action); moved {
			m.exportMainCursor = cursor
			m.focusExportMain()
			return m, nil
		}
	}

	switch key {
	case "ctrl+_", "ctrl+/":
		m.openPopupShortcuts(screenMain, popupExportFile)
	case "q", "esc", "ctrl+g":
		m.popPopup()
	case " ", "enter", "ctrl+j":
		switch exportMainField(m.exportMainCursor) {
		case exportMainFieldFormat:
			m.exportFormatCursor = indexOf(importFormatOptions, m.exportFormat)
			m.importButtonsFocused = false
			m.pushNestedPopup(popupExportFormat)
		case exportMainFieldKeyField:
			m.exportKeyFieldCursor = indexOf(importKeyFieldOptions, m.exportKeyField)
			m.importButtonsFocused = false
			m.pushNestedPopup(popupExportKeyField)
		case exportMainFieldOutputFields:
			m.exportOutputBackup = append([]bool(nil), m.exportOutputFields...)
			m.exportScalarBackup = m.exportScalar
			m.exportScalarFieldBackup = m.exportScalarField
			m.exportOutputCursor = 0
			m.focusExportOutputField()
			m.pushNestedPopup(popupExportOutputFields)
		case exportMainFieldMapFields:
			m.exportMapFieldBackup = snapshotTextInputs(m.exportMapFieldInputs)
			m.exportMapFieldsCursor = 0
			m.focusExportMapField()
			m.pushNestedPopup(popupExportMapFields)
		case exportMainFieldMapPaths:
			m.exportMapPathBackup = snapshotMapPathRows(m.exportMapPathRows)
			m.exportMapPathsCursor = 0
			m.focusExportMapPath()
			m.pushNestedPopup(popupExportMapPaths)
		case exportMainFieldFilePath, exportMainFieldsCount:
			cmd := (&m).openExportFilePickerPopup()
			return m, cmd
		}
	default:
		return m.updateExportMainInput(msg)
	}

	return m, nil
}

func (m model) loadImportFileIntoList() (tea.Model, tea.Cmd) {
	source := "file"
	reader := bytes.NewReader(m.opts.ImportStdin)
	if !m.importFromStdin {
		path := strings.TrimSpace(m.importFilePathInput.Value())
		if path == "" {
			m.errMessage = "Import file path is required."
			m.message = ""
			return m, nil
		}

		data, err := os.ReadFile(importAbsolutePath(path))
		if err != nil {
			m.errMessage = fmt.Sprintf("Read import file: %v", err)
			m.message = ""
			return m, nil
		}
		source = "file"
		reader = bytes.NewReader(data)
	} else if len(bytes.TrimSpace(m.opts.ImportStdin)) == 0 {
		m.errMessage = "Import stdin is empty."
		m.message = ""
		return m, nil
	} else {
		source = "stdin"
	}

	options, err := m.importPlanOptions()
	if err != nil {
		m.errMessage = err.Error()
		m.message = ""
		return m, nil
	}

	planned, err := importer.Plan(m.contextProvider(), options, reader, m.client)
	if err != nil {
		m.errMessage = fmt.Sprintf("Import %s: %v", source, err)
		m.message = ""
		return m, nil
	}

	created, modified, unchanged, err := (&m).applyImportPlan(planned)
	if err != nil {
		m.errMessage = err.Error()
		m.message = ""
		return m, nil
	}

	m.applySortWithRules(m.sortRulesOrDefault())
	if len(planned) > 0 {
		first := planned[0]
		m.selectItem(inventory.Item{Path: first.Record.Path, Region: first.Region})
	}

	m.popPopup()
	m.errMessage = ""
	m.warningMessage = ""
	m.message = m.importLoadedMessage(len(planned), created, modified, unchanged)

	return m, nil
}

func (m model) importLoadedMessage(total, created, modified, unchanged int) string {
	base := fmt.Sprintf("Imported %d record(s): %d new, %d modified.", total, created, modified)
	if created == 0 && modified == 0 {
		return base
	}

	if unchanged > 0 {
		base = fmt.Sprintf("Imported %d record(s): %d new, %d modified, %d unchanged.", total, created, modified, unchanged)
	}

	return base + fmt.Sprintf(" Press P to push %s.", m.mainListScope())
}

func (m model) exportVisibleToFile() (tea.Model, tea.Cmd) {
	return m.exportVisibleToFileConfirmed(false)
}

func (m model) exportVisibleToFileConfirmed(overwriteConfirmed bool) (tea.Model, tea.Cmd) {
	path := strings.TrimSpace(m.exportFilePathInput.Value())
	if path == "" {
		m.errMessage = "Export file path is required."
		m.message = ""
		return m, nil
	}

	target := importAbsolutePath(path)
	info, err := os.Stat(target)
	if err == nil && info.IsDir() {
		m.errMessage = "Export file path must include a file name."
		m.message = ""
		return m, nil
	}
	if err == nil && !overwriteConfirmed {
		m.importButtonsFocused = true
		m.importButtonCursor = importActionPrimary
		m.pushNestedPopup(popupExportOverwriteConfirm)
		return m, nil
	}
	if err != nil && !os.IsNotExist(err) {
		m.errMessage = fmt.Sprintf("Check export file: %v", err)
		m.message = ""
		return m, nil
	}

	fields, mappings, scalarField := m.exportSelection()
	keyField := strings.TrimSpace(m.exportKeyField)
	recordFields := exportplan.RecordFields(fields, scalarField, keyField)

	records, err := m.exportVisibleRecords(recordFields)
	if err != nil {
		m.errMessage = err.Error()
		m.message = ""
		return m, nil
	}
	if len(records) == 0 {
		m.errMessage = "No visible parameters to export."
		m.message = ""
		return m, nil
	}

	file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		m.errMessage = fmt.Sprintf("Open export file: %v", err)
		m.message = ""
		return m, nil
	}
	defer file.Close()

	writer := textio.NewWriter(textio.FormatType(m.exportFormat), file)
	err = exportplan.Write(writer, records, mappings, recordFields, keyField, scalarField)
	if err != nil {
		m.errMessage = fmt.Sprintf("Export file: %v", err)
		m.message = ""
		return m, nil
	}

	m.clearPopupStack()
	m.errMessage = ""
	m.warningMessage = ""
	m.message = fmt.Sprintf("Exported %d %s parameter(s) to %s.", len(records), m.mainListScope(), path)

	return m, nil
}

func (m model) exportVisibleRecords(recordFields textio.Fields) (textio.Records, error) {
	pathMappings, err := m.exportPathMappings()
	if err != nil {
		return nil, err
	}

	records := make(textio.Records, 0, len(m.visible()))
	for _, idx := range m.visible() {
		if idx < 0 || idx >= len(m.statuses) {
			continue
		}

		status := m.statuses[idx]
		if status.PendingOperation() == parameterStateDeleted {
			continue
		}

		record := exportRecordFromStatus(status.localStatus(), recordFields)
		record.Path = pathMappings.ToFile(record.Path)
		records = append(records, record)
	}

	return records, nil
}

func exportRecordFromStatus(status Status, fields textio.Fields) textio.Record {
	return textio.Record{
		Path:        status.Item.Path,
		Fields:      fields,
		Region:      status.Item.Region,
		Value:       status.Value,
		Type:        status.Type,
		Tier:        status.Tier,
		DataType:    status.DataType,
		Policies:    status.Policies,
		Description: status.Description,
		Date:        status.Modified,
		Version:     status.Version,
		Len:         status.Length,
		SHA256:      status.SHA256Prefix,
		User:        status.User,
	}
}

func (m model) exportSelection() (textio.Fields, textio.FieldMappings, string) {
	fields := textio.Fields{}
	mappings := textio.FieldMappings{}
	seen := map[string]int{}

	if m.exportScalar {
		field := m.exportScalarFieldName()
		if field == "" {
			field = textio.FieldValue
		}

		return textio.Fields{field}, textio.FieldMappings{}, field
	}

	if !m.exportAllFieldsSelected() {
		for i := range m.exportOutputFields {
			if !m.exportOutputFields[i] || i >= len(importMapFieldKeys) {
				continue
			}

			awsName := importMapFieldKeys[i]
			seen[awsName] = len(mappings)
			fields = fields.With(awsName)
			mapping, ok := textio.DefaultFieldMappings().Find(awsName)
			if !ok {
				mapping = textio.FieldMapping{AWSName: awsName, FileName: awsName}
			}
			mappings = append(mappings, mapping)
		}
	}

	overrides := m.exportMapFieldMappings()
	if len(fields) == 0 {
		if len(overrides) == 0 {
			return fields, nil, ""
		}

		return fields, overrides, ""
	}

	for _, mapping := range overrides {
		if idx, ok := seen[mapping.AWSName]; ok {
			mappings[idx].FileName = mapping.FileName
		}
	}

	return fields, mappings, ""
}

func (m model) exportAllFieldsSelected() bool {
	if len(m.exportOutputFields) == 0 {
		return true
	}

	for _, selected := range m.exportOutputFields {
		if !selected {
			return false
		}
	}

	return true
}

func (m model) exportScalarFieldName() string {
	if m.exportScalarField >= 0 && m.exportScalarField < len(importMapFieldKeys) {
		return importMapFieldKeys[m.exportScalarField]
	}

	return textio.FieldValue
}

func (m model) exportMapFieldMappings() textio.FieldMappings {
	mappings := textio.FieldMappings{}
	for i := range m.exportMapFieldInputs {
		fileName := strings.TrimSpace(m.exportMapFieldInputs[i].Value())
		if fileName == "" || i >= len(importMapFieldKeys) {
			continue
		}

		mappings = append(mappings, textio.FieldMapping{AWSName: importMapFieldKeys[i], FileName: fileName})
	}

	return mappings
}

func (m model) exportPathMappings() (app.PathMappings, error) {
	m.normalizeExportMapPathRows(&m.opts)

	values := make([]string, 0, len(m.exportMapPathRows))
	for i := range m.exportMapPathRows {
		awsPath := strings.TrimSpace(m.exportMapPathRows[i].awsPath.Value())
		filePath := strings.TrimSpace(m.exportMapPathRows[i].filePath.Value())
		if awsPath == "" && filePath == "" {
			continue
		}
		if awsPath == "" {
			return nil, fmt.Errorf("map path row %d must include an AWS path", i+1)
		}

		values = append(values, awsPath+":"+filePath)
	}

	return app.ParsePathMappings(values)
}

func (m model) importPlanOptions() (*importer.Options, error) {
	region, err := m.importDefaultPlanRegion()
	if err != nil {
		return nil, err
	}

	pathMappings, err := m.importPathMappingsForPlan()
	if err != nil {
		return nil, err
	}

	return &importer.Options{
		Options: &app.Options{
			FilterGroups:   m.opts.FilterGroups,
			Region:         region,
			Regions:        []string{region},
			Profile:        m.opts.Profile,
			WithDecryption: m.opts.ShowSecureValues,
		},
		Format:         textio.FormatType(m.importFormat),
		FieldMappings:  m.importFieldMappingsForPlan(),
		Fields:         textio.Fields{},
		KeyField:       strings.TrimSpace(m.importKeyField),
		PathMappings:   pathMappings,
		DefaultRegion:  region,
		DefaultType:    m.importDefaultType,
		DefaultOptions: m.importDefaultOptionsForPlan(),
		Policy: importer.Policy{
			OnCreate: importer.PolicyNone,
			OnUpdate: importer.PolicyNone,
		},
	}, nil
}

func (m model) importDefaultPlanRegion() (string, error) {
	if region := strings.TrimSpace(m.importDefaultRegion); region != "" {
		return region, nil
	}

	if len(m.opts.Regions) == 1 && strings.TrimSpace(m.opts.Regions[0]) != "" {
		return strings.TrimSpace(m.opts.Regions[0]), nil
	}

	if region := strings.TrimSpace(m.opts.Region); region != "" && region != "all regions" {
		return region, nil
	}

	return "", fmt.Errorf("select a default region before importing")
}

func (m model) importFieldMappingsForPlan() textio.FieldMappings {
	mappings := make(textio.FieldMappings, 0, len(m.importMapFieldInputs))
	for i := range m.importMapFieldInputs {
		fileName := strings.TrimSpace(m.importMapFieldInputs[i].Value())
		if fileName == "" || i >= len(importMapFieldKeys) {
			continue
		}

		mappings = append(mappings, textio.FieldMapping{
			AWSName:  importMapFieldKeys[i],
			FileName: fileName,
		})
	}

	return mappings
}

func (m model) importPathMappingsForPlan() (app.PathMappings, error) {
	m.normalizeMapPathRows(&m.opts)

	values := make([]string, 0, len(m.importMapPathRows))
	for i := range m.importMapPathRows {
		awsPath := strings.TrimSpace(m.importMapPathRows[i].awsPath.Value())
		filePath := strings.TrimSpace(m.importMapPathRows[i].filePath.Value())
		if awsPath == "" && filePath == "" {
			continue
		}

		if awsPath == "" {
			return nil, fmt.Errorf("map path row %d must include an AWS path", i+1)
		}

		values = append(values, awsPath+":"+filePath)
	}

	return app.ParsePathMappings(values)
}

func (m model) importDefaultOptionsForPlan() ssm.PutParameterOptions {
	return ssm.PutParameterOptions{
		Tier:        m.importDefaultTier,
		DataType:    m.importDefaultDataType,
		Description: strings.TrimSpace(m.importDefaultDescription.Value()),
		Policies:    strings.TrimSpace(m.importDefaultPolicies.Value()),
	}
}

func (m *model) applyImportPlan(planned []importer.PlannedRecord) (created, modified, unchanged int, err error) {
	for i := range planned {
		state, changed, err := m.importPlannedStatus(&planned[i])
		if err != nil {
			return created, modified, unchanged, err
		}

		switch {
		case state.PendingOperation() == parameterStateNew:
			created++
		case state.PendingOperation() == parameterStateModified:
			modified++
		case !changed:
			unchanged++
		default:
			unchanged++
		}

		m.replaceStatusByKey(itemKey(state.Item.Region, state.Item.Path), &state)
	}

	return created, modified, unchanged, nil
}

func (m model) importPlannedStatus(planned *importer.PlannedRecord) (Status, bool, error) {
	item := inventory.Item{Path: planned.Record.Path, Region: planned.Region}
	key := itemKey(item.Region, item.Path)

	local := Status{
		Item:        item,
		Exists:      true,
		Type:        planned.Type.String(),
		Tier:        importPlanTier(planned).String(),
		DataType:    importPlanDataType(planned).String(),
		Policies:    importPlanPolicies(planned),
		Description: planned.Options.Description,
		Value:       planned.Record.Value,
	}

	if !planned.Exists {
		local.applyLocalCreate(planned.Type, planned.Options)
		return local, true, nil
	}

	current, found := m.importCurrentStatus(key)
	if found && current.PendingOperation() == parameterStateDeleted {
		return Status{}, false, fmt.Errorf("revert deleted parameter %s before importing over it", item.Path)
	}

	if found && current.PendingOperation() == parameterStateNew {
		local.applyLocalCreate(planned.Type, planned.Options)
		return local, true, nil
	}

	if found {
		cloud := current.Cloud
		if cloud.isZero() {
			cloud = current.snapshot()
		}

		base := current.cloudStatus()
		local.Version = base.Version
		local.Modified = base.Modified
		local.User = base.User
		local.applyLocalModification(cloud, planned.Type, planned.Options)

		return local, local.HasLocalChanges(), nil
	}

	cloudStatus := statusFromMetadata(&item, &planned.Existing)
	cloud := cloudStatus.snapshot()
	local.Version = cloudStatus.Version
	local.Modified = cloudStatus.Modified
	local.User = cloudStatus.User
	local.applyLocalModification(cloud, planned.Type, planned.Options)

	return local, local.HasLocalChanges(), nil
}

func (m model) importCurrentStatus(key string) (Status, bool) {
	for i := range m.statuses {
		if itemKey(m.statuses[i].Item.Region, m.statuses[i].Item.Path) == key {
			return m.statuses[i], true
		}
	}

	return Status{}, false
}

func importPlanTier(planned *importer.PlannedRecord) ssm.ParameterTier {
	if planned.Options.Tier.IsValid() {
		return planned.Options.Tier
	}

	if planned.Existing.Tier != "" {
		if tier, err := ssm.ParseParameterTier(planned.Existing.Tier); err == nil {
			return tier
		}
	}

	return ssm.DefaultParameterTier
}

func importPlanDataType(planned *importer.PlannedRecord) ssm.ParameterDataType {
	if planned.Options.DataType.IsValid() {
		return planned.Options.DataType
	}

	if planned.Existing.DataType != "" {
		if dataType, err := ssm.ParseParameterDataType(planned.Existing.DataType); err == nil {
			return dataType
		}
	}

	return ssm.DefaultParameterDataType
}

func importPlanPolicies(planned *importer.PlannedRecord) string {
	if planned.Options.PoliciesSet && strings.TrimSpace(planned.Options.Policies) == "[{}]" {
		return ""
	}

	return planned.Options.Policies
}

func (component popupUpdateComponent) updateImportFilePickerPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	key := msg.String()

	switch key {
	case "ctrl+_", "ctrl+/":
		m.openPopupShortcuts(m.filePickerShortcutScreen(), popupImportFilePicker)
		return m, nil
	case "q", "esc", "ctrl+g":
		m.closeFilePickerPopup()
		return m, nil
	}

	m.importFilePicker.Height = m.importFilePickerHeight()

	if cmd, handled := (&m).updateImportFilePickerFocus(key); handled {
		return m, cmd
	}

	if importPrimaryActionKey(key) {
		return (&m).chooseImportFilePickerSelection(msg)
	}

	var cmd tea.Cmd
	previousDirectory := m.importFilePicker.CurrentDirectory
	m.importFilePicker, cmd = m.importFilePicker.Update(msg)
	m.updateImportFilePickerWidthOnDirectoryChange(previousDirectory)
	if selected, path := m.importFilePicker.DidSelectFile(msg); selected {
		m.applyImportFilePickerPath(path)
		return m, nil
	}

	if disabled, path := m.importFilePicker.DidSelectDisabledFile(msg); disabled {
		m.warningMessage = "Cannot select " + path
	}

	return m, cmd
}

func (m *model) updateImportFilePickerFocus(key string) (tea.Cmd, bool) {
	if m.importFilePickerButtonsFocused {
		switch key {
		case "tab":
			if m.importButtonCursor == importActionPrimary {
				m.importButtonCursor = importActionCancel
			} else {
				m.importFilePickerButtonsFocused = false
				m.importButtonCursor = importActionPrimary
			}

			return nil, true
		case "shift+tab":
			if m.importButtonCursor == importActionCancel {
				m.importButtonCursor = importActionPrimary
			} else {
				m.importFilePickerButtonsFocused = false
				m.importFilePickerParentFocused = false
				m.importFilePickerCurrentFocused = false
			}

			return nil, true
		case "left":
			m.importButtonCursor = importActionPrimary
			return nil, true
		case "right":
			m.importButtonCursor = importActionCancel
			return nil, true
		case "enter", "ctrl+j":
			if m.importButtonCursor == importActionCancel {
				m.closeFilePickerPopup()
				return nil, true
			}

			updated, cmd := m.chooseImportFilePickerSelection(tea.KeyMsg{Type: tea.KeyEnter})
			if next, ok := updated.(model); ok {
				*m = next
			}
			return cmd, true
		}

		if action, ok := m.interpretNavigationKey(key, false); ok {
			return m.applyImportFilePickerButtonNavigation(action), true
		}

		return nil, true
	}

	switch key {
	case "tab":
		m.importFilePickerButtonsFocused = true
		m.importButtonCursor = importActionPrimary
		return nil, true
	case "shift+tab":
		m.importFilePickerButtonsFocused = true
		m.importButtonCursor = importActionCancel
		return nil, true
	}

	if sideAction, ok := m.importFilePickerSideAction(key); ok {
		switch sideAction {
		case importFilePickerSideParent:
			return m.changeImportFilePickerDirectory(filepath.Dir(m.importFilePicker.CurrentDirectory)), true
		case importFilePickerSideOpen:
			updated, cmd := m.chooseImportFilePickerSelection(tea.KeyMsg{Type: tea.KeyRight})
			if next, ok := updated.(model); ok {
				*m = next
			}
			return cmd, true
		}
	}

	if m.importFilePickerCurrentFocused {
		if importEnterKey(key) {
			m.applyImportFilePickerPath(m.importFilePicker.CurrentDirectory)
			return nil, true
		}

		if action, ok := m.interpretNavigationKey(key, false); ok {
			return m.applyImportFilePickerCurrentNavigation(action), true
		}

		return nil, false
	}

	if m.importFilePickerParentFocused {
		if importEnterKey(key) {
			return m.changeImportFilePickerDirectory(filepath.Dir(m.importFilePicker.CurrentDirectory)), true
		}

		if action, ok := m.interpretNavigationKey(key, false); ok {
			return m.applyImportFilePickerParentNavigation(action), true
		}

		return nil, false
	}

	if action, ok := m.interpretNavigationKey(key, false); ok {
		return m.applyImportFilePickerListNavigation(action), true
	}

	return nil, false
}

func (m *model) applyImportFilePickerButtonNavigation(action navigationAction) tea.Cmd {
	switch action {
	case navPrevious:
		m.importFilePickerButtonsFocused = false
		m.importFilePickerParentFocused = false
		m.importFilePickerCurrentFocused = false
	case navFirst:
		return m.focusImportFilePickerFirst()
	case navLast:
		m.importFilePickerButtonsFocused = false
		m.importFilePickerParentFocused = false
		m.importFilePickerCurrentFocused = false
		return m.updateImportFilePickerNavigation(navLast)
	}

	return nil
}

func (m *model) applyImportFilePickerCurrentNavigation(action navigationAction) tea.Cmd {
	switch action {
	case navNext, navPageDown:
		m.importFilePickerCurrentFocused = false
		m.importFilePickerParentFocused = true
	case navLast:
		m.importFilePickerCurrentFocused = false
		m.importFilePickerParentFocused = false
		return m.updateImportFilePickerNavigation(navLast)
	}

	return nil
}

func (m *model) applyImportFilePickerParentNavigation(action navigationAction) tea.Cmd {
	switch action {
	case navPrevious, navFirst:
		m.importFilePickerParentFocused = false
		m.importFilePickerCurrentFocused = true
	case navNext:
		m.importFilePickerCurrentFocused = false
		return m.focusImportFilePickerFirstList()
	case navLast:
		m.importFilePickerParentFocused = false
		m.importFilePickerCurrentFocused = false
		return m.updateImportFilePickerNavigation(navLast)
	case navPageDown:
		m.importFilePickerParentFocused = false
		m.importFilePickerCurrentFocused = false
		return m.updateImportFilePickerNavigation(navPageDown)
	}

	return nil
}

func (m *model) applyImportFilePickerListNavigation(action navigationAction) tea.Cmd {
	switch action {
	case navPrevious:
		if importFilePickerSelectedIndex(m.importFilePicker) == 0 {
			m.importFilePickerParentFocused = true
			m.importFilePickerCurrentFocused = false
			return nil
		}

		return m.updateImportFilePickerNavigation(navPrevious)
	case navNext, navPageUp, navPageDown, navLast:
		m.importFilePickerParentFocused = false
		m.importFilePickerCurrentFocused = false
		return m.updateImportFilePickerNavigation(action)
	case navFirst:
		return m.focusImportFilePickerFirst()
	}

	return nil
}

func (m *model) focusImportFilePickerFirst() tea.Cmd {
	m.importFilePickerButtonsFocused = false
	m.importFilePickerParentFocused = false
	m.importFilePickerCurrentFocused = true

	return nil
}

func (m *model) focusImportFilePickerFirstList() tea.Cmd {
	m.importFilePickerButtonsFocused = false
	m.importFilePickerParentFocused = false
	m.importFilePickerCurrentFocused = false

	return m.updateImportFilePickerNavigation(navFirst)
}

func (m *model) updateImportFilePickerNavigation(action navigationAction) tea.Cmd {
	if !importFilePickerFilesLoaded(m.importFilePicker) {
		return nil
	}

	msg, ok := importFilePickerNavigationMsg(action)
	if !ok {
		return nil
	}

	var cmd tea.Cmd
	m.importFilePicker, cmd = m.importFilePicker.Update(msg)

	return cmd
}

func importFilePickerNavigationMsg(action navigationAction) (tea.KeyMsg, bool) {
	switch action {
	case navPrevious:
		return tea.KeyMsg{Type: tea.KeyUp}, true
	case navNext:
		return tea.KeyMsg{Type: tea.KeyDown}, true
	case navPageUp:
		return tea.KeyMsg{Type: tea.KeyPgUp}, true
	case navPageDown:
		return tea.KeyMsg{Type: tea.KeyPgDown}, true
	case navFirst:
		return tea.KeyMsg{Type: tea.KeyHome}, true
	case navLast:
		return tea.KeyMsg{Type: tea.KeyEnd}, true
	default:
		return tea.KeyMsg{}, false
	}
}

type importFilePickerSideAction int

const (
	importFilePickerSideParent importFilePickerSideAction = iota
	importFilePickerSideOpen
)

func (m model) importFilePickerSideAction(key string) (importFilePickerSideAction, bool) {
	switch key {
	case "left", "backspace":
		return importFilePickerSideParent, true
	case "right":
		return importFilePickerSideOpen, true
	}

	if m.keymapStyle() == keymapVi {
		switch key {
		case "h":
			return importFilePickerSideParent, true
		case "l":
			return importFilePickerSideOpen, true
		}
	}

	return 0, false
}

func (m *model) chooseImportFilePickerSelection(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.importFilePickerCurrentFocused {
		m.applyImportFilePickerPath(m.importFilePicker.CurrentDirectory)
		return *m, nil
	}

	if m.importFilePickerParentFocused {
		m.applyImportFilePickerPath(filepath.Dir(m.importFilePicker.CurrentDirectory))
		return *m, nil
	}

	if path, ok := importFilePickerSelectedPath(m.importFilePicker); ok {
		m.applyImportFilePickerPath(path)
		return *m, nil
	}

	m.importFilePickerButtonsFocused = false
	m.importFilePicker.Height = m.importFilePickerHeight()

	var cmd tea.Cmd
	previousDirectory := m.importFilePicker.CurrentDirectory
	m.importFilePicker, cmd = m.importFilePicker.Update(msg)
	m.updateImportFilePickerWidthOnDirectoryChange(previousDirectory)

	return *m, cmd
}

func (m *model) updateImportFilePickerWidthOnDirectoryChange(previousDirectory string) {
	if m.importFilePicker.CurrentDirectory == previousDirectory {
		return
	}

	m.importFilePickerMinInnerWidth = importFilePickerStableMinInnerWidth(m.importFilePicker)
}

func (m *model) applyImportFilePickerPath(path string) {
	path = importShortestDisplayPathForPicker(path)

	switch m.importFilePickerTarget {
	case filePickerTargetPopupFileAction:
		m.input.SetValue(path)
		m.input.SetCursor(len([]rune(path)))
	case filePickerTargetExportFile:
		m.exportFilePathInput.SetValue(path)
		m.exportFilePathInput.SetCursor(len([]rune(path)))
		if detected := detectedImportFormatFromPath(path); detected != "" {
			m.exportFormat = detected
		}
	default:
		m.importFilePathInput.SetValue(path)
		m.importFilePathInput.SetCursor(len([]rune(path)))
		if detected := detectedImportFormatFromPath(path); detected != "" {
			m.importFormat = detected
		}
	}

	m.closeFilePickerPopup()
}

func importFilePickerSelectedPath(picker filepicker.Model) (string, bool) {
	entries := importFilePickerSortedEntries(picker.CurrentDirectory, picker.ShowHidden)
	idx := importFilePickerSelectedIndex(picker)
	if idx < 0 || idx >= len(entries) {
		return "", false
	}

	return filepath.Join(picker.CurrentDirectory, entries[idx].Name()), true
}

func importShortestDisplayPathForPicker(path string) string {
	display := importShortestDisplayPath(path)
	absolute := importAbsolutePath(path)
	info, err := os.Stat(absolute)
	if err != nil || !info.IsDir() {
		return display
	}

	if strings.HasSuffix(display, string(os.PathSeparator)) {
		return display
	}

	return display + string(os.PathSeparator)
}

func (m model) filePickerShortcutScreen() screen {
	if m.importFilePickerTarget == filePickerTargetPopupFileAction {
		return screenTextArea
	}

	return screenMain
}

func (m *model) changeImportFilePickerDirectory(path string) tea.Cmd {
	picker := newImportFilePicker(&m.opts)
	picker.CurrentDirectory = importAbsolutePath(path)
	picker.Height = m.importFilePickerHeight()

	m.importFilePicker = picker
	m.importFilePickerCurrentFocused = false
	m.importFilePickerParentFocused = false
	m.importFilePickerButtonsFocused = false
	m.importFilePickerTargetName = ""
	m.importFilePickerMinInnerWidth = importFilePickerStableMinInnerWidth(picker)

	return m.importFilePicker.Init()
}

func (m *model) focusImportFilePickerTarget() {
	if m.importFilePickerTargetName == "" || !importFilePickerFilesLoaded(m.importFilePicker) {
		return
	}

	index := importFilePickerFileIndex(m.importFilePicker.CurrentDirectory, m.importFilePickerTargetName, m.importFilePicker.ShowHidden)
	m.importFilePickerTargetName = ""
	if index < 0 {
		return
	}

	var cmd tea.Cmd
	m.importFilePicker, cmd = m.importFilePicker.Update(tea.KeyMsg{Type: tea.KeyHome})
	_ = cmd
	for i := 0; i < index; i++ {
		m.importFilePicker, cmd = m.importFilePicker.Update(tea.KeyMsg{Type: tea.KeyDown})
		_ = cmd
	}
}

func importFilePickerFilesLoaded(picker filepicker.Model) bool {
	value := reflect.ValueOf(picker)
	field := value.FieldByName("files")
	return field.IsValid() && field.Kind() == reflect.Slice && field.Len() > 0
}

func importFilePickerFileIndex(directory, name string, showHidden bool) int {
	entries := importFilePickerSortedEntries(directory, showHidden)
	for i, entry := range entries {
		if entry.Name() == name {
			return i
		}
	}

	return -1
}

func importFilePickerSortedEntries(directory string, showHidden bool) []os.DirEntry {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil
	}

	if !showHidden {
		filtered := entries[:0]
		for _, entry := range entries {
			hidden, _ := filepicker.IsHidden(entry.Name())
			if !hidden {
				filtered = append(filtered, entry)
			}
		}
		entries = filtered
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() == entries[j].IsDir() {
			return entries[i].Name() < entries[j].Name()
		}

		return entries[i].IsDir()
	})

	return entries
}

func importFilePickerSelectedIndex(picker filepicker.Model) int {
	value := reflect.ValueOf(picker)
	field := value.FieldByName("selected")
	if !field.IsValid() || field.Kind() != reflect.Int {
		return -1
	}

	return int(field.Int())
}

func (component popupUpdateComponent) updateImportFormatPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	key := msg.String()

	if importPrimaryActionKey(key) {
		if len(importFormatOptions) > 0 {
			m.importFormat = importFormatOptions[min(m.importFormatCursor, len(importFormatOptions)-1)]
		}

		m.closeImportChildPopup()
		return m, nil
	}

	if m.importButtonsFocused && importEnterKey(key) {
		if m.importButtonCursor == importActionCancel {
			m.closeImportChildPopup()
			return m, nil
		}

		if len(importFormatOptions) > 0 {
			m.importFormat = importFormatOptions[min(m.importFormatCursor, len(importFormatOptions)-1)]
		}

		m.closeImportChildPopup()
		return m, nil
	}

	if (&m).navigateImportSelectorButtons(key) {
		return m, nil
	}

	if (&m).handleSelectorNavigation(key, &m.importFormatCursor, len(importFormatOptions)) {
		return m, nil
	}

	if idx, ok := importFormatHotkeyIndex(key); ok {
		m.importFormatCursor = idx
		m.importFormat = importFormatOptions[idx]
		m.closeImportChildPopup()

		return m, nil
	}

	switch key {
	case "ctrl+_", "ctrl+/":
		m.openPopupShortcuts(screenMain, popupImportFormat)
	case "q", "esc", "ctrl+g":
		m.closeImportChildPopup()
	case "enter", "ctrl+j":
		if len(importFormatOptions) > 0 {
			m.importFormat = importFormatOptions[min(m.importFormatCursor, len(importFormatOptions)-1)]
		}

		m.closeImportChildPopup()
	}

	return m, nil
}

func (component popupUpdateComponent) updateExportFormatPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	key := msg.String()

	if importPrimaryActionKey(key) || importEnterKey(key) && !m.importButtonsFocused {
		m.exportFormat = importFormatOptions[m.exportFormatCursor]
		m.closeExportChildPopup()
		return m, nil
	}

	if m.importButtonsFocused && importEnterKey(key) {
		if m.importButtonCursor == importActionCancel {
			m.closeExportChildPopup()
		} else {
			m.exportFormat = importFormatOptions[m.exportFormatCursor]
			m.closeExportChildPopup()
		}

		return m, nil
	}

	if key == "tab" || key == "shift+tab" {
		m.moveExportFieldTabFocus(key == "shift+tab", &m.exportFormatCursor, len(importFormatOptions), func() {})
		return m, nil
	}

	if index, ok := importFormatHotkeyIndex(key); ok {
		m.exportFormatCursor = index
		m.exportFormat = importFormatOptions[index]
		m.closeExportChildPopup()
		return m, nil
	}

	if (&m).navigateImportSelectorButtons(key) {
		return m, nil
	}

	if action, ok := m.editorNavigationAction(key); ok {
		if cursor, moved := importFieldCursorFromNavigation(m.exportFormatCursor, len(importFormatOptions), action); moved {
			m.exportFormatCursor = cursor
			return m, nil
		}
	}

	switch key {
	case "ctrl+_", "ctrl+/":
		m.openPopupShortcuts(screenMain, popupExportFormat)
	case "q", "esc", "ctrl+g":
		m.closeExportChildPopup()
	}

	return m, nil
}

func (component popupUpdateComponent) updateExportKeyFieldPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	key := msg.String()

	if importPrimaryActionKey(key) || importEnterKey(key) && !m.importButtonsFocused {
		m.exportKeyField = importKeyFieldOptions[m.exportKeyFieldCursor]
		m.closeExportChildPopup()
		return m, nil
	}

	if m.importButtonsFocused && importEnterKey(key) {
		if m.importButtonCursor == importActionCancel {
			m.closeExportChildPopup()
		} else {
			m.exportKeyField = importKeyFieldOptions[m.exportKeyFieldCursor]
			m.closeExportChildPopup()
		}

		return m, nil
	}

	if key == "tab" || key == "shift+tab" {
		m.moveExportFieldTabFocus(key == "shift+tab", &m.exportKeyFieldCursor, len(importKeyFieldOptions), func() {})
		return m, nil
	}

	if (&m).navigateImportSelectorButtons(key) {
		return m, nil
	}

	if action, ok := m.editorNavigationAction(key); ok {
		if cursor, moved := importFieldCursorFromNavigation(m.exportKeyFieldCursor, len(importKeyFieldOptions), action); moved {
			m.exportKeyFieldCursor = cursor
			return m, nil
		}
	}

	switch key {
	case "ctrl+_", "ctrl+/":
		m.openPopupShortcuts(screenMain, popupExportKeyField)
	case "q", "esc", "ctrl+g":
		m.closeExportChildPopup()
	}

	return m, nil
}

func (component popupUpdateComponent) updateExportOutputFieldsPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	key := msg.String()

	if importPrimaryActionKey(key) {
		m.closeExportChildPopup()
		return m, nil
	}

	if m.importButtonsFocused && importEnterKey(key) {
		if m.importButtonCursor == importActionCancel {
			m.cancelExportOutputFieldsPopup()
		} else {
			m.closeExportChildPopup()
		}

		return m, nil
	}

	if key == "tab" || key == "shift+tab" {
		m.moveExportOutputFieldsTabFocus(key == "shift+tab")
		return m, nil
	}

	if action, ok := m.editorNavigationAction(key); ok {
		if cursor, moved := importFieldCursorFromNavigation(m.exportOutputCursor, len(importMapFieldLabels)+1, action); moved {
			m.exportOutputCursor = cursor
			m.focusExportOutputField()
			return m, nil
		}
	}

	switch key {
	case "ctrl+_", "ctrl+/":
		m.openPopupShortcuts(screenMain, popupExportOutputFields)
	case "q", "esc", "ctrl+g":
		m.cancelExportOutputFieldsPopup()
	case " ", "enter", "ctrl+j":
		m.toggleExportOutputField()
	}

	return m, nil
}

func (component popupUpdateComponent) updateExportMapFieldsPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	return m.updateExportTextFieldsPopup(msg, popupExportMapFields, &m.exportMapFieldsCursor, m.exportMapFieldInputs, m.focusExportMapField, m.cancelExportMapFieldsPopup, m.closeExportChildPopup)
}

func (m model) updateExportTextFieldsPopup(msg tea.KeyMsg, kind popupKind, cursor *int, inputs []textinput.Model, focus func(), cancel func(), apply func()) (tea.Model, tea.Cmd) {
	key := msg.String()

	if importPrimaryActionKey(key) {
		apply()
		return m, nil
	}

	if m.importButtonsFocused && importEnterKey(key) {
		if m.importButtonCursor == importActionCancel {
			cancel()
		} else {
			apply()
		}

		return m, nil
	}

	if key == "tab" || key == "shift+tab" {
		m.moveExportFieldTabFocus(key == "shift+tab", cursor, len(inputs), focus)
		return m, nil
	}

	if action, ok := m.editorNavigationAction(key); ok {
		if next, moved := importFieldCursorFromNavigation(*cursor, len(inputs), action); moved {
			*cursor = next
			focus()
			return m, nil
		}
	}

	switch key {
	case "ctrl+_", "ctrl+/":
		m.openPopupShortcuts(screenMain, kind)
	case "q", "esc", "ctrl+g":
		cancel()
	case "enter", "ctrl+j":
		*cursor = nextCursor(*cursor, len(inputs))
		focus()
	default:
		cmd := m.updateTextInput(&inputs[*cursor], msg)
		return m, cmd
	}

	return m, nil
}

func (component popupUpdateComponent) updateExportMapPathsPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	key := msg.String()

	if importPrimaryActionKey(key) {
		m.closeExportChildPopup()
		return m, nil
	}

	if m.importButtonsFocused && importEnterKey(key) {
		if m.importButtonCursor == importActionCancel {
			m.cancelExportMapPathsPopup()
		} else {
			m.closeExportChildPopup()
		}

		return m, nil
	}

	if key == "tab" || key == "shift+tab" {
		m.moveExportMapPathsTabFocus(key == "shift+tab")
		return m, nil
	}

	if action, ok := m.editorNavigationAction(key); ok {
		if cursor, moved := importFieldCursorFromNavigation(m.exportMapPathsCursor, len(m.exportMapPathRows)*2, action); moved {
			m.exportMapPathsCursor = cursor
			m.focusExportMapPath()
			return m, nil
		}
	}

	switch key {
	case "ctrl+_", "ctrl+/":
		m.openPopupShortcuts(screenMain, popupExportMapPaths)
	case "q", "esc", "ctrl+g":
		m.cancelExportMapPathsPopup()
	case "enter", "ctrl+j":
		m.moveExportMapPathCursorToNextInput()
	default:
		row, side := m.exportMapPathCursorPosition()
		var cmd tea.Cmd
		if side == 0 {
			cmd = m.updateTextInput(&m.exportMapPathRows[row].awsPath, msg)
		} else {
			cmd = m.updateTextInput(&m.exportMapPathRows[row].filePath, msg)
		}

		m.normalizeExportMapPathRows(&m.opts)
		m.focusExportMapPath()
		return m, cmd
	}

	return m, nil
}

func (component popupUpdateComponent) updateExportOverwriteConfirmPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	key := msg.String()

	if importPrimaryActionKey(key) {
		return m.exportVisibleToFileConfirmed(true)
	}

	if m.importButtonsFocused && importEnterKey(key) {
		if m.importButtonCursor == importActionCancel {
			m.popPopup()
			m.focusExportMain()
		} else {
			return m.exportVisibleToFileConfirmed(true)
		}

		return m, nil
	}

	switch key {
	case "tab":
		m.focusImportButton(nextCursor(m.importButtonCursor, importActionCount))
		return m, nil
	case "shift+tab":
		m.focusImportButton(previousCursor(m.importButtonCursor, importActionCount))
		return m, nil
	case "left":
		m.focusImportButton(importActionPrimary)
		return m, nil
	case "right":
		m.focusImportButton(importActionCancel)
		return m, nil
	}

	switch key {
	case "ctrl+_", "ctrl+/":
		m.openPopupShortcuts(screenMain, popupExportOverwriteConfirm)
	case "q", "esc", "ctrl+g":
		m.popPopup()
		m.focusExportMain()
	}

	return m, nil
}

func (m *model) moveExportMapPathsTabFocus(reverse bool) {
	m.normalizeExportMapPathRows(&m.opts)
	row, _ := m.exportMapPathCursorPosition()
	rowCount := len(m.exportMapPathRows)
	if rowCount == 0 {
		return
	}

	if reverse {
		if m.importButtonsFocused {
			m.importButtonsFocused = false
			m.exportMapPathsCursor = (rowCount - 1) * 2
			m.focusExportMapPath()
			return
		}

		if row == 0 {
			m.focusImportButton(importActionCancel)
			return
		}

		m.exportMapPathsCursor = (row - 1) * 2
		m.focusExportMapPath()
		return
	}

	if m.importButtonsFocused {
		m.importButtonCursor = nextCursor(m.importButtonCursor, importActionCount)
		return
	}

	if row >= rowCount-1 {
		m.focusImportButton(importActionPrimary)
		return
	}

	m.exportMapPathsCursor = (row + 1) * 2
	m.focusExportMapPath()
}

func (m *model) moveExportMapPathCursorToNextInput() {
	m.normalizeExportMapPathRows(&m.opts)
	if len(m.exportMapPathRows) == 0 {
		m.exportMapPathRows = append(m.exportMapPathRows, newImportMapPathRow(&m.opts))
	}

	row, side := m.exportMapPathCursorPosition()
	if side == 1 && row >= len(m.exportMapPathRows)-1 {
		if importMapPathRowEmpty(&m.exportMapPathRows[row]) {
			return
		}

		m.exportMapPathRows = append(m.exportMapPathRows, newImportMapPathRow(&m.opts))
	}

	m.exportMapPathsCursor = min(m.exportMapPathsCursor+1, len(m.exportMapPathRows)*2-1)
	m.focusExportMapPath()
}

func (component popupUpdateComponent) updateImportKeyFieldPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	key := msg.String()

	if importPrimaryActionKey(key) {
		if len(importKeyFieldOptions) > 0 {
			m.importKeyField = importKeyFieldOptions[min(m.importKeyFieldCursor, len(importKeyFieldOptions)-1)]
		}

		m.closeImportChildPopup()
		return m, nil
	}

	if m.importButtonsFocused && importEnterKey(key) {
		if m.importButtonCursor == importActionCancel {
			m.closeImportChildPopup()
			return m, nil
		}

		if len(importKeyFieldOptions) > 0 {
			m.importKeyField = importKeyFieldOptions[min(m.importKeyFieldCursor, len(importKeyFieldOptions)-1)]
		}

		m.closeImportChildPopup()
		return m, nil
	}

	if (&m).navigateImportSelectorButtons(key) {
		return m, nil
	}

	if (&m).handleSelectorNavigation(key, &m.importKeyFieldCursor, len(importKeyFieldOptions)) {
		return m, nil
	}

	switch key {
	case "ctrl+_", "ctrl+/":
		m.openPopupShortcuts(screenMain, popupImportKeyField)
	case "q", "esc", "ctrl+g":
		m.closeImportChildPopup()
	case "enter", "ctrl+j":
		if len(importKeyFieldOptions) > 0 {
			m.importKeyField = importKeyFieldOptions[min(m.importKeyFieldCursor, len(importKeyFieldOptions)-1)]
		}

		m.closeImportChildPopup()
	}

	return m, nil
}

func (component popupUpdateComponent) updateImportMapFieldsPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	key := msg.String()

	if importPrimaryActionKey(key) {
		m.closeImportChildPopup()
		return m, nil
	}

	if m.importButtonsFocused && importEnterKey(key) {
		if m.importButtonCursor == importActionCancel {
			m.cancelImportMapFieldsPopup()
			return m, nil
		}

		m.closeImportChildPopup()
		return m, nil
	}

	if key == "tab" || key == "shift+tab" {
		m.moveImportMapFieldsTabFocus(key == "shift+tab")
		return m, nil
	}

	if (&m).navigateImportSelectorButtons(key) {
		if !m.importButtonsFocused {
			m.focusImportMapField()
		}

		return m, nil
	}

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
	case "q", "esc", "ctrl+g":
		m.cancelImportMapFieldsPopup()
	case "enter", "ctrl+j":
		m.moveImportMapFieldCursorToNextLine()
	default:
		if m.importMapFieldBackspaceMovesPrevious(key) {
			m.importMapFieldsCursor = previousCursor(m.importMapFieldsCursor, len(m.importMapFieldInputs))
			m.focusImportMapField()

			return m, nil
		}

		cmd := m.updateTextInput(&m.importMapFieldInputs[m.importMapFieldsCursor], msg)

		return m, cmd
	}

	return m, nil
}

func (m *model) moveImportMapFieldCursorToNextLine() {
	if len(m.importMapFieldInputs) == 0 {
		return
	}

	m.importMapFieldsCursor = min(m.importMapFieldsCursor+1, len(m.importMapFieldInputs)-1)
	m.focusImportMapField()
}

func (component popupUpdateComponent) updateImportMapPathsPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	key := msg.String()

	if importPrimaryActionKey(key) {
		m.closeImportChildPopup()
		return m, nil
	}

	if m.importButtonsFocused && importEnterKey(key) {
		if m.importButtonCursor == importActionCancel {
			m.cancelImportMapPathsPopup()
			return m, nil
		}

		m.closeImportChildPopup()
		return m, nil
	}

	if key == "tab" || key == "shift+tab" {
		m.moveImportMapPathsTabFocus(key == "shift+tab")
		return m, nil
	}

	if (&m).navigateImportSelectorButtons(key) {
		if !m.importButtonsFocused {
			m.focusImportMapPath()
		}

		return m, nil
	}

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
	case "q", "esc", "ctrl+g":
		m.cancelImportMapPathsPopup()
	case "enter", "ctrl+j":
		m.moveImportMapPathCursorToNextInput()
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
			cmd = m.updateTextInput(&m.importMapPathRows[row].awsPath, msg)
		} else {
			cmd = m.updateTextInput(&m.importMapPathRows[row].filePath, msg)
		}

		m.normalizeMapPathRows(&m.opts)
		m.focusImportMapPath()

		return m, cmd
	}

	return m, nil
}

func (m *model) moveImportMapPathCursorToNextInput() {
	m.normalizeMapPathRows(&m.opts)

	if len(m.importMapPathRows) == 0 {
		m.importMapPathRows = append(m.importMapPathRows, newImportMapPathRow(&m.opts))
	}

	row, side := m.importMapPathCursorPosition()
	if side == 1 && row >= len(m.importMapPathRows)-1 {
		if importMapPathRowEmpty(&m.importMapPathRows[row]) {
			return
		}

		m.importMapPathRows = append(m.importMapPathRows, newImportMapPathRow(&m.opts))
	}

	m.importMapPathsCursor = min(m.importMapPathsCursor+1, len(m.importMapPathRows)*2-1)
	m.focusImportMapPath()
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

	if importPrimaryActionKey(key) {
		m.closeImportChildPopup()
		return m, nil
	}

	if m.importButtonsFocused && importEnterKey(key) {
		if m.importButtonCursor == importActionCancel {
			m.cancelImportDefaultsPopup()
			return m, nil
		}

		m.closeImportChildPopup()
		return m, nil
	}

	if key == "tab" || key == "shift+tab" {
		m.moveImportDefaultsTabFocus(key == "shift+tab")
		return m, nil
	}

	if (&m).navigateImportSelectorButtons(key) {
		if !m.importButtonsFocused {
			m.focusImportDefaults()
		}

		return m, nil
	}

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
		m.cancelImportDefaultsPopup()
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
			m.closeImportChildPopup()
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
		cmd = m.updateTextInput(&m.importFilePathInput, msg)
		if detected := detectedImportFormatFromPath(m.importFilePathInput.Value()); detected != "" {
			m.importFormat = detected
		}
	case importMainFieldKeyField,
		importMainFieldFormat,
		importMainFieldMapFields,
		importMainFieldMapPaths,
		importMainFieldDefaults,
		importMainFieldsCount:
	}

	return m, cmd
}

func (m model) updateExportMainInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	if exportMainField(m.exportMainCursor) == exportMainFieldFilePath {
		cmd = m.updateTextInput(&m.exportFilePathInput, msg)
		if detected := detectedImportFormatFromPath(m.exportFilePathInput.Value()); detected != "" {
			m.exportFormat = detected
		}
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
	return importFieldLabel(m.importKeyField)
}

func (m model) importFormatDisplay() string {
	return importFormatLabel(m.importFormat)
}

func importFieldLabel(value string) string {
	switch value {
	case "":
		return ""
	case textio.FieldName:
		return "Name"
	case textio.FieldRegion:
		return "Region"
	case textio.FieldType:
		return "Type"
	case textio.FieldTier:
		return "Tier"
	case textio.FieldDataType:
		return "DataType"
	case textio.FieldPolicies:
		return "Policies"
	case textio.FieldDescription:
		return "Description"
	case textio.FieldValue:
		return "Value"
	case textio.FieldDate:
		return "Date"
	case textio.FieldVersion:
		return "Version"
	case textio.FieldLen:
		return "Len"
	case textio.FieldSHA256:
		return "SHA256"
	case textio.FieldUser:
		return "User"
	default:
		return value
	}
}

func importFormatLabel(value string) string {
	switch value {
	case "":
		return ""
	case importFormatDotenv:
		return "Dotenv"
	case importFormatJSON:
		return "JSON"
	case importFormatYAML:
		return "YAML"
	default:
		return value
	}
}

func (m model) importDefaultRegionDisplay() string {
	if m.importDefaultRegion == "" {
		return ""
	}

	return m.importDefaultRegion
}

func (m model) importDefaultTypeDisplay() string {
	if !m.importDefaultType.IsValid() {
		return ""
	}

	return m.importDefaultType.String()
}

func (m model) importDefaultTierDisplay() string {
	if !m.importDefaultTier.IsValid() {
		return ""
	}

	return m.importDefaultTier.String()
}

func (m model) importDefaultDataTypeDisplay() string {
	if !m.importDefaultDataType.IsValid() {
		return ""
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
