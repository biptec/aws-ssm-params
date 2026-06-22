package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type popupRenderer struct {
	noColor    bool
	width      int
	height     int
	innerWidth int
	layers     []popupKind
	render     func(popupKind) string
}

func newPopupRenderer(m model) popupRenderer {
	return popupRenderer{
		noColor:    m.opts.NoColor,
		width:      m.width,
		height:     m.height,
		innerWidth: m.boxInnerWidth(),
		layers:     m.popupLayers(),
		render: func(kind popupKind) string {
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
		},
	}
}

func (renderer popupRenderer) renderPopupBoxWithActions(title string, lines []string, actions string) string {
	if strings.TrimSpace(actions) != "" {
		lines = append(append([]string(nil), lines...), "", renderer.popupActionLine(actions))
	}
	return renderer.renderPopupBox(title, lines)
}

func (renderer popupRenderer) popupActionLine(actions string) string {
	if renderer.noColor {
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

func (renderer popupRenderer) renderPopupBox(title string, lines []string) string {
	lines = popupPaddedLines(lines)
	maxLineWidth := 0
	for _, line := range lines {
		maxLineWidth = max(maxLineWidth, lipgloss.Width(line))
	}
	availableInner := renderer.innerWidth - 8
	if availableInner <= 0 {
		availableInner = renderer.innerWidth
	}
	innerWidth := max(20, maxLineWidth)
	innerWidth = min(innerWidth, 80)
	innerWidth = min(innerWidth, max(10, availableInner))
	out := make([]string, 0, 1+len(lines)+1)
	out = append(out, renderer.popupBoxTop(title, innerWidth))
	for _, line := range lines {
		out = append(out, renderer.popupBoxLine(line, innerWidth))
	}
	out = append(out, renderer.popupBoxBottom(innerWidth))
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

func (renderer popupRenderer) popupBoxTop(title string, innerWidth int) string {
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
	return renderer.popupFrame("╭") + renderer.popupFrame(strings.Repeat("─", left)) + titleRendered + renderer.popupFrame(strings.Repeat("─", rightLen)) + renderer.popupFrame("╮")
}

func (renderer popupRenderer) popupBoxBottom(innerWidth int) string {
	return renderer.popupFrame("╰") + renderer.popupFrame(strings.Repeat("─", innerWidth)) + renderer.popupFrame("╯")
}

func (renderer popupRenderer) popupBoxLine(content string, innerWidth int) string {
	visible := lipgloss.Width(content)
	if visible > innerWidth {
		content = truncateStyled(content, innerWidth)
		visible = lipgloss.Width(content)
	}
	padWidth := innerWidth - visible
	if padWidth < 0 {
		padWidth = 0
	}
	return renderer.popupFrame("│") + content + strings.Repeat(" ", padWidth) + renderer.popupFrame("│")
}

func (renderer popupRenderer) popupFrame(s string) string {
	if renderer.noColor {
		return s
	}
	return titleStyle.Render(s)
}

func (renderer popupRenderer) renderPopupStack(body string) string {
	for _, kind := range renderer.layers {
		body = renderer.overlayPopupOnBody(body, renderer.renderPopup(kind))
	}
	return body
}

func (renderer popupRenderer) renderPopup(kind popupKind) string {
	return renderer.render(kind)
}

func (renderer popupRenderer) overlayPopupOnBody(body, popup string) string {
	if popup == "" {
		return body
	}
	bodyLines := renderLines(body)
	popupLines := renderLines(popup)
	if len(popupLines) == 0 {
		return body
	}
	contentHeight := renderer.height
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
	viewWidth := renderer.width
	if viewWidth <= 0 {
		viewWidth = max(popupWidth, renderer.innerWidth+2)
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
