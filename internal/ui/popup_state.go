package ui

type popupStateComponent struct {
	model model
}

type popupKind int

const (
	popupNone popupKind = iota
	popupColumns
	popupShortcuts
	popupConfirm
	popupSort
	popupRegionSelect
	popupTypeSelect
	popupTierSelect
	popupDataTypeSelect
	popupOverwriteSelect
	popupValueActions
	popupPoliciesActions
	popupFileAction
	popupFileWriteConfirm
	popupUnsavedChanges
	popupRandomValue
)

func (component *popupStateComponent) openShortcuts(from screen) {
	m := &component.model
	m.shortcutsFor = from
	m.shortcutsPopupFor = popupNone
	m.pushPopup(popupShortcuts)
}

func (component *popupStateComponent) openPopupShortcuts(from screen, popup popupKind) {
	m := &component.model
	m.shortcutsFor = from
	m.shortcutsPopupFor = popup
	m.pushPopup(popupShortcuts)
}

func (component *popupStateComponent) pushPopup(kind popupKind) {
	m := &component.model
	m.popupStack = nil
	m.activePopup = kind
	m.pendingKeySequence = ""
}

func (component *popupStateComponent) pushNestedPopup(kind popupKind) {
	m := &component.model
	m.popupStack = nil
	if m.activePopup != popupNone {
		m.popupStack = append(m.popupStack, m.activePopup)
	}
	m.activePopup = kind
	m.pendingKeySequence = ""
}

func (component *popupStateComponent) popPopup() {
	m := &component.model
	if len(m.popupStack) == 0 {
		m.activePopup = popupNone
		m.pendingKeySequence = ""
		return
	}
	last := len(m.popupStack) - 1
	m.activePopup = m.popupStack[last]
	m.popupStack = m.popupStack[:last]
	m.pendingKeySequence = ""
}

func (component *popupStateComponent) clearPopupStack() {
	m := &component.model
	m.activePopup = popupNone
	m.popupStack = nil
	m.pendingKeySequence = ""
}

func (component popupStateComponent) popupLayers() []popupKind {
	m := component.model
	layers := append([]popupKind(nil), m.popupStack...)
	if m.activePopup != popupNone {
		layers = append(layers, m.activePopup)
	}
	return layers
}
