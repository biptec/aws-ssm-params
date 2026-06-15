package ui

import (
	"fmt"
	"os"
	"strconv"
	"strings"

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
	Environment string
	Region      string
	Regions     []string
	Profile     string
	NoColor     bool
}

// screen identifies the currently active TUI view.
// The Update and View methods switch on this value to route key handling and rendering to screen-specific helpers.
type screen int

const (
	screenLoading screen = iota
	screenMain
	screenDetails
	screenInput
	screenTextArea
	screenColumns
	screenRandom
	screenRandomPreview
	screenConfirm
	screenRegionSelect
	screenTypeSelect
	screenHelp
)

type editField int

const (
	editFieldValue editField = iota
	editFieldSSMPath
	editFieldRegion
	editFieldType
	editFieldFilePath
)

type editDirection int

const (
	editDirectionNext editDirection = iota
	editDirectionPrevious
)

type columnName string

type actionItem struct{ label, value string }

type parameterTypeItem struct {
	label       string
	value       ssm.ParameterType
	description string
}

// model is the full Bubble Tea application state.
// It stores immutable inputs such as the SSM client and inventory, dynamic data such as loaded statuses/search state,
// and per-screen state such as cursors, text inputs, confirmation prompts, and loading messages.
type model struct {
	client   ssm.Client
	opts     Options
	items    []inventory.Item
	statuses []Status
	loadCh   chan tea.Msg

	width  int
	height int

	screen       screen
	returnScreen screen

	selected      int
	detailsScroll int
	detailText    string

	searchMode     bool
	query          string
	effectiveQuery string
	searchInvalid  bool

	revealValues bool

	message    string
	errMessage string

	loadingTitle string
	loadingLines []string

	input              textinput.Model
	textArea           textarea.Model
	editPathInput      textinput.Model
	editFileInput      textinput.Model
	inputMode          string
	generatedValue     string
	editField          editField
	editDirection      editDirection
	editRegionOptions  []string
	confirmWriteSecure bool
	editRegion         string
	editType           ssm.ParameterType
	regionCursor       int
	typeCursor         int
	typeReturnScreen   screen

	columns      map[columnName]bool
	columnCursor int

	randomCursor int

	confirmPrompt   string
	confirmExpected string
	confirmItems    []inventory.Item
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
	err     error
}

// deleteDoneMsg reports the result of deleting one or more visible/selected parameters.
type deleteDoneMsg struct {
	items []inventory.Item
	err   error
}

const (
	columnIndex       columnName = "index"
	columnStatus      columnName = "status"
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
	columnKind        columnName = "kind"
	columnApp         columnName = "app"
	columnComponent   columnName = "component"
	columnSecretName  columnName = "secret"
	columnSource      columnName = "source"
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
	hotkeyStyle      = lipgloss.NewStyle().Bold(true).Foreground(hotkeyFg)
)

// RunInteractive creates and runs the Bubble Tea program in the terminal alternate screen.
// The function returns only after the user quits the TUI or Bubble Tea reports an error.
func RunInteractive(client ssm.Client, items []inventory.Item, opts Options) error {
	m := newModel(client, items, opts)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

// newModel initializes the TUI model with default inputs, textarea settings, visible columns, and loading state.
// Statuses are not loaded here; Init starts that asynchronous work so the UI can show progress immediately.
func newModel(client ssm.Client, items []inventory.Item, opts Options) model {
	input := textinput.New()
	input.Prompt = ""
	input.CharLimit = 0
	input.Width = 80

	editPathInput := textinput.New()
	editPathInput.Prompt = ""
	editPathInput.CharLimit = 0
	editPathInput.Width = 80

	editFileInput := textinput.New()
	editFileInput.Prompt = ""
	editFileInput.CharLimit = 0
	editFileInput.Width = 80

	area := textarea.New()
	area.Prompt = ""
	area.CharLimit = 0
	area.ShowLineNumbers = false

	return model{
		client:        client,
		items:         items,
		opts:          opts,
		loadCh:        make(chan tea.Msg),
		screen:        screenLoading,
		input:         input,
		textArea:      area,
		editPathInput: editPathInput,
		editFileInput: editFileInput,
		columns: map[columnName]bool{
			columnIndex:       true,
			columnStatus:      false,
			columnRegion:      false,
			columnDate:        false,
			columnType:        false,
			columnTier:        false,
			columnVersion:     false,
			columnLength:      false,
			columnHash:        false,
			columnValue:       false,
			columnUser:        false,
			columnDescription: false,
			columnKind:        false,
			columnApp:         false,
			columnComponent:   false,
			columnSecretName:  false,
			columnSource:      false,
		},
	}
}

// Init starts the initial background status load and registers a command that waits for loader messages.
func (m model) Init() tea.Cmd {
	return tea.Batch(startLoadCmd(m.client, m.items, m.opts.Regions, m.loadCh), waitForLoad(m.loadCh))
}

// startLoadCmd launches the initial SSM status scan in a goroutine.
// Progress and final results are sent through loadCh so the Bubble Tea event loop can render loading updates.
func startLoadCmd(client ssm.Client, items []inventory.Item, regions []string, ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		go func() {
			statuses := LoadStatusesBatchForRegions(client, items, true, regions, func(done, total int, region string, chunk []inventory.Item) {
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
		m.editFileInput.Width = max(20, msg.Width-18)
		m.textArea.SetWidth(max(20, msg.Width-14))
		m.textArea.SetHeight(max(8, msg.Height-10))
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
		m.message = msg.message
		m.errMessage = ""
		m.screen = m.returnScreen
		return m, nil

	case deleteDoneMsg:
		if msg.err != nil {
			m.errMessage = msg.err.Error()
			m.screen = m.returnScreen
			return m, nil
		}
		for _, item := range msg.items {
			m.markMissingItem(item)
		}
		m.message = fmt.Sprintf("Deleted %d parameter(s)", len(msg.items))
		m.errMessage = ""
		m.screen = m.returnScreen
		m.ensureSelection()
		return m, nil

	case tea.KeyMsg:
		switch m.screen {
		case screenMain:
			return m.updateMain(msg)
		case screenDetails:
			return m.updateDetails(msg)
		case screenInput:
			return m.updateInput(msg)
		case screenTextArea:
			return m.updateTextArea(msg)
		case screenColumns:
			return m.updateColumns(msg)
		case screenRandom:
			return m.updateRandom(msg)
		case screenRandomPreview:
			return m.updateRandomPreview(msg)
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

	if m.screen == screenInput {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	if m.screen == screenTextArea {
		var cmd tea.Cmd
		m.textArea, cmd = m.textArea.Update(msg)
		return m, cmd
	}
	return m, nil
}

// View renders the active screen plus a fixed footer with the hotkeys valid for that screen.
// Keeping footer text here ensures rendered shortcuts stay aligned with the screen selected by Update.
func (m model) View() string {
	switch m.screen {
	case screenLoading:
		return m.renderFullscreen(m.renderLoading(), m.renderFooter("q quit"))
	case screenMain:
		footer := mainFooterText()
		if m.searchMode {
			footer = searchFooterText()
		}
		return m.renderFullscreen(m.renderMainScreen(), m.renderFooter(footer))
	case screenDetails:
		return m.renderFullscreen(m.renderDetailsScreen(), m.renderFooter(detailsFooterText()))
	case screenInput:
		return m.renderFullscreen(m.renderInputScreen(), m.renderFooter(m.inputFooterText()))
	case screenTextArea:
		return m.renderFullscreen(m.renderTextAreaScreen(), m.renderFooter(m.textAreaFooterText()))
	case screenColumns:
		return m.renderFullscreen(m.renderColumnsScreen(), m.renderFooter("↑/ctrl+p up • ↓/ctrl+n down • tab next option • shift+tab previous option • space toggle • a show all • x hide all • q back"))
	case screenRandom:
		return m.renderFullscreen(m.renderRandomScreen(), m.renderFooter("↑/ctrl+p up • ↓/ctrl+n down • tab next option • shift+tab previous option • enter choose • q back"))
	case screenRandomPreview:
		return m.renderFullscreen(m.renderRandomPreviewScreen(), m.renderFooter("ctrl+s/enter save • r regenerate • q back"))
	case screenConfirm:
		return m.renderFullscreen(m.renderConfirmScreen(), m.renderFooter("enter confirm • esc/ctrl+g back"))
	case screenRegionSelect:
		return m.renderFullscreen(m.renderRegionSelectScreen(), m.renderFooter("↑/ctrl+p up • ↓/ctrl+n down • tab next option • shift+tab previous option • enter choose • q back"))
	case screenTypeSelect:
		return m.renderFullscreen(m.renderTypeSelectScreen(), m.renderFooter("↑/ctrl+p up • ↓/ctrl+n down • tab next option • shift+tab previous option • enter choose • q back"))
	case screenHelp:
		return m.renderFullscreen(m.renderHelpScreen(), m.renderFooter("q back"))
	default:
		return ""
	}
}

// updateMain handles navigation and actions on the main parameter table.
// It also owns search mode, where printable keys update the active filter instead of triggering table shortcuts.
func (m model) updateMain(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.message = ""
	if m.searchMode {
		switch msg.String() {
		case "ctrl+g":
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

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "down", "ctrl+n", "j":
		m.move(1)
	case "up", "ctrl+p", "k":
		m.move(-1)
	case "ctrl+v", "pagedown":
		m.move(pageSize(m.listBodyHeight()))
	case "alt+v", "pageup":
		m.move(-pageSize(m.listBodyHeight()))
	case "home", "alt+<":
		m.selected = 0
	case "end", "alt+>":
		vis := m.visible()
		if len(vis) > 0 {
			m.selected = len(vis) - 1
		}
	case "enter", "ctrl+j":
		m.detailsScroll = 0
		m.screen = screenDetails
	case "/":
		m.searchMode = true
		m.query = m.effectiveQuery
		m.searchInvalid = false
	case "v":
		m.revealValues = !m.revealValues
	case "c":
		m.columnCursor = 0
		m.returnScreen = screenMain
		m.screen = screenColumns
	case "e":
		return m.startMultiline(screenMain)
	case "r":
		m.returnScreen = screenMain
		m.editRegion = m.initialEditRegion()
		m.editType = m.initialEditType()
		m.randomCursor = 0
		m.screen = screenRandom
	case "x":
		m.startConfirm("Delete selected parameter?\n\nType DELETE to confirm:", "DELETE", []inventory.Item{m.currentItem()}, screenMain)
	case "D":
		items := m.visibleItems()
		if len(items) > 0 {
			m.startConfirm(fmt.Sprintf("Delete %d visible parameter(s)?\n\nType DELETE ALL to confirm:", len(items)), "DELETE ALL", items, screenMain)
		}
	case "?":
		m.screen = screenHelp
	}
	m.ensureSelection()
	return m, nil
}

// updateDetails handles scrolling and actions on the expanded selected-parameter view.
func (m model) updateDetails(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.message = ""
	switch msg.String() {
	case "q", "esc", "ctrl+g":
		m.screen = screenMain
		return m, nil
	case "down", "ctrl+n", "j":
		m.detailsScroll++
	case "up", "ctrl+p", "k":
		m.detailsScroll = max(0, m.detailsScroll-1)
	case "ctrl+v", "pagedown":
		m.detailsScroll += pageSize(m.height - 8)
	case "alt+v", "pageup":
		m.detailsScroll = max(0, m.detailsScroll-pageSize(m.height-8))
	case "v":
		m.revealValues = !m.revealValues
	case "e":
		return m.startMultiline(screenDetails)
	case "r":
		m.returnScreen = screenDetails
		m.editRegion = m.initialEditRegion()
		m.editType = m.initialEditType()
		m.randomCursor = 0
		m.screen = screenRandom
	case "x":
		m.startConfirm("Delete selected parameter?\n\nType DELETE to confirm:", "DELETE", []inventory.Item{m.currentItem()}, screenDetails)
	case "?":
		m.screen = screenHelp
	}
	return m, nil
}

// updateInput handles single-line input screens used for custom random lengths and confirmation prompts.
func (m model) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+g":
		m.input.Blur()
		m.screen = m.returnScreen
		return m, nil
	case "ctrl+k":
		m.input.SetValue("")
		return m, nil
	case "enter", "ctrl+s", "ctrl+o":
		if m.inputMode == "random-custom" {
			return m.generateRandom("base64-custom")
		}

		value := m.input.Value()
		return m.saveValue(value)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// updateTextArea handles the unified edit form: editable SSM path, region/type selectors, file path, multiline value, and save/file operations.
func (m model) updateTextArea(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	resetWriteConfirmation := func() {
		m.confirmWriteSecure = false
	}

	switch msg.String() {
	case "esc", "ctrl+g":
		m.blurEditFields()
		m.confirmWriteSecure = false
		m.screen = m.returnScreen
		return m, nil
	case "tab":
		resetWriteConfirmation()
		return m.focusNextEditField()
	case "shift+tab":
		resetWriteConfirmation()
		return m.focusPreviousEditField()
	case "enter", "ctrl+j":
		resetWriteConfirmation()
		switch m.editField {
		case editFieldSSMPath, editFieldFilePath:
			return m.focusNextEditField()
		case editFieldRegion:
			return m.openRegionSelect()
		case editFieldType:
			return m.startTypeSelect(screenTextArea)
		}
	case "ctrl+k":
		resetWriteConfirmation()
		switch m.editField {
		case editFieldSSMPath:
			m.editPathInput.SetValue("")
		case editFieldFilePath:
			m.editFileInput.SetValue("")
		case editFieldValue:
			m.textArea.SetValue("")
		}
		m.message = ""
		m.errMessage = ""
		return m, nil
	case "ctrl+o":
		resetWriteConfirmation()
		return m.loadValueFromFile()
	case "ctrl+w":
		return m.writeValueToFile()
	case "ctrl+v", "pagedown":
		if m.editField != editFieldValue {
			break
		}
		resetWriteConfirmation()
		for i := 0; i < pageSize(m.textArea.Height()); i++ {
			m.textArea.CursorDown()
		}
		return m, nil
	case "alt+v", "pageup":
		if m.editField != editFieldValue {
			break
		}
		resetWriteConfirmation()
		for i := 0; i < pageSize(m.textArea.Height()); i++ {
			m.textArea.CursorUp()
		}
		return m, nil
	case "ctrl+s":
		resetWriteConfirmation()
		return m.saveValue(m.textArea.Value())
	}

	var cmd tea.Cmd
	switch m.editField {
	case editFieldSSMPath:
		m.editPathInput, cmd = m.editPathInput.Update(msg)
	case editFieldFilePath:
		m.editFileInput, cmd = m.editFileInput.Update(msg)
	case editFieldValue:
		m.textArea, cmd = m.textArea.Update(msg)
	}
	m.confirmWriteSecure = false
	m.message = ""
	m.errMessage = ""
	return m, cmd
}

// updateColumns handles the column visibility picker and returns to the screen that opened it.
func (m model) updateColumns(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	cols := columnItems()
	switch msg.String() {
	case "q", "esc", "ctrl+g":
		m.screen = m.returnScreen
	case "down", "ctrl+n", "j", "tab":
		m.columnCursor = min(len(cols)-1, m.columnCursor+1)
	case "up", "ctrl+p", "k", "shift+tab":
		m.columnCursor = max(0, m.columnCursor-1)
	case " ", "enter":
		key := cols[m.columnCursor]
		m.columns[key] = !m.columns[key]
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

// updateRandom handles the random-value generator menu and dispatches generation for the selected format.
func (m model) updateRandom(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := randomItems()
	switch msg.String() {
	case "q", "esc", "ctrl+g":
		m.screen = m.returnScreen
	case "down", "ctrl+n", "j", "tab":
		m.randomCursor = min(len(items)-1, m.randomCursor+1)
	case "up", "ctrl+p", "k", "shift+tab":
		m.randomCursor = max(0, m.randomCursor-1)
	case "enter":
		choice := items[m.randomCursor]
		if choice.value == "base64-custom" {
			return m.startEdit("random-custom", m.returnScreen)
		}
		return m.generateRandom(choice.value)
	}
	return m, nil
}

// updateRandomPreview lets users save the generated value, regenerate it, or return to the generator menu.
func (m model) updateRandomPreview(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc", "ctrl+g":
		m.screen = screenRandom
	case "r":
		return m.generateRandom(m.inputMode)
	case "enter", "ctrl+s":
		return m.saveValue(m.generatedValue)
	}
	return m, nil
}

// updateConfirm verifies a typed confirmation phrase before running destructive delete operations.
func (m model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+g":
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
		return m, deleteCmd(m.client, items)
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
	switch msg.String() {
	case "q", "esc", "ctrl+g":
		m.screen = screenTextArea
		m = m.focusEditField(editFieldRegion)
	case "down", "ctrl+n", "j", "tab":
		m.regionCursor = min(len(regions)-1, m.regionCursor+1)
	case "up", "ctrl+p", "k", "shift+tab":
		m.regionCursor = max(0, m.regionCursor-1)
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
	switch msg.String() {
	case "q", "esc", "ctrl+g":
		m.screen = m.typeReturnScreen
		if m.typeReturnScreen == screenTextArea {
			m = m.focusEditField(editFieldType)
		}
	case "down", "ctrl+n", "j", "tab":
		m.typeCursor = min(len(items)-1, m.typeCursor+1)
	case "up", "ctrl+p", "k", "shift+tab":
		m.typeCursor = max(0, m.typeCursor-1)
	case "enter", "ctrl+j":
		m.editType = items[m.typeCursor].value
		m.screen = m.typeReturnScreen
		if m.typeReturnScreen == screenTextArea {
			m = m.focusEditField(editFieldType)
		}
	}
	return m, nil
}

// updateHelp closes the help screen and returns to the main table.
func (m model) updateHelp(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc", "ctrl+g", "?":
		m.screen = screenMain
	}
	return m, nil
}

// updateLoading handles shortcuts that must remain available while long SSM scans are running.
// The footer advertises q quit on the loading screen, so q and ctrl+c both terminate the Bubble Tea program.
func (m model) updateLoading(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

// startEdit prepares the single-line input screen used by the random custom-length workflow.
// ret records where to return after canceling.
func (m model) startEdit(mode string, ret screen) (tea.Model, tea.Cmd) {
	m.returnScreen = ret
	m.inputMode = mode
	m.editRegion = m.initialEditRegion()
	m.editType = m.initialEditType()
	m.input.Placeholder = ""
	if mode == "random-custom" {
		m.input.SetValue("32")
		m.input.Placeholder = "Bytes length"
	}
	m.input.Focus()
	m.screen = screenInput
	return m, nil
}

// startMultiline opens the selected parameter value in the multiline editor.
func (m model) startMultiline(ret screen) (tea.Model, tea.Cmd) {
	m.returnScreen = ret
	m.editRegion = m.initialEditRegion()
	m.editType = m.initialEditType()
	m.textArea.SetValue(m.currentStatus().Value)
	m.editPathInput.SetValue(m.currentItem().Path)
	m.editPathInput.Placeholder = ""
	m.editPathInput.Blur()
	m.editFileInput.SetValue("")
	m.editFileInput.Placeholder = ""
	m.editFileInput.Blur()
	m.editField = editFieldValue
	m.editDirection = editDirectionNext
	m.textArea.Focus()
	m.confirmWriteSecure = false
	m.message = ""
	m.errMessage = ""
	m.screen = screenTextArea
	return m, nil
}

// focusEditField moves the edit-screen focus to one field and focuses/blurs the underlying input widgets.
func (m model) focusEditField(field editField) model {
	m.blurEditFields()
	m.editField = field
	switch field {
	case editFieldSSMPath:
		m.editPathInput.Focus()
	case editFieldFilePath:
		m.editFileInput.Focus()
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
	m.editPathInput.Blur()
	m.editFileInput.Blur()
}

// focusNextEditField advances the edit-screen focus in the visual field order.
func (m model) focusNextEditField() (tea.Model, tea.Cmd) {
	return m.moveToEditField(nextEditField(m.editField), editDirectionNext)
}

// focusPreviousEditField moves the edit-screen focus backwards in the visual field order.
func (m model) focusPreviousEditField() (tea.Model, tea.Cmd) {
	return m.moveToEditField(previousEditField(m.editField), editDirectionPrevious)
}

// moveToEditField moves focus through all edit fields without opening selector screens automatically.
func (m model) moveToEditField(field editField, direction editDirection) (tea.Model, tea.Cmd) {
	m.editDirection = direction
	return m.focusEditField(field), nil
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
		return editFieldFilePath
	default:
		return editFieldValue
	}
}

func previousEditField(field editField) editField {
	switch field {
	case editFieldValue:
		return editFieldFilePath
	case editFieldFilePath:
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
	m.screen = screenRegionSelect
	return m, nil
}

// ensureRegionSelectOptions lazily asks AWS for the full enabled-region list so saving is not limited to startup regions.
func (m model) ensureRegionSelectOptions() model {
	if len(m.editRegionOptions) > 0 || m.client == nil {
		return m
	}
	regions, err := m.client.ListRegions()
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
	if len(m.editRegionOptions) > 0 {
		return append([]string(nil), m.editRegionOptions...)
	}
	return m.regionOptions()
}

// loadValueFromFile reads the path from the edit screen and replaces the multiline value with that file content.
func (m model) loadValueFromFile() (tea.Model, tea.Cmd) {
	path := strings.TrimSpace(m.editFileInput.Value())
	if path == "" {
		m.errMessage = "file path is required"
		m.message = ""
		return m, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		m.errMessage = err.Error()
		m.message = ""
		return m, nil
	}
	m.textArea.SetValue(string(data))
	m = m.focusEditField(editFieldValue)
	m.errMessage = ""
	m.message = "Loaded value from " + path
	return m, nil
}

// writeValueToFile writes the current multiline value to the path from the edit screen.
// SecureString values require pressing ctrl+w twice to reduce the risk of accidentally writing a secret to disk.
func (m model) writeValueToFile() (tea.Model, tea.Cmd) {
	path := strings.TrimSpace(m.editFileInput.Value())
	if path == "" {
		m.errMessage = "file path is required"
		m.message = ""
		m.confirmWriteSecure = false
		return m, nil
	}
	if m.normalizedEditType() == ssm.ParameterTypeSecureString && !m.confirmWriteSecure {
		m.errMessage = ""
		m.message = "This is a SecureString value. Press ctrl+w again to write it to a local file."
		m.confirmWriteSecure = true
		return m, nil
	}
	if err := os.WriteFile(path, []byte(m.textArea.Value()), 0600); err != nil {
		m.errMessage = err.Error()
		m.message = ""
		m.confirmWriteSecure = false
		return m, nil
	}
	m.errMessage = ""
	m.message = "Wrote value to " + path
	m.confirmWriteSecure = false
	return m, nil
}

// startTypeSelect opens the type picker and remembers which editor/preview screen should be restored afterwards.
func (m model) startTypeSelect(ret screen) (tea.Model, tea.Cmd) {
	m.typeReturnScreen = ret
	m.typeCursor = indexOfParameterType(parameterTypeItems(), m.normalizedEditType())
	m.screen = screenTypeSelect
	return m, nil
}

// startConfirm initializes a confirmation screen for one or more items.
func (m *model) startConfirm(prompt, expected string, items []inventory.Item, ret screen) {
	m.confirmPrompt = prompt
	m.confirmExpected = expected
	m.confirmItems = items
	m.returnScreen = ret
	m.input.SetValue("")
	m.input.Placeholder = expected
	m.input.Focus()
	m.errMessage = ""
	m.screen = screenConfirm
}

// generateRandom creates a new random value according to the selected generator type and opens the preview screen.
func (m model) generateRandom(kind string) (tea.Model, tea.Cmd) {
	var value string
	var err error
	switch kind {
	case "base64-32":
		value, err = randomx.Base64(32)
	case "hex-32":
		value, err = randomx.Hex(32)
	case "uuid":
		value, err = randomx.UUID()
	case "base64-custom":
		n, parseErr := strconv.Atoi(strings.TrimSpace(m.input.Value()))
		if parseErr != nil || n <= 0 {
			err = fmt.Errorf("invalid byte length")
		} else {
			value, err = randomx.Base64(n)
		}
	}
	if err != nil {
		m.errMessage = err.Error()
		return m, nil
	}
	m.generatedValue = value
	m.inputMode = kind
	m.screen = screenRandomPreview
	return m, nil
}

// saveValue captures the current item/region and switches to the loading screen while the save command runs.
func (m model) saveValue(value string) (tea.Model, tea.Cmd) {
	item := m.currentItem()
	oldPath := item.Path
	if m.screen == screenTextArea {
		newPath := strings.TrimSpace(m.editPathInput.Value())
		if newPath == "" {
			m.errMessage = "SSM path is required"
			m.message = ""
			return m, nil
		}
		item.Path = newPath
	}
	if m.editRegion != "" {
		item.Region = m.editRegion
	}
	m.loadingTitle = "Saving parameter..."
	m.loadingLines = []string{item.Path}
	m.screen = screenLoading
	return m, saveValueCmd(m.client, item, oldPath, value, m.normalizedEditType())
}

// saveValueCmd writes one SSM parameter to Parameter Store and reloads its fresh status for the UI.
// Wildcard items must be converted to a concrete region before saving, otherwise the command returns an inline error.
func saveValueCmd(client ssm.Client, item inventory.Item, oldPath, value string, parameterType ssm.ParameterType) tea.Cmd {
	return func() tea.Msg {
		if item.Region == "*" {
			return statusUpdatedMsg{path: item.Path, oldPath: oldPath, err: fmt.Errorf("cannot save %s without a concrete AWS region", item.Path)}
		}
		regionalClient := client.ForRegion(item.Region)
		if err := regionalClient.PutParameter(item.Path, value, parameterType); err != nil {
			return statusUpdatedMsg{path: item.Path, oldPath: oldPath, err: err}
		}
		st := LoadStatuses(regionalClient, []inventory.Item{item}, true)[0]
		return statusUpdatedMsg{path: item.Path, oldPath: oldPath, status: st, message: "Updated " + item.Path}
	}
}

// deleteCmd groups selected items by concrete region and deletes them from SSM.
// Wildcard missing rows are skipped because they do not represent a real parameter in one AWS region.
func deleteCmd(client ssm.Client, items []inventory.Item) tea.Cmd {
	return func() tea.Msg {
		byRegion := map[string][]string{}
		for _, item := range items {
			if item.Region == "*" {
				continue
			}
			byRegion[item.Region] = append(byRegion[item.Region], item.Path)
		}
		for region, paths := range byRegion {
			if err := client.ForRegion(region).DeleteMany(paths); err != nil {
				return deleteDoneMsg{items: items, err: err}
			}
		}
		return deleteDoneMsg{items: items}
	}
}

// renderMainScreen composes the selected-parameter summary and the scrollable table of visible statuses.
func (m model) renderMainScreen() string {
	blocks := []string{
		m.renderSelectedParameterBlock(false),
		m.renderListBlock(),
	}
	if m.errMessage != "" {
		blocks = append(blocks, "", m.applyErr(m.errMessage))
	}
	return strings.Join(blocks, "\n")
}

// renderDetailsScreen renders the expanded parameter metadata/value view and any contextual message or error.
func (m model) renderDetailsScreen() string {
	body := m.renderSelectedParameterBlock(true)
	if m.detailText != "" {
		body = m.renderBox("Help", strings.Split(m.detailText, "\n"), m.height-2)
	}
	if m.message != "" {
		body += "\n\n" + m.muted(m.message)
	}
	if m.errMessage != "" {
		body += "\n\n" + m.applyErr(m.errMessage)
	}
	return body
}

// renderInputScreen renders the single-line form used for direct values, file paths, and custom random byte counts.
func (m model) renderInputScreen() string {
	title := "Set Parameter Value"
	lines := []string{
		"  " + m.formField("Path", m.currentItem().Path),
		"",
		"  > " + m.input.View(),
	}
	if m.errMessage != "" {
		lines = append(lines, "", "  "+m.applyErr(m.errMessage))
	}
	return m.renderBox(title, lines, m.height-2)
}

// renderTextAreaScreen renders the unified editor for multiline values plus editable metadata/file fields.
func (m model) renderTextAreaScreen() string {
	title := "Edit Value"
	labelWidth := 9
	lines := []string{
		m.editFieldLine(editFieldSSMPath, "SSM path", m.editPathInput.View(), labelWidth),
		m.editFieldLine(editFieldRegion, "Region", m.editOptionValue(editFieldRegion, valueOrDash(m.editRegion)), labelWidth),
		m.editFieldLine(editFieldType, "Type", m.editOptionValue(editFieldType, m.normalizedEditType().String()), labelWidth),
		m.editFieldLine(editFieldFilePath, "File path", m.editFileInput.View(), labelWidth),
		"",
		m.label("Value:"),
	}

	textAreaLines := strings.Split(m.textArea.View(), "\n")
	contentLines := promptLineCount(m.textArea.Value())
	for i, line := range textAreaLines {
		if i >= contentLines {
			break
		}
		lines = append(lines, "> "+line)
	}

	if m.message != "" {
		lines = append(lines, "", "  "+m.muted(m.message))
	}
	if m.errMessage != "" {
		lines = append(lines, "", "  "+m.applyErr(m.errMessage))
	}
	return m.renderBox(title, lines, m.height-2)
}

func (m model) editFieldLine(field editField, name, renderedValue string, labelWidth int) string {
	return m.fieldLine(name, renderedValue, labelWidth)
}

func (m model) editOptionValue(field editField, value string) string {
	if m.editField == field {
		value += " ⌵"
	}
	return m.value(value)
}

// renderColumnsScreen renders the table-column chooser with checked/unchecked state.
func (m model) renderColumnsScreen() string {
	cols := columnItems()
	lines := []string{
		"  " + m.muted("PATH is always visible."),
		"",
	}
	for i, c := range cols {
		state := "[ ]"
		if m.columns[c] {
			state = "[x]"
		}
		row := fmt.Sprintf("%s %s", state, columnLabel(c))
		if i == m.columnCursor {
			row = m.selectedMarker() + m.selectedRow(row)
		} else {
			row = "  " + row
		}
		lines = append(lines, "  "+row)
	}
	return m.renderBox("Columns", lines, m.height-2)
}

// renderRandomScreen renders the random-value generator menu.
func (m model) renderRandomScreen() string {
	items := randomItems()
	lines := []string{}
	for i, it := range items {
		row := it.label
		if i == m.randomCursor {
			row = m.selectedMarker() + m.selectedRow(row)
		} else {
			row = "  " + row
		}
		lines = append(lines, "  "+row)
	}
	return m.renderBox("Generate Random Value", lines, m.height-2)
}

// renderRandomPreviewScreen displays the generated value before the user saves it to SSM.
func (m model) renderRandomPreviewScreen() string {
	lines := []string{
		"  " + m.formField("Path", m.currentItem().Path),
		"  " + m.formField("Region", valueOrDash(m.editRegion)),
		"  " + m.formField("Type", m.normalizedEditType().String()),
		"",
		"  " + m.generatedValue,
	}
	return m.renderBox("Generated Value", lines, m.height-2)
}

// renderConfirmScreen renders the destructive-action confirmation prompt and input field.
func (m model) renderConfirmScreen() string {
	lines := []string{}
	for _, line := range strings.Split(m.confirmPrompt, "\n") {
		lines = append(lines, "  "+line)
	}
	lines = append(lines, "", "  > "+m.input.View())
	if m.errMessage != "" {
		lines = append(lines, "", "  "+m.applyErr(m.errMessage))
	}
	return m.renderBox("Confirm", lines, m.height-2)
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
	return m.renderBox("Region", lines, m.height-2)
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
	return m.renderBox("Parameter Type", lines, m.height-2)
}

// renderHelpScreen renders the full shortcut reference.
func (m model) renderHelpScreen() string {
	lines := []string{}
	for _, line := range strings.Split(helpText(), "\n") {
		if line == "" {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, "  "+line)
	}
	return m.renderBox("More", lines, m.height-2)
}

// renderLoading renders current background-operation progress, including the region and paths currently being scanned.
func (m model) renderLoading() string {
	lines := []string{"  " + m.loadingTitle, ""}
	for _, line := range m.loadingLines {
		lines = append(lines, "  "+line)
	}
	return m.renderBox("Loading", lines, m.height-2)
}

// renderSelectedParameterBlock renders either the compact selected-parameter summary or the full details block.
// SecureString values stay hidden until revealValues is enabled; String and StringList values are safe to show immediately.
func (m model) renderSelectedParameterBlock(full bool) string {
	st := m.currentStatus()
	if st.Item.Path == "" {
		return m.renderBox("Selected Parameter", []string{"No parameters found."}, 8)
	}

	value := m.displayValue(st, full)

	fields := [][2]string{{"Path", st.Item.Path}, {"Region", m.statusRegion(st)}, {"Status", m.statusText(st)}, {"Type", valueOrDash(st.Type)}, {"Date", valueOrDash(st.Modified)}, {"Value", value}}
	if full {
		fields = [][2]string{{"Path", st.Item.Path}, {"Region", m.statusRegion(st)}, {"Status", m.statusText(st)}, {"Type", valueOrDash(st.Type)}, {"Tier", valueOrDash(st.Tier)}, {"Version", intOrDash(st.Version)}, {"Len", intOrDash(int64(st.Length))}, {"SHA256", valueOrDash(st.SHA256Prefix)}, {"Description", valueOrDash(st.Description)}, {"User", valueOrDash(st.User)}, {"Date", valueOrDash(st.Modified)}, {"Value", value}}
		if st.Error != "" {
			fields = append(fields, [2]string{"Error", st.Error})
		}
	}

	labelWidth := 6
	if full {
		labelWidth = 11
	}
	lines := m.renderFieldPairs(fields, labelWidth)
	maxHeight := len(lines) + 2
	if full {
		// Keep one fixed separator line before the footer so the hotkey line
		// remains on the same terminal row as on the main screen.
		maxHeight = max(8, m.height-2)
		lines = sliceForScroll(lines, m.detailsScroll, maxHeight-2)
	}
	return m.renderBox("Selected Parameter", lines, maxHeight)
}

// displayValue returns the user-facing value for selected blocks and VALUE table cells.
// SecureString values are treated as sensitive and hidden until the user presses v; String/StringList are shown by default.
func (m model) displayValue(st Status, full bool) string {
	if m.shouldHideValue(st) {
		return "(hidden)"
	}
	value := valueOrDash(st.Value)
	if full {
		return value
	}
	return truncateInline(value, max(20, m.boxInnerWidth()-22))
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

	if m.message != "" {
		lines = append(lines, "  "+m.muted(m.message))
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
	keys := []columnName{}
	for _, key := range columnItems() {
		if m.columns[key] {
			keys = append(keys, key)
		}
	}
	keys = append(keys, columnPath)

	cols := make([]tableColumn, 0, len(keys))
	for _, key := range keys {
		header := columnHeader(key)
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
	case columnValue, columnUser, columnDescription, columnSource:
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
		if col.key == columnStatus && !selected {
			parts = append(parts, padVisible(m.statusText(st), col.width))
			continue
		}
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
	case columnStatus:
		return statusDisplayLabel(st)
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
	case columnKind:
		return valueOrDash(st.Item.Kind)
	case columnApp:
		return valueOrDash(st.Item.App)
	case columnComponent:
		return valueOrDash(st.Item.Component)
	case columnSecretName:
		return valueOrDash(st.Item.SecretName)
	case columnSource:
		return valueOrDash(st.Item.Source)
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
	label := m.label(pad(name+":", labelWidth+1))
	return label + " " + renderedValue
}

func (m model) formField(name, value string) string {
	return m.label(name+":") + " " + m.value(value)
}

// renderBox draws a bordered box, truncating or padding content so screens keep stable heights.
func (m model) renderBox(title string, lines []string, preferredHeight int) string {
	innerWidth := m.boxInnerWidth()
	top := m.boxTop(title, innerWidth)
	bottom := m.boxBottom(innerWidth)

	if preferredHeight < len(lines)+2 {
		preferredHeight = len(lines) + 2
	}
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
	visible := lipgloss.Width(content)
	if visible > innerWidth {
		content = truncateStyled(content, innerWidth)
		visible = lipgloss.Width(content)
	}
	padWidth := innerWidth - visible
	if padWidth < 0 {
		padWidth = 0
	}
	return m.frame("│") + content + strings.Repeat(" ", padWidth) + m.frame("│")
}

func (m model) inputFooterText() string {
	return "ctrl+s save • ctrl+k clear • esc/ctrl+g back"
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
	bodyLines := countLines(body)
	footerLines := countLines(footer)
	padLines := m.height - bodyLines - footerLines
	if padLines < 0 {
		padLines = 0
	}
	separator := strings.Repeat("\n", padLines)
	if separator == "" {
		separator = "\n"
	}
	return body + separator + footer
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
	m.selected = max(0, min(len(vis)-1, m.selected+delta))
}

func (m model) boxInnerWidth() int {
	return max(40, m.width-2)
}

func (m model) listBlockHeight() int {
	// Main page layout:
	// selected parameter block + list block + footer.
	selectedBlockHeight := 8
	footerHeight := 2
	return max(8, m.height-selectedBlockHeight-footerHeight)
}

func (m model) listBodyHeight() int {
	// Top/bottom border + header + header divider + optional message/filter/search lines.
	reserved := 5
	if m.message != "" {
		reserved++
	}
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
	}
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
		columnIndex,
		columnStatus,
		columnRegion,
		columnDate,
		columnType,
		columnTier,
		columnVersion,
		columnLength,
		columnHash,
		columnValue,
		columnUser,
		columnDescription,
		columnKind,
		columnApp,
		columnComponent,
		columnSecretName,
		columnSource,
	}
}

func columnHeader(c columnName) string {
	if c == columnIndex {
		return "#"
	}
	return strings.ToUpper(columnLabel(c))
}

func columnLabel(c columnName) string {
	switch c {
	case columnIndex:
		return "Index"
	case columnStatus:
		return "Status"
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
	case columnKind:
		return "Kind"
	case columnApp:
		return "App"
	case columnComponent:
		return "Component"
	case columnSecretName:
		return "Secret"
	case columnSource:
		return "Source"
	default:
		return string(c)
	}
}

// randomItems returns supported random value generator choices.
func randomItems() []actionItem {
	return []actionItem{{"base64 32 bytes", "base64-32"}, {"hex 32 bytes", "hex-32"}, {"uuid", "uuid"}, {"custom length base64", "base64-custom"}}
}

// itemPaths extracts SSM paths for loading/progress displays.
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
		{label: ssm.ParameterTypeSecureString.String(), value: ssm.ParameterTypeSecureString, description: "encrypted value; best default for secrets"},
		{label: ssm.ParameterTypeString.String(), value: ssm.ParameterTypeString, description: "plain text scalar value"},
		{label: ssm.ParameterTypeStringList.String(), value: ssm.ParameterTypeStringList, description: "comma-separated plain text list"},
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
	common := "ctrl+s save • tab next field • shift+tab previous field"
	suffix := " • esc/ctrl+g back"
	switch m.editField {
	case editFieldValue:
		return common + " • enter newline • ctrl+o load file • ctrl+w write file • ctrl+k clear" + suffix
	case editFieldSSMPath:
		return common + " • enter next field • ctrl+k clear" + suffix
	case editFieldRegion:
		return common + " • enter choose region" + suffix
	case editFieldType:
		return common + " • enter choose type" + suffix
	case editFieldFilePath:
		return common + " • enter next field • ctrl+o load file • ctrl+w write file • ctrl+k clear" + suffix
	default:
		return common + suffix
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
func mainFooterText() string {
	return "↑/ctrl+p up • ↓/ctrl+n down • enter open • / search • v values • c columns • ? help • q quit"
}

func searchFooterText() string {
	return "ctrl+g exit search"
}

func detailsFooterText() string {
	return "↑/ctrl+p scroll up • ↓/ctrl+n scroll down • e edit • r random • x delete • v values • q back"
}

// helpText returns the multi-line shortcut reference shown by the help screen.
func helpText() string {
	return strings.TrimSpace(`Navigation:
  ↑ / ctrl+p      previous
  ↓ / ctrl+n      next
  PgUp / alt+v    page up
  PgDn / ctrl+v   page down
  Home / alt+<    first
  End / alt+>     last

View:
  /               search
  v               reveal/hide values
  c               columns selector
  ?               more

Actions:
  e               edit value
  r               generate random value
  x               delete current value
  D               delete visible/filtered values

Edit form:
  ctrl+s          save
  tab             next field
  shift+tab       previous field
  enter           newline in Value; open Region/Type selector; next field in text inputs
  ctrl+o          load File path content into Value
  ctrl+w          write Value to File path
  ctrl+k          clear active text field

Selectors:
  tab             next option
  shift+tab       previous option
  enter           choose option

Exit:
  q               quit on main page / back on sub-pages
  ctrl+g / esc    back from input screens`)
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

func pad(v string, width int) string {
	if len(v) >= width {
		return v[:width]
	}
	return v + strings.Repeat(" ", width-len(v))
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
	if len(v) <= width {
		return v
	}
	return v[:width-3] + "..."
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
