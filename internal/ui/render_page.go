package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type pageRenderer struct {
	model model
}

func (component pageRenderer) boxTop(title string, innerWidth int) string {
	m := component.model
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

func (component pageRenderer) boxBottom(innerWidth int) string {
	m := component.model
	return m.frame("└") + m.frame(strings.Repeat("─", innerWidth)) + m.frame("┘")
}

func (component pageRenderer) boxLine(content string, innerWidth int) string {
	m := component.model
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
func (component pageRenderer) renderFooter(text string) string {
	m := component.model
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
func (component pageRenderer) renderFullscreen(body, footer string) string {
	m := component.model
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
