package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/randomx"
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
	NoColor                   bool
	Keymap                    string
	ShowColumns               []string
	Sort                      string
	Fields                    []string
	IncludeValues             bool
	ShowSecureValues          bool
	AllowNamesFileUpdate      bool
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

type columnName string

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

	revealValues bool

	message        string
	warningMessage string
	errMessage     string

	loadingTitle string
	loadingLines []string

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

const (
	columnIndex       columnName = "index"
	columnRegion      columnName = "region"
	columnDate        columnName = "date"
	columnType        columnName = "type"
	columnTier        columnName = "tier"
	columnVersion     columnName = "version"
	columnLength      columnName = "len"
	columnHash        columnName = "sha256"
	columnValue       columnName = "value"
	columnUser        columnName = "user"
	columnDescription columnName = "description"
	columnPath        columnName = "path"
)

var (
	frameColor       = lipgloss.Color("24")
	titleBg          = lipgloss.Color("57")
	titleFg          = lipgloss.Color("255")
	labelFg          = lipgloss.Color("214")
	valueFg          = lipgloss.Color("254")
	mutedFg          = lipgloss.Color("244")
	selectedFg       = lipgloss.Color("81")
	dividerFg        = lipgloss.Color("24")
	okFg             = lipgloss.Color("78")
	missFg           = lipgloss.Color("245")
	emptyFg          = lipgloss.Color("45")
	errFg            = lipgloss.Color("203")
	tableHeaderFg    = lipgloss.Color("250")
	tableHeaderBg    = lipgloss.Color("236")
	searchPromptFg   = lipgloss.Color("81")
	statusLineFg     = lipgloss.Color("244")
	warningFg        = lipgloss.Color("214")
	hotkeyFg         = lipgloss.Color("255")
	titleStyle       = lipgloss.NewStyle().Bold(true).Foreground(frameColor)
	labelStyle       = lipgloss.NewStyle().Foreground(labelFg)
	valueStyle       = lipgloss.NewStyle().Foreground(valueFg)
	mutedStyle       = lipgloss.NewStyle().Foreground(mutedFg)
	dividerStyle     = lipgloss.NewStyle().Foreground(dividerFg)
	selectedRowStyle = lipgloss.NewStyle().Foreground(selectedFg)
	tableHeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(tableHeaderFg)
	searchStyle      = lipgloss.NewStyle().Foreground(searchPromptFg)
	errorStyle       = lipgloss.NewStyle().Foreground(errFg)
	footerStyle      = lipgloss.NewStyle().Foreground(statusLineFg)
	warningStyle     = lipgloss.NewStyle().Foreground(warningFg)
	hotkeyStyle      = lipgloss.NewStyle().Bold(true).Foreground(hotkeyFg)
	cursorStyle      = lipgloss.NewStyle().Reverse(true)
)

func parseInitialSortOption(value string) (columnName, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return columnPath, false
	}
	parts := strings.Split(value, ",")
	field := strings.TrimSpace(parts[0])
	column, ok := columnByFieldName(field)
	if !ok {
		return columnPath, false
	}
	descending := false
	if len(parts) > 1 {
		switch strings.ToLower(strings.TrimSpace(parts[1])) {
		case "desc", "descending":
			descending = true
		}
	}
	return column, descending
}

func columnByFieldName(name string) (columnName, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "name" || name == "path" {
		return columnPath, true
	}
	if name == "data-type" || name == "datatype" || name == "data_type" {
		return "", false
	}
	for _, column := range columnItems() {
		if name == string(column) || name == fieldForColumn(column) {
			return column, true
		}
	}
	return "", false
}

// RunInteractive creates and runs the Bubble Tea program in the terminal alternate screen.
// The function returns only after the user quits the TUI or Bubble Tea reports an error.
func RunInteractive(ctx context.Context, client ssm.Client, items []inventory.Item, opts Options) error {
	m := newModel(ctx, client, items, opts)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

// newModel initializes the TUI model with default inputs, textarea settings, visible columns, and loading state.
// Statuses are not loaded here; Init starts that asynchronous work so the UI can show progress immediately.
func newModel(ctx context.Context, client ssm.Client, items []inventory.Item, opts Options) model {
	if ctx == nil {
		ctx = context.Background()
	}
	sortBy, sortDescending := parseInitialSortOption(opts.Sort)
	input := textinput.New()
	input.Prompt = ""
	input.CharLimit = 0
	input.Width = 80

	editPathInput := textinput.New()
	editPathInput.Prompt = ""
	editPathInput.CharLimit = 0
	editPathInput.Width = 80

	editDescriptionInput := textinput.New()
	editDescriptionInput.Prompt = ""
	editDescriptionInput.CharLimit = 0
	editDescriptionInput.Width = 80

	editFileInput := textinput.New()
	editFileInput.Prompt = ""
	editFileInput.CharLimit = 0
	editFileInput.Width = 80

	configureTextInputStyles(&input, opts)
	configureTextInputStyles(&editPathInput, opts)
	configureTextInputStyles(&editDescriptionInput, opts)
	configureTextInputStyles(&editFileInput, opts)

	area := textarea.New()
	area.Prompt = ""
	area.CharLimit = 0
	area.MaxHeight = 0
	area.ShowLineNumbers = false

	policiesArea := textarea.New()
	policiesArea.Prompt = ""
	policiesArea.CharLimit = 0
	policiesArea.MaxHeight = 0
	policiesArea.ShowLineNumbers = false

	descriptionArea := textarea.New()
	descriptionArea.Prompt = ""
	descriptionArea.CharLimit = 0
	descriptionArea.MaxHeight = 0
	descriptionArea.ShowLineNumbers = false

	return model{
		client:               client,
		items:                items,
		opts:                 opts,
		loadCh:               make(chan tea.Msg),
		screen:               screenLoading,
		shortcutsFor:         screenMain,
		input:                input,
		textArea:             area,
		editPoliciesArea:     policiesArea,
		editDescriptionArea:  descriptionArea,
		editPathInput:        editPathInput,
		editDescriptionInput: editDescriptionInput,
		editFileInput:        editFileInput,
		columns:              defaultColumnVisibility(opts.ShowColumns),
		revealValues:         opts.ShowSecureValues,
		sortBy:               sortBy,
		sortDescending:       sortDescending,
		expandedFields:       map[editField]bool{},
		showGutters:          true,
	}
}

func configureTextInputStyles(input *textinput.Model, opts Options) {
	if opts.NoColor {
		return
	}
	input.TextStyle = valueStyle
	input.Cursor.TextStyle = valueStyle
	input.Cursor.Style = valueStyle
}

// Init starts the initial background status load and registers a command that waits for loader messages.
func (m model) Init() tea.Cmd {
	return tea.Batch(startLoadCmd(m.ctx, m.client, m.items, m.opts.Regions, m.opts.IncludeValues, m.loadCh), waitForLoad(m.loadCh))
}

// startLoadCmd launches the initial SSM status scan in a goroutine.
// Progress and final results are sent through loadCh so the Bubble Tea event loop can render loading updates.
func startLoadCmd(ctx context.Context, client ssm.Client, items []inventory.Item, regions []string, includeValues bool, ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		go func() {
			statuses := LoadStatusesBatchForRegions(ctx, client, items, includeValues, regions, func(done, total int, region string, chunk []inventory.Item) {
				ch <- progressMsg{done: done, total: total, region: region, items: append([]inventory.Item(nil), chunk...)}
			})
			ch <- loadedMsg(statuses)
		}()
		return nil
	}
}

// waitForLoad blocks one Bubble Tea command worker until the status loader emits its next message.
// Update schedules it again after each progress message, giving the UI a stream of loading updates.
func waitForLoad(ch <-chan tea.Msg) tea.Cmd { return func() tea.Msg { return <-ch } }

// Update is the Bubble Tea state machine.
// It handles window changes, async loader/save/delete results, and keypresses, then delegates screen-specific input
// to smaller update helpers so each view owns its own shortcuts and transitions.
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
		m.screen = screenLoading
		if msg.region != "" {
			m.loadingTitle = fmt.Sprintf("Loading parameters %d/%d from %s region...", msg.done, msg.total, msg.region)
		} else {
			m.loadingTitle = fmt.Sprintf("Loading parameters %d/%d...", msg.done, msg.total)
		}
		m.loadingLines = itemPaths(msg.items)
		return m, waitForLoad(m.loadCh)

	case loadedMsg:
		m.statuses = []Status(msg)
		m.applySortWithDirection(columnPath, false)
		m.screen = screenMain
		m.loadingTitle = ""
		m.loadingLines = nil
		m.ensureSelection()
		return m, nil

	case statusUpdatedMsg:
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

// View renders the active screen plus a fixed footer with the hotkeys valid for that screen.
// Keeping footer text here ensures rendered shortcuts stay aligned with the screen selected by Update.
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

// updateMain handles navigation and actions on the main parameter table.
// It also owns search mode, where printable keys update the active filter instead of triggering table shortcuts.
func (m model) updateMain(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.message = ""
	key := msg.String()
	if m.searchMode {
		switch key {
		case "ctrl+_", "ctrl+/":
			m.openShortcuts(screenMain)
			return m, nil
		case "esc", "ctrl+g":
			m.searchMode = false
			if m.searchInvalid {
				m.query = m.effectiveQuery
				m.searchInvalid = false
			}
			return m, nil
		case "backspace":
			if len(m.query) > 0 {
				m.applySearchQuery(m.query[:len(m.query)-1])
			}
			return m, nil
		case "enter":
			m.searchMode = false
			if m.searchInvalid {
				m.query = m.effectiveQuery
				m.searchInvalid = false
			}
			return m, nil
		default:
			if len(msg.String()) == 1 {
				m.applySearchQuery(m.query + msg.String())
			}
			return m, nil
		}
	}

	if action, ok, consumed := (&m).handlePendingNavigationSequence(key); consumed {
		if ok {
			m.applyMainNavigation(action)
		}
		m.ensureSelection()
		return m, nil
	}
	if action, ok := m.navigationAction(key); ok {
		m.applyMainNavigation(action)
		m.ensureSelection()
		return m, nil
	}
	if m.keymapStyle() == keymapVi && key == "g" {
		m.pendingKeySequence = "g"
		return m, nil
	}

	switch key {
	case "q", "esc":
		return m, tea.Quit
	case "enter", "ctrl+j":
		if len(m.visible()) == 0 {
			return m, nil
		}
		return m.startMultiline(screenMain)
	case "n":
		return m.startNewParameter(screenMain)
	case "/":
		m.searchMode = true
		m.query = m.effectiveQuery
		m.searchInvalid = false
	case "v":
		m.revealValues = !m.revealValues
	case "d":
		m.selectedExpanded = !m.selectedExpanded
	case "c":
		m.openColumnsPopup()
	case "s":
		m.sortCursor = m.sortCursorForCurrentSort()
		m.pushPopup(popupSort)
	case "0", "1", "2", "3", "4", "5", "6", "7", "8", "9":
		if col, ok := m.visibleSortColumnByHotkey(key); ok {
			m.applySort(col)
		}
	case "x":
		if len(m.visible()) > 0 {
			items := []inventory.Item{m.currentItem()}
			if m.opts.NoConfirmDeleteOne {
				return m, deleteCmd(m.ctx, m.client, items, m.opts.NamesFile, m.opts.AllowNamesFileUpdate)
			}
			m.startConfirm("Delete selected parameter?", "", items, screenMain)
		}
	case "X":
		items := m.visibleItems()
		if len(items) > 0 {
			if m.opts.NoConfirmDeleteAll {
				return m, deleteCmd(m.ctx, m.client, items, m.opts.NamesFile, m.opts.AllowNamesFileUpdate)
			}
			m.startConfirm(fmt.Sprintf("Delete %d visible parameter(s)?", len(items)), "DELETE ALL", items, screenMain)
		}
	case "ctrl+_", "ctrl+/":
		m.openShortcuts(screenMain)
	}
	m.ensureSelection()
	return m, nil
}

func (m *model) applyMainNavigation(action navigationAction) {
	switch action {
	case navPrevious:
		m.move(-1)
	case navNext:
		m.move(1)
	case navPageUp:
		m.move(-pageSize(m.listBodyHeight()))
	case navPageDown:
		m.move(pageSize(m.listBodyHeight()))
	case navFirst:
		m.selected = 0
	case navLast:
		vis := m.visible()
		if len(vis) > 0 {
			m.selected = len(vis) - 1
		}
	}
}

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

func (m *model) openColumnsPopup() {
	m.columnCursor = 0
	m.columnsDraft = cloneColumnVisibility(m.columns)
	m.pushPopup(popupColumns)
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

// updateTextArea handles the unified edit form: editable SSM name, region/type selectors, file path, multiline value, and save/file operations.
func (m model) updateTextArea(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	resetFileConfirmation := func() {
		m.pendingFileWrite = fileWriteConfirmationNone
		m.warningMessage = ""
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
			switch key {
			case "q", "esc", "ctrl+g":
				return m.requestEditorBack()
			case "tab":
				resetFileConfirmation()
				return m.focusNextEditField()
			case "shift+tab":
				resetFileConfirmation()
				return m.focusPreviousEditField()
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
	case "tab":
		resetFileConfirmation()
		return m.focusNextEditField()
	case "shift+tab":
		resetFileConfirmation()
		return m.focusPreviousEditField()
	case "enter", "ctrl+j":
		resetFileConfirmation()
		if m.expandCompactFieldIfNeeded() {
			return m, nil
		}
		switch m.editField {
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
		}
	case "alt+e":
		resetFileConfirmation()
		m.openActionsPopupForFocusedField()
		return m, nil
	case "y":
		switch m.pendingFileWrite {
		case fileWriteConfirmationSecure:
			m.pendingFileWrite = fileWriteConfirmationNone
			m.warningMessage = ""
			return m.writeValueToFile(true, false)
		case fileWriteConfirmationOverwrite:
			m.pendingFileWrite = fileWriteConfirmationNone
			m.warningMessage = ""
			return m.writeValueToFile(true, true)
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
	}
	m.collapseExpandedFieldAfterEdit(beforeEditField, beforeExpandableValue)
	m.pendingFileWrite = fileWriteConfirmationNone
	m.warningMessage = ""
	m.message = ""
	m.errMessage = ""
	return m, cmd
}

// updateColumns handles the column visibility picker and returns to the screen that opened it.
func (m model) updateColumns(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	cols := m.allowedColumnItems()
	key := msg.String()
	if action, ok, consumed := (&m).handlePendingNavigationSequence(key); consumed {
		if ok {
			m.columnCursor = cursorFromNavigation(m.columnCursor, len(cols), action)
		}
		return m, nil
	}
	if action, ok := m.navigationAction(key); ok {
		m.columnCursor = cursorFromNavigation(m.columnCursor, len(cols), action)
		return m, nil
	}
	if m.keymapStyle() == keymapVi && key == "g" {
		m.pendingKeySequence = "g"
		return m, nil
	}
	switch key {
	case "ctrl+_", "ctrl+/":
		m.openShortcuts(screenColumns)
	case "q", "esc", "ctrl+g":
		m.screen = m.returnScreen
	case " ", "enter":
		if len(cols) > 0 {
			key := cols[m.columnCursor]
			m.columns[key] = !m.columns[key]
		}
	case "a":
		for _, c := range cols {
			m.columns[c] = true
		}
	case "x":
		for _, c := range cols {
			m.columns[c] = false
		}
	}
	return m, nil
}

func (m model) updateColumnsPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	cols := m.allowedColumnItems()
	m = m.ensureColumnsDraft()
	key := msg.String()
	if action, ok, consumed := (&m).handlePendingNavigationSequence(key); consumed {
		if ok {
			m.columnCursor = cursorFromNavigation(m.columnCursor, len(cols), action)
		}
		return m, nil
	}
	if action, ok := m.navigationAction(key); ok {
		m.columnCursor = cursorFromNavigation(m.columnCursor, len(cols), action)
		return m, nil
	}
	if m.keymapStyle() == keymapVi && key == "g" {
		m.pendingKeySequence = "g"
		return m, nil
	}
	switch key {
	case "ctrl+_", "ctrl+/":
		m.openPopupShortcuts(screenColumns, popupColumns)
	case "q", "esc", "ctrl+g":
		m.columnsDraft = nil
		m.popPopup()
	case " ":
		if len(cols) > 0 {
			key := cols[m.columnCursor]
			m.columnsDraft[key] = !m.columnsDraft[key]
		}
	case "enter", "ctrl+j":
		m.columns = cloneColumnVisibility(m.columnsDraft)
		m.columnsDraft = nil
		m.popPopup()
	case "a":
		for _, c := range cols {
			m.columnsDraft[c] = true
		}
	case "x":
		for _, c := range cols {
			m.columnsDraft[c] = false
		}
	}
	return m, nil
}

func (m model) updateSortPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := m.popupSortItems()
	key := msg.String()
	if key != "d" {
		if col, ok := m.popupSortColumnByLetterHotkey(key); ok {
			m.applySort(col)
			m.popPopup()
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
	case "d":
		if len(items) > 0 {
			m.sortCursor = min(m.sortCursor, len(items)-1)
			m.toggleSortDirection(items[m.sortCursor].column)
		}
	case "enter", "ctrl+j":
		if len(items) > 0 {
			m.sortCursor = min(m.sortCursor, len(items)-1)
			m.applySort(items[m.sortCursor].column)
		}
		m.popPopup()
	}
	return m, nil
}

func (m *model) openActionsPopupForFocusedField() bool {
	switch m.editField {
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
			m.editPoliciesArea.SetValue("[]")
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
		case fileWriteConfirmationSecure:
			m.pendingFileWrite = fileWriteConfirmationNone
			m.warningMessage = ""
			return finish(m.writeValueToFile(true, false))
		case fileWriteConfirmationOverwrite:
			m.pendingFileWrite = fileWriteConfirmationNone
			m.warningMessage = ""
			return finish(m.writeValueToFile(true, true))
		}
	case "enter", "ctrl+j":
		m.input.Blur()
		var updated tea.Model
		var cmd tea.Cmd
		if m.fileActionMode == "load" {
			m.editFileInput.SetValue(m.input.Value())
			updated, cmd = m.loadValueFromFile()
		} else if m.fileActionMode == "write" {
			m.editFileInput.SetValue(m.input.Value())
			updated, cmd = m.writeValueToFile(false, false)
		} else if m.fileActionMode == "random-custom" {
			updated, cmd = m.generateRandomValueIntoEditor("base64-custom")
		} else {
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

func cursorFromNavigation(cursor, length int, action navigationAction) int {
	if length <= 0 {
		return 0
	}
	switch action {
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
		m.loadingTitle = "Deleting parameters..."
		m.loadingLines = itemPaths(items)
		m.screen = screenLoading
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
		m.loadingTitle = "Deleting parameters..."
		m.loadingLines = itemPaths(items)
		m.activePopup = popupNone
		m.popupStack = nil
		m.screen = screenLoading
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

// updateLoading handles shortcuts that must remain available while long SSM scans are running.
// The footer advertises q quit on the loading screen, while ctrl+c is handled globally with confirmation.
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
	m.editField = editFieldValue
	m.editDirection = editDirectionNext
	m.viInsertMode = m.keymapStyle() != keymapVi
	m.textArea.Focus()
	m.pendingFileWrite = fileWriteConfirmationNone
	m.warningMessage = ""
	m.message = ""
	m.errMessage = ""
	m.editInitialSnapshot = m.currentEditSnapshot()
	m.screen = screenTextArea
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
		field = m.editFieldOrder()[0]
	}
	m.blurEditFields()
	m.editField = field
	switch field {
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

func nextEditField(field editField) editField {
	switch field {
	case editFieldValue:
		return editFieldSSMPath
	case editFieldSSMPath:
		return editFieldRegion
	case editFieldRegion:
		return editFieldType
	case editFieldType:
		return editFieldTier
	case editFieldTier:
		return editFieldDataType
	case editFieldDataType:
		return editFieldOverwrite
	case editFieldOverwrite:
		return editFieldDescription
	case editFieldDescription:
		return editFieldPolicies
	case editFieldPolicies:
		return editFieldValue
	default:
		return editFieldValue
	}
}

func previousEditField(field editField) editField {
	switch field {
	case editFieldValue:
		return editFieldPolicies
	case editFieldPolicies:
		return editFieldDescription
	case editFieldDescription:
		return editFieldOverwrite
	case editFieldOverwrite:
		return editFieldDataType
	case editFieldDataType:
		return editFieldTier
	case editFieldTier:
		return editFieldType
	case editFieldType:
		return editFieldRegion
	case editFieldRegion:
		return editFieldSSMPath
	default:
		return editFieldValue
	}
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
	data, err := os.ReadFile(expandedPath)
	if err != nil {
		m.errMessage = err.Error()
		m.message = ""
		m.warningMessage = ""
		return m, nil
	}
	switch m.fileActionField {
	case editFieldPolicies:
		m.editPoliciesArea.SetValue(prettyPoliciesForEditor(string(data)))
		m = m.focusEditField(editFieldPolicies)
		m.message = "Loaded policies from " + path
	default:
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
	if err := os.WriteFile(expandedPath, []byte(contents), 0600); err != nil {
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
		return randomx.Base64(32)
	case "hex-32":
		return randomx.Hex(32)
	case "uuid":
		return randomx.UUID()
	case "base64-custom":
		n, err := strconv.Atoi(strings.TrimSpace(m.input.Value()))
		if err != nil || n <= 0 {
			return "", fmt.Errorf("invalid byte length")
		}
		return randomx.Base64(n)
	default:
		return "", fmt.Errorf("unknown random value generator")
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
		m.errMessage = "Value cannot be empty."
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
	if m.shouldShowPoliciesField() {
		policies = normalizePoliciesForAWS(m.editPoliciesArea.Value())
	}
	overwrite := true
	if m.shouldShowOverwriteField() {
		overwrite = m.editOverwrite
	}
	m.loadingTitle = "Saving parameter..."
	m.loadingLines = []string{item.Path}
	m.screen = screenLoading
	description := strings.TrimSpace(m.editDescriptionArea.Value())
	if description == "" {
		description = strings.TrimSpace(m.editDescriptionInput.Value())
	}
	return m, saveValueCmd(m.ctx, m.client, item, oldPath, value, m.normalizedEditType(), ssm.PutParameterOptions{Description: description, Tier: m.normalizedEditTier(), DataType: m.normalizedEditDataType(), Policies: policies, Overwrite: overwrite}, m.opts.NamesFile, m.opts.AllowNamesFileUpdate)
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
func (m model) renderMainScreen() string {
	if !m.selectedExpanded || m.currentStatus().Item.Path == "" {
		return m.renderListBlock()
	}
	return strings.Join([]string{m.renderSelectedParameterBlock(true), m.renderListBlock()}, "\n")
}

// renderTextAreaScreen renders the unified editor for multiline values plus editable metadata/file fields.
func (m model) renderTextAreaScreen() string {
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

	if m.editFieldAllowed(editFieldValue) {
		valueLines := m.renderExpandableField(editFieldValue, "Value", m.textArea, labelWidth, max(1, remaining-1), false)
		lines = append(lines, valueLines...)
	}

	return m.renderBox(title, lines, preferredHeight)
}

func (m model) renderTextAreaValueLines(maxRows int) []string {
	return m.renderMultilineFieldLines(editFieldValue, m.textArea, maxRows)
}

func (m model) renderExpandableField(field editField, label string, area textarea.Model, labelWidth, maxRows int, hasNext bool) []string {
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

func (m model) shouldRenderExpandedField(field editField, area textarea.Model, labelWidth int) bool {
	if m.expandedFields[field] {
		return true
	}
	return !m.canRenderCompactValue(area.Value(), labelWidth)
}

func (m model) singleLineFieldWidth(labelWidth int) int {
	labelText := padMin("", labelWidth+1)
	return max(1, m.boxInnerWidth()-lipgloss.Width(labelText)-3)
}

func (m model) singleLineAreaView(field editField, area textarea.Model, labelWidth int) string {
	width := m.singleLineFieldWidth(labelWidth)
	value := strings.ReplaceAll(area.Value(), "\n", " ")
	focused := m.editField == field && area.Focused()
	if !focused {
		return m.value(truncateStyled(value, width))
	}
	_, offset := textAreaCursorLineOffset(area)
	return m.value(m.inputValueWithCursor(value, offset, width))
}

func (m model) expandableFieldValue(field editField) string {
	switch field {
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

func (m *model) collapseExpandedFieldAfterEdit(field editField, before string) {
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

func (m model) canRenderCompactValue(value string, labelWidth int) bool {
	if strings.Contains(value, "\n") {
		return false
	}
	return lipgloss.Width(value) <= m.singleLineFieldWidth(labelWidth)
}

func (m *model) expandCompactFieldIfNeeded() bool {
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

func (m *model) insertNewlineInActiveExpandableField() {
	if !isExpandableEditField(m.editField) {
		return
	}
	value := []rune(m.activeTextValue())
	pos := min(max(0, m.activeTextCursorAbs()), len(value))
	value = append(value[:pos], append([]rune{'\n'}, value[pos:]...)...)
	m.setActiveTextValueAndCursor(string(value), pos+1)
}

func (m model) isCurrentExpandableFieldExpanded() bool {
	switch m.editField {
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

func (m model) renderMultilineFieldLines(field editField, area textarea.Model, maxRows int) []string {
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

func (m model) multilineContentWidth() int {
	if !m.showGutters {
		return max(8, m.boxInnerWidth()-2)
	}
	lineNumberWidth := 4
	prefixWidth := lineNumberWidth + lipgloss.Width(" │ ")
	return max(8, m.boxInnerWidth()-prefixWidth-2)
}

func (m model) multilineVisualRowCount(value string, width int) int {
	width = max(1, width)
	lines := strings.Split(value, "\n")
	if len(lines) == 0 {
		return 1
	}
	count := 0
	for _, line := range lines {
		lineLen := len([]rune(line))
		count += max(1, (lineLen+width-1)/width)
	}
	return max(1, count)
}

func (m model) withCursorMarker(line string, offset int) string {
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
func (m model) textAreaBodyHeight() int {
	if m.height <= 0 {
		return max(8, m.height-2)
	}
	bodyHeight := m.height
	return max(8, bodyHeight)
}

func (m model) editFieldLine(field editField, name, renderedValue string, labelWidth int) string {
	return m.fieldLine(m.editFieldLabel(field, name), renderedValue, labelWidth)
}

func (m model) editTextInputFieldLine(field editField, name string, input textinput.Model, labelWidth int) string {
	label := m.editFieldLabel(field, name)
	labelText := padMin(label+":", labelWidth+1)
	// Bubbles textinput renders the focused cursor as one visible cell in addition to
	// its configured width. Reserve that extra cell so the final styled line does not
	// overflow the box and lose ANSI styling during truncation.
	available := m.boxInnerWidth() - lipgloss.Width(labelText) - 2
	input.Width = max(1, available)
	return m.fieldLine(label, input.View(), labelWidth)
}

func (m model) editFieldLabel(field editField, name string) string {
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

func (m model) shouldTypePrintableQInEditField() bool {
	if !isEditableTextField(m.editField) {
		return false
	}
	return m.keymapStyle() == keymapEmacs || m.viInsertMode
}

func (m model) editOptionValue(field editField, value string) string {
	if m.editField == field {
		value += " <"
	}
	return m.value(value)
}

func (m *model) moveActiveMultilinePage(direction int) {
	height := m.textArea.Height()
	switch m.editField {
	case editFieldDescription:
		height = m.editDescriptionArea.Height()
	case editFieldPolicies:
		height = m.editPoliciesArea.Height()
	}
	for i := 0; i < pageSize(height); i++ {
		m.moveActiveTextLine(direction)
	}
}

// renderColumnsScreen renders the legacy full-screen table-column chooser.
// The main UI now opens the same content as a popup, but keeping this renderer
// makes the shortcuts context and focused tests straightforward.
func (m model) renderColumnsScreen() string {
	return m.renderBox("Columns", m.columnOptionLines(), m.height)
}

func (m model) renderColumnsPopup() string {
	return m.renderPopupBoxWithActions("Columns", m.columnOptionLines(), "Enter apply   Esc cancel")
}

func (m model) renderSortPopup() string {
	return m.renderPopupBoxWithActions("Sort By", m.sortOptionLines(), "Enter sort   Esc cancel")
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
	if m.fileActionMode == "write" {
		title = "Write to file"
		if m.fileActionField == editFieldPolicies {
			title = "Write policies to file"
		}
	} else if m.fileActionMode == "random-custom" {
		title = "Random Value"
		label = "Byte length:"
		inputWidth = 12
	}
	button := "load"
	if m.fileActionMode == "write" {
		button = "write"
	} else if m.fileActionMode == "random-custom" {
		button = "generate"
	}
	lines := []string{m.popupInputLine(label, m.input, inputWidth)}
	return m.renderPopupBoxWithActions(title, lines, "Enter "+button+"   Esc cancel")
}

func (m model) renderFileWriteConfirmPopup() string {
	message := "Confirm file write?"
	switch m.pendingFileWrite {
	case fileWriteConfirmationSecure:
		message = "This is a SecureString value. Write it to a local file in plain text?"
	case fileWriteConfirmationOverwrite:
		message = "File already exists. Overwrite it?"
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
		lines = append(lines, m.singleSelectLine(m.sortPopupLabel(item), i == m.sortCursor, i == m.sortCursor))
	}
	return lines
}

func (m model) columnOptionLines() []string {
	cols := m.allowedColumnItems()
	visible := m.columnsForRendering()
	lines := []string{m.muted("# and NAME are always visible."), ""}
	for i, c := range cols {
		checked := visible[c]
		lines = append(lines, m.multiSelectLine(columnLabel(c), checked, i == m.columnCursor))
	}
	return lines
}

// renderConfirmScreen renders the destructive-action confirmation prompt and input field.
func (m model) renderConfirmScreen() string {
	lines := []string{}
	for _, line := range strings.Split(m.confirmPrompt, "\n") {
		lines = append(lines, "  "+line)
	}
	lines = append(lines, "", "  > "+m.input.View())
	return m.renderBox("Confirm", lines, m.height)
}

func (m model) renderConfirmPopup() string {
	lines := []string{}
	for _, line := range strings.Split(m.confirmPrompt, "\n") {
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
	lines := []string{
		"  " + m.muted("Choose region for saving this value:"),
		"",
	}
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
	lines := []string{
		m.muted("Choose region for saving this value:"),
		"",
	}
	for i, region := range regions {
		lines = append(lines, m.singleSelectLine(region, i == m.regionCursor, i == m.regionCursor))
	}
	return lines
}

// renderTypeSelectScreen renders the AWS SSM parameter type picker used by value editors.
func (m model) renderTypeSelectScreen() string {
	lines := []string{
		"  " + m.muted("Choose how this value should be stored in AWS SSM Parameter Store:"),
		"",
	}
	for i, it := range parameterTypeItems() {
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
	lines := []string{
		m.muted("Choose how this value should be stored in AWS SSM Parameter Store:"),
		"",
	}
	for i, it := range parameterTypeItems() {
		row := fmt.Sprintf("%s — %s", it.label, it.description)
		lines = append(lines, m.singleSelectLine(row, i == m.typeCursor, i == m.typeCursor))
	}
	return lines
}

func (m model) tierSelectLines() []string {
	lines := []string{
		m.muted("Choose the AWS SSM storage tier for this parameter:"),
		"",
	}
	for i, it := range parameterTierItems() {
		row := fmt.Sprintf("%s — %s", it.label, it.description)
		lines = append(lines, m.singleSelectLine(row, i == m.tierCursor, i == m.tierCursor))
	}
	return lines
}

func (m model) dataTypeSelectLines() []string {
	lines := []string{
		m.muted("Choose AWS SSM value validation data type:"),
		"",
	}
	for i, it := range parameterDataTypeItems() {
		row := fmt.Sprintf("%s — %s", it.label, it.description)
		lines = append(lines, m.singleSelectLine(row, i == m.dataTypeCursor, i == m.dataTypeCursor))
	}
	return lines
}

func (m model) overwriteSelectLines() []string {
	lines := []string{
		m.muted("Choose whether AWS SSM may overwrite an existing parameter:"),
		"",
	}
	for i, it := range overwriteItems() {
		row := fmt.Sprintf("%s — %s", it.label, it.description)
		lines = append(lines, m.singleSelectLine(row, i == m.overwriteCursor, i == m.overwriteCursor))
	}
	return lines
}

// renderHelpScreen renders the full shortcut reference.
func (m model) renderHelpScreen() string {
	lines := []string{}
	for _, line := range strings.Split(m.shortcutsText(), "\n") {
		if line == "" {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, "  "+line)
	}
	return m.renderBox("Shortcuts", lines, m.height)
}

func (m model) renderShortcutsPopup() string {
	lines := []string{}
	for _, line := range strings.Split(m.shortcutsText(), "\n") {
		lines = append(lines, line)
	}
	return m.renderPopupBoxWithActions("Shortcuts", lines, "Esc close")
}

// renderLoading renders current background-operation progress, including the region and paths currently being scanned.
func (m model) renderLoading() string {
	lines := []string{"  " + m.loadingTitle, ""}
	for _, line := range m.loadingLines {
		lines = append(lines, "  "+line)
	}
	return m.renderBox("Loading", lines, m.height)
}

// renderSelectedParameterBlock renders the compact or expanded selected-parameter summary shown above the main table.
// Missing parameters only have an expected name, so every field except Name is displayed as a dash.
func (m model) renderSelectedParameterBlock(full bool) string {
	st := m.currentStatus()
	if st.Item.Path == "" {
		return m.renderBox("Selected Parameter", []string{"No parameters found."}, 8)
	}

	fields := m.selectedParameterFields(st, full)
	labelWidth := 6
	if full {
		labelWidth = 11
	}
	lines := m.renderFieldPairs(fields, labelWidth)
	return m.renderBox("Selected Parameter", lines, len(lines)+2)
}

func (m model) selectedParameterFields(st Status, full bool) [][2]string {
	if !st.Exists && st.Error == "" {
		if full {
			return m.filterSelectedParameterFields([][2]string{{"Name", st.Item.Path}, {"Region", "-"}, {"Type", "-"}, {"Tier", "-"}, {"DataType", "-"}, {"Policies", "-"}, {"Version", "-"}, {"Len", "-"}, {"SHA256", "-"}, {"Description", "-"}, {"User", "-"}, {"Date", "-"}, {"Value", "-"}})
		}
		return m.filterSelectedParameterFields([][2]string{{"Name", st.Item.Path}, {"Region", "-"}, {"Type", "-"}, {"Date", "-"}, {"Value", "-"}})
	}

	value := m.displayValue(st, full)
	fields := [][2]string{{"Name", st.Item.Path}, {"Region", m.statusRegion(st)}, {"Type", valueOrDash(st.Type)}, {"Date", valueOrDash(st.Modified)}, {"Value", value}}
	if full {
		fields = [][2]string{{"Name", st.Item.Path}, {"Region", m.statusRegion(st)}, {"Type", valueOrDash(st.Type)}, {"Tier", valueOrDash(st.Tier)}, {"DataType", valueOrDash(st.DataType)}, {"Policies", oneLineValuePreview(st.Policies, max(20, m.boxInnerWidth()-18))}, {"Version", intOrDash(st.Version)}, {"Len", intOrDash(int64(st.Length))}, {"SHA256", valueOrDash(st.SHA256Prefix)}, {"Description", valueOrDash(st.Description)}, {"User", valueOrDash(st.User)}, {"Date", valueOrDash(st.Modified)}, {"Value", value}}
		if st.Error != "" {
			fields = append(fields, [2]string{"Error", st.Error})
		}
	}
	return m.filterSelectedParameterFields(fields)
}

func (m model) filterSelectedParameterFields(fields [][2]string) [][2]string {
	if len(m.opts.Fields) == 0 {
		return fields
	}
	out := make([][2]string, 0, len(fields))
	for _, pair := range fields {
		if m.detailFieldAllowed(pair[0]) {
			out = append(out, pair)
		}
	}
	return out
}

func (m model) detailFieldAllowed(label string) bool {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "name":
		return true
	case "region":
		return m.fieldAllowed("region")
	case "type":
		return m.fieldAllowed("type")
	case "tier":
		return m.fieldAllowed("tier")
	case "datatype", "data type", "data-type":
		return m.fieldAllowed("data-type")
	case "policies":
		return m.fieldAllowed("policies")
	case "version":
		return m.fieldAllowed("version")
	case "len":
		return m.fieldAllowed("len")
	case "sha256":
		return m.fieldAllowed("sha256")
	case "description":
		return m.fieldAllowed("description")
	case "user":
		return m.fieldAllowed("user")
	case "date":
		return m.fieldAllowed("date")
	case "value":
		return m.fieldAllowed("value")
	default:
		return true
	}
}

// displayValue returns the user-facing value for selected blocks and VALUE table cells.
// SecureString values are treated as sensitive and hidden until the user presses v; String/StringList are shown by default.
func (m model) displayValue(st Status, full bool) string {
	if st.Item.Path != "" && !st.Exists && st.Error == "" {
		return "-"
	}
	if m.shouldHideValue(st) {
		return "(hidden)"
	}
	width := max(20, m.boxInnerWidth()-22)
	if full {
		width = max(20, m.boxInnerWidth()-18)
	}
	return oneLineValuePreview(st.Value, width)
}

func oneLineValuePreview(value string, width int) string {
	if value == "" {
		return "-"
	}
	if width < 4 {
		width = 4
	}
	normalized := strings.ReplaceAll(value, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	multiline := strings.Contains(normalized, "\n")
	preview := strings.ReplaceAll(normalized, "\n", `\n`)
	suffix := ""
	if multiline {
		suffix = "..."
	}
	available := width - len(suffix)
	if available < 1 {
		available = 1
	}
	runes := []rune(preview)
	if len(runes) > available {
		return string(runes[:available]) + "..."
	}
	return preview + suffix
}

func (m model) shouldHideValue(st Status) bool {
	if m.revealValues {
		return false
	}
	parameterType, err := ssm.ParseParameterType(st.Type)
	if err != nil {
		return true
	}
	return parameterType == ssm.ParameterTypeSecureString
}

// renderListBlock renders the main table, including dynamic columns, scrolling, search/filter status, and messages.
func (m model) renderListBlock() string {
	vis := m.visible()
	title := fmt.Sprintf("List of %d Parameters", len(vis))

	columns := m.tableColumns(vis)
	header := m.renderListHeader(columns)
	divider := strings.Repeat("─", m.boxInnerWidth())
	lines := []string{"  " + header, m.divider(divider)}

	bodyHeight := m.listBodyHeight()
	start := 0
	if m.selected >= bodyHeight {
		start = m.selected - bodyHeight + 1
	}
	end := min(len(vis), start+bodyHeight)
	for row := start; row < end; row++ {
		st := m.statuses[vis[row]]
		lines = append(lines, m.renderListRow(row+1, st, row == m.selected, columns))
	}
	for len(lines) < 2+bodyHeight {
		lines = append(lines, "")
	}

	if m.searchMode || m.effectiveQuery != "" {
		lines = append(lines, m.divider(divider))
		if m.searchMode {
			lines = append(lines, m.searchLine())
		} else {
			lines = append(lines, m.filteredLine())
		}
	}
	return m.renderBox(title, lines, m.listBlockHeight())
}

type tableColumn struct {
	key    columnName
	header string
	width  int
}

// tableColumns calculates visible table columns and their widths from the current data.
// It shrinks wide columns until the table fits inside the box without moving headers away from row values.
func (m model) tableColumns(vis []int) []tableColumn {
	keys := []columnName{columnIndex, columnPath}
	visibleColumns := m.columnsForRendering()
	for _, key := range columnItems() {
		if m.columnAllowed(key) && visibleColumns[key] {
			keys = append(keys, key)
		}
	}

	cols := make([]tableColumn, 0, len(keys))
	for _, key := range keys {
		header := m.columnHeader(key)
		width := lipgloss.Width(header)
		if key == columnIndex {
			width = max(width, len(strconv.Itoa(max(1, len(vis)))))
		}
		for row, idx := range vis {
			st := m.statuses[idx]
			value := m.tableCellValue(key, row+1, st)
			width = max(width, lipgloss.Width(value))
		}
		cols = append(cols, tableColumn{key: key, header: header, width: width})
	}

	available := max(20, m.boxInnerWidth()-2)
	for tableColumnsWidth(cols) > available {
		idx := widestShrinkableColumn(cols)
		if idx < 0 {
			break
		}
		cols[idx].width--
	}
	return cols
}

// tableColumnsWidth returns the visible width of the table with two spaces between columns.
func tableColumnsWidth(cols []tableColumn) int {
	if len(cols) == 0 {
		return 0
	}
	total := 2 * (len(cols) - 1)
	for _, col := range cols {
		total += col.width
	}
	return total
}

// widestShrinkableColumn finds the widest column that can still shrink without going below its minimum width.
func widestShrinkableColumn(cols []tableColumn) int {
	idx := -1
	width := -1
	for i, col := range cols {
		minWidth := columnMinWidth(col.key, col.header)
		if col.width <= minWidth {
			continue
		}
		if col.width > width {
			idx = i
			width = col.width
		}
	}
	return idx
}

// columnMinWidth protects important columns from becoming unreadably narrow during terminal-width fitting.
func columnMinWidth(key columnName, header string) int {
	switch key {
	case columnPath:
		return max(lipgloss.Width(header), 20)
	case columnDate:
		return max(lipgloss.Width(header), 20)
	case columnValue, columnUser, columnDescription:
		return max(lipgloss.Width(header), 12)
	default:
		return lipgloss.Width(header)
	}
}

// renderListHeader pads and styles the table header row.
func (m model) renderListHeader(cols []tableColumn) string {
	parts := make([]string, 0, len(cols))
	for _, col := range cols {
		parts = append(parts, pad(col.header, col.width))
	}
	s := strings.Join(parts, "  ")
	if m.opts.NoColor {
		return s
	}
	return tableHeaderStyle.Render(s)
}

// renderListRow renders one status row with selection and status-based coloring.
func (m model) renderListRow(index int, st Status, selected bool, cols []tableColumn) string {
	parts := make([]string, 0, len(cols))
	for _, col := range cols {
		value := m.tableCellValue(col.key, index, st)
		parts = append(parts, pad(truncateInline(value, col.width), col.width))
	}
	row := strings.Join(parts, "  ")
	plain := stripANSI(row)
	if selected {
		return m.selectedMarker() + m.rowText(st, plain, true)
	}
	if styled := m.rowText(st, plain, false); styled != plain {
		return "  " + styled
	}
	return "  " + row
}

// rowText applies row-level styling based on selection and status severity.
func (m model) rowText(st Status, row string, selected bool) string {
	if selected {
		return m.selectedRow(row)
	}
	label := statusDisplayLabel(st)
	if label == "ERROR" {
		if m.opts.NoColor {
			return row
		}
		return lipgloss.NewStyle().Foreground(errFg).Render(row)
	}
	if label == "MISSING" {
		if m.opts.NoColor {
			return row
		}
		return lipgloss.NewStyle().Foreground(missFg).Render(row)
	}
	if label == "EMPTY" {
		if m.opts.NoColor {
			return row
		}
		return lipgloss.NewStyle().Foreground(emptyFg).Render(row)
	}
	return row
}

// tableCellValue returns the raw display value for one dynamic table column.
func (m model) tableCellValue(key columnName, index int, st Status) string {
	switch key {
	case columnIndex:
		return strconv.Itoa(index)
	case columnRegion:
		return m.statusRegion(st)
	case columnDate:
		return valueOrDash(st.Modified)
	case columnType:
		return valueOrDash(st.Type)
	case columnTier:
		return valueOrDash(st.Tier)
	case columnVersion:
		return intOrDash(st.Version)
	case columnLength:
		return intOrDash(int64(st.Length))
	case columnHash:
		return valueOrDash(st.SHA256Prefix)
	case columnValue:
		return m.displayValue(st, true)
	case columnUser:
		return valueOrDash(st.User)
	case columnDescription:
		return valueOrDash(st.Description)
	case columnPath:
		return st.Item.Path
	default:
		return "-"
	}
}

// renderFieldPairs converts name/value metadata pairs into aligned lines for boxed detail views.
func (m model) renderFieldPairs(fields [][2]string, labelWidth int) []string {
	lines := make([]string, 0, len(fields))
	for _, f := range fields {
		value := f[1]
		if f[0] == "Status" {
			lines = append(lines, "  "+m.fieldLine(f[0], value, labelWidth))
			continue
		}
		lines = append(lines, "  "+m.fieldLine(f[0], m.value(value), labelWidth))
	}
	return lines
}

func (m model) fieldLine(name, renderedValue string, labelWidth int) string {
	label := m.label(padMin(name+":", labelWidth+1))
	return label + " " + renderedValue
}

func (m model) formField(name, value string) string {
	return m.label(name+":") + " " + m.value(value)
}

// renderBox draws a bordered box, truncating or padding content so screens keep stable heights.
func (m model) renderBox(title string, lines []string, preferredHeight int) string {
	return m.renderBoxWithInnerWidth(title, lines, m.boxInnerWidth(), preferredHeight)
}

func (m model) renderBoxWithInnerWidth(title string, lines []string, innerWidth, preferredHeight int) string {
	top := m.boxTop(title, innerWidth)
	bottom := m.boxBottom(innerWidth)

	if preferredHeight <= 0 {
		preferredHeight = len(lines) + 2
	}
	preferredHeight = max(3, preferredHeight)
	innerHeight := max(1, preferredHeight-2)

	out := []string{top}
	for i := 0; i < innerHeight; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		out = append(out, m.boxLine(line, innerWidth))
	}
	out = append(out, bottom)
	return strings.Join(out, "\n")
}

func (m model) singleSelectLine(label string, selected, focused bool) string {
	marker := "( )"
	if selected {
		marker = "(*)"
	}
	return m.optionLine(marker+" "+label, focused)
}

func (m model) multiSelectLine(label string, checked, focused bool) string {
	marker := "[ ]"
	if checked {
		marker = "[x]"
	}
	return m.optionLine(marker+" "+label, focused)
}

func (m model) optionLine(content string, focused bool) string {
	if focused {
		return m.selectedMarker() + m.selectedRow(content)
	}
	return "  " + content
}

func (m model) popupInputLine(label string, input textinput.Model, inputWidth int) string {
	value := input.Value()
	pos := min(max(0, input.Position()), len([]rune(value)))
	inputText := m.inputValueWithCursor(value, pos, inputWidth)
	separator := " "
	if strings.HasSuffix(label, " ") {
		separator = ""
	}
	return m.label(label) + separator + inputText
}

func (m model) popupInputLinePlainPrefix(prefix string, input textinput.Model, inputWidth int) string {
	value := input.Value()
	pos := min(max(0, input.Position()), len([]rune(value)))
	return prefix + m.inputValueWithCursor(value, pos, inputWidth)
}

func (m model) inputValueWithCursor(value string, pos, width int) string {
	runes := []rune(value)
	pos = min(max(0, pos), len(runes))
	width = max(1, width)
	if len(runes) == 0 {
		return m.value(m.inputCursor())
	}
	start := 0
	if pos >= len(runes) {
		textWidth := max(0, width-1)
		if len(runes) > textWidth {
			start = len(runes) - textWidth
		}
		end := min(len(runes), start+textWidth)
		return m.value(string(runes[start:end]) + m.inputCursor())
	}
	if len(runes) > width {
		start = pos - width + 1
		if start < 0 {
			start = 0
		}
		if start > len(runes)-width {
			start = len(runes) - width
		}
	}
	end := min(len(runes), start+width)
	var b strings.Builder
	for i := start; i < end; i++ {
		if i == pos {
			b.WriteString(m.inputCursorForRune(runes[i]))
			continue
		}
		b.WriteRune(runes[i])
	}
	return m.value(b.String())
}

func (m model) inputCursor() string {
	if m.opts.NoColor {
		return "█"
	}
	return cursorStyle.Render(" ")
}

func (m model) inputCursorForRune(r rune) string {
	if m.opts.NoColor {
		return "█"
	}
	return cursorStyle.Render(string(r))
}

func (m model) renderPopupBoxWithActions(title string, lines []string, actions string) string {
	if strings.TrimSpace(actions) != "" {
		lines = append(append([]string(nil), lines...), "", m.popupActionLine(actions))
	}
	return m.renderPopupBox(title, lines)
}

func (m model) popupActionLine(actions string) string {
	if m.opts.NoColor {
		return actions
	}
	fields := strings.Fields(actions)
	if len(fields) == 0 {
		return ""
	}
	parts := make([]string, 0, (len(fields)+1)/2)
	for i := 0; i < len(fields); i += 2 {
		key := fields[i]
		description := ""
		if i+1 < len(fields) {
			description = fields[i+1]
		}
		part := hotkeyStyle.Render(key)
		if description != "" {
			part += " " + mutedStyle.Render(description)
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, mutedStyle.Render("   "))
}

func (m model) renderPopupBox(title string, lines []string) string {
	lines = popupPaddedLines(lines)
	maxLineWidth := 0
	for _, line := range lines {
		maxLineWidth = max(maxLineWidth, lipgloss.Width(line))
	}
	availableInner := m.boxInnerWidth() - 8
	if availableInner <= 0 {
		availableInner = m.boxInnerWidth()
	}
	innerWidth := max(36, maxLineWidth)
	innerWidth = min(innerWidth, 80)
	innerWidth = min(innerWidth, max(10, availableInner))
	out := []string{m.popupBoxTop(title, innerWidth)}
	for _, line := range lines {
		out = append(out, m.popupBoxLine(line, innerWidth))
	}
	out = append(out, m.popupBoxBottom(innerWidth))
	return strings.Join(out, "\n")
}

func popupPaddedLines(lines []string) []string {
	const horizontalPadding = 2
	const verticalPadding = 1
	out := make([]string, 0, len(lines)+verticalPadding*2)
	for i := 0; i < verticalPadding; i++ {
		out = append(out, "")
	}
	pad := strings.Repeat(" ", horizontalPadding)
	for _, line := range lines {
		out = append(out, pad+line+pad)
	}
	for i := 0; i < verticalPadding; i++ {
		out = append(out, "")
	}
	return out
}

func (m model) popupBoxTop(title string, innerWidth int) string {
	if innerWidth < 10 {
		innerWidth = 10
	}
	titleText := " " + title + " "
	titleLen := lipgloss.Width(titleText)
	if titleLen+2 > innerWidth {
		titleText = " " + truncateInline(title, max(1, innerWidth-6)) + " "
		titleLen = lipgloss.Width(titleText)
	}
	left := max(1, (innerWidth-titleLen)/2)
	rightLen := innerWidth - left - titleLen
	if rightLen < 1 {
		rightLen = 1
	}
	titleRendered := titleText
	if !m.opts.NoColor {
		titleRendered = titleStyle.Render(titleText)
	}
	return m.popupFrame("╭") + m.popupFrame(strings.Repeat("─", left)) + titleRendered + m.popupFrame(strings.Repeat("─", rightLen)) + m.popupFrame("╮")
}

func (m model) popupBoxBottom(innerWidth int) string {
	return m.popupFrame("╰") + m.popupFrame(strings.Repeat("─", innerWidth)) + m.popupFrame("╯")
}

func (m model) popupBoxLine(content string, innerWidth int) string {
	visible := lipgloss.Width(content)
	if visible > innerWidth {
		content = truncateStyled(content, innerWidth)
		visible = lipgloss.Width(content)
	}
	padWidth := innerWidth - visible
	if padWidth < 0 {
		padWidth = 0
	}
	return m.popupFrame("│") + content + strings.Repeat(" ", padWidth) + m.popupFrame("│")
}

func (m model) popupFrame(s string) string {
	if m.opts.NoColor {
		return s
	}
	return titleStyle.Render(s)
}

func (m model) renderPopupStack(body string) string {
	for _, kind := range m.popupLayers() {
		body = m.overlayPopupOnBody(body, m.renderPopup(kind))
	}
	return body
}

func (m model) renderPopup(kind popupKind) string {
	switch kind {
	case popupColumns:
		return m.renderColumnsPopup()
	case popupShortcuts:
		return m.renderShortcutsPopup()
	case popupConfirm:
		return m.renderConfirmPopup()
	case popupSort:
		return m.renderSortPopup()
	case popupRegionSelect:
		return m.renderRegionSelectPopup()
	case popupTypeSelect:
		return m.renderTypeSelectPopup()
	case popupTierSelect:
		return m.renderTierSelectPopup()
	case popupDataTypeSelect:
		return m.renderDataTypeSelectPopup()
	case popupOverwriteSelect:
		return m.renderOverwriteSelectPopup()
	case popupValueActions:
		return m.renderValueActionsPopup()
	case popupPoliciesActions:
		return m.renderPoliciesActionsPopup()
	case popupFileAction:
		return m.renderFileActionPopup()
	case popupFileWriteConfirm:
		return m.renderFileWriteConfirmPopup()
	case popupUnsavedChanges:
		return m.renderUnsavedChangesPopup()
	case popupRandomValue:
		return m.renderRandomValuePopup()
	default:
		return ""
	}
}

func (m model) overlayPopupOnBody(body, popup string) string {
	if popup == "" {
		return body
	}
	bodyLines := renderLines(body)
	popupLines := renderLines(popup)
	if len(popupLines) == 0 {
		return body
	}
	contentHeight := m.height
	if contentHeight <= 0 {
		contentHeight = max(len(bodyLines), len(popupLines))
	}
	for len(bodyLines) < contentHeight {
		bodyLines = append(bodyLines, "")
	}
	if len(bodyLines) > contentHeight {
		bodyLines = bodyLines[:contentHeight]
	}
	popupHeight := len(popupLines)
	if popupHeight > contentHeight {
		popupLines = popupLines[:contentHeight]
		popupHeight = len(popupLines)
	}
	popupWidth := 0
	for _, line := range popupLines {
		popupWidth = max(popupWidth, lipgloss.Width(line))
	}
	viewWidth := m.width
	if viewWidth <= 0 {
		viewWidth = max(popupWidth, m.boxInnerWidth()+2)
	}
	top := max(0, (contentHeight-popupHeight)/2)
	left := max(0, (viewWidth-popupWidth)/2)
	for i, line := range popupLines {
		bodyLines[top+i] = overlayPopupLine(bodyLines[top+i], line, left, popupWidth, viewWidth)
	}
	return strings.Join(bodyLines, "\n")
}

func overlayPopupLine(baseLine, popupLine string, left, popupWidth, viewWidth int) string {
	base := stripANSI(baseLine)
	if viewWidth <= 0 {
		viewWidth = max(lipgloss.Width(base), left+popupWidth)
	}
	base = padVisible(base, viewWidth)
	prefix := takeVisibleColumns(base, left)
	popup := popupLine
	if pad := popupWidth - lipgloss.Width(popup); pad > 0 {
		popup += strings.Repeat(" ", pad)
	}
	suffix := dropVisibleColumns(base, left+popupWidth)
	row := prefix + popup + suffix
	if pad := viewWidth - lipgloss.Width(row); pad > 0 {
		row += strings.Repeat(" ", pad)
	}
	return row
}

func takeVisibleColumns(s string, width int) string {
	if width <= 0 {
		return ""
	}
	out := strings.Builder{}
	used := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if used+rw > width {
			break
		}
		out.WriteRune(r)
		used += rw
	}
	if used < width {
		out.WriteString(strings.Repeat(" ", width-used))
	}
	return out.String()
}

func dropVisibleColumns(s string, start int) string {
	if start <= 0 {
		return s
	}
	used := 0
	for idx, r := range s {
		rw := lipgloss.Width(string(r))
		if used+rw > start {
			return s[idx:]
		}
		used += rw
		if used >= start {
			return s[idx+len(string(r)):]
		}
	}
	return ""
}

func (m model) boxTop(title string, innerWidth int) string {
	if innerWidth < 10 {
		innerWidth = 10
	}
	titleText := " " + title + " "
	titleLen := lipgloss.Width(titleText)
	if titleLen+2 > innerWidth {
		titleText = " " + truncateInline(title, max(1, innerWidth-6)) + " "
		titleLen = lipgloss.Width(titleText)
	}
	left := max(1, (innerWidth-titleLen)/2)
	rightLen := innerWidth - left - titleLen
	if rightLen < 1 {
		rightLen = 1
	}
	titleRendered := titleText
	if !m.opts.NoColor {
		titleRendered = titleStyle.Render(titleText)
	}
	return m.frame("┌") + m.frame(strings.Repeat("─", left)) + titleRendered + m.frame(strings.Repeat("─", rightLen)) + m.frame("┐")
}

func (m model) boxBottom(innerWidth int) string {
	return m.frame("└") + m.frame(strings.Repeat("─", innerWidth)) + m.frame("┘")
}

func (m model) boxLine(content string, innerWidth int) string {
	rawLeft := strings.HasPrefix(content, rawLeftLinePrefix)
	if rawLeft {
		content = strings.TrimPrefix(content, rawLeftLinePrefix)
		innerWidth++
	}
	visible := lipgloss.Width(content)
	if visible > innerWidth {
		content = truncateStyled(content, innerWidth)
		visible = lipgloss.Width(content)
	}
	padWidth := innerWidth - visible
	if padWidth < 0 {
		padWidth = 0
	}
	leftFrame := m.frame("│")
	if rawLeft {
		leftFrame = ""
	}
	return leftFrame + content + strings.Repeat(" ", padWidth) + m.frame("│")
}

// renderFooter formats the fixed bottom hotkey/status line.
func (m model) renderFooter(text string) string {
	if m.opts.NoColor || text == "" {
		return text
	}
	parts := strings.Split(text, " • ")
	for i, part := range parts {
		key, description, ok := strings.Cut(part, " ")
		if !ok {
			parts[i] = hotkeyStyle.Render(part)
			continue
		}
		parts[i] = hotkeyStyle.Render(key) + " " + footerStyle.Render(description)
	}
	return strings.Join(parts, footerStyle.Render(" • "))
}

// renderFullscreen combines a screen body and footer, padding vertical space so the footer stays at the bottom.
func (m model) renderFullscreen(body, footer string) string {
	body = indentBlock(body, 0)
	footer = indentBlock(footer, 0)
	if m.height <= 0 {
		if footer == "" {
			return body
		}
		return body + "\n" + footer
	}

	bodyLines := renderLines(body)
	footerLines := renderLines(footer)
	bodyHeight := max(0, m.height-len(footerLines))
	if len(bodyLines) > bodyHeight {
		bodyLines = bodyLines[:bodyHeight]
	}

	padLines := max(0, m.height-len(bodyLines)-len(footerLines))
	out := make([]string, 0, m.height)
	out = append(out, bodyLines...)
	for i := 0; i < padLines; i++ {
		out = append(out, "")
	}
	out = append(out, footerLines...)
	return strings.Join(out, "\n")
}

func renderLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func (m model) label(s string) string {
	if m.opts.NoColor {
		return s
	}
	return labelStyle.Render(s)
}

func (m model) value(s string) string {
	if m.opts.NoColor {
		return s
	}
	return valueStyle.Render(s)
}

func (m model) hotkey(s string) string {
	if m.opts.NoColor {
		return s
	}
	return hotkeyStyle.Render(s)
}

func (m model) muted(s string) string {
	if m.opts.NoColor {
		return s
	}
	return mutedStyle.Render(s)
}

func (m model) divider(s string) string {
	return strings.Repeat(" ", lipgloss.Width(s))
}

func (m model) frame(s string) string {
	return strings.Repeat(" ", lipgloss.Width(s))
}

func (m model) selectedRow(s string) string {
	if m.opts.NoColor {
		return s
	}
	return selectedRowStyle.Render(s)
}

func (m model) selectedMarker() string {
	if m.opts.NoColor {
		return "| "
	}
	return lipgloss.NewStyle().Foreground(selectedFg).Render("| ")
}

func (m model) searchLine() string {
	line := "Search > " + m.query
	if m.searchInvalid {
		return m.applyErr(line)
	}
	return m.searchPrompt() + m.value(m.query)
}

func (m model) filteredLine() string {
	return m.filteredPrompt() + m.value(m.effectiveQuery)
}

func (m model) searchPrompt() string {
	if m.opts.NoColor {
		return "Search > "
	}
	return searchStyle.Render("Search > ")
}

func (m model) filteredPrompt() string {
	if m.opts.NoColor {
		return "Filtered > "
	}
	return searchStyle.Render("Filtered > ")
}

func (m model) applyErr(s string) string {
	if m.opts.NoColor {
		return s
	}
	return errorStyle.Render(s)
}

func (m model) applyWarning(s string) string {
	if m.opts.NoColor {
		return s
	}
	return warningStyle.Render(s)
}

func (m model) renderFooterWithStatus(text string) string {
	footer := m.renderFooter(text)
	status := m.renderStatusMessage()
	if status == "" {
		return strings.Join([]string{" ", footer, " "}, "\n")
	}
	return strings.Join([]string{" ", status, " ", footer, " "}, "\n")
}

func (m model) renderFooterWithFixedStatus(text string) string {
	status := m.renderStatusMessage()
	if status == "" {
		status = " "
	}
	return strings.Join([]string{status, " ", m.renderFooter(text), " "}, "\n")
}
func quitConfirmationMessage(key string) string {
	return `Are you sure you want to quit? Press "y" to confirm.`
}

func (m model) renderStatusMessage() string {
	switch {
	case m.errMessage != "":
		return m.applyErr(m.errMessage)
	case m.warningMessage != "":
		return m.applyWarning(m.warningMessage)
	case m.message != "":
		return m.muted(m.message)
	default:
		return ""
	}
}

func (m *model) clearTransientStatus() {
	m.message = ""
	m.warningMessage = ""
	m.errMessage = ""
	m.pendingQuit = false
	m.pendingQuitKey = ""
	m.pendingFileWrite = fileWriteConfirmationNone
}

func nextCursor(current, count int) int {
	if count <= 0 {
		return 0
	}
	return (current + 1) % count
}

func previousCursor(current, count int) int {
	if count <= 0 {
		return 0
	}
	return (current - 1 + count) % count
}

func expandLocalPath(path string) (string, error) {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return home, nil
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
	}
	return path, nil
}

func (m model) statusRegion(st Status) string {
	if st.Item.Region == "*" {
		return "-"
	}
	if st.Item.Region != "" {
		return st.Item.Region
	}
	return valueOrDash(m.opts.Region)
}

func (m model) statusText(st Status) string {
	label := statusDisplayLabel(st)
	if m.opts.NoColor {
		return label
	}
	switch label {
	case "OK":
		return lipgloss.NewStyle().Foreground(okFg).Render(label)
	case "MISSING":
		return lipgloss.NewStyle().Foreground(missFg).Render(label)
	case "EMPTY":
		return lipgloss.NewStyle().Foreground(emptyFg).Render(label)
	case "ERROR":
		return lipgloss.NewStyle().Foreground(errFg).Render(label)
	default:
		return label
	}
}

func (m model) currentStatus() Status {
	vis := m.visible()
	if len(vis) == 0 || m.selected < 0 || m.selected >= len(vis) {
		return Status{}
	}
	return m.statuses[vis[m.selected]]
}

func (m model) currentItem() inventory.Item {
	return m.currentStatus().Item
}

func (m model) visible() []int {
	return m.matchesFor(m.effectiveQuery)
}

func (m model) matchesFor(query string) []int {
	q := strings.ToLower(query)
	out := []int{}
	for i, st := range m.statuses {
		if q == "" || strings.Contains(strings.ToLower(st.Item.Path), q) {
			out = append(out, i)
		}
	}
	return out
}

// applySearchQuery updates the search query, validates it against visible rows, and keeps selection in range.
func (m *model) applySearchQuery(query string) {
	m.query = query
	if query == "" {
		m.effectiveQuery = ""
		m.searchInvalid = false
		m.selected = 0
		return
	}
	if len(m.matchesFor(query)) > 0 {
		m.effectiveQuery = query
		m.searchInvalid = false
		m.selected = 0
		return
	}
	m.searchInvalid = true
	m.ensureSelection()
}

func (m model) visiblePaths() []string {
	vis := m.visible()
	out := make([]string, 0, len(vis))
	for _, idx := range vis {
		out = append(out, m.statuses[idx].Item.Path)
	}
	return out
}

func (m model) visibleItems() []inventory.Item {
	vis := m.visible()
	out := make([]inventory.Item, 0, len(vis))
	for _, idx := range vis {
		out = append(out, m.statuses[idx].Item)
	}
	return out
}

// ensureSelection clamps the selected row so it always points at a visible item when possible.
func (m *model) ensureSelection() {
	vis := m.visible()
	if len(vis) == 0 {
		m.selected = 0
		return
	}
	if m.selected < 0 {
		m.selected = 0
	}
	if m.selected >= len(vis) {
		m.selected = len(vis) - 1
	}
}

// move changes the selected row by delta within the currently visible result set.
func (m *model) move(delta int) {
	vis := m.visible()
	if len(vis) == 0 {
		return
	}
	if delta == 1 {
		m.selected = nextCursor(m.selected, len(vis))
		return
	}
	if delta == -1 {
		m.selected = previousCursor(m.selected, len(vis))
		return
	}
	m.selected = max(0, min(len(vis)-1, m.selected+delta))
}

func (m model) boxInnerWidth() int {
	return max(40, m.width-2)
}

func (m model) listBlockHeight() int {
	// Main page content layout: optional selected parameter block + dynamic list block.
	return max(8, m.height-m.selectedParameterBlockHeight())
}

func (m model) selectedParameterBlockHeight() int {
	if !m.selectedExpanded {
		return 0
	}
	st := m.currentStatus()
	if st.Item.Path == "" {
		return 0
	}
	return len(m.renderFieldPairs(m.selectedParameterFields(st, true), 11)) + 2
}

func (m model) listBodyHeight() int {
	// Top/bottom border + header + header divider + optional filter/search lines.
	reserved := 4
	if m.searchMode || m.effectiveQuery != "" {
		reserved += 2
	}
	return max(3, m.listBlockHeight()-reserved)
}

// replaceStatus updates the status list after saving a value.
// It prefers the exact path+region row so multi-region screens do not replace the wrong regional value;
// when a wildcard missing row was saved to a concrete region, it replaces that wildcard row as a fallback.
func (m *model) replaceStatus(path string, st Status) {
	fallback := -1
	for i := range m.statuses {
		if m.statuses[i].Item.Path != path {
			continue
		}
		if sameItem(m.statuses[i].Item, st.Item) {
			m.statuses[i] = st
			return
		}
		if m.statuses[i].Item.Region == st.Item.Region {
			fallback = i
			continue
		}
		if fallback < 0 || m.statuses[i].Item.Region == "*" {
			fallback = i
		}
	}
	if fallback >= 0 {
		m.statuses[fallback] = st
		return
	}
	m.statuses = append(m.statuses, st)
	m.selected = len(m.statuses) - 1
}

func (m *model) removeItemRows(items []inventory.Item) {
	targets := map[string]bool{}
	for _, item := range items {
		targets[itemKey(item.Region, item.Path)] = true
	}
	kept := m.statuses[:0]
	for _, st := range m.statuses {
		if targets[itemKey(st.Item.Region, st.Item.Path)] {
			continue
		}
		kept = append(kept, st)
	}
	m.statuses = kept
}

// markMissingItem updates the UI after deletion by replacing matching concrete rows with a missing status.
func (m *model) markMissingItem(item inventory.Item) {
	for i := range m.statuses {
		if sameItem(m.statuses[i].Item, item) {
			m.statuses[i] = Status{Item: item, Type: ssm.DefaultParameterType.String()}
			return
		}
	}
}

// sameItem compares inventory identity fields that uniquely identify a row in the UI.
func sameItem(a, b inventory.Item) bool {
	return a.Path == b.Path && a.Region == b.Region
}

// columnItems returns optional table columns in the order presented to the user.
func columnItems() []columnName {
	return []columnName{
		columnValue,
		columnType,
		columnRegion,
		columnDate,
		columnVersion,
		columnTier,
		columnLength,
		columnHash,
		columnUser,
		columnDescription,
	}
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

type sortItem struct {
	hotkey string
	column columnName
	label  string
}

func sortItems() []sortItem {
	return []sortItem{
		{hotkey: "n", column: columnPath, label: "Name"},
		{hotkey: "v", column: columnValue, label: "Value"},
		{hotkey: "t", column: columnType, label: "Type"},
		{hotkey: "r", column: columnRegion, label: "Region"},
		{hotkey: "a", column: columnDate, label: "Date"},
		{hotkey: "o", column: columnVersion, label: "Version"},
		{hotkey: "i", column: columnTier, label: "Tier"},
		{hotkey: "z", column: columnLength, label: "Len"},
		{hotkey: "s", column: columnHash, label: "SHA256"},
		{hotkey: "u", column: columnUser, label: "User"},
		{hotkey: "e", column: columnDescription, label: "Description"},
	}
}

func sortColumnByLetterHotkey(key string) (columnName, bool) {
	for _, item := range sortItems() {
		if item.hotkey == key {
			return item.column, true
		}
	}
	return "", false
}

func (m model) popupSortItems() []sortItem {
	visible := map[columnName]bool{columnPath: true}
	for _, col := range columnItems() {
		if m.columnAllowed(col) && m.columns[col] {
			visible[col] = true
		}
	}
	items := make([]sortItem, 0, len(visible))
	for _, item := range sortItems() {
		if visible[item.column] {
			items = append(items, item)
		}
	}
	return items
}

func (m model) popupSortColumnByLetterHotkey(key string) (columnName, bool) {
	for _, item := range m.popupSortItems() {
		if item.hotkey == key {
			return item.column, true
		}
	}
	return "", false
}

func (m model) visibleSortItems() []sortItem {
	cols := []columnName{columnPath}
	for _, col := range columnItems() {
		if m.columnAllowed(col) && m.columns[col] {
			cols = append(cols, col)
		}
	}
	items := make([]sortItem, 0, len(cols))
	for i, col := range cols {
		n := i + 1
		if n > 10 {
			break
		}
		hotkey := strconv.Itoa(n)
		if n == 10 {
			hotkey = "0"
		}
		items = append(items, sortItem{hotkey: hotkey, column: col, label: columnLabel(col)})
	}
	return items
}

func (m model) visibleSortColumnByHotkey(key string) (columnName, bool) {
	for _, item := range m.visibleSortItems() {
		if item.hotkey == key {
			return item.column, true
		}
	}
	return "", false
}

func (m model) sortCursorForCurrentSort() int {
	items := m.popupSortItems()
	for i, item := range items {
		if item.column == m.sortBy {
			return i
		}
	}
	return 0
}

func (m *model) applySort(column columnName) {
	if column == "" {
		return
	}
	descending := false
	if column == m.sortBy {
		descending = !m.sortDescending
	}
	m.applySortWithDirection(column, descending)
}

func (m *model) toggleSortDirection(column columnName) {
	if column == "" {
		return
	}
	descending := true
	if column == m.sortBy {
		descending = !m.sortDescending
	}
	m.applySortWithDirection(column, descending)
}

func (m *model) applySortWithDirection(column columnName, descending bool) {
	if column == "" {
		return
	}
	var selectedKey string
	if len(m.visible()) > 0 && m.selected < len(m.visible()) {
		st := m.statuses[m.visible()[m.selected]]
		selectedKey = itemKey(st.Item.Region, st.Item.Path)
	}
	m.sortBy = column
	m.sortDescending = descending
	sort.SliceStable(m.statuses, func(i, j int) bool {
		cmp := naturalCompare(m.tableCellValue(column, 0, m.statuses[i]), m.tableCellValue(column, 0, m.statuses[j]))
		if descending {
			return cmp > 0
		}
		return cmp < 0
	})
	if selectedKey != "" {
		for idx, visIdx := range m.visible() {
			st := m.statuses[visIdx]
			if itemKey(st.Item.Region, st.Item.Path) == selectedKey {
				m.selected = idx
				return
			}
		}
	}
	m.ensureSelection()
}

func naturalCompare(left, right string) int {
	leftRunes := []rune(strings.ToLower(strings.TrimSpace(left)))
	rightRunes := []rune(strings.ToLower(strings.TrimSpace(right)))
	i, j := 0, 0
	for i < len(leftRunes) && j < len(rightRunes) {
		if unicode.IsDigit(leftRunes[i]) && unicode.IsDigit(rightRunes[j]) {
			li, rj := i, j
			for i < len(leftRunes) && unicode.IsDigit(leftRunes[i]) {
				i++
			}
			for j < len(rightRunes) && unicode.IsDigit(rightRunes[j]) {
				j++
			}
			if cmp := compareDigitRuns(leftRunes[li:i], rightRunes[rj:j]); cmp != 0 {
				return cmp
			}
			continue
		}
		if leftRunes[i] < rightRunes[j] {
			return -1
		}
		if leftRunes[i] > rightRunes[j] {
			return 1
		}
		i++
		j++
	}
	if len(leftRunes)-i < len(rightRunes)-j {
		return -1
	}
	if len(leftRunes)-i > len(rightRunes)-j {
		return 1
	}
	return 0
}

func compareDigitRuns(left, right []rune) int {
	leftTrimmed := trimLeadingZeroes(left)
	rightTrimmed := trimLeadingZeroes(right)
	if len(leftTrimmed) < len(rightTrimmed) {
		return -1
	}
	if len(leftTrimmed) > len(rightTrimmed) {
		return 1
	}
	for i := range leftTrimmed {
		if leftTrimmed[i] < rightTrimmed[i] {
			return -1
		}
		if leftTrimmed[i] > rightTrimmed[i] {
			return 1
		}
	}
	if len(left) < len(right) {
		return -1
	}
	if len(left) > len(right) {
		return 1
	}
	return 0
}

func trimLeadingZeroes(value []rune) []rune {
	idx := 0
	for idx < len(value)-1 && value[idx] == '0' {
		idx++
	}
	return value[idx:]
}

func (m model) columnHeader(c columnName) string {
	if c == columnIndex {
		return "#"
	}
	header := strings.ToUpper(columnLabel(c))
	if c == m.sortBy {
		header += " " + m.sortDirectionArrow()
	}
	return header
}

func (m model) sortDirectionArrow() string {
	if m.sortDescending {
		return "↓"
	}
	return "↑"
}

func (m model) sortPopupLabel(item sortItem) string {
	if item.column == m.sortBy {
		return item.label + " " + m.sortDirectionArrow()
	}
	return item.label
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

func (m model) columnAllowed(column columnName) bool {
	return m.fieldAllowed(fieldForColumn(column))
}

func fieldForColumn(column columnName) string {
	switch column {
	case columnPath:
		return "name"
	case columnRegion:
		return "region"
	case columnDate:
		return "date"
	case columnType:
		return "type"
	case columnTier:
		return "tier"
	case columnVersion:
		return "version"
	case columnLength:
		return "len"
	case columnHash:
		return "sha256"
	case columnValue:
		return "value"
	case columnUser:
		return "user"
	case columnDescription:
		return "description"
	default:
		return string(column)
	}
}

func (m model) allowedColumnItems() []columnName {
	items := columnItems()
	out := make([]columnName, 0, len(items))
	for _, column := range items {
		if m.columnAllowed(column) {
			out = append(out, column)
		}
	}
	return out
}

func (m model) editFieldAllowed(field editField) bool {
	switch field {
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

func columnLabel(c columnName) string {
	switch c {
	case columnIndex:
		return "Index"
	case columnPath:
		return "Name"
	case columnRegion:
		return "Region"
	case columnDate:
		return "Date"
	case columnType:
		return "Type"
	case columnTier:
		return "Tier"
	case columnVersion:
		return "Version"
	case columnLength:
		return "Len"
	case columnHash:
		return "SHA256"
	case columnValue:
		return "Value"
	case columnUser:
		return "User"
	case columnDescription:
		return "Description"
	default:
		return string(c)
	}
}

func cloneColumnVisibility(columns map[columnName]bool) map[columnName]bool {
	clone := map[columnName]bool{}
	for _, column := range columnItems() {
		clone[column] = columns[column]
	}
	return clone
}

func (m model) ensureColumnsDraft() model {
	if m.columnsDraft == nil {
		m.columnsDraft = cloneColumnVisibility(m.columns)
	}
	return m
}

func (m model) columnsForRendering() map[columnName]bool {
	if m.activePopup == popupColumns && m.columnsDraft != nil {
		return m.columnsDraft
	}
	return m.columns
}

func defaultColumnVisibility(selected []string) map[columnName]bool {
	columns := map[columnName]bool{}
	for _, column := range columnItems() {
		columns[column] = false
	}
	for _, name := range selected {
		if column, ok := optionalColumnByName(name); ok {
			columns[column] = true
		}
	}
	return columns
}

// ParseColumnOption validates a comma-separated list of optional TUI columns.
// The # and NAME columns are always visible and therefore are intentionally not accepted here.
func ParseColumnOption(value string) ([]string, error) {
	names := compactColumnNames(value)
	if len(names) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		column, ok := optionalColumnByName(name)
		if !ok {
			return nil, fmt.Errorf("unsupported --show-columns value %q; supported columns: %s", name, strings.Join(ValidColumnNames(), ","))
		}
		canonical := string(column)
		if !seen[canonical] {
			seen[canonical] = true
			out = append(out, canonical)
		}
	}
	return out, nil
}

func ValidColumnNames() []string {
	columns := columnItems()
	out := make([]string, 0, len(columns))
	for _, column := range columns {
		out = append(out, string(column))
	}
	return out
}

func compactColumnNames(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.ToLower(strings.TrimSpace(part))
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func optionalColumnByName(name string) (columnName, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, column := range columnItems() {
		if name == string(column) {
			return column, true
		}
	}
	return "", false
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

// textAreaFooterText includes region-switching shortcut help only when multiple concrete regions are available.
func (m model) textAreaFooterText() string {
	valueAction := ""
	switch m.editField {
	case editFieldValue:
		valueAction = " • alt+e value actions"
	case editFieldPolicies:
		valueAction = " • alt+e policies actions"
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

func indexOf(values []string, value string) int {
	for i, candidate := range values {
		if candidate == value {
			return i
		}
	}
	return 0
}

// mainFooterText returns shortcuts for the main table screen.
func mainFooterText(detailsShown bool) string {
	detailsAction := "d show details"
	if detailsShown {
		detailsAction = "d hide details"
	}
	return "ctrl+/ help • enter edit • n new • " + detailsAction + " • / search • c columns • s sort • x delete • X delete visible • esc quit"
}

func searchFooterText() string {
	return "ctrl+/ help • enter apply • esc cancel"
}

func (m model) popupFooterText(kind popupKind) string {
	switch kind {
	case popupShortcuts:
		return "esc close"
	case popupSort:
		return m.sortPopupScreenFooter()
	case popupColumns:
		return "ctrl+/ help • space toggle • enter apply • a all • x none • esc cancel"
	case popupValueActions:
		return "ctrl+/ help • enter select • c clear • r random • l load • w write • esc cancel"
	case popupPoliciesActions:
		return "ctrl+/ help • enter select • c clear • l load • w write • esc cancel"
	case popupRandomValue:
		return "ctrl+/ help • enter select • b base64 • x hex • u uuid • c custom • esc cancel"
	case popupFileAction:
		button := "confirm"
		switch m.fileActionMode {
		case "load":
			button = "load"
		case "write":
			button = "write"
		case "random-custom":
			button = "generate"
		}
		return "ctrl+/ help • enter " + button + " • esc cancel"
	case popupFileWriteConfirm:
		return "ctrl+/ help • enter yes • esc cancel"
	case popupUnsavedChanges:
		return "ctrl+/ help • enter discard • esc cancel"
	case popupConfirm:
		return "ctrl+/ help • enter confirm • esc cancel"
	case popupRegionSelect:
		return "ctrl+/ help • enter select • esc cancel"
	case popupTypeSelect:
		return "ctrl+/ help • enter select • e secure • s string • l list • esc cancel"
	case popupTierSelect:
		return "ctrl+/ help • enter select • i intelligent • s standard • a advanced • esc cancel"
	case popupDataTypeSelect:
		return "ctrl+/ help • enter select • t text • a AMI • i integration • esc cancel"
	case popupOverwriteSelect:
		return "ctrl+/ help • enter select • t true • f false • esc cancel"
	default:
		return "ctrl+/ help • esc cancel"
	}
}

func (m model) sortPopupScreenFooter() string {
	parts := []string{"ctrl+/ help", "enter sort/toggle", "d direction"}
	for _, item := range m.popupSortItems() {
		parts = append(parts, item.hotkey+" "+strings.ToLower(item.label))
	}
	parts = append(parts, "esc cancel")
	return strings.Join(parts, " • ")
}

// shortcutsText returns the context-sensitive shortcut reference shown by the Shortcuts screen.
func (m model) shortcutsText() string {
	forScreen := m.shortcutsFor
	if forScreen == 0 && m.screen == screenHelp {
		forScreen = screenMain
	}
	if m.shortcutsPopupFor != popupNone {
		return m.popupShortcutsText(m.shortcutsPopupFor)
	}
	sections := []string{m.actionsShortcuts(forScreen), m.sortShortcuts(forScreen), m.navigationShortcuts(forScreen), globalShortcuts(forScreen)}
	out := []string{}
	for _, section := range sections {
		section = strings.TrimSpace(section)
		if section == "" {
			continue
		}
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, strings.Split(section, "\n")...)
	}
	return strings.Join(out, "\n")
}

func (m model) popupShortcutsText(kind popupKind) string {
	sections := []string{m.popupActionsShortcuts(kind), m.popupSortShortcuts(kind), m.popupNavigationShortcuts(kind), globalShortcuts(m.shortcutsFor)}
	out := []string{}
	for _, section := range sections {
		section = strings.TrimSpace(section)
		if section == "" {
			continue
		}
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, strings.Split(section, "\n")...)
	}
	return strings.Join(out, "\n")
}

func (m model) popupActionsShortcuts(kind popupKind) string {
	switch kind {
	case popupSort:
		return strings.TrimSpace(`Actions
  enter        sort selected column / toggle active direction
  d            toggle selected direction
  esc / q / ctrl+g  cancel`)
	case popupColumns:
		return strings.TrimSpace(`Actions
  space        toggle focused column
  enter        apply columns
  a            show all columns
  x            hide all optional columns
  esc / q / ctrl+g  cancel`)
	case popupValueActions:
		return strings.TrimSpace(`Actions
  enter        select focused action
  c            clear value
  r            random value
  l            load from file
  w            write to file
  esc / q / ctrl+g  cancel`)
	case popupPoliciesActions:
		return strings.TrimSpace(`Actions
  enter        select focused action
  c            clear policies
  l            load from file
  w            write to file
  esc / q / ctrl+g  cancel`)
	case popupFileAction:
		return strings.TrimSpace(`Actions
  enter        confirm input
  esc / q / ctrl+g  cancel`)
	case popupFileWriteConfirm:
		return strings.TrimSpace(`Actions
  enter        yes / continue
  y            yes / continue
  esc / q / ctrl+g  cancel`)
	case popupUnsavedChanges:
		return strings.TrimSpace(`Actions
  enter        discard changes
  esc / q / ctrl+g  cancel`)
	case popupRandomValue:
		return strings.TrimSpace(`Actions
  enter        select focused option
  b            base64 32 bytes
  x            hex 32 bytes
  u            uuid
  c            custom length base64
  esc / q / ctrl+g  cancel`)
	case popupTypeSelect:
		return strings.TrimSpace(`Actions
  enter        select focused option
  e            SecureString
  s            String
  l            StringList
  esc / q / ctrl+g  cancel`)
	case popupTierSelect:
		return strings.TrimSpace(`Actions
  enter        select focused option
  i            Intelligent-Tiering
  s            Standard
  a            Advanced
  esc / q / ctrl+g  cancel`)
	case popupDataTypeSelect:
		return strings.TrimSpace(`Actions
  enter        select focused option
  t            text
  a            aws:ec2:image
  i            aws:ssm:integration
  esc / q / ctrl+g  cancel`)
	case popupOverwriteSelect:
		return strings.TrimSpace(`Actions
  enter        select focused option
  t            true
  f            false
  esc / q / ctrl+g  cancel`)
	case popupRegionSelect:
		return strings.TrimSpace(`Actions
  enter        select focused option
  esc / q / ctrl+g  cancel`)
	default:
		return strings.TrimSpace(`Actions
  esc / q / ctrl+g  cancel`)
	}
}

func (m model) popupSortShortcuts(kind popupKind) string {
	if kind != popupSort {
		return ""
	}
	lines := []string{"Sort"}
	for _, item := range m.popupSortItems() {
		lines = append(lines, fmt.Sprintf("  %-12s sort by %s", item.hotkey, columnLabel(item.column)))
	}
	lines = append(lines, "  d            toggle selected direction")
	return strings.Join(lines, "\n")
}

func (m model) popupNavigationShortcuts(kind popupKind) string {
	switch kind {
	case popupSort, popupColumns, popupRegionSelect, popupTypeSelect, popupTierSelect, popupDataTypeSelect, popupOverwriteSelect, popupValueActions, popupPoliciesActions, popupRandomValue:
		return m.navigationShortcuts(screenColumns)
	default:
		return ""
	}
}

func (m model) actionsShortcuts(forScreen screen) string {
	switch forScreen {
	case screenMain:
		return strings.TrimSpace(`Actions
  enter        edit value
  n            new parameter
  d            show/hide details
  /            search
  c            columns
  s            sort popup
  x            delete selected value
  X            delete visible/filtered values
  v            reveal/hide values
  esc / q      quit`)
	case screenTextArea:
		if m.keymapStyle() == keymapVi {
			if m.viInsertMode {
				return strings.TrimSpace(`Actions
  esc          normal mode
  ctrl+s       save
  alt+e        value/policies actions popup
  y            confirm pending file write warning`)
			}
			return strings.TrimSpace(`Actions
  i            insert mode
  ctrl+s       save
  alt+e        value/policies actions popup
  y            confirm pending file write warning
  esc / q / ctrl+g  back`)
		}
		return strings.TrimSpace(`Actions
  ctrl+s       save
  alt+e        value/policies actions popup
  enter        expand/newline in Description/Policies/Value / choose selectors / next field
  y            confirm pending file write warning
  esc / q / ctrl+g  back`)
	case screenColumns:
		return strings.TrimSpace(`Actions
  space/enter  toggle column
  a            show all columns
  x            hide all optional columns
  esc / q / ctrl+g  back`)
	case screenConfirm:
		return strings.TrimSpace(`Actions
  enter        confirm
  esc / q / ctrl+g  back`)
	case screenRegionSelect, screenTypeSelect:
		return strings.TrimSpace(`Actions
  enter        choose option
  esc / q / ctrl+g  back`)
	case screenLoading:
		return strings.TrimSpace(`Actions
  esc / q      quit`)
	default:
		return strings.TrimSpace(`Actions
  esc / q      back`)
	}
}

func (m model) sortShortcuts(forScreen screen) string {
	if forScreen != screenMain {
		return ""
	}
	items := m.visibleSortItems()
	if len(items) == 0 {
		return ""
	}
	lines := []string{"Sort"}
	for _, item := range items {
		lines = append(lines, fmt.Sprintf("  %-12s sort by %s", item.hotkey, columnLabel(item.column)))
	}
	return strings.Join(lines, "\n")
}

func (m model) navigationShortcuts(forScreen screen) string {
	if forScreen == screenMain || forScreen == screenColumns || forScreen == screenRegionSelect || forScreen == screenTypeSelect {
		if m.keymapStyle() == keymapVi {
			return strings.TrimSpace(`Navigation
  ↑ / k / shift+tab          previous row/option
  ↓ / j / tab                next row/option
  PgUp                       page up
  PgDn                       page down
  Home / gg                  first row/option
  End / G                    last row/option`)
		}
		return strings.TrimSpace(`Navigation
  ↑ / ctrl+p / shift+tab     previous row/option
  ↓ / ctrl+n / tab           next row/option
  PgUp / alt+v               page up
  PgDn / ctrl+v              page down
  Home / alt+<               first row/option
  End / alt+>                last row/option`)
	}
	if forScreen == screenTextArea {
		if m.keymapStyle() == keymapVi {
			return strings.TrimSpace(`Mode
  i                          enter insert mode
  esc                        leave insert mode / back from normal mode

Navigation
  h / l                      backward/forward character
  j / k                      next/previous line in Description/Policies/Value
  PgDn / ctrl+f              page down in Description/Policies/Value
  PgUp / ctrl+b              page up in Description/Policies/Value
  w / b                      forward/backward word
  0 / $                      start/end of line
  gg / G                     start/end of text
  tab                        next field
  shift+tab                  previous field
  PgUp / PgDn                page in Description/Policies/Value

Editing
  x                          delete current character
  D                          delete to end of real line / join next line
  dw                         delete next word
  db                         delete previous word
  ctrl+l                     show/hide line numbers`)
		}
		return strings.TrimSpace(`Navigation
  tab                        next field
  shift+tab                  previous field
  ctrl+f / ctrl+b            forward/backward character
  ctrl+p / ctrl+n            previous/next line
  PgDn / ctrl+v              page down in Description/Policies/Value
  PgUp / alt+v               page up in Description/Policies/Value
  ctrl+a / ctrl+e            start/end of line
  alt+f / alt+b              forward/backward word
  alt+< / alt+>              start/end of text
  ctrl+d                     delete current character
  ctrl+k                     delete to end of real line / join next line
  alt+d                      delete next word
  alt+backspace              delete previous word
  ctrl+l                     show/hide line numbers`)
	}
	return ""
}

func globalShortcuts(forScreen screen) string {
	return strings.TrimSpace(`Global
  ctrl+/       open shortcuts`)
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

func promptLineCount(value string) int {
	if value == "" {
		return 1
	}
	return len(strings.Split(value, "\n"))
}

// statusDisplayLabel converts Status to the longer labels used in the interactive table.
func statusDisplayLabel(st Status) string {
	switch statusLabel(st) {
	case "MISS":
		return "MISSING"
	case "ERR":
		return "ERROR"
	default:
		return statusLabel(st)
	}
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func firstLines(s string, maxLines int) string {
	if s == "" || maxLines <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= maxLines {
		return s
	}
	return strings.Join(lines[:maxLines], "\n")
}

func indentBlock(s string, spaces int) string {
	if s == "" {
		return ""
	}
	prefix := strings.Repeat(" ", spaces)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

func padMin(v string, width int) string {
	if len(v) >= width {
		return v
	}
	return v + strings.Repeat(" ", width-len(v))
}

func pad(v string, width int) string {
	visible := lipgloss.Width(v)
	if visible >= width {
		return truncateStyled(v, width)
	}
	return v + strings.Repeat(" ", width-visible)
}

func padVisible(v string, width int) string {
	plain := stripANSI(v)
	if len(plain) >= width {
		return v
	}
	return v + strings.Repeat(" ", width-len(plain))
}

func truncateInline(v string, width int) string {
	v = strings.ReplaceAll(v, "\r", "")
	v = strings.ReplaceAll(v, "\n", " ")
	if width < 4 {
		width = 4
	}
	if lipgloss.Width(v) <= width {
		return v
	}
	runes := []rune(v)
	out := make([]rune, 0, min(len(runes), width))
	for _, r := range runes {
		if lipgloss.Width(string(out))+lipgloss.Width(string(r))+3 > width {
			break
		}
		out = append(out, r)
	}
	return string(out) + "..."
}

func truncateStyled(v string, width int) string {
	plain := stripANSI(v)
	if len(plain) <= width {
		return v
	}
	return truncateInline(plain, width)
}

// stripANSI removes ANSI escape sequences so width calculations work with styled strings.
func stripANSI(s string) string {
	out := make([]rune, 0, len(s))
	inEsc := false
	for i, r := range s {
		_ = i
		if !inEsc && r == 0x1b {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		out = append(out, r)
	}
	return string(out)
}

// sliceForScroll returns a fixed-height window over a larger line list.
func sliceForScroll(lines []string, scroll, height int) []string {
	if height <= 0 || len(lines) <= height {
		return lines
	}
	if scroll < 0 {
		scroll = 0
	}
	if scroll > len(lines)-height {
		scroll = len(lines) - height
	}
	return lines[scroll : scroll+height]
}

func pageSize(h int) int { return max(5, h-4) }

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
