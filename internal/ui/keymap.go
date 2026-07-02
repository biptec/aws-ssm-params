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

type textEditAction int

const (
	textEditNone textEditAction = iota
	textEditEnterInsertMode
	textEditForwardChar
	textEditBackwardChar
	textEditPreviousLine
	textEditNextLine
	textEditLineStart
	textEditLineEnd
	textEditWordForward
	textEditWordBackward
	textEditTextStart
	textEditTextEnd
	textEditPageUp
	textEditPageDown
	textEditDeleteChar
	textEditDeleteToLineEnd
	textEditDeleteWordForward
	textEditDeleteWordBackward
	textEditPendingStart
	textEditPendingDelete
)

type navigationBinding struct {
	binding keyBindingMatcher
	action  navigationAction
}

type keyBindingMatcher interface {
	Keys() []string
}

var (
	commonNavigationBindings = []navigationBinding{
		{binding: commonPreviousNavigationShortcut, action: navPrevious},
		{binding: commonNextNavigationShortcut, action: navNext},
		{binding: commonPageUpShortcut, action: navPageUp},
		{binding: commonPageDownShortcut, action: navPageDown},
		{binding: commonFirstShortcut, action: navFirst},
		{binding: commonLastShortcut, action: navLast},
	}

	viNavigationBindings = []navigationBinding{
		{binding: viPreviousNavigationShortcut, action: navPrevious},
		{binding: viNextNavigationShortcut, action: navNext},
		{binding: viLastNavigationShortcut, action: navLast},
	}

	emacsNavigationBindings = []navigationBinding{
		{binding: emacsPreviousNavigationShortcut, action: navPrevious},
		{binding: emacsNextNavigationShortcut, action: navNext},
		{binding: emacsPageUpShortcut, action: navPageUp},
		{binding: emacsPageDownShortcut, action: navPageDown},
		{binding: emacsFirstShortcut, action: navFirst},
		{binding: emacsLastShortcut, action: navLast},
	}
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
	if action, ok := matchNavigationBinding(key, commonNavigationBindings); ok {
		return action, true
	}

	if keys.keymapStyle() == keymapVi {
		if action, ok := matchNavigationBinding(key, viNavigationBindings); ok {
			return action, true
		}

		return navNone, false
	}

	if action, ok := matchNavigationBinding(key, emacsNavigationBindings); ok {
		return action, true
	}

	return navNone, false
}

func (keys keymap) editorNavigationAction(key string) (navigationAction, bool) {
	action, ok := keys.navigationAction(key)
	if !ok {
		return navNone, false
	}

	if keys.keymapStyle() == keymapVi && keys.viInsertMode {
		viNormalMotionKey := bindingMatchesString(viNextNavigationShortcut, key) ||
			bindingMatchesString(viPreviousNavigationShortcut, key) ||
			bindingMatchesString(viLastNavigationShortcut, key)

		if viNormalMotionKey {
			return navNone, false
		}
	}

	return action, true
}

func matchNavigationBinding(key string, bindings []navigationBinding) (navigationAction, bool) {
	for _, candidate := range bindings {
		if bindingMatchesKeys(candidate.binding, key) {
			return candidate.action, true
		}
	}

	return navNone, false
}

func (keys keymap) resolvePendingNavigationSequence(pending, key string) (navigationAction, bool, bool) {
	if pending == "" {
		return navNone, false, false
	}

	if keys.keymapStyle() == keymapVi && bindingMatchesString(viFirstNavigationSequence, pending+key) {
		return navFirst, true, true
	}

	return navNone, false, true
}

func (keys keymap) emacsTextEditAction(key string) (textEditAction, bool) {
	switch {
	case bindingMatchesString(emacsTextForwardCharShortcut, key):
		return textEditForwardChar, true
	case bindingMatchesString(emacsTextBackwardCharShortcut, key):
		return textEditBackwardChar, true
	case bindingMatchesString(emacsTextPreviousLineShortcut, key):
		return textEditPreviousLine, true
	case bindingMatchesString(emacsTextNextLineShortcut, key):
		return textEditNextLine, true
	case bindingMatchesString(emacsTextLineStartShortcut, key):
		return textEditLineStart, true
	case bindingMatchesString(emacsTextLineEndShortcut, key):
		return textEditLineEnd, true
	case bindingMatchesString(emacsTextPageDownShortcut, key):
		return textEditPageDown, true
	case bindingMatchesString(emacsTextPageUpShortcut, key):
		return textEditPageUp, true
	case bindingMatchesString(emacsWordForwardShortcut, key):
		return textEditWordForward, true
	case bindingMatchesString(emacsWordBackwardShortcut, key):
		return textEditWordBackward, true
	case bindingMatchesString(emacsTextStartShortcut, key):
		return textEditTextStart, true
	case bindingMatchesString(emacsTextEndShortcut, key):
		return textEditTextEnd, true
	case bindingMatchesString(emacsTextDeleteCharShortcut, key):
		return textEditDeleteChar, true
	case bindingMatchesString(emacsTextKillLineShortcut, key):
		return textEditDeleteToLineEnd, true
	case bindingMatchesString(emacsTextDeleteNextWord, key):
		return textEditDeleteWordForward, true
	case bindingMatchesString(emacsTextDeletePrevWord, key):
		return textEditDeleteWordBackward, true
	default:
		return textEditNone, false
	}
}

func (keys keymap) viTextEditAction(key string) (textEditAction, bool) {
	switch {
	case bindingMatchesString(viInsertModeShortcut, key):
		return textEditEnterInsertMode, true
	case bindingMatchesString(viTextBackwardCharShortcut, key):
		return textEditBackwardChar, true
	case bindingMatchesString(viTextForwardCharShortcut, key):
		return textEditForwardChar, true
	case bindingMatchesString(viTextNextLineShortcut, key):
		return textEditNextLine, true
	case bindingMatchesString(viTextPageDownShortcut, key):
		return textEditPageDown, true
	case bindingMatchesString(viTextPageUpShortcut, key):
		return textEditPageUp, true
	case bindingMatchesString(viTextPreviousLineShortcut, key):
		return textEditPreviousLine, true
	case bindingMatchesString(viTextLineStartShortcut, key):
		return textEditLineStart, true
	case bindingMatchesString(viTextLineEndShortcut, key):
		return textEditLineEnd, true
	case bindingMatchesString(viWordForwardShortcut, key):
		return textEditWordForward, true
	case bindingMatchesString(viWordBackwardShortcut, key):
		return textEditWordBackward, true
	case bindingMatchesString(viTextStartShortcut, key):
		return textEditTextEnd, true
	case bindingMatchesString(viTextStartPrefixShortcut, key):
		return textEditPendingStart, true
	case bindingMatchesString(viTextDeletePrefixShortcut, key):
		return textEditPendingDelete, true
	case bindingMatchesString(viTextDeleteToLineEnd, key):
		return textEditDeleteToLineEnd, true
	case bindingMatchesString(viTextDeleteCharShortcut, key):
		return textEditDeleteChar, true
	default:
		return textEditNone, false
	}
}

func (keys keymap) resolvePendingTextEditSequence(pending, key string) (textEditAction, bool, bool) {
	if pending == "" {
		return textEditNone, false, false
	}

	sequence := pending + key
	switch {
	case keys.keymapStyle() == keymapVi && bindingMatchesString(viTextStartSequenceShortcut, sequence):
		return textEditTextStart, true, true
	case keys.keymapStyle() == keymapVi && bindingMatchesString(viDeleteNextWordShortcut, sequence):
		return textEditDeleteWordForward, true, true
	case keys.keymapStyle() == keymapVi && bindingMatchesString(viDeletePrevWordShortcut, sequence):
		return textEditDeleteWordBackward, true, true
	default:
		return textEditNone, false, true
	}
}
