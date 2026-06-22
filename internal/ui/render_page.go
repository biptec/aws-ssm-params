package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type pageRenderer struct {
	height int
	styleRenderer
}

func newPageRenderer(m model) pageRenderer {
	return pageRenderer{height: m.height, styleRenderer: newStyleRenderer(m)}
}

func (renderer pageRenderer) boxTop(title string, innerWidth int) string {
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
	if !renderer.noColor {
		titleRendered = titleStyle.Render(titleText)
	}
	return renderer.frame("┌") + renderer.frame(strings.Repeat("─", left)) + titleRendered + renderer.frame(strings.Repeat("─", rightLen)) + renderer.frame("┐")
}

func (renderer pageRenderer) boxBottom(innerWidth int) string {
	return renderer.frame("└") + renderer.frame(strings.Repeat("─", innerWidth)) + renderer.frame("┘")
}

func (renderer pageRenderer) boxLine(content string, innerWidth int) string {
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
	leftFrame := renderer.frame("│")
	if rawLeft {
		leftFrame = ""
	}
	return leftFrame + content + strings.Repeat(" ", padWidth) + renderer.frame("│")
}

// renderFooter formats the fixed bottom hotkey/status line.
func (renderer pageRenderer) renderFooter(text string) string {
	if renderer.noColor || text == "" {
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
func (renderer pageRenderer) renderFullscreen(body, footer string) string {
	body = indentBlock(body, 0)
	footer = indentBlock(footer, 0)
	if renderer.height <= 0 {
		if footer == "" {
			return body
		}
		return body + "\n" + footer
	}

	bodyLines := renderLines(body)
	footerLines := renderLines(footer)
	bodyHeight := max(0, renderer.height-len(footerLines))
	if len(bodyLines) > bodyHeight {
		bodyLines = bodyLines[:bodyHeight]
	}

	padLines := max(0, renderer.height-len(bodyLines)-len(footerLines))
	out := make([]string, 0, renderer.height)
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
