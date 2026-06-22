package ui

import (
	"context"
	"time"

	crerr "github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/filter"
	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type runtimeComponent struct {
	model model
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
func (component runtimeComponent) Init() tea.Cmd {
	m := component.model
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
