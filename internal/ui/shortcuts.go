package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
)

type shortcutRenderer struct {
	screen                screen
	shortcutsFor          screen
	shortcutsPopupFor     blockKind
	fileActionMode        string
	fileActionUsesButtons bool
	viInsertMode          bool
	keys                  keymap
	popupSortOptions      []sortItem
	visibleSortOptions    []sortItem
	importMainCursor      int
	exportMainCursor      int
	importDefaultsCursor  int
	importButtonsFocused  bool
	importButtonCursor    int
	importSelectorActive  bool
	editField             editField
	editorButtonsFocused  bool
	editorButtonCursor    int
	editorAreaExpanded    bool
	editorSelector        bool
	editorPopupActive     bool
	applyImmediately      bool

	importDefaultPoliciesExpanded    bool
	importDefaultDescriptionExpanded bool
}

func newShortcuts(m model) *shortcutRenderer {
	return &shortcutRenderer{
		screen:                m.screen,
		shortcutsFor:          m.shortcutsFor,
		shortcutsPopupFor:     m.shortcutsPopupFor,
		fileActionMode:        m.fileActionMode,
		fileActionUsesButtons: m.fileActionUsesButtons(),
		viInsertMode:          m.viInsertMode,
		keys:                  newKeymap(m),
		popupSortOptions:      m.popupSortItems(),
		visibleSortOptions:    m.visibleSortItems(),
		importMainCursor:      m.importMainCursor,
		exportMainCursor:      m.exportMainCursor,
		importDefaultsCursor:  m.importDefaultsCursor,
		importButtonsFocused:  m.importButtonsFocused,
		importButtonCursor:    m.importButtonCursor,
		importSelectorActive:  m.importSelectorActive(),
		editField:             m.editField,
		editorButtonsFocused:  m.editorButtonsFocused,
		editorButtonCursor:    m.editorButtonCursor,
		editorAreaExpanded:    m.isCurrentExpandableFieldExpanded(),
		editorSelector:        m.editorSelectorActive(),
		editorPopupActive:     m.editorPopupActiveOrStack(),
		applyImmediately:      m.opts.ApplyImmediately,

		importDefaultPoliciesExpanded:    m.importDefaultAreaExpanded(&m.importDefaultPolicies),
		importDefaultDescriptionExpanded: m.importDefaultAreaExpanded(&m.importDefaultDescription),
	}
}

func (m *shortcutRenderer) keymapStyle() keymapStyle {
	return m.keys.keymapStyle()
}

func (m *shortcutRenderer) popupSortItems() []sortItem {
	return m.popupSortOptions
}

func (m *shortcutRenderer) visibleSortItems() []sortItem {
	return m.visibleSortOptions
}

// mainFooterText returns shortcuts for the main table screen.
func mainFooterText(detailsShown, filtered, applyImmediately bool) string {
	return renderFooterBindings(mainFooterBindings(detailsShown, filtered, applyImmediately))
}

func filterFooterText() string {
	return renderBlockFooter(filterBlock)
}

func (m *shortcutRenderer) popupFooterText(kind blockKind) string {
	return renderFooterBindings(m.popupFooterBindings(kind))
}

func (m *shortcutRenderer) popupFooterBindings(kind blockKind) []key.Binding {
	switch kind {
	case popupNone:
		return nil
	case popupSort:
		return sortFooterBindings(m.popupSortItems())
	case popupValueActions, popupPoliciesActions, popupDescriptionActions:
		return m.editorActionPopupBindings(kind, false)
	case popupRandomValue:
		return m.randomValueBindings(false)
	case popupEditor:
		return m.editorPopupFooterBindings()
	case popupFileAction:
		return m.fileActionBindings(false)
	case popupFileWriteConfirm:
		return m.fileWriteConfirmBindings(false)
	case popupUnsavedChanges:
		return m.unsavedChangesBindings(false)
	case popupRegionSelect, popupTypeSelect, popupTierSelect, popupDataTypeSelect, popupOverwriteSelect:
		return m.selectorFooterBindings(kind)
	case popupImportFile:
		return importFileFooterBindings(m.importFileEnterAction())
	case popupImportKeyField:
		return selectorFooterBindings(m.importSelectorEnterAction("select"))
	case popupImportFormat:
		return formatFooterBindings(m.importSelectorEnterAction("select"))
	case popupImportFilePicker:
		return filePickerFooterBindings(m.filePickerParentShortcut())
	case popupImportDefaults:
		return m.importDefaultsFooterBindings()
	case popupImportMapFields, popupImportMapPaths:
		return m.mappingPopupFooterBindings(kind)
	case popupExportFile:
		return exportFileFooterBindings(m.exportFileEnterAction())
	case popupExportKeyField:
		return selectorFooterBindings(m.importSelectorEnterAction("select"))
	case popupExportFormat:
		return formatFooterBindings(m.importSelectorEnterAction("select"))
	case popupExportOutputFields, popupExportMapFields:
		return m.exportOutputOrMapFieldsFooterBindings(kind)
	case popupExportMapPaths:
		return m.mappingPopupFooterBindings(kind)
	case popupExportOverwriteConfirm:
		return overwriteFooterBindings(m.importFocusedButtonAction("overwrite"))
	case parameterListBlock,
		selectedParameterBlock,
		filterBlock,
		editorBlock,
		columnsBlock,
		confirmBlock,
		regionSelectBlock,
		typeSelectBlock,
		loadingBlock,
		popupColumns,
		popupShortcuts,
		popupConfirm,
		popupQuitConfirm:
		return bindingsForBlock(kind)
	}

	return nil
}

func (m *shortcutRenderer) editorActionPopupBindings(kind blockKind, helpText bool) []key.Binding {
	actions := []key.Binding{}
	if m.editorPopupActive {
		actions = append(actions, editorActionPopupFooterBindings(true, m.editorSelectorEnterAction("select"))...)
	} else {
		actions = append(actions, editorActionPopupFooterBindings(false, "select focused action")...)
	}

	actionEnd := len(actions) - 1
	base := actions[:actionEnd]
	cancel := actions[actionEnd]

	if kind == popupValueActions {
		base = append(base, clearValueShortcut, randomValueShortcut, loadFromFileShortcut, writeToFileShortcut)
	}

	if kind == popupPoliciesActions {
		base = append(base, clearPoliciesShortcut, loadFromFileShortcut, writeToFileShortcut)
	}

	if kind == popupDescriptionActions {
		base = append(base, clearDescriptionShortcut, loadFromFileShortcut, writeToFileShortcut)
	}

	if helpText && m.editorPopupActive {
		base = append(base, tabActionsButtonsShortcut)
	}

	return append(base, cancel)
}

func (m *shortcutRenderer) randomValueBindings(helpText bool) []key.Binding {
	if m.editorPopupActive {
		return randomValueFooterBindings(true, m.editorSelectorEnterAction("select"), helpText)
	}

	return randomValueFooterBindings(false, "select focused option", false)
}

func (m *shortcutRenderer) fileActionBindings(helpText bool) []key.Binding {
	if !m.fileActionUsesButtons {
		return inputConfirmFooterBindings("confirm input")
	}

	primaryAction := m.fileActionPrimaryAction()

	return fileActionFooterBindings(primaryAction, m.fileActionEnterAction(primaryAction), helpText)
}

func (m *shortcutRenderer) fileActionPrimaryAction() string {
	switch m.fileActionMode {
	case "load":
		return "load"
	case "write":
		return "write"
	case "random-custom":
		return "generate"
	default:
		return "confirm"
	}
}

func (m *shortcutRenderer) fileActionEnterAction(primaryAction string) string {
	if m.fileActionMode == "random-custom" {
		return "confirm input"
	}

	if m.editorButtonsFocused {
		return m.editorFocusedButtonAction(primaryAction)
	}

	return "browse"
}

func (m *shortcutRenderer) fileWriteConfirmBindings(helpText bool) []key.Binding {
	enterAction := "yes"
	if m.editorPopupActive {
		enterAction = m.editorSelectorEnterAction("yes")
	}

	return fileWriteConfirmFooterBindings(enterAction, helpText && m.editorPopupActive)
}

func (m *shortcutRenderer) unsavedChangesBindings(helpText bool) []key.Binding {
	return unsavedChangesFooterBindings(m.editorSelectorEnterAction("discard"), helpText)
}

func (m *shortcutRenderer) selectorFooterBindings(kind blockKind) []key.Binding {
	if m.importSelectorActive {
		return selectorFooterBindings(m.importSelectorEnterAction("select"), selectorOptionBindings(kind)...)
	} else if m.editorSelector {
		return selectorFooterBindings(m.editorSelectorEnterAction("select"), selectorOptionBindings(kind)...)
	}

	return selectorFallbackFooterBindings(selectorOptionBindings(kind)...)
}

func selectorOptionBindings(kind blockKind) []key.Binding {
	if bindings, ok := selectorOptionBindingsByKind[kind]; ok {
		return bindings
	}

	return nil
}

var selectorOptionBindingsByKind = map[blockKind][]key.Binding{
	popupTypeSelect:      typeSelectorOptionBindings(),
	popupTierSelect:      tierSelectorOptionBindings(),
	popupDataTypeSelect:  dataTypeSelectorOptionBindings(),
	popupOverwriteSelect: overwriteSelectorOptionBindings(),
}

func (m *shortcutRenderer) editorPopupFooterBindings() []key.Binding {
	return editorPopupFooterBindings(m.editorPopupEnterAction(), m.editorPopupFocusedActionShortcut())
}

func (m *shortcutRenderer) editorPopupEnterAction() string {
	if m.editorButtonsFocused {
		return m.editorFocusedButtonAction("save")
	}

	switch m.editField {
	case editFieldRegion:
		return "choose region"
	case editFieldType:
		return "choose type"
	case editFieldTier:
		return "choose tier"
	case editFieldDataType:
		return "choose data type"
	case editFieldOverwrite:
		return "choose overwrite"
	case editFieldDescription, editFieldPolicies, editFieldValue:
		if m.editorAreaExpanded {
			return "newline"
		}

		return "expand/newline"
	case editFieldSSMPath, editFieldFilePath:
		return "next"
	}

	return "next"
}

func (m *shortcutRenderer) editorPopupFocusedActionShortcut() key.Binding {
	if m.editorButtonsFocused {
		return key.Binding{}
	}

	return editorFieldActionShortcut(m.editField)
}

func (m *shortcutRenderer) importDefaultsFooterBindings() []key.Binding {
	enterAction := "apply"

	switch {
	case m.importButtonsFocused:
		enterAction = m.importFocusedButtonAction("apply")
	case m.importDefaultsCursor >= 0 && m.importDefaultsCursor <= 3:
		enterAction = "select"
	case m.importDefaultsCursor == 4 || m.importDefaultsCursor == 5:
		enterAction = "expand/newline"
		if m.importDefaultAreaExpanded() {
			enterAction = "newline"
		}

		return importChildFooterBindings(enterAction, altActionsShortcut)
	}

	return importChildFooterBindings(enterAction)
}

func (m *shortcutRenderer) mappingPopupFooterBindings(kind blockKind) []key.Binding {
	enterAction := m.importSelectorEnterAction("apply")
	if kind == popupImportMapFields && !m.importButtonsFocused {
		enterAction = "new line"
	}

	if (kind == popupImportMapPaths || kind == popupExportMapPaths) && !m.importButtonsFocused {
		enterAction = "next input"
	}

	return importChildFooterBindings(enterAction)
}

func (m *shortcutRenderer) exportOutputOrMapFieldsFooterBindings(kind blockKind) []key.Binding {
	enterAction := m.importSelectorEnterAction("apply")
	if !m.importButtonsFocused {
		enterAction = "toggle"
		if kind == popupExportMapFields {
			enterAction = "new line"
		}
	}

	return importChildFooterBindings(enterAction)
}

func (m *shortcutRenderer) importFileEnterAction() string {
	if m.importButtonsFocused {
		return m.importFocusedButtonAction("load")
	}

	if importMainField(m.importMainCursor) == importMainFieldFilePath {
		return "browse"
	}

	return "open"
}

func (m *shortcutRenderer) exportFileEnterAction() string {
	if m.importButtonsFocused {
		return m.importFocusedButtonAction("export")
	}

	if exportMainField(m.exportMainCursor) == exportMainFieldFilePath {
		return "browse"
	}

	return "open"
}

func (m *shortcutRenderer) importSelectorEnterAction(primary string) string {
	if m.importButtonsFocused {
		return m.importFocusedButtonAction(primary)
	}

	return primary
}

func (m *shortcutRenderer) editorSelectorEnterAction(primary string) string {
	if m.editorButtonsFocused {
		return m.editorFocusedButtonAction(primary)
	}

	return primary
}

func (m *shortcutRenderer) editorFocusedButtonAction(primary string) string {
	if m.editorButtonCursor == importActionCancel {
		return "cancel"
	}

	return primary
}

func (m *shortcutRenderer) importFocusedButtonAction(primary string) string {
	if m.importButtonCursor == importActionCancel {
		return "cancel"
	}

	return primary
}

func (m *shortcutRenderer) filePickerParentShortcut() key.Binding {
	return filePickerParentShortcut(m.keymapStyle())
}

// shortcutsText returns the context-sensitive shortcut reference shown by the Shortcuts screen.
func (m *shortcutRenderer) shortcutsText() string {
	forScreen := m.shortcutsFor
	if forScreen == 0 && m.screen == screenHelp {
		forScreen = screenMain
	}

	if m.shortcutsPopupFor != popupNone {
		return m.popupShortcutsText(m.shortcutsPopupFor)
	}

	sections := []string{m.actionsShortcuts(forScreen), m.sortShortcuts(forScreen), m.navigationShortcuts(forScreen), renderGlobalShortcuts()}

	return joinShortcutSections(sections...)
}

func (m *shortcutRenderer) popupShortcutsText(kind blockKind) string {
	sections := []string{m.popupActionsShortcuts(kind), m.popupSortShortcuts(kind), m.popupNavigationShortcuts(kind), renderGlobalShortcuts()}

	return joinShortcutSections(sections...)
}

func joinShortcutSections(sections ...string) string {
	out := []string{}

	for _, section := range sections {
		section = strings.TrimSpace(section)
		if section == "" {
			continue
		}

		if len(out) > 0 {
			out = append(out, "")
		}

		out = append(out, strings.Split(section, "\n")...)
	}

	return strings.Join(out, "\n")
}

func (m *shortcutRenderer) popupActionsShortcuts(kind blockKind) string {
	bindings := m.popupActionBindings(kind)
	if len(bindings) == 0 {
		return ""
	}

	return renderActionsShortcutSection(bindings)
}

func (m *shortcutRenderer) popupActionBindings(kind blockKind) []key.Binding {
	switch kind {
	case popupNone, popupShortcuts:
		return nil
	case popupValueActions, popupPoliciesActions, popupDescriptionActions:
		return m.editorActionPopupBindings(kind, true)
	case popupRandomValue:
		return m.randomValueBindings(true)
	case popupEditor:
		return m.editorPopupActionBindings()
	case popupFileAction:
		return m.fileActionBindings(true)
	case popupFileWriteConfirm:
		return m.fileWriteConfirmBindings(true)
	case popupUnsavedChanges:
		return m.unsavedChangesBindings(true)
	case popupTypeSelect, popupTierSelect, popupDataTypeSelect, popupOverwriteSelect, popupRegionSelect:
		return selectorActionBindings(kind)
	case popupImportFile:
		return importFileActionBindings(m.importFileEnterAction())
	case popupImportKeyField, popupExportKeyField:
		return selectOptionActionBindings(m.importSelectorEnterAction("select"))
	case popupImportFormat, popupExportFormat:
		return formatActionBindings(m.importSelectorEnterAction("select"))
	case popupImportFilePicker:
		return filePickerActionBindings()
	case popupImportMapFields, popupImportMapPaths, popupImportDefaults, popupExportOutputFields, popupExportMapFields, popupExportMapPaths:
		return m.importChildActionBindings(kind)
	case popupExportFile:
		return exportFileActionBindings(m.exportFileEnterAction())
	case popupExportOverwriteConfirm:
		return exportOverwriteActionBindings()
	case parameterListBlock,
		selectedParameterBlock,
		filterBlock,
		editorBlock,
		columnsBlock,
		confirmBlock,
		regionSelectBlock,
		typeSelectBlock,
		loadingBlock,
		popupColumns,
		popupConfirm,
		popupSort,
		popupQuitConfirm:
		bindings := bindingsForBlock(kind)
		if len(bindings) == 0 {
			return []key.Binding{cancelShortcut}
		}

		return bindings
	}

	return []key.Binding{cancelShortcut}
}

func (m *shortcutRenderer) editorPopupActionBindings() []key.Binding {
	bindings := editorPopupActionBindings(m.editorPopupEnterAction())

	if action := m.editorPopupFocusedActionShortcut(); action.Help().Key != "" {
		bindings = append(bindings, focusedFieldActionsShortcut)
	}

	return append(bindings, tabFieldsButtonsShortcut, cancelShortcut)
}

func selectorActionBindings(kind blockKind) []key.Binding {
	return selectFocusedOptionActionBindings(kind)
}

func (m *shortcutRenderer) importChildActionBindings(kind blockKind) []key.Binding {
	enterAction := "apply values"
	tabAction := tabFieldsButtonsShortcut

	if (kind == popupImportMapFields || kind == popupExportMapFields) && !m.importButtonsFocused {
		enterAction = "move to next line"
	}

	if kind == popupExportOutputFields && !m.importButtonsFocused {
		enterAction = "toggle"
	}

	if (kind == popupImportMapPaths || kind == popupExportMapPaths) && !m.importButtonsFocused {
		enterAction = "move to next input"
		tabAction = tabRowsButtonsShortcut
	}

	if kind == popupImportDefaults && m.importDefaultsCursor >= 0 && m.importDefaultsCursor <= 3 {
		enterAction = "choose focused option"
	}

	if kind == popupImportDefaults && (m.importDefaultsCursor == 4 || m.importDefaultsCursor == 5) {
		enterAction = "expand/newline in focused text area"
		if m.importDefaultAreaExpanded() {
			enterAction = "insert newline"
		}

		return importChildActionBindings(enterAction, tabAction, altActionsShortcut)
	}

	return importChildActionBindings(enterAction, tabAction)
}

func (m *shortcutRenderer) importDefaultAreaExpanded() bool {
	switch m.importDefaultsCursor {
	case 4:
		return m.importDefaultPoliciesExpanded
	case 5:
		return m.importDefaultDescriptionExpanded
	default:
		return false
	}
}

func (m *shortcutRenderer) popupSortShortcuts(kind blockKind) string {
	if kind != popupSort {
		return ""
	}

	return renderSortShortcutSection(m.popupSortItems())
}

func (m *shortcutRenderer) popupNavigationShortcuts(kind blockKind) string {
	switch kind {
	case popupNone, popupShortcuts, popupConfirm, popupFileAction, popupFileWriteConfirm, popupUnsavedChanges, popupQuitConfirm:
		return ""
	case popupSort,
		popupColumns,
		popupRegionSelect,
		popupTypeSelect,
		popupTierSelect,
		popupDataTypeSelect,
		popupOverwriteSelect,
		popupValueActions,
		popupPoliciesActions,
		popupDescriptionActions,
		popupRandomValue,
		popupImportKeyField,
		popupImportFormat,
		popupExportKeyField,
		popupExportFormat,
		popupExportOutputFields:
		if m.editorPopupActive && kind != popupSort && kind != popupColumns {
			return renderNavigationShortcutSection(optionNavigationBindings(m.keymapStyle()))
		}

		return renderNavigationShortcutSection(rowNavigationBindings(m.keymapStyle()))
	case popupImportFilePicker:
		return renderNavigationShortcutSection(filePickerNavigationBindings(m.keymapStyle()))
	case popupEditor:
		return renderNavigationShortcutSection(editorPopupNavigationBindings(m.keymapStyle(), m.editorButtonsFocused, m.editField, m.viInsertMode))
	case popupImportFile, popupImportMapFields, popupImportMapPaths, popupImportDefaults, popupExportFile, popupExportMapFields, popupExportMapPaths:
		return renderNavigationShortcutSection(fieldNavigationBindings(m.keymapStyle()))
	case parameterListBlock,
		selectedParameterBlock,
		filterBlock,
		editorBlock,
		columnsBlock,
		confirmBlock,
		regionSelectBlock,
		typeSelectBlock,
		loadingBlock,
		popupExportOverwriteConfirm:
		return ""
	}

	return ""
}

func (m *shortcutRenderer) actionsShortcuts(forScreen screen) string {
	bindings := screenActionBindings(forScreen, m.keymapStyle(), m.viInsertMode, m.applyImmediately)
	if len(bindings) == 0 {
		return ""
	}

	return renderActionsShortcutSection(bindings)
}

func (m *shortcutRenderer) sortShortcuts(forScreen screen) string {
	if forScreen != screenMain {
		return ""
	}

	return renderSortShortcutSection(m.visibleSortItems())
}

func (m *shortcutRenderer) navigationShortcuts(forScreen screen) string {
	switch forScreen {
	case screenMain, screenColumns, screenRegionSelect, screenTypeSelect:
		return renderNavigationShortcutSection(rowNavigationBindings(m.keymapStyle()))
	case screenTextArea:
		return renderShortcutSections(textAreaNavigationSections(m.keymapStyle()))
	case screenLoading, screenConfirm, screenHelp:
		return ""
	}

	return ""
}
