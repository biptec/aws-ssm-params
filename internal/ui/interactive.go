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
	"time"

	crerr "github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/fileio"
	"github.com/biptec/aws-ssm-params/internal/filter"
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

// RunInteractive creates and runs the Bubble Tea program in the terminal alternate screen.
// The function returns only after the user quits the TUI or Bubble Tea reports an error.
func RunInteractive(ctx context.Context, client ssm.Client, items []inventory.Item, opts Options) error {
	m := newModel(ctx, client, items, opts)
	programOptions := []tea.ProgramOption{tea.WithAltScreen()}
	if opts.UseInputTTY {
		programOptions = append(programOptions, tea.WithInputTTY())
	}
	p := tea.NewProgram(m, programOptions...)
	_, err := p.Run()
	return crerr.Wrap(err, "run interactive TUI")
}

// newModel initializes the TUI model with default inputs, textarea settings, visible columns, and loading state.
// Statuses are not loaded here; Init starts that asynchronous work so the UI can show progress immediately.
func newModel(ctx context.Context, client ssm.Client, items []inventory.Item, opts Options) model {
	sortRules := parseInitialSortOptions(opts.Sort)
	sortBy, sortDescending := primarySortRule(sortRules)
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
		ctx:                  ctx,
		items:                items,
		statuses:             pendingStatuses(items),
		opts:                 opts,
		loadCh:               make(chan tea.Msg),
		screen:               screenLoading,
		shortcutsFor:         screenLoading,
		loadingTitle:         "Loading parameters",
		input:                input,
		textArea:             area,
		editPoliciesArea:     policiesArea,
		editDescriptionArea:  descriptionArea,
		editPathInput:        editPathInput,
		editDescriptionInput: editDescriptionInput,
		editFileInput:        editFileInput,
		columns:              defaultColumnVisibility(opts.ShowColumns),
		sortBy:               sortBy,
		sortDescending:       sortDescending,
		sortRules:            sortRules,
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
	return tea.Batch(startLoadCmd(m.ctx, m.client, m.items, m.opts.FilterGroups, m.opts.Regions, m.opts.IncludeValues, m.loadCh), waitForLoad(m.loadCh), tickLoadingSpinner())
}

// startLoadCmd launches the initial SSM status scan in a goroutine.
// Progress and final results are sent through loadCh so the Bubble Tea event loop can render loading updates.
func startLoadCmd(ctx context.Context, client ssm.Client, items []inventory.Item, groups []filter.Group, regions []string, includeValues bool, ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		go func() {
			loader := func(done, total int, region string, chunk []inventory.Item) {
				ch <- progressMsg{done: done, total: total, region: region, items: append([]inventory.Item(nil), chunk...)}
			}
			emitBatch := func(statuses []Status) {
				statuses = FilterStatusesByGroups(statuses, groups)
				if len(statuses) > 0 {
					ch <- statusBatchMsg(append([]Status(nil), statuses...))
				}
			}
			var statuses []Status
			if len(groups) > 0 && len(items) == 0 {
				statuses = LoadFilteredStatusesBatchForRegions(ctx, client, groups, includeValues, regions, loader)
			} else {
				statuses = LoadStatusesBatchForRegionsStream(ctx, client, items, includeValues, regions, loader, emitBatch)
				statuses = FilterStatusesByGroups(statuses, groups)
			}
			ch <- loadedMsg(statuses)
		}()
		return nil
	}
}

// waitForLoad blocks one Bubble Tea command worker until the status loader emits its next message.
// Update schedules it again after each progress message, giving the UI a stream of loading updates.
func waitForLoad(ch <-chan tea.Msg) tea.Cmd { return func() tea.Msg { return <-ch } }

func tickLoadingSpinner() tea.Cmd {
	return tea.Tick(loadingSpinnerInterval, func(time.Time) tea.Msg { return loadingTickMsg{} })
}

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
		m.mergeStatusBatch([]Status(msg))
		return m, waitForLoad(m.loadCh)

	case loadedMsg:
		m.statuses = []Status(msg)
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

			default:
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
			if m.query != "" {
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
	case navNone:
		return
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

	default:
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

func (m model) updateSortPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := m.popupSortItems()
	key := msg.String()
	if key != "d" {
		if col, ok := m.popupSortColumnByLetterHotkey(key); ok {
			m.toggleSortColumn(col)
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
	case " ", "enter", "ctrl+j":
		if len(items) > 0 {
			m.sortCursor = min(m.sortCursor, len(items)-1)
			m.toggleSortColumn(items[m.sortCursor].column)
		}
	case "d":
		if len(items) > 0 {
			m.sortCursor = min(m.sortCursor, len(items)-1)
			m.toggleSortDirection(items[m.sortCursor].column)
		}
	}
	return m, nil
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

func cursorFromNavigation(cursor, length int, action navigationAction) int {
	if length <= 0 {
		return 0
	}
	switch action {
	case navNone:
		return cursor
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
		m.busyMessage = fmt.Sprintf("Deleting %d parameter(s)...", len(items))
		m.loadingTitle = ""
		m.loadingLines = nil
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
		m.busyMessage = fmt.Sprintf("Deleting %d parameter(s)...", len(items))
		m.loadingTitle = ""
		m.loadingLines = nil
		m.activePopup = popupNone
		m.popupStack = nil
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
func (m model) renderMainScreen() string {
	if !m.selectedExpanded || m.currentStatus().Item.Path == "" {
		return m.renderListBlock()
	}
	return m.renderSelectedParameterBlock(true) + "\n" + m.renderListBlock()
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

	if m.shouldShowEncryptedEditPlaceholder() {
		lines = append(lines, m.editFieldLine(editFieldValue, "Value", m.encryptedPlaceholder(), labelWidth))
	} else if m.editFieldAllowed(editFieldValue) {
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

func (m model) renderSortPopup() string {
	return m.renderPopupBoxWithActions("Sort By", m.sortOptionLines(), "Space toggle   D direction   Esc close")
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
	switch m.fileActionMode {
	case "write":
		title = "Write to file"
		if m.fileActionField == editFieldPolicies {
			title = "Write policies to file"
		}
	case "random-custom":
		title = "Random Value"
		label = "Byte length:"
		inputWidth = 12
	}
	button := "load"
	switch m.fileActionMode {
	case "write":
		button = "write"
	case "random-custom":
		button = "generate"
	}
	lines := []string{m.popupInputLine(label, m.input, inputWidth)}
	return m.renderPopupBoxWithActions(title, lines, "Enter "+button+"   Esc cancel")
}

func (m model) renderFileWriteConfirmPopup() string {
	message := "Confirm file write?"
	switch m.pendingFileWrite {
	case fileWriteConfirmationNone:
	case fileWriteConfirmationSecure:
		message = "This is a SecureString value. Write it to a local file in plain text?"
	case fileWriteConfirmationOverwrite:
		message = "File already exists. Overwrite it?"

	default:
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
		_, checked := sortRuleForColumn(m.sortRules, item.column)
		lines = append(lines, m.multiSelectLine(m.sortPopupLabel(item), checked, i == m.sortCursor))
	}
	return lines
}

// renderConfirmScreen renders the destructive-action confirmation prompt and input field.
func (m model) renderConfirmScreen() string {
	confirmLines := strings.Split(m.confirmPrompt, "\n")
	lines := make([]string, 0, len(confirmLines)+2)
	for _, line := range confirmLines {
		lines = append(lines, "  "+line)
	}
	lines = append(lines, "", "  > "+m.input.View())
	return m.renderBox("Confirm", lines, m.height)
}

func (m model) renderConfirmPopup() string {
	confirmLines := strings.Split(m.confirmPrompt, "\n")
	lines := make([]string, 0, len(confirmLines)+2)
	for _, line := range confirmLines {
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
	lines := make([]string, 0, 2+len(regions))
	lines = append(lines, "  "+m.muted("Choose region for saving this value:"), "")
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
	lines := make([]string, 0, 2+len(regions))
	lines = append(lines, m.muted("Choose region for saving this value:"), "")
	for i, region := range regions {
		lines = append(lines, m.singleSelectLine(region, i == m.regionCursor, i == m.regionCursor))
	}
	return lines
}

// renderTypeSelectScreen renders the AWS SSM parameter type picker used by value editors.
func (m model) renderTypeSelectScreen() string {
	typeItems := parameterTypeItems()
	lines := make([]string, 0, 2+len(typeItems))
	lines = append(lines, "  "+m.muted("Choose how this value should be stored in AWS SSM Parameter Store:"), "")
	for i, it := range typeItems {
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
	typeItems := parameterTypeItems()
	lines := make([]string, 0, 2+len(typeItems))
	lines = append(lines, m.muted("Choose how this value should be stored in AWS SSM Parameter Store:"), "")
	for i, it := range typeItems {
		row := fmt.Sprintf("%s — %s", it.label, it.description)
		lines = append(lines, m.singleSelectLine(row, i == m.typeCursor, i == m.typeCursor))
	}
	return lines
}

func (m model) tierSelectLines() []string {
	tierItems := parameterTierItems()
	lines := make([]string, 0, 2+len(tierItems))
	lines = append(lines, m.muted("Choose the AWS SSM storage tier for this parameter:"), "")
	for i, it := range tierItems {
		row := fmt.Sprintf("%s — %s", it.label, it.description)
		lines = append(lines, m.singleSelectLine(row, i == m.tierCursor, i == m.tierCursor))
	}
	return lines
}

func (m model) dataTypeSelectLines() []string {
	dataTypeItems := parameterDataTypeItems()
	lines := make([]string, 0, 2+len(dataTypeItems))
	lines = append(lines, m.muted("Choose AWS SSM value validation data type:"), "")
	for i, it := range dataTypeItems {
		row := fmt.Sprintf("%s — %s", it.label, it.description)
		lines = append(lines, m.singleSelectLine(row, i == m.dataTypeCursor, i == m.dataTypeCursor))
	}
	return lines
}

func (m model) overwriteSelectLines() []string {
	overwriteItems := overwriteItems()
	lines := make([]string, 0, 2+len(overwriteItems))
	lines = append(lines, m.muted("Choose whether AWS SSM may overwrite an existing parameter:"), "")
	for i, it := range overwriteItems {
		row := fmt.Sprintf("%s — %s", it.label, it.description)
		lines = append(lines, m.singleSelectLine(row, i == m.overwriteCursor, i == m.overwriteCursor))
	}
	return lines
}

// renderHelpScreen renders the full shortcut reference.
func (m model) renderHelpScreen() string {
	shortcutLines := strings.Split(m.shortcutsText(), "\n")
	lines := make([]string, 0, len(shortcutLines))
	for _, line := range shortcutLines {
		if line == "" {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, "  "+line)
	}
	return m.renderBox("Shortcuts", lines, m.height)
}

func (m model) renderShortcutsPopup() string {
	lines := strings.Split(m.shortcutsText(), "\n")
	return m.renderPopupBoxWithActions("Shortcuts", lines, "Esc close")
}

// renderLoading renders a centered loading overlay while the initial background scan is running.
func (m model) renderLoading() string {
	bodyLines := make([]string, max(1, m.height))
	body := strings.Join(bodyLines, "\n")
	return m.overlayPopupOnBody(body, m.renderLoadingPopup())
}

func (m model) renderLoadingPopup() string {
	title := strings.TrimSpace(m.loadingTitle)
	if title == "" {
		title = "Loading parameters"
	}
	spinner := loadingSpinnerFrames[m.loadingSpinnerFrame%len(loadingSpinnerFrames)]
	return m.renderPopupBox("Loading", []string{fmt.Sprintf("%s %s", title, spinner)})
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

// textAreaFooterText includes region-switching shortcut help only when multiple concrete regions are available.
func (m model) textAreaFooterText() string {
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
	return "ctrl+/ help • esc close"
}

func (m model) popupFooterText(kind popupKind) string {
	switch kind {
	case popupNone:
		return ""
	case popupShortcuts:
		return "esc close"
	case popupSort:
		return m.sortPopupScreenFooter()
	case popupColumns:
		return "ctrl+/ help • space toggle • a all • x none • esc close"
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
	sortItems := m.popupSortItems()
	parts := make([]string, 0, 2+len(sortItems)+1)
	parts = append(parts, "ctrl+/ help", "d direction")
	for _, item := range sortItems {
		parts = append(parts, item.hotkey+" "+strings.ToLower(item.label))
	}
	parts = append(parts, "esc close")
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
	case popupNone, popupShortcuts, popupConfirm:
		return ""
	case popupSort:
		return strings.TrimSpace(`Actions
  d            toggle selected direction
  esc / q / ctrl+g  close`)
	case popupColumns:
		return strings.TrimSpace(`Actions
  space        toggle focused column
  a            show all columns
  x            hide all optional columns
  esc / q / ctrl+g  close`)
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
	case popupNone, popupShortcuts, popupConfirm, popupFileAction, popupFileWriteConfirm, popupUnsavedChanges:
		return ""
	case popupSort, popupColumns, popupRegionSelect, popupTypeSelect, popupTierSelect, popupDataTypeSelect, popupOverwriteSelect, popupValueActions, popupPoliciesActions, popupRandomValue:
		return m.navigationShortcuts(screenColumns)
	default:
		return ""
	}
}

func (m model) actionsShortcuts(forScreen screen) string {
	switch forScreen {
	case screenHelp:
		return ""
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

func globalShortcuts(_ screen) string {
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

func countLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
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
func pageSize(h int) int { return max(5, h-4) }
