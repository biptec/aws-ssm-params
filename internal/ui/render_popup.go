package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type popupRenderer struct {
	model model
}

func (component popupRenderer) renderPopupBoxWithActions(title string, lines []string, actions string) string {
	m := component.model
	if strings.TrimSpace(actions) != "" {
		lines = append(append([]string(nil), lines...), "", m.popupActionLine(actions))
	}
	return m.renderPopupBox(title, lines)
}

func (component popupRenderer) popupActionLine(actions string) string {
	m := component.model
	if m.opts.NoColor {
		return actions
	}
	fields := strings.Fields(actions)
	if len(fields) == 0 {
		return ""
	}
	parts := make([]string, 0, (len(fields)+1)/2)
	for i := 0; i < len(fields); i += 2 {
		key := fields[i]
		description := ""
		if i+1 < len(fields) {
			description = fields[i+1]
		}
		part := hotkeyStyle.Render(key)
		if description != "" {
			part += " " + mutedStyle.Render(description)
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, mutedStyle.Render("   "))
}

func (component popupRenderer) renderPopupBox(title string, lines []string) string {
	m := component.model
	lines = popupPaddedLines(lines)
	maxLineWidth := 0
	for _, line := range lines {
		maxLineWidth = max(maxLineWidth, lipgloss.Width(line))
	}
	availableInner := m.boxInnerWidth() - 8
	if availableInner <= 0 {
		availableInner = m.boxInnerWidth()
	}
	innerWidth := max(20, maxLineWidth)
	innerWidth = min(innerWidth, 80)
	innerWidth = min(innerWidth, max(10, availableInner))
	out := make([]string, 0, 1+len(lines)+1)
	out = append(out, m.popupBoxTop(title, innerWidth))
	for _, line := range lines {
		out = append(out, m.popupBoxLine(line, innerWidth))
	}
	out = append(out, m.popupBoxBottom(innerWidth))
	return strings.Join(out, "\n")
}

func popupPaddedLines(lines []string) []string {
	const horizontalPadding = 2
	const verticalPadding = 1
	out := make([]string, 0, len(lines)+verticalPadding*2)
	for i := 0; i < verticalPadding; i++ {
		out = append(out, "")
	}
	pad := strings.Repeat(" ", horizontalPadding)
	for _, line := range lines {
		out = append(out, pad+line+pad)
	}
	for i := 0; i < verticalPadding; i++ {
		out = append(out, "")
	}
	return out
}

func (component popupRenderer) popupBoxTop(title string, innerWidth int) string {
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
	return m.popupFrame("╭") + m.popupFrame(strings.Repeat("─", left)) + titleRendered + m.popupFrame(strings.Repeat("─", rightLen)) + m.popupFrame("╮")
}

func (component popupRenderer) popupBoxBottom(innerWidth int) string {
	m := component.model
	return m.popupFrame("╰") + m.popupFrame(strings.Repeat("─", innerWidth)) + m.popupFrame("╯")
}

func (component popupRenderer) popupBoxLine(content string, innerWidth int) string {
	m := component.model
	visible := lipgloss.Width(content)
	if visible > innerWidth {
		content = truncateStyled(content, innerWidth)
		visible = lipgloss.Width(content)
	}
	padWidth := innerWidth - visible
	if padWidth < 0 {
		padWidth = 0
	}
	return m.popupFrame("│") + content + strings.Repeat(" ", padWidth) + m.popupFrame("│")
}

func (component popupRenderer) popupFrame(s string) string {
	m := component.model
	if m.opts.NoColor {
		return s
	}
	return titleStyle.Render(s)
}

func (component popupRenderer) renderPopupStack(body string) string {
	m := component.model
	for _, kind := range m.popupLayers() {
		body = m.overlayPopupOnBody(body, m.renderPopup(kind))
	}
	return body
}

func (component popupRenderer) renderPopup(kind popupKind) string {
	m := component.model
	switch kind {
	case popupNone:
		return ""
	case popupColumns:
		return m.renderColumnsPopup()
	case popupShortcuts:
		return m.renderShortcutsPopup()
	case popupConfirm:
		return m.renderConfirmPopup()
	case popupSort:
		return m.renderSortPopup()
	case popupRegionSelect:
		return m.renderRegionSelectPopup()
	case popupTypeSelect:
		return m.renderTypeSelectPopup()
	case popupTierSelect:
		return m.renderTierSelectPopup()
	case popupDataTypeSelect:
		return m.renderDataTypeSelectPopup()
	case popupOverwriteSelect:
		return m.renderOverwriteSelectPopup()
	case popupValueActions:
		return m.renderValueActionsPopup()
	case popupPoliciesActions:
		return m.renderPoliciesActionsPopup()
	case popupFileAction:
		return m.renderFileActionPopup()
	case popupFileWriteConfirm:
		return m.renderFileWriteConfirmPopup()
	case popupUnsavedChanges:
		return m.renderUnsavedChangesPopup()
	case popupRandomValue:
		return m.renderRandomValuePopup()
	default:
		return ""
	}
}

func (component popupRenderer) overlayPopupOnBody(body, popup string) string {
	m := component.model
	if popup == "" {
		return body
	}
	bodyLines := renderLines(body)
	popupLines := renderLines(popup)
	if len(popupLines) == 0 {
		return body
	}
	contentHeight := m.height
	if contentHeight <= 0 {
		contentHeight = max(len(bodyLines), len(popupLines))
	}
	for len(bodyLines) < contentHeight {
		bodyLines = append(bodyLines, "")
	}
	if len(bodyLines) > contentHeight {
		bodyLines = bodyLines[:contentHeight]
	}
	popupHeight := len(popupLines)
	if popupHeight > contentHeight {
		popupLines = popupLines[:contentHeight]
		popupHeight = len(popupLines)
	}
	popupWidth := 0
	for _, line := range popupLines {
		popupWidth = max(popupWidth, lipgloss.Width(line))
	}
	viewWidth := m.width
	if viewWidth <= 0 {
		viewWidth = max(popupWidth, m.boxInnerWidth()+2)
	}
	top := max(0, (contentHeight-popupHeight)/2)
	left := max(0, (viewWidth-popupWidth)/2)
	for i, line := range popupLines {
		bodyLines[top+i] = overlayPopupLine(bodyLines[top+i], line, left, popupWidth, viewWidth)
	}
	return strings.Join(bodyLines, "\n")
}

func overlayPopupLine(baseLine, popupLine string, left, popupWidth, viewWidth int) string {
	base := stripANSI(baseLine)
	if viewWidth <= 0 {
		viewWidth = max(lipgloss.Width(base), left+popupWidth)
	}
	base = padVisible(base, viewWidth)
	prefix := takeVisibleColumns(base, left)
	popup := popupLine
	if pad := popupWidth - lipgloss.Width(popup); pad > 0 {
		popup += strings.Repeat(" ", pad)
	}
	suffix := dropVisibleColumns(base, left+popupWidth)
	row := prefix + popup + suffix
	if pad := viewWidth - lipgloss.Width(row); pad > 0 {
		row += strings.Repeat(" ", pad)
	}
	return row
}

func takeVisibleColumns(s string, width int) string {
	if width <= 0 {
		return ""
	}
	out := strings.Builder{}
	used := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if used+rw > width {
			break
		}
		out.WriteRune(r)
		used += rw
	}
	if used < width {
		out.WriteString(strings.Repeat(" ", width-used))
	}
	return out.String()
}

func dropVisibleColumns(s string, start int) string {
	if start <= 0 {
		return s
	}
	used := 0
	for idx, r := range s {
		rw := lipgloss.Width(string(r))
		if used+rw > start {
			return s[idx:]
		}
		used += rw
		if used >= start {
			return s[idx+len(string(r)):]
		}
	}
	return ""
}
