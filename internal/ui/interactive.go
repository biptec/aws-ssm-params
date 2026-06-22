package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	crerr "github.com/cockroachdb/errors"

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

func (m model) renderMainScreen() string {
	if !m.selectedExpanded || m.currentStatus().Item.Path == "" {
		return m.renderListBlock()
	}
	return m.renderSelectedParameterBlock(true) + "\n" + m.renderListBlock()
}

// renderTextAreaScreen renders the unified editor for multiline values plus editable metadata/file fields.
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

func indexOf(values []string, value string) int {
	for i, candidate := range values {
		if candidate == value {
			return i
		}
	}
	return 0
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
