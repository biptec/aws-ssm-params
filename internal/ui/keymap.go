package ui

type keymapStyle string

const (
	keymapEmacs keymapStyle = "emacs"
	keymapVi    keymapStyle = "vi"
)

type navigationAction int

const (
	navNone navigationAction = iota
	navPrevious
	navNext
	navPageUp
	navPageDown
	navFirst
	navLast
)

func normalizeKeymapStyle(value string) keymapStyle {
	switch keymapStyle(value) {
	case keymapVi:
		return keymapVi
	default:
		return keymapEmacs
	}
}

func (m model) keymapStyle() keymapStyle {
	return normalizeKeymapStyle(m.opts.Keymap)
}

func (m model) navigationAction(key string) (navigationAction, bool) {
	switch key {
	case "up", "shift+tab":
		return navPrevious, true
	case "down", "tab":
		return navNext, true
	case "pageup":
		return navPageUp, true
	case "pagedown":
		return navPageDown, true
	case "home":
		return navFirst, true
	case "end":
		return navLast, true
	}

	if m.keymapStyle() == keymapVi {
		switch key {
		case "k":
			return navPrevious, true
		case "j":
			return navNext, true
		case "G":
			return navLast, true
		}
		return navNone, false
	}

	switch key {
	case "ctrl+p":
		return navPrevious, true
	case "ctrl+n":
		return navNext, true
	case "alt+v":
		return navPageUp, true
	case "ctrl+v":
		return navPageDown, true
	case "alt+<":
		return navFirst, true
	case "alt+>":
		return navLast, true
	}
	return navNone, false
}

func (m model) handlePendingNavigationSequence(key string) (navigationAction, bool, bool) {
	if m.pendingKeySequence == "" {
		return navNone, false, false
	}
	pending := m.pendingKeySequence
	m.pendingKeySequence = ""
	if m.keymapStyle() == keymapVi && pending == "g" && key == "g" {
		return navFirst, true, true
	}
	return navNone, false, true
}
