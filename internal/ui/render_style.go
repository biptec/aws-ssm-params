package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type styleRenderer struct {
	noColor         bool
	effectiveFilter string
	message         string
	warningMessage  string
	errMessage      string
	busyMessage     string
}

func newStyleRenderer(m model) *styleRenderer {
	return &styleRenderer{
		noColor:         m.opts.NoColor,
		effectiveFilter: m.effectiveFilter,
		message:         m.message,
		warningMessage:  m.warningMessage,
		errMessage:      m.errMessage,
		busyMessage:     m.busyMessage,
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

func (renderer *styleRenderer) focusMarker() string {
	if renderer.noColor {
		return "> "
	}

	return lipgloss.NewStyle().Foreground(selectedFg).Render("> ")
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

func (renderer *styleRenderer) filteredLine() string {
	return renderer.filteredPrompt() + renderer.value(renderer.effectiveFilter)
}

func (renderer *styleRenderer) filterPrompt() string {
	if renderer.noColor {
		return "Filter > "
	}

	return searchStyle.Render("Filter > ")
}

func (renderer *styleRenderer) filteredPrompt() string {
	if renderer.noColor {
		return "Filtered > "
	}

	return tableHeaderStyle.Render("Filtered > ")
}

func (renderer *styleRenderer) applyErr(s string) string {
	if renderer.noColor {
		return s
	}

	return errorStyle.Render(s)
}

func (renderer *styleRenderer) stateValue(state parameterState) string {
	value := string(state)
	if renderer.noColor || value == "" {
		return value
	}

	switch state {
	case parameterStateModified:
		return lipgloss.NewStyle().Foreground(stateModifiedFg).Render(value)
	case parameterStateNew:
		return lipgloss.NewStyle().Foreground(stateNewFg).Render(value)
	case parameterStateDeleted:
		return lipgloss.NewStyle().Foreground(stateDeletedFg).Render(value)
	case parameterStateError:
		return lipgloss.NewStyle().Foreground(stateErrorFg).Render(value)
	case parameterStateClean:
		return value
	}

	return value
}

func (renderer *styleRenderer) diffCloudValue(s string) string {
	if renderer.noColor {
		return s
	}

	return lipgloss.NewStyle().Foreground(diffCloudFg).Render(s)
}

func (renderer *styleRenderer) diffLocalValue(s string) string {
	if renderer.noColor {
		return s
	}

	return lipgloss.NewStyle().Foreground(diffLocalFg).Render(s)
}

func (renderer *styleRenderer) applyWarning(s string) string {
	if renderer.noColor {
		return s
	}

	return warningStyle.Render(s)
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
