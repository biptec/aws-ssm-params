package ui

type keymap struct {
	style        keymapStyle
	viInsertMode bool
}

func newKeymap(m model) keymap {
	return keymap{style: normalizeKeymapStyle(m.opts.Keymap), viInsertMode: m.viInsertMode}
}

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
	case keymapEmacs:
		return keymapEmacs
	case keymapVi:
		return keymapVi
	default:
		return keymapEmacs
	}
}

func (keys keymap) keymapStyle() keymapStyle {
	return keys.style
}

func (keys keymap) navigationAction(key string) (navigationAction, bool) {
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

	if keys.keymapStyle() == keymapVi {
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

func (keys keymap) editorNavigationAction(key string) (navigationAction, bool) {
	action, ok := keys.navigationAction(key)
	if !ok {
		return navNone, false
	}
	if keys.keymapStyle() == keymapVi && keys.viInsertMode {
		switch key {
		case "j", "k", "G":
			return navNone, false
		}
	}
	return action, true
}

func (keys keymap) resolvePendingNavigationSequence(pending, key string) (navigationAction, bool, bool) {
	if pending == "" {
		return navNone, false, false
	}
	if keys.keymapStyle() == keymapVi && pending == "g" && key == "g" {
		return navFirst, true, true
	}
	return navNone, false, true
}

func isHelpKey(key string) bool {
	return key == "ctrl+_" || key == "ctrl+/" || key == "ctrl+?"
}
