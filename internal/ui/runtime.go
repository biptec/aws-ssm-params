package ui

import (
	"context"
	"time"

	"github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/filter"
	"github.com/biptec/aws-ssm-params/internal/inventory"
	ssmclient "github.com/biptec/aws-ssm-params/internal/ssm/client"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type runtimeState struct {
	contextProvider func() context.Context
	items           inventory.Items
	loadCh          chan tea.Msg

	width  int
	height int

	screen       screen
	returnScreen screen

	message        string
	warningMessage string
	errMessage     string
	busyMessage    string

	loadingTitle        string
	loadingSpinnerFrame int

	pendingQuit    bool
	pendingQuitKey string

	importStdinOpened bool
}

// RunInteractive creates and runs the Bubble Tea program in the terminal alternate screen.
// The function returns only after the user quits the TUI or Bubble Tea reports an error.
func RunInteractive(ctx context.Context, client ssmclient.Client, items inventory.Items, opts *Options) error {
	m := newModel(ctx, client, items, opts)

	programOptions := []tea.ProgramOption{tea.WithAltScreen()}
	if opts.UseInputTTY {
		programOptions = append(programOptions, tea.WithInputTTY())
	}

	p := tea.NewProgram(m, programOptions...)
	_, err := p.Run()

	return errors.Wrap(err, "run interactive TUI")
}

// newModel initializes the TUI model with default inputs, textarea settings, visible columns, and loading state.
// Statuses are not loaded here; Init starts that asynchronous work so the UI can show progress immediately.
func newModel(ctx context.Context, client ssmclient.Client, items inventory.Items, opts *Options) model {
	sortRules := parseInitialSortOptions(opts.Sort)
	sortBy, sortDescending := sortRules.primary()
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

	filterInput := textinput.New()
	filterInput.Prompt = ""
	filterInput.CharLimit = 0
	filterInput.Width = 80

	configureTextInputStyles(&input, opts)
	configureTextInputStyles(&editPathInput, opts)
	configureTextInputStyles(&editDescriptionInput, opts)
	configureTextInputStyles(&editFileInput, opts)
	configureTextInputStyles(&filterInput, opts)

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

	m := model{modelState: &modelState{
		client:  client,
		backend: newDefaultBackend(client),
		opts:    *opts,
	}}
	m.contextProvider = func() context.Context { return ctx }
	m.items = items
	m.statuses = pendingStatuses(items)
	m.loadCh = make(chan tea.Msg)
	m.screen = screenLoading
	m.shortcutsFor = screenLoading
	m.loadingTitle = "Loading parameters"
	m.input = input
	m.textArea = area
	m.editPoliciesArea = policiesArea
	m.editDescriptionArea = descriptionArea
	m.editPathInput = editPathInput
	m.editDescriptionInput = editDescriptionInput
	m.editFileInput = editFileInput
	m.filterInput = filterInput
	m.importState = newImportState(opts)
	m.columns = defaultColumnVisibility(opts.ShowColumns)
	m.sortBy = sortBy
	m.sortDescending = sortDescending
	m.sortRules = sortRules
	m.expandedFields = map[editField]bool{}
	m.showGutters = true

	return m
}

func configureTextInputStyles(input *textinput.Model, opts *Options) {
	if opts.NoColor {
		return
	}

	input.TextStyle = valueStyle
	input.Cursor.TextStyle = valueStyle
	input.Cursor.Style = valueStyle
}

// startLoadCmd launches the initial SSM status scan in a goroutine.
// Progress and final results are sent through loadCh so the Bubble Tea event loop can render loading updates.
func startLoadCmdWithBackend(ctx context.Context, backend uiBackend, items inventory.Items, groups filter.Groups, regions []string, includeValues bool, ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		go func() {
			loader := func(done, total int, region string, chunk inventory.Items) {
				_ = chunk

				ch <- progressMsg{done: done, total: total, region: region}
			}
			emitBatch := func(statuses Statuses) {
				statuses = statuses.Filter(groups)
				if len(statuses) > 0 {
					ch <- statusBatchMsg(append(Statuses(nil), statuses...))
				}
			}

			statuses := backend.loadStatuses(ctx, items, groups, regions, includeValues, loader, emitBatch)
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
