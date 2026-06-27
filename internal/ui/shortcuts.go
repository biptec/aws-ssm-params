package ui

import (
	"fmt"
	"strings"
)

type shortcuts struct {
	screen               screen
	shortcutsFor         screen
	shortcutsPopupFor    popupKind
	fileActionMode       string
	fileActionUsesButtons bool
	viInsertMode         bool
	keys                 keymap
	popupSortOptions     []sortItem
	visibleSortOptions   []sortItem
	importMainCursor     int
	exportMainCursor     int
	importDefaultsCursor int
	importButtonsFocused bool
	importButtonCursor   int
	importSelectorActive bool
	editField            editField
	editorButtonsFocused bool
	editorButtonCursor   int
	editorAreaExpanded   bool
	editorSelector       bool
	editorPopupActive    bool

	importDefaultPoliciesExpanded    bool
	importDefaultDescriptionExpanded bool
}

func newShortcuts(m model) *shortcuts {
	return &shortcuts{
		screen:               m.screen,
		shortcutsFor:         m.shortcutsFor,
		shortcutsPopupFor:    m.shortcutsPopupFor,
		fileActionMode:       m.fileActionMode,
		fileActionUsesButtons: m.fileActionUsesButtons(),
		viInsertMode:         m.viInsertMode,
		keys:                 newKeymap(m),
		popupSortOptions:     m.popupSortItems(),
		visibleSortOptions:   m.visibleSortItems(),
		importMainCursor:     m.importMainCursor,
		exportMainCursor:     m.exportMainCursor,
		importDefaultsCursor: m.importDefaultsCursor,
		importButtonsFocused: m.importButtonsFocused,
		importButtonCursor:   m.importButtonCursor,
		importSelectorActive: m.importSelectorActive(),
		editField:            m.editField,
		editorButtonsFocused: m.editorButtonsFocused,
		editorButtonCursor:   m.editorButtonCursor,
		editorAreaExpanded:   m.isCurrentExpandableFieldExpanded(),
		editorSelector:       m.editorSelectorActive(),
		editorPopupActive:    m.editorPopupActiveOrStack(),

		importDefaultPoliciesExpanded:    m.importDefaultAreaExpanded(&m.importDefaultPolicies),
		importDefaultDescriptionExpanded: m.importDefaultAreaExpanded(&m.importDefaultDescription),
	}
}

func (m *shortcuts) keymapStyle() keymapStyle {
	return m.keys.keymapStyle()
}

func (m *shortcuts) popupSortItems() []sortItem {
	return m.popupSortOptions
}

func (m *shortcuts) visibleSortItems() []sortItem {
	return m.visibleSortOptions
}

// mainFooterText returns shortcuts for the main table screen.
func mainFooterText(detailsShown, filtered bool) string {
	detailsAction := "d show details"
	if detailsShown {
		detailsAction = "d hide details"
	}

	scope := "all"
	if filtered {
		scope = "filtered"
	}

	return "ctrl+/ help • enter edit • n new • i import • e export • " + detailsAction + " • / filter • f filter • c columns • s sort • x delete • X delete " + scope + " • r revert • R revert " + scope + " • p push • P push " + scope + " • esc quit"
}

func filterFooterText() string {
	return "ctrl+/ help • esc close"
}

func (m *shortcuts) popupFooterText(kind popupKind) string {
	switch kind {
	case popupNone:
		return ""
	case popupShortcuts:
		return "esc close"
	case popupSort:
		return m.sortPopupScreenFooter()
	case popupColumns:
		return "ctrl+/ help • ctrl+m close • enter focused control • space toggle • a all • x none • esc close"
	case popupValueActions:
		if m.editorPopupActive {
			return "ctrl+/ help • ctrl+m select • enter " + m.editorSelectorEnterAction("select") + " • c clear • r random • l load • w write • esc cancel"
		}

		return "ctrl+/ help • enter select • c clear • r random • l load • w write • esc cancel"
	case popupPoliciesActions:
		if m.editorPopupActive {
			return "ctrl+/ help • ctrl+m select • enter " + m.editorSelectorEnterAction("select") + " • c clear • l load • w write • esc cancel"
		}

		return "ctrl+/ help • enter select • c clear • l load • w write • esc cancel"
	case popupDescriptionActions:
		if m.editorPopupActive {
			return "ctrl+/ help • ctrl+m select • enter " + m.editorSelectorEnterAction("select") + " • c clear • l load • w write • esc cancel"
		}

		return "ctrl+/ help • enter select • c clear • l load • w write • esc cancel"
	case popupRandomValue:
		if m.editorPopupActive {
			return "ctrl+/ help • ctrl+m select • enter " + m.editorSelectorEnterAction("select") + " • b base64 • x hex • u uuid • c custom • esc cancel"
		}

		return "ctrl+/ help • enter select • b base64 • x hex • u uuid • c custom • esc cancel"
	case popupEditor:
		return m.editorPopupFooterText()
	case popupFileAction:
		button := "confirm"

		switch m.fileActionMode {
		case "load":
			button = "load"
		case "write":
			button = "write"
		case "random-custom":
			button = "generate"
		}

		if m.fileActionUsesButtons {
			enterAction := m.editorSelectorEnterAction(button)
			if !m.editorButtonsFocused && m.fileActionMode != "random-custom" {
				enterAction = "browse"
			}

			return "ctrl+/ help • ctrl+m " + button + " • enter " + enterAction + " • esc cancel"
		}

		return "ctrl+/ help • enter " + button + " • esc cancel"
	case popupFileWriteConfirm:
		if m.editorPopupActive {
			return "ctrl+/ help • ctrl+m yes • enter " + m.editorSelectorEnterAction("yes") + " • esc cancel"
		}

		return "ctrl+/ help • enter yes • esc cancel"
	case popupUnsavedChanges:
		return "ctrl+/ help • ctrl+m discard • enter " + m.editorSelectorEnterAction("discard") + " • esc cancel"
	case popupQuitConfirm:
		return "ctrl+/ help • ctrl+m quit • enter focused button • esc cancel"
	case popupConfirm:
		return "ctrl+/ help • ctrl+m confirm • enter focused control • space toggle • esc cancel"
	case popupRegionSelect:
		if m.importSelectorActive {
			return "ctrl+/ help • ctrl+m select • enter " + m.importSelectorEnterAction("select") + " • esc cancel"
		}

		if m.editorSelector {
			return "ctrl+/ help • ctrl+m select • enter " + m.editorSelectorEnterAction("select") + " • esc cancel"
		}

		return "ctrl+/ help • enter select • esc cancel"
	case popupTypeSelect:
		if m.importSelectorActive {
			return "ctrl+/ help • ctrl+m select • enter " + m.importSelectorEnterAction("select") + " • e secure • s string • l list • esc cancel"
		}

		if m.editorSelector {
			return "ctrl+/ help • ctrl+m select • enter " + m.editorSelectorEnterAction("select") + " • e secure • s string • l list • esc cancel"
		}

		return "ctrl+/ help • enter select • e secure • s string • l list • esc cancel"
	case popupTierSelect:
		if m.importSelectorActive {
			return "ctrl+/ help • ctrl+m select • enter " + m.importSelectorEnterAction("select") + " • i intelligent • s standard • a advanced • esc cancel"
		}

		if m.editorSelector {
			return "ctrl+/ help • ctrl+m select • enter " + m.editorSelectorEnterAction("select") + " • i intelligent • s standard • a advanced • esc cancel"
		}

		return "ctrl+/ help • enter select • i intelligent • s standard • a advanced • esc cancel"
	case popupDataTypeSelect:
		if m.importSelectorActive {
			return "ctrl+/ help • ctrl+m select • enter " + m.importSelectorEnterAction("select") + " • t text • a AMI • i integration • esc cancel"
		}

		if m.editorSelector {
			return "ctrl+/ help • ctrl+m select • enter " + m.editorSelectorEnterAction("select") + " • t text • a AMI • i integration • esc cancel"
		}

		return "ctrl+/ help • enter select • t text • a AMI • i integration • esc cancel"
	case popupOverwriteSelect:
		if m.editorSelector {
			return "ctrl+/ help • ctrl+m select • enter " + m.editorSelectorEnterAction("select") + " • t true • f false • esc cancel"
		}

		return "ctrl+/ help • enter select • t true • f false • esc cancel"
	case popupImportFile:
		return "ctrl+/ help • ctrl+m load • enter " + m.importFileEnterAction() + " • esc cancel"
	case popupImportKeyField:
		return "ctrl+/ help • ctrl+m select • enter " + m.importSelectorEnterAction("select") + " • esc cancel"
	case popupImportFormat:
		return "ctrl+/ help • ctrl+m select • enter " + m.importSelectorEnterAction("select") + " • d dotenv • j json • y yaml • esc cancel"
	case popupImportFilePicker:
		return "ctrl+/ help • enter select/open • tab buttons • " + m.filePickerParentFooterShortcut() + " parent • esc cancel"
	case popupImportDefaults:
		enterAction := "apply"
		if m.importButtonsFocused {
			enterAction = m.importFocusedButtonAction("apply")
		}

		if !m.importButtonsFocused && m.importDefaultsCursor >= 0 && m.importDefaultsCursor <= 3 {
			enterAction = "select"
		}

		if !m.importButtonsFocused && (m.importDefaultsCursor == 4 || m.importDefaultsCursor == 5) {
			enterAction = "expand/newline"
			if m.importDefaultAreaExpanded() {
				enterAction = "newline"
			}

			return "ctrl+/ help • ctrl+m apply • enter " + enterAction + " • alt+e actions • esc cancel"
		}

		return "ctrl+/ help • ctrl+m apply • enter " + enterAction + " • esc cancel"
	case popupImportMapFields, popupImportMapPaths:
		enterAction := m.importSelectorEnterAction("apply")
		if kind == popupImportMapFields && !m.importButtonsFocused {
			enterAction = "new line"
		}

		if kind == popupImportMapPaths && !m.importButtonsFocused {
			enterAction = "next input"
		}

		return "ctrl+/ help • ctrl+m apply • enter " + enterAction + " • esc cancel"
	case popupExportFile:
		return "ctrl+/ help • ctrl+m export • enter " + m.exportFileEnterAction() + " • esc cancel"
	case popupExportKeyField:
		return "ctrl+/ help • ctrl+m select • enter " + m.importSelectorEnterAction("select") + " • esc cancel"
	case popupExportFormat:
		return "ctrl+/ help • ctrl+m select • enter " + m.importSelectorEnterAction("select") + " • d dotenv • j json • y yaml • esc cancel"
	case popupExportOutputFields, popupExportMapFields:
		enterAction := m.importSelectorEnterAction("apply")
		if !m.importButtonsFocused {
			enterAction = "toggle"
			if kind == popupExportMapFields {
				enterAction = "new line"
			}
		}

		return "ctrl+/ help • ctrl+m apply • enter " + enterAction + " • esc cancel"
	case popupExportMapPaths:
		enterAction := m.importSelectorEnterAction("apply")
		if !m.importButtonsFocused {
			enterAction = "next input"
		}

		return "ctrl+/ help • ctrl+m apply • enter " + enterAction + " • esc cancel"
	case popupExportOverwriteConfirm:
		return "ctrl+/ help • ctrl+m overwrite • enter " + m.importFocusedButtonAction("overwrite") + " • esc cancel"
	default:
		return "ctrl+/ help • esc cancel"
	}
}

func (m *shortcuts) editorPopupFooterText() string {
	enterAction := "next"
	if m.editorButtonsFocused {
		if m.editorButtonCursor == importActionCancel {
			enterAction = "cancel"
		} else {
			enterAction = "save"
		}
	} else {
		switch m.editField {
		case editFieldRegion:
			enterAction = "choose region"
		case editFieldType:
			enterAction = "choose type"
		case editFieldTier:
			enterAction = "choose tier"
		case editFieldDataType:
			enterAction = "choose data type"
		case editFieldOverwrite:
			enterAction = "choose overwrite"
		case editFieldDescription, editFieldPolicies, editFieldValue:
			enterAction = "expand/newline"
			if m.editorAreaExpanded {
				enterAction = "newline"
			}
		}
	}

	parts := []string{"ctrl+/ help", "ctrl+s save", "enter " + enterAction}
	if !m.editorButtonsFocused {
		switch m.editField {
		case editFieldDescription:
			parts = append(parts, "alt+e description actions")
		case editFieldPolicies:
			parts = append(parts, "alt+e policies actions")
		case editFieldValue:
			parts = append(parts, "alt+e value actions")
		}
	}

	return strings.Join(append(parts, "esc cancel"), " • ")
}

func (m *shortcuts) importFileEnterAction() string {
	if m.importButtonsFocused {
		return m.importFocusedButtonAction("load")
	}

	if importMainField(m.importMainCursor) == importMainFieldFilePath {
		return "browse"
	}

	return "open"
}

func (m *shortcuts) exportFileEnterAction() string {
	if m.importButtonsFocused {
		return m.importFocusedButtonAction("export")
	}

	if exportMainField(m.exportMainCursor) == exportMainFieldFilePath {
		return "browse"
	}

	return "open"
}

func (m *shortcuts) importSelectorEnterAction(primary string) string {
	if m.importButtonsFocused {
		return m.importFocusedButtonAction(primary)
	}

	return primary
}

func (m *shortcuts) editorSelectorEnterAction(primary string) string {
	if m.editorButtonsFocused {
		return m.editorFocusedButtonAction(primary)
	}

	return primary
}

func (m *shortcuts) editorFocusedButtonAction(primary string) string {
	if m.editorButtonCursor == importActionCancel {
		return "cancel"
	}

	return primary
}

func (m *shortcuts) importFocusedButtonAction(primary string) string {
	if m.importButtonCursor == importActionCancel {
		return "cancel"
	}

	return primary
}

func (m *shortcuts) filePickerParentFooterShortcut() string {
	if m.keymapStyle() == keymapVi {
		return "h/←/backspace"
	}

	return "←/backspace"
}

func (m *shortcuts) sortPopupScreenFooter() string {
	sortItems := m.popupSortItems()
	parts := make([]string, 0, 2+len(sortItems)+1)

	parts = append(parts, "ctrl+/ help", "ctrl+m close", "enter focused control", "d direction")
	for _, item := range sortItems {
		parts = append(parts, item.hotkey+" "+strings.ToLower(item.label))
	}

	parts = append(parts, "esc close")

	return strings.Join(parts, " • ")
}

// shortcutsText returns the context-sensitive shortcut reference shown by the Shortcuts screen.
func (m *shortcuts) shortcutsText() string {
	forScreen := m.shortcutsFor
	if forScreen == 0 && m.screen == screenHelp {
		forScreen = screenMain
	}

	if m.shortcutsPopupFor != popupNone {
		return m.popupShortcutsText(m.shortcutsPopupFor)
	}

	sections := []string{m.actionsShortcuts(forScreen), m.sortShortcuts(forScreen), m.navigationShortcuts(forScreen), globalShortcuts()}
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

func (m *shortcuts) popupShortcutsText(kind popupKind) string {
	sections := []string{m.popupActionsShortcuts(kind), m.popupSortShortcuts(kind), m.popupNavigationShortcuts(kind), globalShortcuts()}
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

func (m *shortcuts) popupActionsShortcuts(kind popupKind) string {
	switch kind {
	case popupNone, popupShortcuts:
		return ""
	case popupConfirm:
		return strings.TrimSpace(`Actions
  ctrl+m       confirm
  enter        focused control
  space        toggle focused checkbox
  tab          move between controls
  esc / q / ctrl+g  cancel`)
	case popupSort:
		return strings.TrimSpace(`Actions
  ctrl+m       close
  enter        toggle focused column / focused button
  d            toggle selected direction
  tab          move between list and buttons
  esc / q / ctrl+g  close`)
	case popupColumns:
		return strings.TrimSpace(`Actions
  ctrl+m       close
  enter        toggle focused column / focused button
  space        toggle focused column
  a            show all columns
  x            hide all optional columns
  tab          move between list and buttons
  esc / q / ctrl+g  close`)
	case popupValueActions:
		if m.editorPopupActive {
			return strings.TrimSpace(`Actions
  ctrl+m       select focused action
  enter        select focused action / focused button
  c            clear value
  r            random value
  l            load from file
  w            write to file
  tab          move between actions and buttons
  esc / q / ctrl+g  cancel`)
		}

		return strings.TrimSpace(`Actions
  enter        select focused action
  c            clear value
  r            random value
  l            load from file
  w            write to file
  esc / q / ctrl+g  cancel`)
	case popupPoliciesActions:
		if m.editorPopupActive {
			return strings.TrimSpace(`Actions
  ctrl+m       select focused action
  enter        select focused action / focused button
  c            clear policies
  l            load from file
  w            write to file
  tab          move between actions and buttons
  esc / q / ctrl+g  cancel`)
		}

		return strings.TrimSpace(`Actions
  enter        select focused action
  c            clear policies
  l            load from file
  w            write to file
  esc / q / ctrl+g  cancel`)
	case popupDescriptionActions:
		if m.editorPopupActive {
			return strings.TrimSpace(`Actions
  ctrl+m       select focused action
  enter        select focused action / focused button
  c            clear description
  l            load from file
  w            write to file
  tab          move between actions and buttons
  esc / q / ctrl+g  cancel`)
		}

		return strings.TrimSpace(`Actions
  enter        select focused action
  c            clear description
  l            load from file
  w            write to file
  esc / q / ctrl+g  cancel`)
	case popupFileAction:
		if m.fileActionUsesButtons {
			primaryAction := "confirm input"
			switch m.fileActionMode {
			case "load":
				primaryAction = "load"
			case "write":
				primaryAction = "write"
			case "random-custom":
				primaryAction = "generate"
			}

			enterAction := "open file picker"
			if m.fileActionMode == "random-custom" {
				enterAction = "confirm input"
			}

			if m.editorButtonsFocused {
				enterAction = "focused button"
			}

			return strings.TrimSpace(fmt.Sprintf(`Actions
  ctrl+m       %s
  enter        %s
  tab          move between input and buttons
  esc / q / ctrl+g  cancel`, primaryAction, enterAction))
		}

		return strings.TrimSpace(`Actions
  enter        confirm input
  esc / q / ctrl+g  cancel`)
	case popupFileWriteConfirm:
		if m.editorPopupActive {
			return strings.TrimSpace(`Actions
  ctrl+m       yes / continue
  enter        yes / continue / focused button
  y            yes / continue
  tab          move between buttons
  esc / q / ctrl+g  cancel`)
		}

		return strings.TrimSpace(`Actions
  enter        yes / continue
  y            yes / continue
  esc / q / ctrl+g  cancel`)
	case popupUnsavedChanges:
		return strings.TrimSpace(`Actions
  ctrl+m       discard changes
  enter        discard changes / focused button
  tab          move between buttons
  esc / q / ctrl+g  cancel`)
	case popupQuitConfirm:
		return strings.TrimSpace(`Actions
  ctrl+m       quit
  enter        focused button
  tab          move between buttons
  esc / q / ctrl+g  cancel`)
	case popupRandomValue:
		if m.editorPopupActive {
			return strings.TrimSpace(`Actions
  ctrl+m       select focused option
  enter        select focused option / focused button
  b            base64 32 bytes
  x            hex 32 bytes
  u            uuid
  c            custom length base64
  tab          move between options and buttons
  esc / q / ctrl+g  cancel`)
		}

		return strings.TrimSpace(`Actions
  enter        select focused option
  b            base64 32 bytes
  x            hex 32 bytes
  u            uuid
  c            custom length base64
  esc / q / ctrl+g  cancel`)
	case popupEditor:
		return m.editorPopupActionsShortcuts()
	case popupTypeSelect:
		return strings.TrimSpace(`Actions
  enter        select focused option
  e            SecureString
  s            String
  l            StringList
  esc / q / ctrl+g  cancel`)
	case popupTierSelect:
		return strings.TrimSpace(`Actions
  enter        select focused option
  i            Intelligent-Tiering
  s            Standard
  a            Advanced
  esc / q / ctrl+g  cancel`)
	case popupDataTypeSelect:
		return strings.TrimSpace(`Actions
  enter        select focused option
  t            text
  a            aws:ec2:image
  i            aws:ssm:integration
  esc / q / ctrl+g  cancel`)
	case popupOverwriteSelect:
		return strings.TrimSpace(`Actions
  enter        select focused option
  t            true
  f            false
  esc / q / ctrl+g  cancel`)
	case popupRegionSelect:
		return strings.TrimSpace(`Actions
  enter        select focused option
  esc / q / ctrl+g  cancel`)
	case popupImportFile:
		return m.importFileActionsShortcuts()
	case popupImportKeyField:
		return strings.TrimSpace(fmt.Sprintf(`Actions
  ctrl+m      select focused option
  enter       %s
  tab         move between options and buttons
  esc / q / ctrl+g  cancel`, m.importSelectorEnterAction("select")))
	case popupImportFormat:
		return strings.TrimSpace(fmt.Sprintf(`Actions
  ctrl+m      select focused option
  enter       %s
  d           Dotenv
  j           JSON
  y           YAML
  tab         move between options and buttons
  esc / q / ctrl+g  cancel`, m.importSelectorEnterAction("select")))
	case popupImportFilePicker:
		return strings.TrimSpace(`Actions
  enter       select focused file or open focused directory
  tab         move between list and buttons
  esc / q / ctrl+g  cancel`)
	case popupImportMapFields, popupImportMapPaths, popupImportDefaults:
		return m.importChildActionsShortcuts(kind)
	case popupExportFile:
		return strings.TrimSpace(fmt.Sprintf(`Actions
  ctrl+m      export visible parameters
  enter       %s
  tab         move between form and buttons
  esc / q / ctrl+g  cancel`, m.exportFileEnterAction()))
	case popupExportKeyField:
		return strings.TrimSpace(fmt.Sprintf(`Actions
  ctrl+m      select focused option
  enter       %s
  tab         move between options and buttons
  esc / q / ctrl+g  cancel`, m.importSelectorEnterAction("select")))
	case popupExportFormat:
		return strings.TrimSpace(fmt.Sprintf(`Actions
  ctrl+m      select focused option
  enter       %s
  d           Dotenv
  j           JSON
  y           YAML
  tab         move between options and buttons
  esc / q / ctrl+g  cancel`, m.importSelectorEnterAction("select")))
	case popupExportOutputFields, popupExportMapFields, popupExportMapPaths:
		return m.importChildActionsShortcuts(kind)
	case popupExportOverwriteConfirm:
		return strings.TrimSpace(`Actions
  ctrl+m      overwrite existing file
  enter       focused button
  tab         move between buttons
  esc / q / ctrl+g  cancel`)
	default:
		return strings.TrimSpace(`Actions
  esc / q / ctrl+g  cancel`)
	}
}

func (m *shortcuts) editorPopupActionsShortcuts() string {
	enterAction := "next field"
	if m.editorButtonsFocused {
		enterAction = m.editorFocusedButtonAction("save")
	} else {
		switch m.editField {
		case editFieldRegion, editFieldType, editFieldTier, editFieldDataType, editFieldOverwrite:
			enterAction = "open selector"
		case editFieldDescription, editFieldPolicies, editFieldValue:
			enterAction = "expand/newline"
			if m.editorAreaExpanded {
				enterAction = "insert newline"
			}
		}
	}

	lines := []string{
		"Actions",
		"  ctrl+s      save",
		"  enter       " + enterAction,
	}

	if !m.editorButtonsFocused {
		switch m.editField {
		case editFieldDescription, editFieldPolicies, editFieldValue:
			lines = append(lines, "  alt+e       focused field actions popup")
		}
	}

	lines = append(lines,
		"  tab         move through fields and buttons",
		"  esc / q / ctrl+g  cancel",
	)

	return strings.Join(lines, "\n")
}

func (m *shortcuts) importFileActionsShortcuts() string {
	return strings.TrimSpace(fmt.Sprintf(`Actions
  ctrl+m      load file
  enter       %s
  tab         move through fields and buttons
  esc / q / ctrl+g  cancel`, m.importFileEnterAction()))
}

func (m *shortcuts) importChildActionsShortcuts(kind popupKind) string {
	enterAction := "apply values"
	tabAction := "move through fields and buttons"
	if (kind == popupImportMapFields || kind == popupExportMapFields) && !m.importButtonsFocused {
		enterAction = "move to next line"
	}
	if kind == popupExportOutputFields && !m.importButtonsFocused {
		enterAction = "toggle"
	}

	if (kind == popupImportMapPaths || kind == popupExportMapPaths) && !m.importButtonsFocused {
		enterAction = "move to next input"
		tabAction = "move through rows and buttons"
	}

	if kind == popupImportDefaults && m.importDefaultsCursor >= 0 && m.importDefaultsCursor <= 3 {
		enterAction = "choose focused option"
	}

	if kind == popupImportDefaults && (m.importDefaultsCursor == 4 || m.importDefaultsCursor == 5) {
		enterAction = "expand/newline in focused text area"
		if m.importDefaultAreaExpanded() {
			enterAction = "insert newline"
		}

		return strings.TrimSpace(fmt.Sprintf(`Actions
  ctrl+m      apply values
  enter       %s
  alt+e       actions popup
  tab         %s
  esc / q / ctrl+g  cancel`, enterAction, tabAction))
	}

	return strings.TrimSpace(fmt.Sprintf(`Actions
  ctrl+m      apply values
  enter       %s
  tab         %s
  esc / q / ctrl+g  cancel`, enterAction, tabAction))
}

func (m *shortcuts) importDefaultAreaExpanded() bool {
	switch m.importDefaultsCursor {
	case 4:
		return m.importDefaultPoliciesExpanded
	case 5:
		return m.importDefaultDescriptionExpanded
	default:
		return false
	}
}

func (m *shortcuts) popupSortShortcuts(kind popupKind) string {
	if kind != popupSort {
		return ""
	}

	lines := []string{"Sort"}
	for _, item := range m.popupSortItems() {
		lines = append(lines, fmt.Sprintf("  %-12s sort by %s", item.hotkey, item.column.Label()))
	}

	lines = append(lines, "  d            toggle selected direction")

	return strings.Join(lines, "\n")
}

func (m *shortcuts) popupNavigationShortcuts(kind popupKind) string {
	switch kind {
	case popupNone, popupShortcuts, popupConfirm, popupFileAction, popupFileWriteConfirm, popupUnsavedChanges:
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
		popupImportFormat:
		if m.editorPopupActive && kind != popupSort && kind != popupColumns {
			return m.popupOptionNavigationShortcuts()
		}

		return m.navigationShortcuts(screenColumns)
	case popupImportFilePicker:
		return m.filePickerNavigationShortcuts()
	case popupEditor:
		return m.editorPopupNavigationShortcuts()
	case popupImportFile, popupImportMapFields, popupImportMapPaths, popupImportDefaults:
		return m.fieldNavigationShortcuts()
	default:
		return ""
	}
}

func (m *shortcuts) popupOptionNavigationShortcuts() string {
	if m.keymapStyle() == keymapVi {
		return strings.TrimSpace(`Navigation
  ↑ / k                      previous option
  ↓ / j                      next option
  PgUp                       page up
  PgDn                       page down
  Home / gg                  first option
  End / G                    last option`)
	}

	return strings.TrimSpace(`Navigation
  ↑ / ctrl+p                 previous option
  ↓ / ctrl+n                 next option
  PgUp / alt+v               page up
  PgDn / ctrl+v              page down
  Home / alt+<               first option
  End / alt+>                last option`)
}

func (m *shortcuts) editorPopupNavigationShortcuts() string {
	if m.editorButtonsFocused {
		if m.keymapStyle() == keymapVi {
			return strings.TrimSpace(`Navigation
  ←                          previous button
  →                          next button
  ↑ / k                      return to form
  Home / gg                  first field
  End / G                    cancel button`)
		}

		return strings.TrimSpace(`Navigation
  ←                          previous button
  →                          next button
  ↑ / ctrl+p                 return to form
  Home / alt+<               first field
  End / alt+>                cancel button`)
	}

	if m.keymapStyle() == keymapVi {
		lines := []string{
			"Navigation",
			"  ↑ / k                      previous field",
			"  ↓ / j                      next field",
		}
		if isEditableTextField(m.editField) {
			if m.viInsertMode {
				lines = append(lines, "  alt+b / alt+f              backward/forward word")
			} else {
				lines = append(lines, "  b / w                      backward/forward word")
			}
		}
		if isMultilineEditField(m.editField) {
			lines = append(lines,
				"  PgUp / ctrl+b              page up",
				"  PgDn / ctrl+f              page down",
			)
		}
		lines = append(lines,
			"  Home / gg                  first field",
			"  End / G                    last field",
		)

		return strings.Join(lines, "\n")
	}

	lines := []string{
		"Navigation",
		"  ↑ / ctrl+p                 previous field",
		"  ↓ / ctrl+n                 next field",
	}
	if isEditableTextField(m.editField) {
		lines = append(lines, "  alt+b / alt+f              backward/forward word")
	}
	if isMultilineEditField(m.editField) {
		lines = append(lines,
			"  PgUp / alt+v               page up",
			"  PgDn / ctrl+v              page down",
		)
	}
	lines = append(lines,
		"  Home / alt+<               first field",
		"  End / alt+>                last field",
	)

	return strings.Join(lines, "\n")
}

func (m *shortcuts) actionsShortcuts(forScreen screen) string {
	switch forScreen {
	case screenHelp:
		return ""
	case screenMain:
		return strings.TrimSpace(`Actions
  enter        edit value
  n            new parameter
  i            import from file
  e            export to file
  d            show/hide details
  / / f        filter
  c            columns
  s            sort popup
  x            mark selected value for deletion
  X            mark filtered values for deletion
  r            revert current local change
  p            push current local change
  P            push filtered local changes
  esc / q      quit`)
	case screenTextArea:
		if m.keymapStyle() == keymapVi {
			if m.viInsertMode {
				return strings.TrimSpace(`Actions
  esc          normal mode
  ctrl+s       save
  alt+e        value/description/policies actions popup
  y            confirm pending file write warning`)
			}

			return strings.TrimSpace(`Actions
  i            insert mode
  ctrl+s       save
  alt+e        value/description/policies actions popup
  y            confirm pending file write warning
  esc / q / ctrl+g  back`)
		}

		return strings.TrimSpace(`Actions
  ctrl+s       save
  alt+e        value/description/policies actions popup
  enter        expand/newline in Description/Policies/Value / choose selectors / next field
  y            confirm pending file write warning
  esc / q / ctrl+g  back`)
	case screenColumns:
		return strings.TrimSpace(`Actions
  space/enter  toggle column
  a            show all columns
  x            hide all optional columns
  esc / q / ctrl+g  back`)
	case screenConfirm:
		return strings.TrimSpace(`Actions
  enter        confirm
  esc / q / ctrl+g  back`)
	case screenRegionSelect, screenTypeSelect:
		return strings.TrimSpace(`Actions
  enter        choose option
  esc / q / ctrl+g  back`)
	case screenLoading:
		return strings.TrimSpace(`Actions
  esc / q      quit`)
	default:
		return strings.TrimSpace(`Actions
  esc / q      back`)
	}
}

func (m *shortcuts) sortShortcuts(forScreen screen) string {
	if forScreen != screenMain {
		return ""
	}

	items := m.visibleSortItems()
	if len(items) == 0 {
		return ""
	}

	lines := []string{"Sort"}
	for _, item := range items {
		lines = append(lines, fmt.Sprintf("  %-12s sort by %s", item.hotkey, item.column.Label()))
	}

	return strings.Join(lines, "\n")
}

func (m *shortcuts) navigationShortcuts(forScreen screen) string {
	if forScreen == screenMain || forScreen == screenColumns || forScreen == screenRegionSelect || forScreen == screenTypeSelect {
		if m.keymapStyle() == keymapVi {
			return strings.TrimSpace(`Navigation
  ↑ / k / shift+tab          previous row/option
  ↓ / j / tab                next row/option
  PgUp                       page up
  PgDn                       page down
  Home / gg                  first row/option
  End / G                    last row/option`)
		}

		return strings.TrimSpace(`Navigation
  ↑ / ctrl+p / shift+tab     previous row/option
  ↓ / ctrl+n / tab           next row/option
  PgUp / alt+v               page up
  PgDn / ctrl+v              page down
  Home / alt+<               first row/option
  End / alt+>                last row/option`)
	}

	if forScreen == screenTextArea {
		if m.keymapStyle() == keymapVi {
			return strings.TrimSpace(`Mode
  i                          enter insert mode
  esc                        leave insert mode / back from normal mode

Navigation
  h / l                      backward/forward character
  j / k                      next/previous line in Description/Policies/Value
  PgDn / ctrl+f              page down in Description/Policies/Value
  PgUp / ctrl+b              page up in Description/Policies/Value
  w / b                      forward/backward word
  0 / $                      start/end of line
  gg / G                     start/end of text
  tab                        next field
  shift+tab                  previous field
  PgUp / PgDn                page in Description/Policies/Value

Editing
  x                          delete current character
  D                          delete to end of real line / join next line
  dw                         delete next word
  db                         delete previous word
  ctrl+l                     show/hide line numbers`)
		}

		return strings.TrimSpace(`Navigation
  tab                        next field
  shift+tab                  previous field
  ctrl+f / ctrl+b            forward/backward character
  ctrl+p / ctrl+n            previous/next line
  PgDn / ctrl+v              page down in Description/Policies/Value
  PgUp / alt+v               page up in Description/Policies/Value
  ctrl+a / ctrl+e            start/end of line
  alt+f / alt+b              forward/backward word
  alt+< / alt+>              start/end of text
  ctrl+d                     delete current character
  ctrl+k                     delete to end of real line / join next line
  alt+d                      delete next word
  alt+backspace              delete previous word
  ctrl+l                     show/hide line numbers`)
	}

	return ""
}

func (m *shortcuts) filePickerNavigationShortcuts() string {
	if m.keymapStyle() == keymapVi {
		return strings.TrimSpace(`Navigation
  ↑ / k                  previous item
  ↓ / j                  next item
  PgUp                   page up
  PgDn                   page down
  Home / gg              first item
  End / G                last item
  h / ← / backspace      parent directory
  l / →                  open directory`)
	}

	return strings.TrimSpace(`Navigation
  ↑ / ctrl+p             previous item
  ↓ / ctrl+n             next item
  PgUp / alt+v           page up
  PgDn / ctrl+v          page down
  Home / alt+<           first item
  End / alt+>            last item
  ← / backspace          parent directory
  →                      open directory`)
}

func (m *shortcuts) fieldNavigationShortcuts() string {
	if m.keymapStyle() == keymapVi {
		return strings.TrimSpace(`Navigation
  ↑ / k / shift+tab          previous field
  ↓ / j / tab                next field
  alt+b / alt+f              backward/forward word in inputs`)
	}

	return strings.TrimSpace(`Navigation
  ↑ / ctrl+p / shift+tab     previous field
  ↓ / ctrl+n / tab           next field
  alt+b / alt+f              backward/forward word in inputs`)
}

func globalShortcuts() string {
	return strings.TrimSpace(`Global
  ctrl+/       open shortcuts`)
}
