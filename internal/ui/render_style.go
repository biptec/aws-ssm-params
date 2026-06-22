package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type styleRenderer struct {
	model model
}

func (component styleRenderer) label(s string) string {
	m := component.model
	if m.opts.NoColor {
		return s
	}
	return labelStyle.Render(s)
}

func (component styleRenderer) value(s string) string {
	m := component.model
	if m.opts.NoColor {
		return s
	}
	return valueStyle.Render(s)
}

func (component styleRenderer) muted(s string) string {
	m := component.model
	if m.opts.NoColor {
		return s
	}
	return mutedStyle.Render(s)
}

func (component styleRenderer) encryptedPlaceholder() string {
	m := component.model
	return m.muted(encryptedPlaceholderText)
}

func (component styleRenderer) divider(s string) string {
	return strings.Repeat(" ", lipgloss.Width(s))
}

func (component styleRenderer) frame(s string) string {
	return strings.Repeat(" ", lipgloss.Width(s))
}

func (component styleRenderer) selectedRow(s string) string {
	m := component.model
	if m.opts.NoColor {
		return s
	}
	return selectedRowStyle.Render(s)
}

func (component styleRenderer) selectedMarker() string {
	m := component.model
	if m.opts.NoColor {
		return "| "
	}
	return lipgloss.NewStyle().Foreground(selectedFg).Render("| ")
}

func (component styleRenderer) searchLine() string {
	m := component.model
	line := "Search > " + m.query
	if m.searchInvalid {
		return m.applyErr(line)
	}
	return m.searchPrompt() + m.value(m.query)
}

func (component styleRenderer) filteredLine() string {
	m := component.model
	return m.filteredPrompt() + m.value(m.effectiveQuery)
}

func (component styleRenderer) searchPrompt() string {
	m := component.model
	if m.opts.NoColor {
		return "Search > "
	}
	return searchStyle.Render("Search > ")
}

func (component styleRenderer) filteredPrompt() string {
	m := component.model
	if m.opts.NoColor {
		return "Filtered > "
	}
	return searchStyle.Render("Filtered > ")
}

func (component styleRenderer) applyErr(s string) string {
	m := component.model
	if m.opts.NoColor {
		return s
	}
	return errorStyle.Render(s)
}

func (component styleRenderer) applyWarning(s string) string {
	m := component.model
	if m.opts.NoColor {
		return s
	}
	return warningStyle.Render(s)
}

func (component styleRenderer) renderFooterWithStatus(text string) string {
	m := component.model
	footer := m.renderFooter(text)
	status := m.renderStatusMessage()
	if status == "" {
		return strings.Join([]string{" ", footer, " "}, "\n")
	}
	return strings.Join([]string{" ", status, " ", footer, " "}, "\n")
}

func quitConfirmationMessage(_ string) string {
	return `Are you sure you want to quit? Press "y" to confirm.`
}

func (component styleRenderer) renderStatusMessage() string {
	m := component.model
	switch {
	case m.errMessage != "":
		return m.applyErr(m.errMessage)
	case m.warningMessage != "":
		return m.applyWarning(m.warningMessage)
	case m.busyMessage != "":
		return m.muted(m.busyMessage)
	case m.message != "":
		return m.muted(m.message)
	default:
		return ""
	}
}

func (component *styleRenderer) clearTransientStatus() {
	m := &component.model
	m.message = ""
	m.warningMessage = ""
	m.errMessage = ""
	m.pendingQuit = false
	m.pendingQuitKey = ""
	m.pendingFileWrite = fileWriteConfirmationNone
}
