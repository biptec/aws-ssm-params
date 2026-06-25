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
	viInsertMode         bool
	keys                 keymap
	popupSortOptions     []sortItem
	visibleSortOptions   []sortItem
	importMainCursor     int
	importDefaultsCursor int
	importButtonsFocused bool
	importButtonCursor   int
	importSelectorActive bool

	importDefaultPoliciesExpanded    bool
	importDefaultDescriptionExpanded bool
}

func newShortcuts(m model) *shortcuts {
	return &shortcuts{
		screen:               m.screen,
		shortcutsFor:         m.shortcutsFor,
		shortcutsPopupFor:    m.shortcutsPopupFor,
		fileActionMode:       m.fileActionMode,
		viInsertMode:         m.viInsertMode,
		keys:                 newKeymap(m),
		popupSortOptions:     m.popupSortItems(),
		visibleSortOptions:   m.visibleSortItems(),
		importMainCursor:     m.importMainCursor,
		importDefaultsCursor: m.importDefaultsCursor,
		importButtonsFocused: m.importButtonsFocused,
		importButtonCursor:   m.importButtonCursor,
		importSelectorActive: m.importSelectorActive(),

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
func mainFooterText(detailsShown bool) string {
	detailsAction := "d show details"
	if detailsShown {
		detailsAction = "d hide details"
	}

	return "ctrl+/ help • enter edit • n new • i import • " + detailsAction + " • / search • c columns • s sort • x delete • X delete visible • esc quit"
}

func searchFooterText() string {
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
		return "ctrl+/ help • space toggle • a all • x none • esc close"
	case popupValueActions:
		return "ctrl+/ help • enter select • c clear • r random • l load • w write • esc cancel"
	case popupPoliciesActions:
		return "ctrl+/ help • enter select • c clear • l load • w write • esc cancel"
	case popupDescriptionActions:
		return "ctrl+/ help • enter select • c clear • l load • w write • esc cancel"
	case popupRandomValue:
		return "ctrl+/ help • enter select • b base64 • x hex • u uuid • c custom • esc cancel"
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

		return "ctrl+/ help • enter " + button + " • esc cancel"
	case popupFileWriteConfirm:
		return "ctrl+/ help • enter yes • esc cancel"
	case popupUnsavedChanges:
		return "ctrl+/ help • enter discard • esc cancel"
	case popupConfirm:
		return "ctrl+/ help • enter confirm • esc cancel"
	case popupRegionSelect:
		if m.importSelectorActive {
			return "ctrl+/ help • ctrl+m select • enter " + m.importSelectorEnterAction("select") + " • esc cancel"
		}

		return "ctrl+/ help • enter select • esc cancel"
	case popupTypeSelect:
		if m.importSelectorActive {
			return "ctrl+/ help • ctrl+m select • enter " + m.importSelectorEnterAction("select") + " • e secure • s string • l list • esc cancel"
		}

		return "ctrl+/ help • enter select • e secure • s string • l list • esc cancel"
	case popupTierSelect:
		if m.importSelectorActive {
			return "ctrl+/ help • ctrl+m select • enter " + m.importSelectorEnterAction("select") + " • i intelligent • s standard • a advanced • esc cancel"
		}

		return "ctrl+/ help • enter select • i intelligent • s standard • a advanced • esc cancel"
	case popupDataTypeSelect:
		if m.importSelectorActive {
			return "ctrl+/ help • ctrl+m select • enter " + m.importSelectorEnterAction("select") + " • t text • a AMI • i integration • esc cancel"
		}

		return "ctrl+/ help • enter select • t text • a AMI • i integration • esc cancel"
	case popupOverwriteSelect:
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
	default:
		return "ctrl+/ help • esc cancel"
	}
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

func (m *shortcuts) importSelectorEnterAction(primary string) string {
	if m.importButtonsFocused {
		return m.importFocusedButtonAction(primary)
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

	parts = append(parts, "ctrl+/ help", "d direction")
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
	case popupNone, popupShortcuts, popupConfirm:
		return ""
	case popupSort:
		return strings.TrimSpace(`Actions
  d            toggle selected direction
  esc / q / ctrl+g  close`)
	case popupColumns:
		return strings.TrimSpace(`Actions
  space        toggle focused column
  a            show all columns
  x            hide all optional columns
  esc / q / ctrl+g  close`)
	case popupValueActions:
		return strings.TrimSpace(`Actions
  enter        select focused action
  c            clear value
  r            random value
  l            load from file
  w            write to file
  esc / q / ctrl+g  cancel`)
	case popupPoliciesActions:
		return strings.TrimSpace(`Actions
  enter        select focused action
  c            clear policies
  l            load from file
  w            write to file
  esc / q / ctrl+g  cancel`)
	case popupDescriptionActions:
		return strings.TrimSpace(`Actions
  enter        select focused action
  c            clear description
  l            load from file
  w            write to file
  esc / q / ctrl+g  cancel`)
	case popupFileAction:
		return strings.TrimSpace(`Actions
  enter        confirm input
  esc / q / ctrl+g  cancel`)
	case popupFileWriteConfirm:
		return strings.TrimSpace(`Actions
  enter        yes / continue
  y            yes / continue
  esc / q / ctrl+g  cancel`)
	case popupUnsavedChanges:
		return strings.TrimSpace(`Actions
  enter        discard changes
  esc / q / ctrl+g  cancel`)
	case popupRandomValue:
		return strings.TrimSpace(`Actions
  enter        select focused option
  b            base64 32 bytes
  x            hex 32 bytes
  u            uuid
  c            custom length base64
  esc / q / ctrl+g  cancel`)
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
	default:
		return strings.TrimSpace(`Actions
  esc / q / ctrl+g  cancel`)
	}
}

func (m *shortcuts) importFileActionsShortcuts() string {
	return strings.TrimSpace(fmt.Sprintf(`Actions
  ctrl+m      load file
  enter       %s
  tab         move between fields and buttons
  esc / q / ctrl+g  cancel`, m.importFileEnterAction()))
}

func (m *shortcuts) importChildActionsShortcuts(kind popupKind) string {
	enterAction := "apply values"
	if kind == popupImportMapFields && !m.importButtonsFocused {
		enterAction = "move to next line"
	}

	if kind == popupImportMapPaths && !m.importButtonsFocused {
		enterAction = "move to next input"
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
  tab         move between fields and buttons
  esc / q / ctrl+g  cancel`, enterAction))
	}

	return strings.TrimSpace(fmt.Sprintf(`Actions
  ctrl+m      apply values
  enter       %s
  tab         move between fields and buttons
  esc / q / ctrl+g  cancel`, enterAction))
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
		return m.navigationShortcuts(screenColumns)
	case popupImportFilePicker:
		return m.filePickerNavigationShortcuts()
	case popupImportFile, popupImportMapFields, popupImportMapPaths, popupImportDefaults:
		return m.fieldNavigationShortcuts()
	default:
		return ""
	}
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
  d            show/hide details
  /            search
  c            columns
  s            sort popup
  x            delete selected value
  X            delete visible/filtered values
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
  ↓ / j / tab                next field`)
	}

	return strings.TrimSpace(`Navigation
  ↑ / ctrl+p / shift+tab     previous field
  ↓ / ctrl+n / tab           next field`)
}

func globalShortcuts() string {
	return strings.TrimSpace(`Global
  ctrl+/       open shortcuts`)
}
