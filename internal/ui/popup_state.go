package ui

import "github.com/biptec/aws-ssm-params/internal/inventory"

type popupState struct {
	activePopup popupKind
	popupStack  []popupKind

	regionCursor      int
	typeCursor        int
	tierCursor        int
	dataTypeCursor    int
	overwriteCursor   int
	randomCursor      int
	valueActionCursor int

	confirmPrompt              string
	confirmExpected            string
	confirmItems               inventory.Items
	confirmAction              confirmAction
	confirmButtonCursor        int
	confirmFocus               int
	confirmStatusIndexes       []int
	confirmStateFilterOrder    []parameterState
	confirmStateFilterSelected map[parameterState]bool

	shortcutsFor       screen
	shortcutsPopupFor  popupKind
	pendingKeySequence string
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
	popupDescriptionActions
	popupFileAction
	popupFileWriteConfirm
	popupUnsavedChanges
	popupQuitConfirm
	popupRandomValue
	popupEditor
	popupImportFile
	popupImportKeyField
	popupImportFormat
	popupImportFilePicker
	popupImportMapFields
	popupImportMapPaths
	popupImportDefaults
	popupExportFile
	popupExportKeyField
	popupExportFormat
	popupExportOutputFields
	popupExportMapFields
	popupExportMapPaths
	popupExportOverwriteConfirm
)

type confirmAction int

const (
	confirmActionDelete confirmAction = iota
	confirmActionPush
)

const (
	confirmFocusPrimaryButton = -2
	confirmFocusCancelButton  = -1
)

func (m *popupState) openShortcuts(from screen) {
	m.shortcutsFor = from
	m.shortcutsPopupFor = popupNone
	m.pushPopup(popupShortcuts)
}

func (m *popupState) openPopupShortcuts(from screen, popup popupKind) {
	m.shortcutsFor = from
	m.shortcutsPopupFor = popup
	m.pushNestedPopup(popupShortcuts)
}

func (m *popupState) pushPopup(kind popupKind) {
	m.popupStack = nil
	m.activePopup = kind
	m.pendingKeySequence = ""
}

func (m *popupState) pushNestedPopup(kind popupKind) {
	if m.activePopup != popupNone {
		m.popupStack = append(m.popupStack, m.activePopup)
	}

	m.activePopup = kind
	m.pendingKeySequence = ""
}

func (m *popupState) popPopup() {
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

func (m *popupState) clearPopupStack() {
	m.activePopup = popupNone
	m.popupStack = nil
	m.pendingKeySequence = ""
}

func (m *popupState) popupLayers() []popupKind {
	layers := append([]popupKind(nil), m.popupStack...)
	if m.activePopup != popupNone {
		layers = append(layers, m.activePopup)
	}

	return layers
}
