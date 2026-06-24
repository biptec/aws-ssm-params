package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type styleRenderer struct {
	noColor        bool
	query          string
	effectiveQuery string
	searchInvalid  bool
	message        string
	warningMessage string
	errMessage     string
	busyMessage    string
}

func newStyleRenderer(m model) *styleRenderer {
	return &styleRenderer{
		noColor:        m.opts.NoColor,
		query:          m.query,
		effectiveQuery: m.effectiveQuery,
		searchInvalid:  m.searchInvalid,
		message:        m.message,
		warningMessage: m.warningMessage,
		errMessage:     m.errMessage,
		busyMessage:    m.busyMessage,
	}
}

func (renderer *styleRenderer) label(s string) string {
	if renderer.noColor {
		return s
	}

	return labelStyle.Render(s)
}

func (renderer *styleRenderer) value(s string) string {
	if renderer.noColor {
		return s
	}

	return valueStyle.Render(s)
}

func (renderer *styleRenderer) muted(s string) string {
	if renderer.noColor {
		return s
	}

	return mutedStyle.Render(s)
}

func (renderer *styleRenderer) focusMarker(s string) string {
	if renderer.noColor {
		return s
	}

	return lipgloss.NewStyle().Foreground(selectedFg).Render(s)
}

func (renderer *styleRenderer) encryptedPlaceholder() string {
	return renderer.muted(encryptedPlaceholderText)
}

func (renderer *styleRenderer) divider(s string) string {
	return strings.Repeat(" ", lipgloss.Width(s))
}

func (renderer *styleRenderer) frame(s string) string {
	return strings.Repeat(" ", lipgloss.Width(s))
}

func (renderer *styleRenderer) selectedRow(s string) string {
	if renderer.noColor {
		return s
	}

	return selectedRowStyle.Render(s)
}

func (renderer *styleRenderer) selectedMarker() string {
	if renderer.noColor {
		return "> "
	}

	return lipgloss.NewStyle().Foreground(selectedFg).Render("> ")
}

func (renderer *styleRenderer) searchLine() string {
	line := "Search > " + renderer.query
	if renderer.searchInvalid {
		return renderer.applyErr(line)
	}

	return renderer.searchPrompt() + renderer.value(renderer.query)
}

func (renderer *styleRenderer) filteredLine() string {
	return renderer.filteredPrompt() + renderer.value(renderer.effectiveQuery)
}

func (renderer *styleRenderer) searchPrompt() string {
	if renderer.noColor {
		return "Search > "
	}

	return searchStyle.Render("Search > ")
}

func (renderer *styleRenderer) filteredPrompt() string {
	if renderer.noColor {
		return "Filtered > "
	}

	return searchStyle.Render("Filtered > ")
}

func (renderer *styleRenderer) applyErr(s string) string {
	if renderer.noColor {
		return s
	}

	return errorStyle.Render(s)
}

func (renderer *styleRenderer) applyWarning(s string) string {
	if renderer.noColor {
		return s
	}

	return warningStyle.Render(s)
}

func quitConfirmationMessage() string {
	return `Are you sure you want to quit? Press "y" to confirm.`
}

func (renderer *styleRenderer) renderStatusMessage() string {
	switch {
	case renderer.errMessage != "":
		return renderer.applyErr(renderer.errMessage)
	case renderer.warningMessage != "":
		return renderer.applyWarning(renderer.warningMessage)
	case renderer.busyMessage != "":
		return renderer.muted(renderer.busyMessage)
	case renderer.message != "":
		return renderer.muted(renderer.message)
	default:
		return ""
	}
}
