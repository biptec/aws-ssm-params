package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
)

type boxRenderer struct {
	innerWidth int
	styleRenderer
	pageRenderer
}

func newBoxRenderer(m model) boxRenderer {
	return boxRenderer{
		innerWidth:    m.boxInnerWidth(),
		styleRenderer: newStyleRenderer(m),
		pageRenderer:  newPageRenderer(m),
	}
}

// renderFieldPairs converts name/value metadata pairs into aligned lines for boxed detail views.
func (renderer boxRenderer) renderFieldPairs(fields [][2]string, labelWidth int) []string {
	lines := make([]string, 0, len(fields))
	for _, f := range fields {
		value := f[1]
		if f[0] == "Status" {
			lines = append(lines, "  "+renderer.fieldLine(f[0], value, labelWidth))
			continue
		}
		renderedValue := renderer.value(value)
		if f[0] == "Value" && value == encryptedPlaceholderText {
			renderedValue = renderer.encryptedPlaceholder()
		}
		lines = append(lines, "  "+renderer.fieldLine(f[0], renderedValue, labelWidth))
	}
	return lines
}

func (renderer boxRenderer) fieldLine(name, renderedValue string, labelWidth int) string {
	label := renderer.label(padMin(name+":", labelWidth+1))
	return label + " " + renderedValue
}

// renderBox draws a bordered box, truncating or padding content so screens keep stable heights.
func (renderer boxRenderer) renderBox(title string, lines []string, preferredHeight int) string {
	return renderer.renderBoxWithInnerWidth(title, lines, renderer.innerWidth, preferredHeight)
}

func (renderer boxRenderer) renderBoxWithInnerWidth(title string, lines []string, innerWidth, preferredHeight int) string {
	top := renderer.boxTop(title, innerWidth)
	bottom := renderer.boxBottom(innerWidth)

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
		out = append(out, renderer.boxLine(line, innerWidth))
	}
	out = append(out, bottom)
	return strings.Join(out, "\n")
}

func (renderer boxRenderer) singleSelectLine(label string, selected, focused bool) string {
	marker := "( )"
	if selected {
		marker = "(*)"
	}
	return renderer.optionLine(marker+" "+label, focused)
}

func (renderer boxRenderer) multiSelectLine(label string, checked, focused bool) string {
	marker := "[ ]"
	if checked {
		marker = "[x]"
	}
	return renderer.optionLine(marker+" "+label, focused)
}

func (renderer boxRenderer) optionLine(content string, focused bool) string {
	if focused {
		return renderer.selectedMarker() + renderer.selectedRow(content)
	}
	return "  " + content
}

func (renderer boxRenderer) popupInputLine(label string, input textinput.Model, inputWidth int) string {
	value := input.Value()
	pos := min(max(0, input.Position()), len([]rune(value)))
	inputText := renderer.inputValueWithCursor(value, pos, inputWidth)
	separator := " "
	if strings.HasSuffix(label, " ") {
		separator = ""
	}
	return renderer.label(label) + separator + inputText
}

func (renderer boxRenderer) popupInputLinePlainPrefix(prefix string, input textinput.Model, inputWidth int) string {
	value := input.Value()
	pos := min(max(0, input.Position()), len([]rune(value)))
	return prefix + renderer.inputValueWithCursor(value, pos, inputWidth)
}

func (renderer boxRenderer) inputValueWithCursor(value string, pos, width int) string {
	runes := []rune(value)
	pos = min(max(0, pos), len(runes))
	width = max(1, width)
	if len(runes) == 0 {
		return renderer.value(renderer.inputCursor())
	}
	start := 0
	if pos >= len(runes) {
		textWidth := max(0, width-1)
		if len(runes) > textWidth {
			start = len(runes) - textWidth
		}
		end := min(len(runes), start+textWidth)
		return renderer.value(string(runes[start:end]) + renderer.inputCursor())
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
			b.WriteString(renderer.inputCursorForRune(runes[i]))
			continue
		}
		b.WriteRune(runes[i])
	}
	return renderer.value(b.String())
}

func (renderer boxRenderer) inputCursor() string {
	if renderer.noColor {
		return "█"
	}
	return cursorStyle.Render(" ")
}

func (renderer boxRenderer) inputCursorForRune(r rune) string {
	if renderer.noColor {
		return "█"
	}
	return cursorStyle.Render(string(r))
}
