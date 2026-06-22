package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
)

type boxRenderer struct {
	model model
}

// renderFieldPairs converts name/value metadata pairs into aligned lines for boxed detail views.
func (component boxRenderer) renderFieldPairs(fields [][2]string, labelWidth int) []string {
	m := component.model
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

func (component boxRenderer) fieldLine(name, renderedValue string, labelWidth int) string {
	m := component.model
	label := m.label(padMin(name+":", labelWidth+1))
	return label + " " + renderedValue
}

// renderBox draws a bordered box, truncating or padding content so screens keep stable heights.
func (component boxRenderer) renderBox(title string, lines []string, preferredHeight int) string {
	m := component.model
	return m.renderBoxWithInnerWidth(title, lines, m.boxInnerWidth(), preferredHeight)
}

func (component boxRenderer) renderBoxWithInnerWidth(title string, lines []string, innerWidth, preferredHeight int) string {
	m := component.model
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

func (component boxRenderer) singleSelectLine(label string, selected, focused bool) string {
	m := component.model
	marker := "( )"
	if selected {
		marker = "(*)"
	}
	return m.optionLine(marker+" "+label, focused)
}

func (component boxRenderer) multiSelectLine(label string, checked, focused bool) string {
	m := component.model
	marker := "[ ]"
	if checked {
		marker = "[x]"
	}
	return m.optionLine(marker+" "+label, focused)
}

func (component boxRenderer) optionLine(content string, focused bool) string {
	m := component.model
	if focused {
		return m.selectedMarker() + m.selectedRow(content)
	}
	return "  " + content
}

func (component boxRenderer) popupInputLine(label string, input textinput.Model, inputWidth int) string {
	m := component.model
	value := input.Value()
	pos := min(max(0, input.Position()), len([]rune(value)))
	inputText := m.inputValueWithCursor(value, pos, inputWidth)
	separator := " "
	if strings.HasSuffix(label, " ") {
		separator = ""
	}
	return m.label(label) + separator + inputText
}

func (component boxRenderer) popupInputLinePlainPrefix(prefix string, input textinput.Model, inputWidth int) string {
	m := component.model
	value := input.Value()
	pos := min(max(0, input.Position()), len([]rune(value)))
	return prefix + m.inputValueWithCursor(value, pos, inputWidth)
}

func (component boxRenderer) inputValueWithCursor(value string, pos, width int) string {
	m := component.model
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

func (component boxRenderer) inputCursor() string {
	m := component.model
	if m.opts.NoColor {
		return "█"
	}
	return cursorStyle.Render(" ")
}

func (component boxRenderer) inputCursorForRune(r rune) string {
	m := component.model
	if m.opts.NoColor {
		return "█"
	}
	return cursorStyle.Render(string(r))
}
