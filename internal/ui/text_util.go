package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func nextCursor(current, count int) int {
	if count <= 0 {
		return 0
	}

	return (current + 1) % count
}

func previousCursor(current, count int) int {
	if count <= 0 {
		return 0
	}

	return (current - 1 + count) % count
}

func indexOf(values []string, value string) int {
	for i, candidate := range values {
		if candidate == value {
			return i
		}
	}

	return 0
}

func promptLineCount(value string) int {
	if value == "" {
		return 1
	}

	return len(strings.Split(value, "\n"))
}

func countLines(s string) int {
	if s == "" {
		return 0
	}

	return strings.Count(s, "\n") + 1
}

func indentBlock(s string, spaces int) string {
	if s == "" {
		return ""
	}

	prefix := strings.Repeat(" ", spaces)

	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}

	return strings.Join(lines, "\n")
}

func padMin(v string, width int) string {
	if len(v) >= width {
		return v
	}

	return v + strings.Repeat(" ", width-len(v))
}

func pad(v string, width int) string {
	visible := lipgloss.Width(v)
	if visible >= width {
		return truncateStyled(v, width)
	}

	return v + strings.Repeat(" ", width-visible)
}

func padVisible(v string, width int) string {
	plain := stripANSI(v)
	if len(plain) >= width {
		return v
	}

	return v + strings.Repeat(" ", width-len(plain))
}

func truncateInline(v string, width int) string {
	v = strings.ReplaceAll(v, "\r", "")
	v = strings.ReplaceAll(v, "\n", " ")

	if width < 4 {
		width = 4
	}

	if lipgloss.Width(v) <= width {
		return v
	}

	runes := []rune(v)

	out := make([]rune, 0, min(len(runes), width))
	for _, r := range runes {
		if lipgloss.Width(string(out))+lipgloss.Width(string(r))+3 > width {
			break
		}

		out = append(out, r)
	}

	return string(out) + "..."
}

func truncateStyled(v string, width int) string {
	plain := stripANSI(v)
	if len(plain) <= width {
		return v
	}

	return truncateInline(plain, width)
}

// stripANSI removes ANSI escape sequences so width calculations work with styled strings.
func stripANSI(s string) string {
	out := make([]rune, 0, len(s))
	inEsc := false

	for i, r := range s {
		_ = i

		if !inEsc && r == 0x1b {
			inEsc = true
			continue
		}

		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}

			continue
		}

		out = append(out, r)
	}

	return string(out)
}

// sliceForScroll returns a fixed-height window over a larger line list.
func pageSize(h int) int { return max(5, h-4) }
