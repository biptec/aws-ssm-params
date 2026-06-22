package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
)

// renderFieldPairs converts name/value metadata pairs into aligned lines for boxed detail views.
func (m model) renderFieldPairs(fields [][2]string, labelWidth int) []string {
	lines := make([]string, 0, len(fields))
	for _, f := range fields {
		value := f[1]
		if f[0] == "Status" {
			lines = append(lines, "  "+m.fieldLine(f[0], value, labelWidth))
			continue
		}
		renderedValue := m.value(value)
		if f[0] == "Value" && value == encryptedPlaceholderText {
			renderedValue = m.encryptedPlaceholder()
		}
		lines = append(lines, "  "+m.fieldLine(f[0], renderedValue, labelWidth))
	}
	return lines
}

func (m model) fieldLine(name, renderedValue string, labelWidth int) string {
	label := m.label(padMin(name+":", labelWidth+1))
	return label + " " + renderedValue
}

// renderBox draws a bordered box, truncating or padding content so screens keep stable heights.
func (m model) renderBox(title string, lines []string, preferredHeight int) string {
	return m.renderBoxWithInnerWidth(title, lines, m.boxInnerWidth(), preferredHeight)
}

func (m model) renderBoxWithInnerWidth(title string, lines []string, innerWidth, preferredHeight int) string {
	top := m.boxTop(title, innerWidth)
	bottom := m.boxBottom(innerWidth)

	if preferredHeight <= 0 {
		preferredHeight = len(lines) + 2
	}
	preferredHeight = max(3, preferredHeight)
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

func (m model) singleSelectLine(label string, selected, focused bool) string {
	marker := "( )"
	if selected {
		marker = "(*)"
	}
	return m.optionLine(marker+" "+label, focused)
}

func (m model) multiSelectLine(label string, checked, focused bool) string {
	marker := "[ ]"
	if checked {
		marker = "[x]"
	}
	return m.optionLine(marker+" "+label, focused)
}

func (m model) optionLine(content string, focused bool) string {
	if focused {
		return m.selectedMarker() + m.selectedRow(content)
	}
	return "  " + content
}

func (m model) popupInputLine(label string, input textinput.Model, inputWidth int) string {
	value := input.Value()
	pos := min(max(0, input.Position()), len([]rune(value)))
	inputText := m.inputValueWithCursor(value, pos, inputWidth)
	separator := " "
	if strings.HasSuffix(label, " ") {
		separator = ""
	}
	return m.label(label) + separator + inputText
}

func (m model) popupInputLinePlainPrefix(prefix string, input textinput.Model, inputWidth int) string {
	value := input.Value()
	pos := min(max(0, input.Position()), len([]rune(value)))
	return prefix + m.inputValueWithCursor(value, pos, inputWidth)
}

func (m model) inputValueWithCursor(value string, pos, width int) string {
	runes := []rune(value)
	pos = min(max(0, pos), len(runes))
	width = max(1, width)
	if len(runes) == 0 {
		return m.value(m.inputCursor())
	}
	start := 0
	if pos >= len(runes) {
		textWidth := max(0, width-1)
		if len(runes) > textWidth {
			start = len(runes) - textWidth
		}
		end := min(len(runes), start+textWidth)
		return m.value(string(runes[start:end]) + m.inputCursor())
	}
	if len(runes) > width {
		start = pos - width + 1
		if start < 0 {
			start = 0
		}
		if start > len(runes)-width {
			start = len(runes) - width
		}
	}
	end := min(len(runes), start+width)
	var b strings.Builder
	for i := start; i < end; i++ {
		if i == pos {
			b.WriteString(m.inputCursorForRune(runes[i]))
			continue
		}
		b.WriteRune(runes[i])
	}
	return m.value(b.String())
}

func (m model) inputCursor() string {
	if m.opts.NoColor {
		return "█"
	}
	return cursorStyle.Render(" ")
}

func (m model) inputCursorForRune(r rune) string {
	if m.opts.NoColor {
		return "█"
	}
	return cursorStyle.Render(string(r))
}

func (m model) renderPopupBoxWithActions(title string, lines []string, actions string) string {
	if strings.TrimSpace(actions) != "" {
		lines = append(append([]string(nil), lines...), "", m.popupActionLine(actions))
	}
	return m.renderPopupBox(title, lines)
}

func (m model) popupActionLine(actions string) string {
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

func (m model) renderPopupBox(title string, lines []string) string {
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

func (m model) popupBoxTop(title string, innerWidth int) string {
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

func (m model) popupBoxBottom(innerWidth int) string {
	return m.popupFrame("╰") + m.popupFrame(strings.Repeat("─", innerWidth)) + m.popupFrame("╯")
}

func (m model) popupBoxLine(content string, innerWidth int) string {
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

func (m model) popupFrame(s string) string {
	if m.opts.NoColor {
		return s
	}
	return titleStyle.Render(s)
}

func (m model) renderPopupStack(body string) string {
	for _, kind := range m.popupLayers() {
		body = m.overlayPopupOnBody(body, m.renderPopup(kind))
	}
	return body
}

func (m model) renderPopup(kind popupKind) string {
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

func (m model) overlayPopupOnBody(body, popup string) string {
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

func (m model) encryptedPlaceholder() string {
	return m.muted(encryptedPlaceholderText)
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

func (m model) applyWarning(s string) string {
	if m.opts.NoColor {
		return s
	}
	return warningStyle.Render(s)
}

func (m model) renderFooterWithStatus(text string) string {
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

func (m model) renderStatusMessage() string {
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

func (m *model) clearTransientStatus() {
	m.message = ""
	m.warningMessage = ""
	m.errMessage = ""
	m.pendingQuit = false
	m.pendingQuitKey = ""
	m.pendingFileWrite = fileWriteConfirmationNone
}
