package ui

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

func (m model) updateTextInput(input *textinput.Model, msg tea.KeyMsg) tea.Cmd {
	if m.applyTextInputNavigation(input, msg.String()) {
		return nil
	}

	var cmd tea.Cmd
	*input, cmd = input.Update(msg)

	return cmd
}

func (m model) applyTextInputNavigation(input *textinput.Model, key string) bool {
	if input == nil {
		return false
	}

	switch m.keymapStyle() {
	case keymapEmacs:
		switch key {
		case "alt+f":
			moveTextInputWordForward(input)
			return true
		case "alt+b":
			moveTextInputWordBackward(input)
			return true
		}
	case keymapVi:
		switch key {
		case "alt+f":
			moveTextInputWordForward(input)
			return true
		case "alt+b":
			moveTextInputWordBackward(input)
			return true
		}
	}

	return false
}

func moveTextInputWordForward(input *textinput.Model) {
	input.SetCursor(wordForwardIndex([]rune(input.Value()), input.Position()))
}

func moveTextInputWordBackward(input *textinput.Model) {
	input.SetCursor(wordBackwardIndex([]rune(input.Value()), input.Position()))
}
