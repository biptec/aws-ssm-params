// Package ui implements the interactive terminal user interface.
package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/biptec/aws-ssm-params/internal/filter"
	outputfmt "github.com/biptec/aws-ssm-params/internal/format"
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
	FilterGroups              filter.Groups
	NoColor                   bool
	Keymap                    string
	ShowColumns               []string
	Sort                      []string
	Fields                    outputfmt.Fields
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
	client  ssm.Client
	backend uiBackend
	opts    Options

	runtimeState
	listState
	editorState
	tableState
	popupState
}

// progressMsg is sent from the background status loader to update the loading screen with the current region/chunk.
type progressMsg struct {
	done, total int
	region      string
	items       inventory.Items
}

// statusBatchMsg streams partial status rows while the initial loader is still running.
type statusBatchMsg Statuses

// loadedMsg is sent once the initial status load has finished and the main table can be shown.
type loadedMsg Statuses

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
	items      inventory.Items
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

func pendingStatuses(items inventory.Items) Statuses {
	statuses := make(Statuses, 0, len(items))
	for _, item := range items {
		statuses = append(statuses, Status{Item: item, Pending: true})
	}
	return statuses
}

func (m *model) mergeStatusBatch(batch Statuses) {
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
	merged := make(Statuses, 0, len(m.statuses)+len(incoming))
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
	component := newEditorCursor(m)
	component.moveActiveTextCursor(delta)
}

func (m *model) moveActiveTextLine(delta int) {
	component := newEditorCursor(m)
	component.moveActiveTextLine(delta)
}

func (m *model) activeTextLineStart() {
	component := newEditorCursor(m)
	component.activeTextLineStart()
}

func (m *model) activeTextLineEnd() {
	component := newEditorCursor(m)
	component.activeTextLineEnd()
}

func (m *model) activeTextStart() {
	component := newEditorCursor(m)
	component.activeTextStart()
}

func (m *model) activeTextEnd() {
	component := newEditorCursor(m)
	component.activeTextEnd()
}

func (m *model) activeTextWordForward() {
	component := newEditorCursor(m)
	component.activeTextWordForward()
}

func (m *model) activeTextWordBackward() {
	component := newEditorCursor(m)
	component.activeTextWordBackward()
}

func (m *model) activeTextDeleteChar() {
	component := newEditorCursor(m)
	component.activeTextDeleteChar()
}

func (m *model) activeTextDeleteToLineEnd() {
	component := newEditorCursor(m)
	component.activeTextDeleteToLineEnd()
}

func (m *model) activeTextDeleteWordForward() {
	component := newEditorCursor(m)
	component.activeTextDeleteWordForward()
}

func (m *model) activeTextDeleteWordBackward() {
	component := newEditorCursor(m)
	component.activeTextDeleteWordBackward()
}

func (m *model) activeTextValue() string {
	component := newEditorCursor(m)
	return component.activeTextValue()
}

func (m *model) activeTextCursorAbs() int {
	component := newEditorCursor(m)
	return component.activeTextCursorAbs()
}

func (m *model) setActiveTextValueAndCursor(value string, pos int) {
	component := newEditorCursor(m)
	component.setActiveTextValueAndCursor(value, pos)
}

func (m *model) textAreaCursorAbs() int {
	component := newEditorCursor(m)
	return component.textAreaCursorAbs()
}

func (m *model) setTextAreaCursorAbs(pos int) {
	component := newEditorCursor(m)
	component.setTextAreaCursorAbs(pos)
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

func (m *model) startConfirm(prompt, expected string, items inventory.Items, ret screen) {
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

func (m model) editFieldAllowed(field editField) bool {
	component := newEditorOptions(m)
	return component.editFieldAllowed(field)
}

func (m model) initialEditType() ssm.ParameterType {
	component := newEditorOptions(m)
	return component.initialEditType()
}

func (m model) normalizedEditType() ssm.ParameterType {
	component := newEditorOptions(m)
	return component.normalizedEditType()
}

func (m model) initialEditTier() ssm.ParameterTier {
	component := newEditorOptions(m)
	return component.initialEditTier()
}

func (m model) normalizedEditTier() ssm.ParameterTier {
	component := newEditorOptions(m)
	return component.normalizedEditTier()
}

func (m model) shouldShowPoliciesField() bool {
	component := newEditorOptions(m)
	return component.shouldShowPoliciesField()
}

func (m model) shouldShowOverwriteField() bool {
	component := newEditorOptions(m)
	return component.shouldShowOverwriteField()
}

func (m model) initialEditDataType() ssm.ParameterDataType {
	component := newEditorOptions(m)
	return component.initialEditDataType()
}

func (m model) normalizedEditDataType() ssm.ParameterDataType {
	component := newEditorOptions(m)
	return component.normalizedEditDataType()
}

func (m model) initialEditRegion() string {
	component := newEditorOptions(m)
	return component.initialEditRegion()
}

func (m model) regionOptions() []string {
	component := newEditorOptions(m)
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

func (m model) keymapStyle() keymapStyle {
	return newKeymap(m).keymapStyle()
}

func (m model) navigationAction(key string) (navigationAction, bool) {
	return newKeymap(m).navigationAction(key)
}

func (m model) editorNavigationAction(key string) (navigationAction, bool) {
	return newKeymap(m).editorNavigationAction(key)
}

func (m model) handlePendingNavigationSequence(key string) (navigationAction, bool, bool) {
	pending := m.pendingKeySequence
	m.pendingKeySequence = ""
	return newKeymap(m).resolvePendingNavigationSequence(pending, key)
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
	component := newBoxRenderer(m)
	return component.renderFieldPairs(fields, labelWidth)
}

func (m model) fieldLine(name, renderedValue string, labelWidth int) string {
	component := newBoxRenderer(m)
	return component.fieldLine(name, renderedValue, labelWidth)
}

func (m model) renderBox(title string, lines []string, preferredHeight int) string {
	component := newBoxRenderer(m)
	return component.renderBox(title, lines, preferredHeight)
}

func (m model) singleSelectLine(label string, selected, focused bool) string {
	component := newBoxRenderer(m)
	return component.singleSelectLine(label, selected, focused)
}

func (m model) multiSelectLine(label string, checked, focused bool) string {
	component := newBoxRenderer(m)
	return component.multiSelectLine(label, checked, focused)
}

func (m model) popupInputLine(label string, input textinput.Model, inputWidth int) string {
	component := newBoxRenderer(m)
	return component.popupInputLine(label, input, inputWidth)
}

func (m model) popupInputLinePlainPrefix(prefix string, input textinput.Model, inputWidth int) string {
	component := newBoxRenderer(m)
	return component.popupInputLinePlainPrefix(prefix, input, inputWidth)
}

func (m model) inputValueWithCursor(value string, pos, width int) string {
	component := newBoxRenderer(m)
	return component.inputValueWithCursor(value, pos, width)
}

func (m model) renderFooter(text string) string {
	component := newPageRenderer(m)
	return component.renderFooter(text)
}

func (m model) renderFullscreen(body, footer string) string {
	component := newPageRenderer(m)
	return component.renderFullscreen(body, footer)
}

func (m model) renderPopupBoxWithActions(title string, lines []string, actions string) string {
	component := newPopupRenderer(m)
	return component.renderPopupBoxWithActions(title, lines, actions)
}

func (m model) popupActionLine(actions string) string {
	component := newPopupRenderer(m)
	return component.popupActionLine(actions)
}

func (m model) renderPopupBox(title string, lines []string) string {
	component := newPopupRenderer(m)
	return component.renderPopupBox(title, lines)
}

func (m model) renderPopupStack(body string) string {
	component := newPopupRenderer(m)
	return component.renderPopupStack(body)
}

func (m model) overlayPopupOnBody(body, popup string) string {
	component := newPopupRenderer(m)
	return component.overlayPopupOnBody(body, popup)
}

func (m model) label(s string) string {
	return newStyleRenderer(m).label(s)
}

func (m model) value(s string) string {
	return newStyleRenderer(m).value(s)
}

func (m model) muted(s string) string {
	return newStyleRenderer(m).muted(s)
}

func (m model) encryptedPlaceholder() string {
	return newStyleRenderer(m).encryptedPlaceholder()
}

func (m model) divider(s string) string {
	return newStyleRenderer(m).divider(s)
}

func (m model) selectedRow(s string) string {
	return newStyleRenderer(m).selectedRow(s)
}

func (m model) selectedMarker() string {
	return newStyleRenderer(m).selectedMarker()
}

func (m model) searchLine() string {
	return newStyleRenderer(m).searchLine()
}

func (m model) filteredLine() string {
	return newStyleRenderer(m).filteredLine()
}

func (m model) renderFooterWithStatus(text string) string {
	footer := m.renderFooter(text)
	status := m.renderStatusMessage()
	if status == "" {
		return strings.Join([]string{" ", footer, " "}, "\n")
	}
	return strings.Join([]string{" ", status, " ", footer, " "}, "\n")
}

func (m model) renderStatusMessage() string {
	return newStyleRenderer(m).renderStatusMessage()
}

func (m *model) clearTransientStatus() {
	m.message = ""
	m.warningMessage = ""
	m.errMessage = ""
	m.pendingQuit = false
	m.pendingQuitKey = ""
	m.pendingFileWrite = fileWriteConfirmationNone
}

func (m model) popupFooterText(kind popupKind) string {
	component := newShortcuts(m)
	return component.popupFooterText(kind)
}

func (m model) shortcutsText() string {
	component := newShortcuts(m)
	return component.shortcutsText()
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
	component := newTableSorter(&m)
	return component.popupSortItems()
}

func (m model) popupSortColumnByLetterHotkey(key string) (columnName, bool) {
	component := newTableSorter(&m)
	return component.popupSortColumnByLetterHotkey(key)
}

func (m model) visibleSortItems() []sortItem {
	component := newTableSorter(&m)
	return component.visibleSortItems()
}

func (m model) visibleSortColumnByHotkey(key string) (columnName, bool) {
	component := newTableSorter(&m)
	return component.visibleSortColumnByHotkey(key)
}

func (m model) sortCursorForCurrentSort() int {
	component := newTableSorter(&m)
	return component.sortCursorForCurrentSort()
}

func (m model) sortRulesOrDefault() sortRules {
	component := newTableSorter(&m)
	return component.sortRulesOrDefault()
}

func (m *model) setSortRules(rules sortRules) {
	component := newTableSorter(m)
	component.setSortRules(rules)
}

func (m *model) applySort(column columnName) {
	component := newTableSorter(m)
	component.applySort(column)
}

func (m *model) toggleSortColumn(column columnName) {
	component := newTableSorter(m)
	component.toggleSortColumn(column)
}

func (m *model) toggleSortDirection(column columnName) {
	component := newTableSorter(m)
	component.toggleSortDirection(column)
}

func (m *model) applySortWithDirection(column columnName, descending bool) {
	component := newTableSorter(m)
	component.applySortWithDirection(column, descending)
}

func (m *model) applySortWithRules(rules sortRules) {
	component := newTableSorter(m)
	component.applySortWithRules(rules)
}

func (m model) columnHeader(c columnName) string {
	component := newTableSorter(&m)
	return component.columnHeader(c)
}

func (m model) sortPopupLabel(item sortItem) string {
	component := newTableSorter(&m)
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

// Init starts the asynchronous status load and the loading spinner.
func (m model) Init() tea.Cmd {
	return tea.Batch(
		startLoadCmdWithBackend(m.ctx, backendFor(m), m.items, m.opts.FilterGroups, m.opts.Regions, m.opts.IncludeValues, m.loadCh),
		waitForLoad(m.loadCh),
		tickLoadingSpinner(),
	)
}

// Update is the root Bubble Tea state machine. Domain-specific input is delegated
// after root-level window, asynchronous operation, and popup routing is handled.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.Width = max(20, msg.Width-12)
		m.editPathInput.Width = max(20, msg.Width-18)
		m.editDescriptionInput.Width = max(20, msg.Width-18)
		m.editFileInput.Width = max(20, msg.Width-18)
		m.textArea.SetWidth(max(20, msg.Width-14))
		m.textArea.SetHeight(max(8, msg.Height-10))
		m.editPoliciesArea.SetWidth(max(20, msg.Width-14))
		m.editPoliciesArea.SetHeight(max(1, msg.Height-10))
		m.editDescriptionArea.SetWidth(max(20, msg.Width-14))
		m.editDescriptionArea.SetHeight(max(1, msg.Height-10))
		return m, nil

	case progressMsg:
		m.loadingTitle = "Loading parameters"
		m.loadingLines = nil
		if msg.region != "" {
			m.busyMessage = fmt.Sprintf("Loading parameters %d/%d from %s region...", msg.done, msg.total, msg.region)
		} else {
			m.busyMessage = fmt.Sprintf("Loading parameters %d/%d...", msg.done, msg.total)
		}
		return m, waitForLoad(m.loadCh)

	case loadingTickMsg:
		if m.screen == screenLoading {
			m.loadingSpinnerFrame = (m.loadingSpinnerFrame + 1) % len(loadingSpinnerFrames)
			return m, tickLoadingSpinner()
		}
		return m, nil

	case statusBatchMsg:
		m.mergeStatusBatch(Statuses(msg))
		return m, waitForLoad(m.loadCh)

	case loadedMsg:
		m.statuses = Statuses(msg)
		m.applySortWithRules(m.sortRulesOrDefault())
		m.screen = screenMain
		m.busyMessage = ""
		m.loadingTitle = ""
		m.loadingLines = nil
		m.ensureSelection()
		return m, nil

	case statusUpdatedMsg:
		m.busyMessage = ""
		if msg.err != nil {
			m.errMessage = msg.err.Error()
			m.screen = m.returnScreen
			return m, nil
		}
		matchPath := msg.oldPath
		if matchPath == "" {
			matchPath = msg.path
		}
		m.replaceStatus(matchPath, msg.status)
		m.ensureSelection()
		m.message = msg.message
		m.warningMessage = msg.warning
		m.errMessage = ""
		m.screen = m.returnScreen
		return m, nil

	case deleteDoneMsg:
		m.busyMessage = ""
		if msg.err != nil {
			m.errMessage = msg.err.Error()
			m.screen = m.returnScreen
			return m, nil
		}
		if msg.removeRows {
			m.removeItemRows(msg.items)
		} else {
			for _, item := range msg.items {
				m.markMissingItem(item)
			}
		}
		m.message = fmt.Sprintf("Deleted %d parameter(s)", len(msg.items))
		m.warningMessage = msg.warning
		m.errMessage = ""
		m.screen = m.returnScreen
		m.ensureSelection()
		return m, nil

	case tea.KeyMsg:
		key := msg.String()
		if m.pendingQuit && key == "y" {
			return m, tea.Quit
		}
		if key == "ctrl+c" || key == "ctrl+q" {
			m.message = ""
			m.errMessage = ""
			m.warningMessage = quitConfirmationMessage(key)
			m.pendingQuit = true
			m.pendingQuitKey = key
			m.pendingFileWrite = fileWriteConfirmationNone
			return m, nil
		}
		fileWriteConfirmKey := m.pendingFileWrite != fileWriteConfirmationNone && (key == "y" || key == "enter" || key == "ctrl+j" || key == "esc" || key == "q" || key == "ctrl+g" || m.activePopup == popupFileWriteConfirm)
		if !fileWriteConfirmKey {
			m.clearTransientStatus()
		}
		if m.activePopup != popupNone {
			switch m.activePopup {
			case popupNone:
			case popupColumns:
				return m.updateColumnsPopup(msg)
			case popupShortcuts:
				return m.updateShortcutsPopup(msg)
			case popupConfirm:
				return m.updateConfirmPopup(msg)
			case popupSort:
				return m.updateSortPopup(msg)
			case popupRegionSelect:
				return m.updateRegionSelectPopup(msg)
			case popupTypeSelect:
				return m.updateTypeSelectPopup(msg)
			case popupTierSelect:
				return m.updateTierSelectPopup(msg)
			case popupDataTypeSelect:
				return m.updateDataTypeSelectPopup(msg)
			case popupOverwriteSelect:
				return m.updateOverwriteSelectPopup(msg)
			case popupValueActions:
				return m.updateValueActionsPopup(msg)
			case popupPoliciesActions:
				return m.updatePoliciesActionsPopup(msg)
			case popupFileAction:
				return m.updateFileActionPopup(msg)
			case popupFileWriteConfirm:
				return m.updateFileWriteConfirmPopup(msg)
			case popupUnsavedChanges:
				return m.updateUnsavedChangesPopup(msg)
			case popupRandomValue:
				return m.updateRandomValuePopup(msg)
			}
		}

		switch m.screen {
		case screenMain:
			return m.updateMain(msg)
		case screenTextArea:
			return m.updateTextArea(msg)
		case screenColumns:
			return m.updateColumns(msg)
		case screenConfirm:
			return m.updateConfirm(msg)
		case screenRegionSelect:
			return m.updateRegionSelect(msg)
		case screenTypeSelect:
			return m.updateTypeSelect(msg)
		case screenHelp:
			return m.updateHelp(msg)
		case screenLoading:
			return m.updateLoading(msg)
		}
	}

	if m.screen == screenTextArea {
		var cmd tea.Cmd
		if m.editField == editFieldPolicies {
			m.editPoliciesArea, cmd = m.editPoliciesArea.Update(msg)
		} else {
			m.textArea, cmd = m.textArea.Update(msg)
		}
		return m, cmd
	}
	return m, nil
}

// View renders the active screen and its context-specific footer.
func (m model) View() string {
	switch m.screen {
	case screenLoading:
		return m.renderPage("ctrl+/ help • esc quit", func(content model) string { return content.renderLoading() })
	case screenMain:
		footer := mainFooterText(m.selectedExpanded && m.currentStatus().Item.Path != "")
		if m.searchMode {
			footer = searchFooterText()
		}
		return m.renderPage(footer, func(content model) string { return content.renderMainScreen() })
	case screenTextArea:
		return m.renderPage(m.textAreaFooterText(), func(content model) string { return content.renderTextAreaScreen() })
	case screenColumns:
		return m.renderPage("ctrl+/ help • space/enter toggle • a show all • x hide all • esc back", func(content model) string { return content.renderColumnsScreen() })
	case screenConfirm:
		return m.renderPage("ctrl+/ help • enter confirm • esc back", func(content model) string { return content.renderConfirmScreen() })
	case screenRegionSelect:
		return m.renderPage("ctrl+/ help • enter choose • esc back", func(content model) string { return content.renderRegionSelectScreen() })
	case screenTypeSelect:
		return m.renderPage("ctrl+/ help • enter choose • esc back", func(content model) string { return content.renderTypeSelectScreen() })
	case screenHelp:
		return m.renderPage("esc back", func(content model) string { return content.renderHelpScreen() })
	default:
		return ""
	}
}

func (m model) renderPage(footerText string, renderBody func(model) string) string {
	if m.activePopup != popupNone {
		footerText = m.popupFooterText(m.activePopup)
	}
	bottom := m.renderFooterWithStatus(footerText)
	content := m
	if m.height > 0 {
		content.height = max(1, m.height-countLines(bottom))
	}
	body := renderBody(content)
	body = content.renderPopupStack(body)
	return m.renderFullscreen(body, bottom)
}

func (m model) updateLoading(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+_", "ctrl+/":
		m.openShortcuts(screenLoading)
		return m, nil
	case "q", "esc":
		return m, tea.Quit
	}
	return m, nil
}
