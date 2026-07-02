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

	if m.keymapStyle() != keymapEmacs {
		return false
	}

	action, ok := newKeymap(m).emacsTextEditAction(key)
	if !ok {
		return false
	}

	if action == textEditWordForward {
		moveTextInputWordForward(input)
		return true
	}

	if action == textEditWordBackward {
		moveTextInputWordBackward(input)
		return true
	}

	return false
}

func moveTextInputWordForward(input *textinput.Model) {
	input.SetCursor(wordForwardIndex([]rune(input.Value()), input.Position()))
}

func moveTextInputWordBackward(input *textinput.Model) {
	input.SetCursor(wordBackwardIndex([]rune(input.Value()), input.Position()))
}
