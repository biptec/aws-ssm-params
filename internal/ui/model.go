// Package ui implements the interactive terminal user interface.
package ui

import (
	"context"
	"time"

	"github.com/biptec/aws-ssm-params/internal/filter"
	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Options contains runtime settings passed from the CLI layer into the interactive TUI.
// It deliberately excludes CLI parsing details so the Bubble Tea model can be tested independently.
type Options struct {
	Environment               string
	Region                    string
	Regions                   []string
	Profile                   string
	NamesFile                 string
	FilterGroups              []filter.Group
	NoColor                   bool
	Keymap                    string
	ShowColumns               []string
	Sort                      []string
	Fields                    []string
	IncludeValues             bool
	ShowSecureValues          bool
	AllowNamesFileUpdate      bool
	UseInputTTY               bool
	NoConfirmOverwriteFile    bool
	NoConfirmWriteSecureValue bool
	NoConfirmDeleteOne        bool
	NoConfirmDeleteAll        bool
}

// screen identifies the currently active TUI view.
// The Update and View methods switch on this value to route key handling and rendering to screen-specific helpers.
type screen int

const (
	screenLoading screen = iota
	screenMain
	screenTextArea
	screenColumns
	screenConfirm
	screenRegionSelect
	screenTypeSelect
	screenHelp
)

const rawLeftLinePrefix = "\x00raw-left\x00"

const encryptedPlaceholderText = "(encrypted)"

const loadingSpinnerInterval = 120 * time.Millisecond

var loadingSpinnerFrames = []string{"|", "/", "-", "\\"}

type loadingTickMsg struct{}

// model is the full Bubble Tea application state.
// It stores immutable inputs such as the SSM client and inventory, dynamic data such as loaded statuses/search state,
// and per-screen state such as cursors, text inputs, confirmation prompts, and loading messages.
type model struct {
	client   ssm.Client
	ctx      context.Context
	opts     Options
	items    []inventory.Item
	statuses []Status
	loadCh   chan tea.Msg

	width  int
	height int

	screen       screen
	returnScreen screen
	activePopup  popupKind
	popupStack   []popupKind

	selected         int
	selectedExpanded bool

	searchMode     bool
	query          string
	effectiveQuery string
	searchInvalid  bool

	message        string
	warningMessage string
	errMessage     string
	busyMessage    string

	loadingTitle        string
	loadingLines        []string
	loadingSpinnerFrame int

	input                textinput.Model
	textArea             textarea.Model
	editPoliciesArea     textarea.Model
	editDescriptionArea  textarea.Model
	editPathInput        textinput.Model
	editDescriptionInput textinput.Model
	editFileInput        textinput.Model
	editField            editField
	editDirection        editDirection
	viInsertMode         bool
	editRegionOptions    []string
	pendingFileWrite     fileWriteConfirmation
	pendingQuit          bool
	pendingQuitKey       string
	editRegion           string
	editType             ssm.ParameterType
	editTier             ssm.ParameterTier
	editDataType         ssm.ParameterDataType
	editOverwrite        bool
	editNewParameter     bool
	editInitialSnapshot  editSnapshot
	regionCursor         int
	typeCursor           int
	tierCursor           int
	dataTypeCursor       int
	overwriteCursor      int
	typeReturnScreen     screen

	columns        map[columnName]bool
	columnsDraft   map[columnName]bool
	expandedFields map[editField]bool
	showGutters    bool
	columnCursor   int
	sortBy         columnName
	sortDescending bool
	sortRules      []sortRule
	sortCursor     int

	randomCursor      int
	valueActionCursor int
	fileActionMode    string
	fileActionField   editField

	confirmPrompt   string
	confirmExpected string
	confirmItems    []inventory.Item

	shortcutsFor       screen
	shortcutsPopupFor  popupKind
	pendingKeySequence string
}

// progressMsg is sent from the background status loader to update the loading screen with the current region/chunk.
type progressMsg struct {
	done, total int
	region      string
	items       []inventory.Item
}

// statusBatchMsg streams partial status rows while the initial loader is still running.
type statusBatchMsg []Status

// loadedMsg is sent once the initial status load has finished and the main table can be shown.
type loadedMsg []Status

// statusUpdatedMsg reports the result of saving one parameter value from an edit screen.
type statusUpdatedMsg struct {
	path    string
	oldPath string
	status  Status
	message string
	warning string
	err     error
}

// deleteDoneMsg reports the result of deleting one or more visible/selected parameters.
type deleteDoneMsg struct {
	items      []inventory.Item
	removeRows bool
	warning    string
	err        error
}

var (
	frameColor       = lipgloss.Color("24")
	labelFg          = lipgloss.Color("214")
	valueFg          = lipgloss.Color("254")
	mutedFg          = lipgloss.Color("244")
	selectedFg       = lipgloss.Color("81")
	missFg           = lipgloss.Color("245")
	emptyFg          = lipgloss.Color("45")
	errFg            = lipgloss.Color("203")
	tableHeaderFg    = lipgloss.Color("250")
	searchPromptFg   = lipgloss.Color("81")
	statusLineFg     = lipgloss.Color("244")
	warningFg        = lipgloss.Color("214")
	hotkeyFg         = lipgloss.Color("255")
	titleStyle       = lipgloss.NewStyle().Bold(true).Foreground(frameColor)
	labelStyle       = lipgloss.NewStyle().Foreground(labelFg)
	valueStyle       = lipgloss.NewStyle().Foreground(valueFg)
	mutedStyle       = lipgloss.NewStyle().Foreground(mutedFg)
	selectedRowStyle = lipgloss.NewStyle().Foreground(selectedFg)
	tableHeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(tableHeaderFg)
	searchStyle      = lipgloss.NewStyle().Foreground(searchPromptFg)
	errorStyle       = lipgloss.NewStyle().Foreground(errFg)
	footerStyle      = lipgloss.NewStyle().Foreground(statusLineFg)
	warningStyle     = lipgloss.NewStyle().Foreground(warningFg)
	hotkeyStyle      = lipgloss.NewStyle().Bold(true).Foreground(hotkeyFg)
	cursorStyle      = lipgloss.NewStyle().Reverse(true)
)

func pendingStatuses(items []inventory.Item) []Status {
	statuses := make([]Status, 0, len(items))
	for _, item := range items {
		statuses = append(statuses, Status{Item: item, Pending: true})
	}
	return statuses
}

func (m *model) mergeStatusBatch(batch []Status) {
	if len(batch) == 0 {
		return
	}
	incoming := map[string]*Status{}
	removePendingPath := map[string]bool{}
	for i := range batch {
		status := &batch[i]
		if status.Item.Path == "" {
			continue
		}
		incoming[itemKey(status.Item.Region, status.Item.Path)] = status
		if status.Item.Region != "" && status.Item.Region != "*" {
			removePendingPath[status.Item.Path] = true
		}
	}
	if len(incoming) == 0 {
		return
	}
	used := map[string]bool{}
	merged := make([]Status, 0, len(m.statuses)+len(incoming))
	for i := range m.statuses {
		status := m.statuses[i]
		key := itemKey(status.Item.Region, status.Item.Path)
		if next, ok := incoming[key]; ok {
			merged = append(merged, *next)
			used[key] = true
			continue
		}
		if status.Pending && removePendingPath[status.Item.Path] && (status.Item.Region == "" || status.Item.Region == "*") {
			continue
		}
		merged = append(merged, status)
	}
	for key, status := range incoming {
		if !used[key] {
			merged = append(merged, *status)
		}
	}
	m.statuses = merged
	m.applySortWithRules(m.sortRulesOrDefault())
	m.ensureSelection()
}

func (m *model) openActionsPopupForFocusedField() bool {
	component := editorActionsComponent{model: *m}
	defer func() { *m = component.model }()
	return component.openActionsPopupForFocusedField()
}

func (m model) updateValueActionsPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	component := editorActionsComponent{model: m}
	return component.updateValueActionsPopup(msg)
}

func (m model) updatePoliciesActionsPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	component := editorActionsComponent{model: m}
	return component.updatePoliciesActionsPopup(msg)
}

func (m model) updateFileActionPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	component := editorActionsComponent{model: m}
	return component.updateFileActionPopup(msg)
}

func (m model) updateFileWriteConfirmPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	component := editorActionsComponent{model: m}
	return component.updateFileWriteConfirmPopup(msg)
}

func (m model) updateUnsavedChangesPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	component := editorActionsComponent{model: m}
	return component.updateUnsavedChangesPopup(msg)
}

func (m model) updateRandomValuePopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	component := editorActionsComponent{model: m}
	return component.updateRandomValuePopup(msg)
}

func (m *model) moveActiveTextCursor(delta int) {
	component := editorCursorComponent{model: *m}
	defer func() { *m = component.model }()
	component.moveActiveTextCursor(delta)
}

func (m *model) moveActiveTextLine(delta int) {
	component := editorCursorComponent{model: *m}
	defer func() { *m = component.model }()
	component.moveActiveTextLine(delta)
}

func (m *model) moveActiveWrappedLine(delta int) {
	component := editorCursorComponent{model: *m}
	defer func() { *m = component.model }()
	component.moveActiveWrappedLine(delta)
}

func (m *model) activeTextCursorLineOffset() (line, offset int) {
	component := editorCursorComponent{model: *m}
	defer func() { *m = component.model }()
	return component.activeTextCursorLineOffset()
}

func (m *model) activeTextLineStart() {
	component := editorCursorComponent{model: *m}
	defer func() { *m = component.model }()
	component.activeTextLineStart()
}

func (m *model) activeTextLineEnd() {
	component := editorCursorComponent{model: *m}
	defer func() { *m = component.model }()
	component.activeTextLineEnd()
}

func (m *model) activeTextStart() {
	component := editorCursorComponent{model: *m}
	defer func() { *m = component.model }()
	component.activeTextStart()
}

func (m *model) activeTextEnd() {
	component := editorCursorComponent{model: *m}
	defer func() { *m = component.model }()
	component.activeTextEnd()
}

func (m *model) activeTextWordForward() {
	component := editorCursorComponent{model: *m}
	defer func() { *m = component.model }()
	component.activeTextWordForward()
}

func (m *model) activeTextWordBackward() {
	component := editorCursorComponent{model: *m}
	defer func() { *m = component.model }()
	component.activeTextWordBackward()
}

func (m *model) activeTextDeleteChar() {
	component := editorCursorComponent{model: *m}
	defer func() { *m = component.model }()
	component.activeTextDeleteChar()
}

func (m *model) activeTextDeleteToLineEnd() {
	component := editorCursorComponent{model: *m}
	defer func() { *m = component.model }()
	component.activeTextDeleteToLineEnd()
}

func (m *model) activeTextDeleteWordForward() {
	component := editorCursorComponent{model: *m}
	defer func() { *m = component.model }()
	component.activeTextDeleteWordForward()
}

func (m *model) activeTextDeleteWordBackward() {
	component := editorCursorComponent{model: *m}
	defer func() { *m = component.model }()
	component.activeTextDeleteWordBackward()
}

func (m *model) activeTextValue() string {
	component := editorCursorComponent{model: *m}
	defer func() { *m = component.model }()
	return component.activeTextValue()
}

func (m *model) activeTextCursorAbs() int {
	component := editorCursorComponent{model: *m}
	defer func() { *m = component.model }()
	return component.activeTextCursorAbs()
}

func (m *model) setActiveTextCursorAbs(pos int) {
	component := editorCursorComponent{model: *m}
	defer func() { *m = component.model }()
	component.setActiveTextCursorAbs(pos)
}

func (m *model) setActiveTextValueAndCursor(value string, pos int) {
	component := editorCursorComponent{model: *m}
	defer func() { *m = component.model }()
	component.setActiveTextValueAndCursor(value, pos)
}

func (m *model) textAreaCursorAbs() int {
	component := editorCursorComponent{model: *m}
	defer func() { *m = component.model }()
	return component.textAreaCursorAbs()
}

func (m *model) descriptionAreaCursorAbs() int {
	component := editorCursorComponent{model: *m}
	defer func() { *m = component.model }()
	return component.descriptionAreaCursorAbs()
}

func (m *model) policiesAreaCursorAbs() int {
	component := editorCursorComponent{model: *m}
	defer func() { *m = component.model }()
	return component.policiesAreaCursorAbs()
}

func (m *model) setTextAreaCursorAbs(pos int) {
	component := editorCursorComponent{model: *m}
	defer func() { *m = component.model }()
	component.setTextAreaCursorAbs(pos)
}

func (m *model) setDescriptionAreaCursorAbs(pos int) {
	component := editorCursorComponent{model: *m}
	defer func() { *m = component.model }()
	component.setDescriptionAreaCursorAbs(pos)
}

func (m *model) setPoliciesAreaCursorAbs(pos int) {
	component := editorCursorComponent{model: *m}
	defer func() { *m = component.model }()
	component.setPoliciesAreaCursorAbs(pos)
}

func (m *model) openFileWriteConfirmation(kind fileWriteConfirmation) {
	component := editorIOComponent{model: *m}
	defer func() { *m = component.model }()
	component.openFileWriteConfirmation(kind)
}

func (m model) loadValueFromFile() (tea.Model, tea.Cmd) {
	component := editorIOComponent{model: m}
	return component.loadValueFromFile()
}

func (m model) writeValueToFile(secureConfirmed, overwriteConfirmed bool) (tea.Model, tea.Cmd) {
	component := editorIOComponent{model: m}
	return component.writeValueToFile(secureConfirmed, overwriteConfirmed)
}

func (m model) fileActionContents() string {
	component := editorIOComponent{model: m}
	return component.fileActionContents()
}

func (m *model) startConfirm(prompt, expected string, items []inventory.Item, ret screen) {
	component := editorIOComponent{model: *m}
	defer func() { *m = component.model }()
	component.startConfirm(prompt, expected, items, ret)
}

func (m model) startRandomFromPopup(kind string) (tea.Model, tea.Cmd) {
	component := editorIOComponent{model: m}
	return component.startRandomFromPopup(kind)
}

func (m model) generateRandomValueIntoEditor(kind string) (tea.Model, tea.Cmd) {
	component := editorIOComponent{model: m}
	return component.generateRandomValueIntoEditor(kind)
}

func (m model) randomValue(kind string) (string, error) {
	component := editorIOComponent{model: m}
	return component.randomValue(kind)
}

func (m model) saveValue(value string) (tea.Model, tea.Cmd) {
	component := editorIOComponent{model: m}
	return component.saveValue(value)
}

func (m model) usesViEditMode() bool {
	component := editorKeybindingsComponent{model: m}
	return component.usesViEditMode()
}

func (m model) updateEmacsTextFieldKey(key string) (model, bool) {
	component := editorKeybindingsComponent{model: m}
	return component.updateEmacsTextFieldKey(key)
}

func (m model) updateViTextFieldNormal(key string) (model, bool) {
	component := editorKeybindingsComponent{model: m}
	return component.updateViTextFieldNormal(key)
}

func (m *model) handlePendingEditSequence(key string) (handled, consumed bool) {
	component := editorKeybindingsComponent{model: *m}
	defer func() { *m = component.model }()
	return component.handlePendingEditSequence(key)
}

func (m model) fieldAllowed(field string) bool {
	component := editorOptionsComponent{model: m}
	return component.fieldAllowed(field)
}

func (m model) editFieldAllowed(field editField) bool {
	component := editorOptionsComponent{model: m}
	return component.editFieldAllowed(field)
}

func (m model) initialEditType() ssm.ParameterType {
	component := editorOptionsComponent{model: m}
	return component.initialEditType()
}

func (m model) normalizedEditType() ssm.ParameterType {
	component := editorOptionsComponent{model: m}
	return component.normalizedEditType()
}

func (m model) initialEditTier() ssm.ParameterTier {
	component := editorOptionsComponent{model: m}
	return component.initialEditTier()
}

func (m model) normalizedEditTier() ssm.ParameterTier {
	component := editorOptionsComponent{model: m}
	return component.normalizedEditTier()
}

func (m model) shouldShowPoliciesField() bool {
	component := editorOptionsComponent{model: m}
	return component.shouldShowPoliciesField()
}

func (m model) shouldShowOverwriteField() bool {
	component := editorOptionsComponent{model: m}
	return component.shouldShowOverwriteField()
}

func (m model) initialEditDataType() ssm.ParameterDataType {
	component := editorOptionsComponent{model: m}
	return component.initialEditDataType()
}

func (m model) normalizedEditDataType() ssm.ParameterDataType {
	component := editorOptionsComponent{model: m}
	return component.normalizedEditDataType()
}

func (m model) initialEditRegion() string {
	component := editorOptionsComponent{model: m}
	return component.initialEditRegion()
}

func (m model) regionOptions() []string {
	component := editorOptionsComponent{model: m}
	return component.regionOptions()
}

func (m model) startMultiline(ret screen) (tea.Model, tea.Cmd) {
	component := editorStateComponent{model: m}
	return component.startMultiline(ret)
}

func (m model) startNewParameter(ret screen) (tea.Model, tea.Cmd) {
	component := editorStateComponent{model: m}
	return component.startNewParameter(ret)
}

func (m model) focusEditField(field editField) model {
	component := editorStateComponent{model: m}
	return component.focusEditField(field)
}

func (m *model) blurEditFields() {
	component := editorStateComponent{model: *m}
	defer func() { *m = component.model }()
	component.blurEditFields()
}

func (m model) requestEditorBack() (tea.Model, tea.Cmd) {
	component := editorStateComponent{model: m}
	return component.requestEditorBack()
}

func (m *model) discardEditorChanges() {
	component := editorStateComponent{model: *m}
	defer func() { *m = component.model }()
	component.discardEditorChanges()
}

func (m model) editorHasUnsavedChanges() bool {
	component := editorStateComponent{model: m}
	return component.editorHasUnsavedChanges()
}

func (m model) currentEditSnapshot() editSnapshot {
	component := editorStateComponent{model: m}
	return component.currentEditSnapshot()
}

func (m model) focusNextEditField() (tea.Model, tea.Cmd) {
	component := editorStateComponent{model: m}
	return component.focusNextEditField()
}

func (m model) focusPreviousEditField() (tea.Model, tea.Cmd) {
	component := editorStateComponent{model: m}
	return component.focusPreviousEditField()
}

func (m model) moveToEditField(field editField, direction editDirection) (tea.Model, tea.Cmd) {
	component := editorStateComponent{model: m}
	return component.moveToEditField(field, direction)
}

func (m model) editFieldOrder() []editField {
	component := editorStateComponent{model: m}
	return component.editFieldOrder()
}

func (m model) hasVisibleFieldAfter(field editField) bool {
	component := editorStateComponent{model: m}
	return component.hasVisibleFieldAfter(field)
}

func (m model) nextEditField() editField {
	component := editorStateComponent{model: m}
	return component.nextEditField()
}

func (m model) previousEditField() editField {
	component := editorStateComponent{model: m}
	return component.previousEditField()
}

func (m model) openRegionSelect() (tea.Model, tea.Cmd) {
	component := editorStateComponent{model: m}
	return component.openRegionSelect()
}

func (m model) ensureRegionSelectOptions() model {
	component := editorStateComponent{model: m}
	return component.ensureRegionSelectOptions()
}

func (m model) regionSelectOptions() []string {
	component := editorStateComponent{model: m}
	return component.regionSelectOptions()
}

func (m model) startTypeSelect(ret screen) (tea.Model, tea.Cmd) {
	component := editorStateComponent{model: m}
	return component.startTypeSelect(ret)
}

func (m model) startTierSelect(ret screen) (tea.Model, tea.Cmd) {
	component := editorStateComponent{model: m}
	return component.startTierSelect(ret)
}

func (m model) startDataTypeSelect(ret screen) (tea.Model, tea.Cmd) {
	component := editorStateComponent{model: m}
	return component.startDataTypeSelect(ret)
}

func (m model) startOverwriteSelect(ret screen) (tea.Model, tea.Cmd) {
	component := editorStateComponent{model: m}
	return component.startOverwriteSelect(ret)
}

func (m model) updateTextArea(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	component := editorUpdateComponent{model: m}
	return component.updateTextArea(msg)
}

func (m model) renderTextAreaScreen() string {
	component := editorViewComponent{model: m}
	return component.renderTextAreaScreen()
}

func (m model) renderTextAreaValueLines(maxRows int) []string {
	component := editorViewComponent{model: m}
	return component.renderTextAreaValueLines(maxRows)
}

func (m model) renderExpandableField(field editField, label string, area textarea.Model, labelWidth, maxRows int, hasNext bool) []string {
	component := editorViewComponent{model: m}
	return component.renderExpandableField(field, label, area, labelWidth, maxRows, hasNext)
}

func (m model) shouldRenderExpandedField(field editField, area textarea.Model, labelWidth int) bool {
	component := editorViewComponent{model: m}
	return component.shouldRenderExpandedField(field, area, labelWidth)
}

func (m model) singleLineFieldWidth(labelWidth int) int {
	component := editorViewComponent{model: m}
	return component.singleLineFieldWidth(labelWidth)
}

func (m model) singleLineAreaView(field editField, area textarea.Model, labelWidth int) string {
	component := editorViewComponent{model: m}
	return component.singleLineAreaView(field, area, labelWidth)
}

func (m model) expandableFieldValue(field editField) string {
	component := editorViewComponent{model: m}
	return component.expandableFieldValue(field)
}

func (m *model) collapseExpandedFieldAfterEdit(field editField, before string) {
	component := editorViewComponent{model: *m}
	defer func() { *m = component.model }()
	component.collapseExpandedFieldAfterEdit(field, before)
}

func (m model) canRenderCompactValue(value string, labelWidth int) bool {
	component := editorViewComponent{model: m}
	return component.canRenderCompactValue(value, labelWidth)
}

func (m *model) expandCompactFieldIfNeeded() bool {
	component := editorViewComponent{model: *m}
	defer func() { *m = component.model }()
	return component.expandCompactFieldIfNeeded()
}

func (m *model) insertNewlineInActiveExpandableField() {
	component := editorViewComponent{model: *m}
	defer func() { *m = component.model }()
	component.insertNewlineInActiveExpandableField()
}

func (m model) isCurrentExpandableFieldExpanded() bool {
	component := editorViewComponent{model: m}
	return component.isCurrentExpandableFieldExpanded()
}

func (m model) renderMultilineFieldLines(field editField, area textarea.Model, maxRows int) []string {
	component := editorViewComponent{model: m}
	return component.renderMultilineFieldLines(field, area, maxRows)
}

func (m model) multilineContentWidth() int {
	component := editorViewComponent{model: m}
	return component.multilineContentWidth()
}

func (m model) withCursorMarker(line string, offset int) string {
	component := editorViewComponent{model: m}
	return component.withCursorMarker(line, offset)
}

func (m model) textAreaBodyHeight() int {
	component := editorViewComponent{model: m}
	return component.textAreaBodyHeight()
}

func (m model) editFieldLine(field editField, name, renderedValue string, labelWidth int) string {
	component := editorViewComponent{model: m}
	return component.editFieldLine(field, name, renderedValue, labelWidth)
}

func (m model) editTextInputFieldLine(field editField, name string, input textinput.Model, labelWidth int) string {
	component := editorViewComponent{model: m}
	return component.editTextInputFieldLine(field, name, input, labelWidth)
}

func (m model) editFieldLabel(field editField, name string) string {
	component := editorViewComponent{model: m}
	return component.editFieldLabel(field, name)
}

func (m model) shouldTypePrintableQInEditField() bool {
	component := editorViewComponent{model: m}
	return component.shouldTypePrintableQInEditField()
}

func (m model) editOptionValue(field editField, value string) string {
	component := editorViewComponent{model: m}
	return component.editOptionValue(field, value)
}

func (m *model) moveActiveMultilinePage(direction int) {
	component := editorViewComponent{model: *m}
	defer func() { *m = component.model }()
	component.moveActiveMultilinePage(direction)
}

func (m model) textAreaFooterText() string {
	component := editorViewComponent{model: m}
	return component.textAreaFooterText()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	component := interactiveComponent{model: m}
	return component.Update(msg)
}

func (m model) View() string {
	component := interactiveComponent{model: m}
	return component.View()
}

func (m model) renderPage(footerText string, renderBody func(model) string) string {
	component := interactiveComponent{model: m}
	return component.renderPage(footerText, renderBody)
}

func (m model) updateLoading(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	component := interactiveComponent{model: m}
	return component.updateLoading(msg)
}

func (m model) keymapStyle() keymapStyle {
	component := keymapComponent{model: m}
	return component.keymapStyle()
}

func (m model) navigationAction(key string) (navigationAction, bool) {
	component := keymapComponent{model: m}
	return component.navigationAction(key)
}

func (m model) editorNavigationAction(key string) (navigationAction, bool) {
	component := keymapComponent{model: m}
	return component.editorNavigationAction(key)
}

func (m model) handlePendingNavigationSequence(key string) (navigationAction, bool, bool) {
	component := keymapComponent{model: m}
	return component.handlePendingNavigationSequence(key)
}

func (m model) currentStatus() Status {
	component := listStateComponent{model: m}
	return component.currentStatus()
}

func (m model) currentItem() inventory.Item {
	component := listStateComponent{model: m}
	return component.currentItem()
}

func (m model) visible() []int {
	component := listStateComponent{model: m}
	return component.visible()
}

func (m model) matchesFor(query string) []int {
	component := listStateComponent{model: m}
	return component.matchesFor(query)
}

func (m *model) applySearchQuery(query string) {
	component := listStateComponent{model: *m}
	defer func() { *m = component.model }()
	component.applySearchQuery(query)
}

func (m model) visiblePaths() []string {
	component := listStateComponent{model: m}
	return component.visiblePaths()
}

func (m model) visibleItems() []inventory.Item {
	component := listStateComponent{model: m}
	return component.visibleItems()
}

func (m *model) ensureSelection() {
	component := listStateComponent{model: *m}
	defer func() { *m = component.model }()
	component.ensureSelection()
}

func (m *model) move(delta int) {
	component := listStateComponent{model: *m}
	defer func() { *m = component.model }()
	component.move(delta)
}

func (m *model) replaceStatus(path string, st Status) {
	component := listStateComponent{model: *m}
	defer func() { *m = component.model }()
	component.replaceStatus(path, st)
}

func (m *model) removeItemRows(items []inventory.Item) {
	component := listStateComponent{model: *m}
	defer func() { *m = component.model }()
	component.removeItemRows(items)
}

func (m *model) markMissingItem(item inventory.Item) {
	component := listStateComponent{model: *m}
	defer func() { *m = component.model }()
	component.markMissingItem(item)
}

func (m model) updateMain(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	component := mainScreenComponent{model: m}
	return component.updateMain(msg)
}

func (m *model) applyMainNavigation(action navigationAction) {
	component := mainScreenComponent{model: *m}
	defer func() { *m = component.model }()
	component.applyMainNavigation(action)
}

func (m model) renderMainScreen() string {
	component := mainScreenComponent{model: m}
	return component.renderMainScreen()
}

func (m model) renderHelpScreen() string {
	component := mainScreenComponent{model: m}
	return component.renderHelpScreen()
}

func (m model) renderShortcutsPopup() string {
	component := mainScreenComponent{model: m}
	return component.renderShortcutsPopup()
}

func (m model) renderLoading() string {
	component := mainScreenComponent{model: m}
	return component.renderLoading()
}

func (m model) renderLoadingPopup() string {
	component := mainScreenComponent{model: m}
	return component.renderLoadingPopup()
}

func (m *model) openShortcuts(from screen) {
	component := popupStateComponent{model: *m}
	defer func() { *m = component.model }()
	component.openShortcuts(from)
}

func (m *model) openPopupShortcuts(from screen, popup popupKind) {
	component := popupStateComponent{model: *m}
	defer func() { *m = component.model }()
	component.openPopupShortcuts(from, popup)
}

func (m *model) pushPopup(kind popupKind) {
	component := popupStateComponent{model: *m}
	defer func() { *m = component.model }()
	component.pushPopup(kind)
}

func (m *model) pushNestedPopup(kind popupKind) {
	component := popupStateComponent{model: *m}
	defer func() { *m = component.model }()
	component.pushNestedPopup(kind)
}

func (m *model) popPopup() {
	component := popupStateComponent{model: *m}
	defer func() { *m = component.model }()
	component.popPopup()
}

func (m *model) clearPopupStack() {
	component := popupStateComponent{model: *m}
	defer func() { *m = component.model }()
	component.clearPopupStack()
}

func (m model) popupLayers() []popupKind {
	component := popupStateComponent{model: m}
	return component.popupLayers()
}

func (m model) updateSortPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	component := popupUpdateComponent{model: m}
	return component.updateSortPopup(msg)
}

func (m model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	component := popupUpdateComponent{model: m}
	return component.updateConfirm(msg)
}

func (m model) updateRegionSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	component := popupUpdateComponent{model: m}
	return component.updateRegionSelect(msg)
}

func (m model) updateTypeSelect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	component := popupUpdateComponent{model: m}
	return component.updateTypeSelect(msg)
}

func (m model) updateHelp(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	component := popupUpdateComponent{model: m}
	return component.updateHelp(msg)
}

func (m model) updateShortcutsPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	component := popupUpdateComponent{model: m}
	return component.updateShortcutsPopup(msg)
}

func (m model) updateConfirmPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	component := popupUpdateComponent{model: m}
	return component.updateConfirmPopup(msg)
}

func (m model) updateRegionSelectPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	component := popupUpdateComponent{model: m}
	return component.updateRegionSelectPopup(msg)
}

func (m model) updateTypeSelectPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	component := popupUpdateComponent{model: m}
	return component.updateTypeSelectPopup(msg)
}

func (m model) updateTierSelectPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	component := popupUpdateComponent{model: m}
	return component.updateTierSelectPopup(msg)
}

func (m model) updateDataTypeSelectPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	component := popupUpdateComponent{model: m}
	return component.updateDataTypeSelectPopup(msg)
}

func (m model) updateOverwriteSelectPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	component := popupUpdateComponent{model: m}
	return component.updateOverwriteSelectPopup(msg)
}

func (m model) renderSortPopup() string {
	component := popupViewComponent{model: m}
	return component.renderSortPopup()
}

func (m model) renderValueActionsPopup() string {
	component := popupViewComponent{model: m}
	return component.renderValueActionsPopup()
}

func (m model) renderPoliciesActionsPopup() string {
	component := popupViewComponent{model: m}
	return component.renderPoliciesActionsPopup()
}

func (m model) renderFileActionPopup() string {
	component := popupViewComponent{model: m}
	return component.renderFileActionPopup()
}

func (m model) renderFileWriteConfirmPopup() string {
	component := popupViewComponent{model: m}
	return component.renderFileWriteConfirmPopup()
}

func (m model) renderUnsavedChangesPopup() string {
	component := popupViewComponent{model: m}
	return component.renderUnsavedChangesPopup()
}

func (m model) renderRandomValuePopup() string {
	component := popupViewComponent{model: m}
	return component.renderRandomValuePopup()
}

func (m model) sortOptionLines() []string {
	component := popupViewComponent{model: m}
	return component.sortOptionLines()
}

func (m model) renderConfirmScreen() string {
	component := popupViewComponent{model: m}
	return component.renderConfirmScreen()
}

func (m model) renderConfirmPopup() string {
	component := popupViewComponent{model: m}
	return component.renderConfirmPopup()
}

func (m model) renderRegionSelectScreen() string {
	component := popupViewComponent{model: m}
	return component.renderRegionSelectScreen()
}

func (m model) renderRegionSelectPopup() string {
	component := popupViewComponent{model: m}
	return component.renderRegionSelectPopup()
}

func (m model) regionSelectLines() []string {
	component := popupViewComponent{model: m}
	return component.regionSelectLines()
}

func (m model) renderTypeSelectScreen() string {
	component := popupViewComponent{model: m}
	return component.renderTypeSelectScreen()
}

func (m model) renderTypeSelectPopup() string {
	component := popupViewComponent{model: m}
	return component.renderTypeSelectPopup()
}

func (m model) renderTierSelectPopup() string {
	component := popupViewComponent{model: m}
	return component.renderTierSelectPopup()
}

func (m model) renderDataTypeSelectPopup() string {
	component := popupViewComponent{model: m}
	return component.renderDataTypeSelectPopup()
}

func (m model) renderOverwriteSelectPopup() string {
	component := popupViewComponent{model: m}
	return component.renderOverwriteSelectPopup()
}

func (m model) typeSelectLines() []string {
	component := popupViewComponent{model: m}
	return component.typeSelectLines()
}

func (m model) tierSelectLines() []string {
	component := popupViewComponent{model: m}
	return component.tierSelectLines()
}

func (m model) dataTypeSelectLines() []string {
	component := popupViewComponent{model: m}
	return component.dataTypeSelectLines()
}

func (m model) overwriteSelectLines() []string {
	component := popupViewComponent{model: m}
	return component.overwriteSelectLines()
}

func (m model) renderFieldPairs(fields [][2]string, labelWidth int) []string {
	component := boxRenderer{model: m}
	return component.renderFieldPairs(fields, labelWidth)
}

func (m model) fieldLine(name, renderedValue string, labelWidth int) string {
	component := boxRenderer{model: m}
	return component.fieldLine(name, renderedValue, labelWidth)
}

func (m model) renderBox(title string, lines []string, preferredHeight int) string {
	component := boxRenderer{model: m}
	return component.renderBox(title, lines, preferredHeight)
}

func (m model) renderBoxWithInnerWidth(title string, lines []string, innerWidth, preferredHeight int) string {
	component := boxRenderer{model: m}
	return component.renderBoxWithInnerWidth(title, lines, innerWidth, preferredHeight)
}

func (m model) singleSelectLine(label string, selected, focused bool) string {
	component := boxRenderer{model: m}
	return component.singleSelectLine(label, selected, focused)
}

func (m model) multiSelectLine(label string, checked, focused bool) string {
	component := boxRenderer{model: m}
	return component.multiSelectLine(label, checked, focused)
}

func (m model) optionLine(content string, focused bool) string {
	component := boxRenderer{model: m}
	return component.optionLine(content, focused)
}

func (m model) popupInputLine(label string, input textinput.Model, inputWidth int) string {
	component := boxRenderer{model: m}
	return component.popupInputLine(label, input, inputWidth)
}

func (m model) popupInputLinePlainPrefix(prefix string, input textinput.Model, inputWidth int) string {
	component := boxRenderer{model: m}
	return component.popupInputLinePlainPrefix(prefix, input, inputWidth)
}

func (m model) inputValueWithCursor(value string, pos, width int) string {
	component := boxRenderer{model: m}
	return component.inputValueWithCursor(value, pos, width)
}

func (m model) inputCursor() string {
	component := boxRenderer{model: m}
	return component.inputCursor()
}

func (m model) inputCursorForRune(r rune) string {
	component := boxRenderer{model: m}
	return component.inputCursorForRune(r)
}

func (m model) boxTop(title string, innerWidth int) string {
	component := pageRenderer{model: m}
	return component.boxTop(title, innerWidth)
}

func (m model) boxBottom(innerWidth int) string {
	component := pageRenderer{model: m}
	return component.boxBottom(innerWidth)
}

func (m model) boxLine(content string, innerWidth int) string {
	component := pageRenderer{model: m}
	return component.boxLine(content, innerWidth)
}

func (m model) renderFooter(text string) string {
	component := pageRenderer{model: m}
	return component.renderFooter(text)
}

func (m model) renderFullscreen(body, footer string) string {
	component := pageRenderer{model: m}
	return component.renderFullscreen(body, footer)
}

func (m model) renderPopupBoxWithActions(title string, lines []string, actions string) string {
	component := popupRenderer{model: m}
	return component.renderPopupBoxWithActions(title, lines, actions)
}

func (m model) popupActionLine(actions string) string {
	component := popupRenderer{model: m}
	return component.popupActionLine(actions)
}

func (m model) renderPopupBox(title string, lines []string) string {
	component := popupRenderer{model: m}
	return component.renderPopupBox(title, lines)
}

func (m model) popupBoxTop(title string, innerWidth int) string {
	component := popupRenderer{model: m}
	return component.popupBoxTop(title, innerWidth)
}

func (m model) popupBoxBottom(innerWidth int) string {
	component := popupRenderer{model: m}
	return component.popupBoxBottom(innerWidth)
}

func (m model) popupBoxLine(content string, innerWidth int) string {
	component := popupRenderer{model: m}
	return component.popupBoxLine(content, innerWidth)
}

func (m model) popupFrame(s string) string {
	component := popupRenderer{model: m}
	return component.popupFrame(s)
}

func (m model) renderPopupStack(body string) string {
	component := popupRenderer{model: m}
	return component.renderPopupStack(body)
}

func (m model) renderPopup(kind popupKind) string {
	component := popupRenderer{model: m}
	return component.renderPopup(kind)
}

func (m model) overlayPopupOnBody(body, popup string) string {
	component := popupRenderer{model: m}
	return component.overlayPopupOnBody(body, popup)
}

func (m model) label(s string) string {
	component := styleRenderer{model: m}
	return component.label(s)
}

func (m model) value(s string) string {
	component := styleRenderer{model: m}
	return component.value(s)
}

func (m model) muted(s string) string {
	component := styleRenderer{model: m}
	return component.muted(s)
}

func (m model) encryptedPlaceholder() string {
	component := styleRenderer{model: m}
	return component.encryptedPlaceholder()
}

func (m model) divider(s string) string {
	component := styleRenderer{model: m}
	return component.divider(s)
}

func (m model) frame(s string) string {
	component := styleRenderer{model: m}
	return component.frame(s)
}

func (m model) selectedRow(s string) string {
	component := styleRenderer{model: m}
	return component.selectedRow(s)
}

func (m model) selectedMarker() string {
	component := styleRenderer{model: m}
	return component.selectedMarker()
}

func (m model) searchLine() string {
	component := styleRenderer{model: m}
	return component.searchLine()
}

func (m model) filteredLine() string {
	component := styleRenderer{model: m}
	return component.filteredLine()
}

func (m model) searchPrompt() string {
	component := styleRenderer{model: m}
	return component.searchPrompt()
}

func (m model) filteredPrompt() string {
	component := styleRenderer{model: m}
	return component.filteredPrompt()
}

func (m model) applyErr(s string) string {
	component := styleRenderer{model: m}
	return component.applyErr(s)
}

func (m model) applyWarning(s string) string {
	component := styleRenderer{model: m}
	return component.applyWarning(s)
}

func (m model) renderFooterWithStatus(text string) string {
	component := styleRenderer{model: m}
	return component.renderFooterWithStatus(text)
}

func (m model) renderStatusMessage() string {
	component := styleRenderer{model: m}
	return component.renderStatusMessage()
}

func (m *model) clearTransientStatus() {
	component := styleRenderer{model: *m}
	defer func() { *m = component.model }()
	component.clearTransientStatus()
}

func (m model) Init() tea.Cmd {
	component := runtimeComponent{model: m}
	return component.Init()
}

func (m model) popupFooterText(kind popupKind) string {
	component := shortcutsComponent{model: m}
	return component.popupFooterText(kind)
}

func (m model) sortPopupScreenFooter() string {
	component := shortcutsComponent{model: m}
	return component.sortPopupScreenFooter()
}

func (m model) shortcutsText() string {
	component := shortcutsComponent{model: m}
	return component.shortcutsText()
}

func (m model) popupShortcutsText(kind popupKind) string {
	component := shortcutsComponent{model: m}
	return component.popupShortcutsText(kind)
}

func (m model) popupActionsShortcuts(kind popupKind) string {
	component := shortcutsComponent{model: m}
	return component.popupActionsShortcuts(kind)
}

func (m model) popupSortShortcuts(kind popupKind) string {
	component := shortcutsComponent{model: m}
	return component.popupSortShortcuts(kind)
}

func (m model) popupNavigationShortcuts(kind popupKind) string {
	component := shortcutsComponent{model: m}
	return component.popupNavigationShortcuts(kind)
}

func (m model) actionsShortcuts(forScreen screen) string {
	component := shortcutsComponent{model: m}
	return component.actionsShortcuts(forScreen)
}

func (m model) sortShortcuts(forScreen screen) string {
	component := shortcutsComponent{model: m}
	return component.sortShortcuts(forScreen)
}

func (m model) navigationShortcuts(forScreen screen) string {
	component := shortcutsComponent{model: m}
	return component.navigationShortcuts(forScreen)
}

func (m *model) openColumnsPopup() {
	component := tableColumnsComponent{model: *m}
	defer func() { *m = component.model }()
	component.openColumnsPopup()
}

func (m model) updateColumns(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	component := tableColumnsComponent{model: m}
	return component.updateColumns(msg)
}

func (m model) updateColumnsPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	component := tableColumnsComponent{model: m}
	return component.updateColumnsPopup(msg)
}

func (m model) renderColumnsScreen() string {
	component := tableColumnsComponent{model: m}
	return component.renderColumnsScreen()
}

func (m model) renderColumnsPopup() string {
	component := tableColumnsComponent{model: m}
	return component.renderColumnsPopup()
}

func (m model) columnOptionLines() []string {
	component := tableColumnsComponent{model: m}
	return component.columnOptionLines()
}

func (m model) columnAllowed(column columnName) bool {
	component := tableColumnsComponent{model: m}
	return component.columnAllowed(column)
}

func (m model) allowedColumnItems() []columnName {
	component := tableColumnsComponent{model: m}
	return component.allowedColumnItems()
}

func (m model) columnsForRendering() map[columnName]bool {
	component := tableColumnsComponent{model: m}
	return component.columnsForRendering()
}

func (m model) popupSortItems() []sortItem {
	component := tableSortComponent{model: m}
	return component.popupSortItems()
}

func (m model) popupSortColumnByLetterHotkey(key string) (columnName, bool) {
	component := tableSortComponent{model: m}
	return component.popupSortColumnByLetterHotkey(key)
}

func (m model) visibleSortItems() []sortItem {
	component := tableSortComponent{model: m}
	return component.visibleSortItems()
}

func (m model) visibleSortColumnByHotkey(key string) (columnName, bool) {
	component := tableSortComponent{model: m}
	return component.visibleSortColumnByHotkey(key)
}

func (m model) sortCursorForCurrentSort() int {
	component := tableSortComponent{model: m}
	return component.sortCursorForCurrentSort()
}

func (m model) sortRulesOrDefault() []sortRule {
	component := tableSortComponent{model: m}
	return component.sortRulesOrDefault()
}

func (m *model) setSortRules(rules []sortRule) {
	component := tableSortComponent{model: *m}
	defer func() { *m = component.model }()
	component.setSortRules(rules)
}

func (m *model) applySort(column columnName) {
	component := tableSortComponent{model: *m}
	defer func() { *m = component.model }()
	component.applySort(column)
}

func (m *model) toggleSortColumn(column columnName) {
	component := tableSortComponent{model: *m}
	defer func() { *m = component.model }()
	component.toggleSortColumn(column)
}

func (m *model) toggleSortDirection(column columnName) {
	component := tableSortComponent{model: *m}
	defer func() { *m = component.model }()
	component.toggleSortDirection(column)
}

func (m *model) applySortWithDirection(column columnName, descending bool) {
	component := tableSortComponent{model: *m}
	defer func() { *m = component.model }()
	component.applySortWithDirection(column, descending)
}

func (m *model) applySortWithRules(rules []sortRule) {
	component := tableSortComponent{model: *m}
	defer func() { *m = component.model }()
	component.applySortWithRules(rules)
}

func (m model) columnHeader(c columnName) string {
	component := tableSortComponent{model: m}
	return component.columnHeader(c)
}

func (m model) sortPopupLabel(item sortItem) string {
	component := tableSortComponent{model: m}
	return component.sortPopupLabel(item)
}

func (m model) renderSelectedParameterBlock(full bool) string {
	component := tableViewComponent{model: m}
	return component.renderSelectedParameterBlock(full)
}

func (m model) selectedParameterFields(st Status, full bool) [][2]string {
	component := tableViewComponent{model: m}
	return component.selectedParameterFields(st, full)
}

func (m model) filterSelectedParameterFields(fields [][2]string) [][2]string {
	component := tableViewComponent{model: m}
	return component.filterSelectedParameterFields(fields)
}

func (m model) detailFieldAllowed(label string) bool {
	component := tableViewComponent{model: m}
	return component.detailFieldAllowed(label)
}

func (m model) displayValue(st Status, full bool) string {
	component := tableViewComponent{model: m}
	return component.displayValue(st, full)
}

func (m model) shouldDisplayEncryptedValue(st Status) bool {
	component := tableViewComponent{model: m}
	return component.shouldDisplayEncryptedValue(st)
}

func (m model) encryptedValueLocked() bool {
	component := tableViewComponent{model: m}
	return component.encryptedValueLocked()
}

func (m model) shouldShowEncryptedEditPlaceholder() bool {
	component := tableViewComponent{model: m}
	return component.shouldShowEncryptedEditPlaceholder()
}

func (m model) renderListBlock() string {
	component := tableViewComponent{model: m}
	return component.renderListBlock()
}

func (m model) tableColumns(vis []int) []tableColumn {
	component := tableViewComponent{model: m}
	return component.tableColumns(vis)
}

func (m model) renderListHeader(cols []tableColumn) string {
	component := tableViewComponent{model: m}
	return component.renderListHeader(cols)
}

func (m model) renderListRow(index int, st Status, selected bool, cols []tableColumn) string {
	component := tableViewComponent{model: m}
	return component.renderListRow(index, st, selected, cols)
}

func (m model) renderListCell(col tableColumn, index int, st Status) string {
	component := tableViewComponent{model: m}
	return component.renderListCell(col, index, st)
}

func (m model) rowText(st Status, row string, selected bool) string {
	component := tableViewComponent{model: m}
	return component.rowText(st, row, selected)
}

func (m model) tableCellValue(key columnName, index int, st Status) string {
	component := tableViewComponent{model: m}
	return component.tableCellValue(key, index, st)
}

func (m model) boxInnerWidth() int {
	component := tableViewComponent{model: m}
	return component.boxInnerWidth()
}

func (m model) listBlockHeight() int {
	component := tableViewComponent{model: m}
	return component.listBlockHeight()
}

func (m model) selectedParameterBlockHeight() int {
	component := tableViewComponent{model: m}
	return component.selectedParameterBlockHeight()
}

func (m model) listBodyHeight() int {
	component := tableViewComponent{model: m}
	return component.listBodyHeight()
}
