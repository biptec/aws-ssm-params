package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// keysBindBlock is the canonical registry shape for shortcut bindings per UI block.
type keysBindBlock map[blockKind][]key.Binding

type shortcutSection struct {
	title    string
	bindings []key.Binding
}

func ctrlBind(value string) string {
	return "ctrl+" + value
}

const (
	altWordBackwardKey = "alt+b"
	altWordForwardKey  = "alt+f"
	viWordBackwardKey  = "b"
	viWordForwardKey   = "w"
)

var (
	helpShortcut = key.NewBinding(
		key.WithKeys(ctrlBind("_"), ctrlBind("?"), ctrlBind("/")),
		key.WithHelp("ctrl+/", "help"),
	)

	openShortcutsShortcut = key.NewBinding(
		key.WithKeys(ctrlBind("_"), ctrlBind("?"), ctrlBind("/")),
		key.WithHelp("ctrl+/", "open shortcuts"),
	)

	cancelShortcut = key.NewBinding(
		key.WithKeys("esc", "q", ctrlBind("g")),
		key.WithHelp("esc", "cancel"),
	)

	closeShortcut = key.NewBinding(
		key.WithKeys("esc", "q", ctrlBind("g")),
		key.WithHelp("esc", "close"),
	)

	escapeCloseShortcut = key.NewBinding(
		key.WithKeys("esc", ctrlBind("g")),
		key.WithHelp("esc", "close"),
	)

	backShortcut = key.NewBinding(
		key.WithKeys("esc", "q", ctrlBind("g")),
		key.WithHelp("esc", "back"),
	)

	shortcutPopupCloseShortcut = key.NewBinding(
		key.WithKeys("?", "esc", "q", ctrlBind("g")),
		key.WithHelp("esc", "close"),
	)

	quitShortcut = key.NewBinding(
		key.WithKeys("esc", "q"),
		key.WithHelp("esc", "quit"),
	)

	quitConfirmShortcut = key.NewBinding(
		key.WithKeys(ctrlBind("c"), ctrlBind("q")),
	)

	toggleShortcut = key.NewBinding(
		key.WithKeys(" ", "space"),
		key.WithHelp("space", "toggle"),
	)

	tabShortcut = key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "move"),
	)

	shiftTabShortcut = key.NewBinding(
		key.WithKeys("shift+tab"),
		key.WithHelp("shift+tab", "move previous"),
	)

	leftShortcut = key.NewBinding(
		key.WithKeys("left"),
		key.WithHelp("←", "left"),
	)

	rightShortcut = key.NewBinding(
		key.WithKeys("right"),
		key.WithHelp("→", "right"),
	)

	primaryConfirmShortcut         = primaryShortcut("confirm")
	primaryCloseShortcut           = primaryShortcut("close")
	primaryConfirmContinueShortcut = primaryShortcut("confirm / continue")
	primaryDiscardShortcut         = primaryShortcut("discard")
	primaryDiscardChangesShortcut  = primaryShortcut("discard changes")
	primaryExportShortcut          = primaryShortcut("export")
	primaryExportVisibleShortcut   = primaryShortcut("export visible parameters")
	primaryGenerateShortcut        = primaryShortcut("generate")
	primaryLoadShortcut            = primaryShortcut("load")
	primaryLoadFileShortcut        = primaryShortcut("load file")
	primaryOverwriteShortcut       = primaryShortcut("overwrite")
	primaryOverwriteFileShortcut   = primaryShortcut("overwrite existing file")
	primaryQuitShortcut            = primaryShortcut("quit")
	primarySaveShortcut            = primaryShortcut("save")
	primarySelectShortcut          = primaryShortcut("select")
	primarySelectActionShortcut    = primaryShortcut("select focused action")
	primarySelectOptionShortcut    = primaryShortcut("select focused option")
	primaryApplyShortcut           = primaryShortcut("apply")
	primaryApplyValuesShortcut     = primaryShortcut("apply values")
	primaryWriteShortcut           = primaryShortcut("write")
	primaryYesShortcut             = primaryShortcut("yes")

	enterConfirmShortcut        = enterShortcut("confirm")
	enterEditShortcut           = enterShortcut("edit")
	enterFocusedButtonShortcut  = enterShortcut("focused button")
	enterFocusedControlShortcut = enterShortcut("focused control")
	enterNextFieldShortcut      = enterShortcut("next field")
	enterOpenShortcut           = enterShortcut("open")
	enterSelectShortcut         = enterShortcut("select")

	dotenvFormatShortcut = keyShortcut("d", "dotenv")
	jsonFormatShortcut   = keyShortcut("j", "json")
	yamlFormatShortcut   = keyShortcut("y", "yaml")

	altActionsShortcut     = keyShortcut("alt+e", "actions popup")
	altDescriptionShortcut = keyShortcut("alt+e", "description actions")
	altPoliciesShortcut    = keyShortcut("alt+e", "policies actions")
	altValueShortcut       = keyShortcut("alt+e", "value actions")
	lineNumbersShortcut    = keyShortcut("ctrl+l", "lines")

	commonPreviousNavigationShortcut = keyAliasesShortcut([]string{"up", "shift+tab"}, "↑ / shift+tab", "previous")
	commonNextNavigationShortcut     = keyAliasesShortcut([]string{"down", "tab"}, "↓ / tab", "next")
	commonPageUpShortcut             = keyAliasesShortcut([]string{"pgup", "pageup"}, "PgUp", "page up")
	commonPageDownShortcut           = keyAliasesShortcut([]string{"pgdown", "pagedown"}, "PgDn", "page down")
	commonFirstShortcut              = keyShortcut("home", "first")
	commonLastShortcut               = keyShortcut("end", "last")
	viPreviousNavigationShortcut     = keyShortcut("k", "previous")
	viNextNavigationShortcut         = keyShortcut("j", "next")
	viLastNavigationShortcut         = keyShortcut("G", "last")
	viFirstNavigationPrefixShortcut  = keyShortcut("g", "first command")
	viFirstNavigationSequence        = keyShortcut("gg", "first")
	emacsPreviousNavigationShortcut  = keyShortcut(ctrlBind("p"), "previous")
	emacsNextNavigationShortcut      = keyShortcut(ctrlBind("n"), "next")
	emacsPageUpShortcut              = keyShortcut("alt+v", "page up")
	emacsPageDownShortcut            = keyShortcut(ctrlBind("v"), "page down")
	emacsFirstShortcut               = keyShortcut("alt+<", "first")
	emacsLastShortcut                = keyShortcut("alt+>", "last")

	filePickerEmacsPreviousShortcut = keyAliasesShortcut([]string{"up", ctrlBind("p")}, "↑ / ctrl+p", "previous item")
	filePickerEmacsNextShortcut     = keyAliasesShortcut([]string{"down", ctrlBind("n")}, "↓ / ctrl+n", "next item")
	filePickerEmacsPageUpShortcut   = keyAliasesShortcut([]string{"pgup", "pageup", "alt+v"}, "PgUp / alt+v", "page up")
	filePickerEmacsPageDownShortcut = keyAliasesShortcut([]string{"pgdown", "pagedown", ctrlBind("v")}, "PgDn / ctrl+v", "page down")
	filePickerEmacsFirstShortcut    = keyAliasesShortcut([]string{"home", "alt+<"}, "Home / alt+<", "first item")
	filePickerEmacsLastShortcut     = keyAliasesShortcut([]string{"end", "alt+>"}, "End / alt+>", "last item")
	filePickerEmacsParentShortcut   = keyAliasesShortcut([]string{"left", "backspace"}, "← / backspace", "parent directory")
	filePickerEmacsOpenShortcut     = keyShortcut("right", "open directory")

	filePickerViPreviousShortcut = keyAliasesShortcut([]string{"up", "k"}, "↑ / k", "previous item")
	filePickerViNextShortcut     = keyAliasesShortcut([]string{"down", "j"}, "↓ / j", "next item")
	filePickerViPageUpShortcut   = keyAliasesShortcut([]string{"pgup", "pageup"}, "PgUp", "page up")
	filePickerViPageDownShortcut = keyAliasesShortcut([]string{"pgdown", "pagedown"}, "PgDn", "page down")
	filePickerViFirstShortcut    = keyAliasesShortcut([]string{"home", "g"}, "Home / gg", "first item")
	filePickerViLastShortcut     = keyAliasesShortcut([]string{"end", "G"}, "End / G", "last item")
	filePickerViParentShortcut   = keyAliasesShortcut([]string{"h", "left", "backspace"}, "h / ← / backspace", "parent directory")
	filePickerViOpenShortcut     = keyAliasesShortcut([]string{"l", "right"}, "l / →", "open directory")
	filePickerSelectShortcut     = enterShortcut("select focused file or open focused directory")

	emacsTextForwardCharShortcut  = keyAliasesShortcut([]string{ctrlBind("f"), "right"}, "ctrl+f / →", "forward character")
	emacsTextBackwardCharShortcut = keyAliasesShortcut([]string{ctrlBind("b"), "left"}, "ctrl+b / ←", "backward character")
	emacsTextPreviousLineShortcut = keyAliasesShortcut([]string{ctrlBind("p"), "up"}, "ctrl+p / ↑", "previous line")
	emacsTextNextLineShortcut     = keyAliasesShortcut([]string{ctrlBind("n"), "down"}, "ctrl+n / ↓", "next line")
	emacsTextLineStartShortcut    = keyAliasesShortcut([]string{ctrlBind("a"), "home"}, "ctrl+a / Home", "start of line")
	emacsTextLineEndShortcut      = keyAliasesShortcut([]string{ctrlBind("e"), "end"}, "ctrl+e / End", "end of line")
	emacsTextPageDownShortcut     = keyAliasesShortcut([]string{"pgdown", "pagedown", ctrlBind("v")}, "PgDn / ctrl+v", "page down")
	emacsTextPageUpShortcut       = keyAliasesShortcut([]string{"pgup", "pageup", "alt+v"}, "PgUp / alt+v", "page up")
	emacsTextStartShortcut        = keyAliasesShortcut([]string{"alt+<"}, "alt+<", "start of text")
	emacsTextEndShortcut          = keyAliasesShortcut([]string{"alt+>"}, "alt+>", "end of text")
	emacsTextDeleteCharShortcut   = keyShortcut(ctrlBind("d"), "delete current character")
	emacsTextKillLineShortcut     = keyShortcut(ctrlBind("k"), "delete to end of real line / join next line")
	emacsTextDeleteNextWord       = keyShortcut("alt+d", "delete next word")
	emacsTextDeletePrevWord       = keyShortcut("alt+backspace", "delete previous word")
	emacsWordBackwardShortcut     = keyShortcut(altWordBackwardKey, "backward word")
	emacsWordForwardShortcut      = keyShortcut(altWordForwardKey, "forward word")
	emacsWordNavigationShortcut   = combinedShortcut(emacsWordBackwardShortcut, emacsWordForwardShortcut, "backward/forward word")

	viInsertModeShortcut        = keyShortcut("i", "insert mode")
	viNormalModeShortcut        = keyShortcut("esc", "normal mode")
	viTextForwardCharShortcut   = keyAliasesShortcut([]string{"l", "right"}, "l / →", "forward character")
	viTextBackwardCharShortcut  = keyAliasesShortcut([]string{"h", "left"}, "h / ←", "backward character")
	viTextNextLineShortcut      = keyAliasesShortcut([]string{"j", "down"}, "j / ↓", "next line")
	viTextPreviousLineShortcut  = keyAliasesShortcut([]string{"k", "up"}, "k / ↑", "previous line")
	viTextPageDownShortcut      = keyAliasesShortcut([]string{"pagedown", "pgdown", ctrlBind("f")}, "PgDn / ctrl+f", "page down")
	viTextPageUpShortcut        = keyAliasesShortcut([]string{"pageup", "pgup", ctrlBind("b")}, "PgUp / ctrl+b", "page up")
	viTextLineStartShortcut     = keyAliasesShortcut([]string{"0", "home"}, "0 / Home", "start of line")
	viTextLineEndShortcut       = keyAliasesShortcut([]string{"$", "end"}, "$ / End", "end of line")
	viTextStartShortcut         = keyShortcut("G", "end of text")
	viTextStartPrefixShortcut   = keyShortcut("g", "start command")
	viTextDeletePrefixShortcut  = keyShortcut("d", "delete command")
	viTextDeleteToLineEnd       = keyShortcut("D", "delete to end of real line / join next line")
	viTextDeleteCharShortcut    = keyShortcut("x", "delete current character")
	viWordBackwardShortcut      = keyShortcut(viWordBackwardKey, "backward word")
	viWordForwardShortcut       = keyShortcut(viWordForwardKey, "forward word")
	viWordNavigationShortcut    = combinedShortcut(viWordBackwardShortcut, viWordForwardShortcut, "backward/forward word")
	viTextStartSequenceShortcut = keyShortcut("gg", "start of text")
	viDeleteNextWordShortcut    = keyShortcut("dw", "delete next word")
	viDeletePrevWordShortcut    = keyShortcut("db", "delete previous word")

	deleteBackwardShortcut        = keyAliasesShortcut([]string{"backspace", ctrlBind("h")}, "backspace", "delete backward")
	reservedMultilineEditShortcut = keyAliasesShortcut([]string{ctrlBind("w"), ctrlBind("r")}, "", "")
	focusedFieldActionsShortcut   = keyShortcut("alt+e", "focused field actions popup")

	columnsShortcut          = keyShortcut("c", "columns")
	detailsShortcut          = keyShortcut("d", "details")
	exportShortcut           = keyShortcut("e", "export")
	filterShortcut           = keyShortcut("f", "filter")
	filterSlashShortcut      = keyShortcut("/", "filter")
	importShortcut           = keyShortcut("i", "import")
	newParameterShortcut     = keyShortcut("n", "new")
	sortShortcut             = keyShortcut("s", "sort")
	deleteOneShortcut        = keyShortcut("x", "delete")
	deleteVisibleShortcutKey = keyShortcut("X", "delete visible")
	revertVisibleShortcutKey = keyShortcut("R", "revert visible")
	pushVisibleShortcutKey   = keyShortcut("P", "push visible")
)

var shortcuts = keysBindBlock{
	parameterListBlock: {
		helpShortcut,
		enterEditShortcut,
		newParameterShortcut,
		importShortcut,
		exportShortcut,
		filterSlashShortcut,
		filterShortcut,
		columnsShortcut,
		sortShortcut,
		deleteOneShortcut,
		quitShortcut,
	},
	selectedParameterBlock: {
		helpShortcut,
	},
	filterBlock: {
		helpShortcut,
		closeShortcut,
	},
	editorBlock: {
		helpShortcut,
		primarySaveShortcut,
		enterNextFieldShortcut,
		cancelShortcut,
	},
	columnsBlock: {
		helpShortcut,
		columnsToggleShortcut,
		columnsShowAllShortcut,
		columnsHideOptionalShortcut,
		backShortcut,
	},
	confirmBlock: {
		helpShortcut,
		enterConfirmShortcut,
		backShortcut,
	},
	regionSelectBlock: {
		helpShortcut,
		enterShortcut("choose"),
		backShortcut,
	},
	typeSelectBlock: {
		helpShortcut,
		enterShortcut("choose"),
		backShortcut,
	},
	loadingBlock: {
		helpShortcut,
		quitShortcut,
	},
	popupShortcuts: {
		closeShortcut,
	},
	popupColumns: {
		helpShortcut,
		primaryCloseShortcut,
		enterFocusedControlShortcut,
		columnsToggleShortcut,
		columnsShowAllShortcut,
		columnsHideOptionalShortcut,
		closeShortcut,
	},
	popupConfirm: {
		helpShortcut,
		primaryConfirmShortcut,
		enterFocusedControlShortcut,
		toggleShortcut,
		tabControlsShortcut,
		cancelShortcut,
	},
	popupSort: {
		helpShortcut,
		primaryCloseShortcut,
		enterFocusedControlShortcut,
		columnsToggleShortcut,
		sortDirectionShortcut,
		tabListButtonsShortcut,
		closeShortcut,
	},
	popupRegionSelect: {
		helpShortcut,
		primarySelectShortcut,
		enterSelectShortcut,
		cancelShortcut,
	},
	popupTypeSelect: {
		helpShortcut,
		primarySelectShortcut,
		enterSelectShortcut,
		cancelShortcut,
	},
	popupTierSelect: {
		helpShortcut,
		primarySelectShortcut,
		enterSelectShortcut,
		cancelShortcut,
	},
	popupDataTypeSelect: {
		helpShortcut,
		primarySelectShortcut,
		enterSelectShortcut,
		cancelShortcut,
	},
	popupOverwriteSelect: {
		helpShortcut,
		primarySelectShortcut,
		enterSelectShortcut,
		cancelShortcut,
	},
	popupValueActions: {
		helpShortcut,
		enterSelectShortcut,
		clearValueShortcut,
		randomValueShortcut,
		loadFromFileShortcut,
		writeToFileShortcut,
		cancelShortcut,
	},
	popupPoliciesActions: {
		helpShortcut,
		enterSelectShortcut,
		clearPoliciesShortcut,
		loadFromFileShortcut,
		writeToFileShortcut,
		cancelShortcut,
	},
	popupDescriptionActions: {
		helpShortcut,
		enterSelectShortcut,
		clearDescriptionShortcut,
		loadFromFileShortcut,
		writeToFileShortcut,
		cancelShortcut,
	},
	popupFileAction: {
		helpShortcut,
		primaryConfirmShortcut,
		enterConfirmShortcut,
		cancelShortcut,
	},
	popupFileWriteConfirm: {
		helpShortcut,
		primaryYesShortcut,
		enterShortcut("yes"),
		cancelShortcut,
	},
	popupUnsavedChanges: {
		helpShortcut,
		primaryDiscardShortcut,
		enterShortcut("discard"),
		cancelShortcut,
	},
	popupQuitConfirm: {
		helpShortcut,
		primaryQuitShortcut,
		enterFocusedButtonShortcut,
		tabButtonsShortcut,
		cancelShortcut,
	},
	popupRandomValue: {
		helpShortcut,
		enterSelectShortcut,
		randomBase64Shortcut,
		randomHexShortcut,
		randomUUIDShortcut,
		randomCustomShortcut,
		cancelShortcut,
	},
	popupEditor: {
		helpShortcut,
		primarySaveShortcut,
		cancelShortcut,
	},
	popupImportFile: {
		helpShortcut,
		primaryLoadShortcut,
		enterOpenShortcut,
		cancelShortcut,
	},
	popupImportKeyField: {
		helpShortcut,
		primarySelectShortcut,
		enterSelectShortcut,
		cancelShortcut,
	},
	popupImportFormat: {
		helpShortcut,
		primarySelectShortcut,
		enterSelectShortcut,
		dotenvFormatShortcut,
		jsonFormatShortcut,
		yamlFormatShortcut,
		cancelShortcut,
	},
	popupImportFilePicker: {
		helpShortcut,
		enterShortcut("select/open"),
		tabShortcut,
		cancelShortcut,
	},
	popupImportMapFields: {
		helpShortcut,
		primaryApplyShortcut,
		enterShortcut("next"),
		cancelShortcut,
	},
	popupImportMapPaths: {
		helpShortcut,
		primaryApplyShortcut,
		enterShortcut("next"),
		cancelShortcut,
	},
	popupImportDefaults: {
		helpShortcut,
		primaryApplyShortcut,
		enterShortcut("open/newline"),
		cancelShortcut,
	},
	popupExportFile: {
		helpShortcut,
		primaryExportShortcut,
		enterOpenShortcut,
		cancelShortcut,
	},
	popupExportKeyField: {
		helpShortcut,
		primarySelectShortcut,
		enterSelectShortcut,
		cancelShortcut,
	},
	popupExportFormat: {
		helpShortcut,
		primarySelectShortcut,
		enterSelectShortcut,
		dotenvFormatShortcut,
		jsonFormatShortcut,
		yamlFormatShortcut,
		cancelShortcut,
	},
	popupExportOutputFields: {
		helpShortcut,
		primaryApplyShortcut,
		toggleShortcut,
		cancelShortcut,
	},
	popupExportMapFields: {
		helpShortcut,
		primaryApplyShortcut,
		enterShortcut("next"),
		cancelShortcut,
	},
	popupExportMapPaths: {
		helpShortcut,
		primaryApplyShortcut,
		enterShortcut("next"),
		cancelShortcut,
	},
	popupExportOverwriteConfirm: {
		helpShortcut,
		primaryOverwriteShortcut,
		enterFocusedButtonShortcut,
		cancelShortcut,
	},
}

func keyShortcut(keyValue, description string) key.Binding {
	return key.NewBinding(
		key.WithKeys(keyValue),
		key.WithHelp(keyValue, description),
	)
}

func keyAliasesShortcut(keys []string, helpKey, description string) key.Binding {
	return key.NewBinding(
		key.WithKeys(keys...),
		key.WithHelp(helpKey, description),
	)
}

func combinedShortcut(first, second key.Binding, description string) key.Binding {
	keys := append(append([]string(nil), first.Keys()...), second.Keys()...)
	helpKey := first.Help().Key + " / " + second.Help().Key

	return key.NewBinding(
		key.WithKeys(keys...),
		key.WithHelp(helpKey, description),
	)
}

func primaryShortcut(description string) key.Binding {
	return key.NewBinding(
		key.WithKeys(ctrlBind("@"), ctrlBind("space")),
		key.WithHelp("ctrl+space", description),
	)
}

func shortcutWithDescription(binding key.Binding, description string) key.Binding {
	return key.NewBinding(
		key.WithKeys(binding.Keys()...),
		key.WithHelp(binding.Help().Key, description),
	)
}

func enterShortcut(description string) key.Binding {
	return key.NewBinding(
		key.WithKeys("enter", ctrlBind("j")),
		key.WithHelp("enter", description),
	)
}

func bindingsForBlock(kind blockKind) []key.Binding {
	return append([]key.Binding(nil), shortcuts[kind]...)
}

func renderFooterBindings(bindings []key.Binding) string {
	parts := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		help := binding.Help()
		if help.Key == "" || help.Desc == "" {
			continue
		}

		parts = append(parts, help.Key+" "+help.Desc)
	}

	return strings.Join(parts, " • ")
}

func renderFooter(bindings ...key.Binding) string {
	return renderFooterBindings(bindings)
}

func renderBlockFooter(kind blockKind) string {
	return renderFooterBindings(bindingsForBlock(kind))
}

func renderShortcutSections(sections []shortcutSection) string {
	out := make([]string, 0, len(sections)*4)
	for _, section := range sections {
		if len(section.bindings) == 0 {
			continue
		}

		helpItems := make([]key.Help, 0, len(section.bindings))
		keyWidth := 0

		for _, binding := range section.bindings {
			help := binding.Help()
			if help.Key == "" || help.Desc == "" {
				continue
			}

			helpItems = append(helpItems, help)
			keyWidth = max(keyWidth, lipgloss.Width(help.Key))
		}

		if len(helpItems) == 0 {
			continue
		}

		if len(out) > 0 {
			out = append(out, "")
		}

		out = append(out, section.title)

		for _, help := range helpItems {
			padding := strings.Repeat(" ", keyWidth-lipgloss.Width(help.Key)+2)
			out = append(out, "  "+help.Key+padding+help.Desc)
		}
	}

	return strings.Join(out, "\n")
}

func renderShortcutSection(title string, bindings ...key.Binding) string {
	return renderShortcutSections([]shortcutSection{{title: title, bindings: bindings}})
}

func renderActionsShortcutSection(bindings []key.Binding) string {
	return renderShortcutSections([]shortcutSection{{title: "Actions", bindings: bindings}})
}

func renderNavigationShortcutSection(bindings []key.Binding) string {
	return renderShortcutSections([]shortcutSection{{title: "Navigation", bindings: bindings}})
}

func renderGlobalShortcuts() string {
	return renderShortcutSection("Global", openShortcutsShortcut)
}

func bindingMatchesString(binding key.Binding, value string) bool {
	return bindingMatchesKeys(binding, value)
}

func bindingMatchesKeys(binding interface{ Keys() []string }, value string) bool {
	for _, candidate := range binding.Keys() {
		if candidate == value {
			return true
		}
	}

	return false
}

func firstBindingKey(binding key.Binding) string {
	keys := binding.Keys()
	if len(keys) == 0 {
		return ""
	}

	return keys[0]
}

func isHelpKeyMsg(msg tea.KeyMsg) bool {
	return key.Matches(msg, helpShortcut)
}

func isCancelKeyMsg(msg tea.KeyMsg) bool {
	return key.Matches(msg, cancelShortcut)
}

func isPrintableCancelKeyMsg(msg tea.KeyMsg) bool {
	if msg.Type != tea.KeyRunes || len(msg.Runes) != 1 {
		return false
	}

	return bindingMatchesString(cancelShortcut, string(msg.Runes))
}

func isCloseKeyMsg(msg tea.KeyMsg) bool {
	return key.Matches(msg, closeShortcut)
}

func isEscapeCloseKeyMsg(msg tea.KeyMsg) bool {
	return key.Matches(msg, escapeCloseShortcut)
}

func isBackKeyMsg(msg tea.KeyMsg) bool {
	return key.Matches(msg, backShortcut)
}

func isShortcutPopupCloseKeyMsg(msg tea.KeyMsg) bool {
	return key.Matches(msg, shortcutPopupCloseShortcut)
}

func isPrimaryActionMsg(msg tea.KeyMsg) bool {
	return key.Matches(msg, primarySelectShortcut)
}

func isEnterKeyMsg(msg tea.KeyMsg) bool {
	return key.Matches(msg, enterSelectShortcut)
}

func isToggleKeyMsg(msg tea.KeyMsg) bool {
	return key.Matches(msg, toggleShortcut)
}

func isQuitConfirmKeyMsg(msg tea.KeyMsg) bool {
	return key.Matches(msg, quitConfirmShortcut)
}

func isQuitKeyMsg(msg tea.KeyMsg) bool {
	return key.Matches(msg, quitShortcut)
}

func isViNormalModeKeyMsg(msg tea.KeyMsg) bool {
	return key.Matches(msg, viNormalModeShortcut)
}

func isLineNumbersKeyMsg(msg tea.KeyMsg) bool {
	return key.Matches(msg, lineNumbersShortcut)
}

func isFocusedFieldActionsKeyMsg(msg tea.KeyMsg) bool {
	return key.Matches(msg, focusedFieldActionsShortcut)
}

func isDeleteBackwardKeyMsg(msg tea.KeyMsg) bool {
	return key.Matches(msg, deleteBackwardShortcut)
}

func isReservedMultilineEditKeyMsg(msg tea.KeyMsg) bool {
	return key.Matches(msg, reservedMultilineEditShortcut)
}

func isTabNavigationKeyString(value string) bool {
	return bindingMatchesString(tabShortcut, value) || bindingMatchesString(shiftTabShortcut, value)
}

func isForwardTabKeyString(value string) bool {
	return bindingMatchesString(tabShortcut, value)
}

func isBackwardTabKeyString(value string) bool {
	return bindingMatchesString(shiftTabShortcut, value)
}

func isLeftKeyString(value string) bool {
	return bindingMatchesString(leftShortcut, value)
}

func isRightKeyString(value string) bool {
	return bindingMatchesString(rightShortcut, value)
}

func isViFirstNavigationPrefixString(value string) bool {
	return bindingMatchesString(viFirstNavigationPrefixShortcut, value)
}

func filePickerPreviousShortcut(style keymapStyle) key.Binding {
	if style == keymapVi {
		return filePickerViPreviousShortcut
	}

	return filePickerEmacsPreviousShortcut
}

func filePickerNextShortcut(style keymapStyle) key.Binding {
	if style == keymapVi {
		return filePickerViNextShortcut
	}

	return filePickerEmacsNextShortcut
}

func filePickerFirstShortcut(style keymapStyle) key.Binding {
	if style == keymapVi {
		return filePickerViFirstShortcut
	}

	return filePickerEmacsFirstShortcut
}

func filePickerLastShortcut(style keymapStyle) key.Binding {
	if style == keymapVi {
		return filePickerViLastShortcut
	}

	return filePickerEmacsLastShortcut
}

func filePickerPageUpShortcut(style keymapStyle) key.Binding {
	if style == keymapVi {
		return filePickerViPageUpShortcut
	}

	return filePickerEmacsPageUpShortcut
}

func filePickerPageDownShortcut(style keymapStyle) key.Binding {
	if style == keymapVi {
		return filePickerViPageDownShortcut
	}

	return filePickerEmacsPageDownShortcut
}

func filePickerParentShortcut(style keymapStyle) key.Binding {
	if style == keymapVi {
		return filePickerViParentShortcut
	}

	return filePickerEmacsParentShortcut
}

func filePickerOpenShortcut(style keymapStyle) key.Binding {
	if style == keymapVi {
		return filePickerViOpenShortcut
	}

	return filePickerEmacsOpenShortcut
}

func isFilePickerParentKeyString(style keymapStyle, value string) bool {
	return bindingMatchesString(filePickerParentShortcut(style), value)
}

func isFilePickerOpenKeyString(style keymapStyle, value string) bool {
	return bindingMatchesString(filePickerOpenShortcut(style), value)
}

func (m *model) startViFirstNavigationSequence(value string) bool {
	if m.keymapStyle() != keymapVi || !isViFirstNavigationPrefixString(value) {
		return false
	}

	m.pendingKeySequence = firstBindingKey(viFirstNavigationPrefixShortcut)

	return true
}

func isNewParameterShortcutMsg(msg tea.KeyMsg) bool {
	return key.Matches(msg, newParameterShortcut)
}

func isImportShortcutMsg(msg tea.KeyMsg) bool {
	return key.Matches(msg, importShortcut)
}

func isExportShortcutMsg(msg tea.KeyMsg) bool {
	return key.Matches(msg, exportShortcut)
}

func isFilterShortcutMsg(msg tea.KeyMsg) bool {
	return key.Matches(msg, filterShortcut) || key.Matches(msg, filterSlashShortcut)
}

func isDetailsShortcutMsg(msg tea.KeyMsg) bool {
	return key.Matches(msg, detailsShortcut)
}

func isColumnsShortcutMsg(msg tea.KeyMsg) bool {
	return key.Matches(msg, columnsShortcut)
}

func isSortShortcutMsg(msg tea.KeyMsg) bool {
	return key.Matches(msg, sortShortcut)
}

func isDeleteOneShortcutMsg(msg tea.KeyMsg) bool {
	return key.Matches(msg, deleteOneShortcut)
}

func isDeleteVisibleShortcutMsg(msg tea.KeyMsg) bool {
	return key.Matches(msg, deleteVisibleShortcutKey)
}

func isRevertOneShortcutMsg(msg tea.KeyMsg) bool {
	return key.Matches(msg, revertOneShortcut)
}

func isRevertVisibleShortcutMsg(msg tea.KeyMsg) bool {
	return key.Matches(msg, revertVisibleShortcutKey)
}

func isPushOneShortcutMsg(msg tea.KeyMsg) bool {
	return key.Matches(msg, pushOneShortcut)
}

func isPushVisibleShortcutMsg(msg tea.KeyMsg) bool {
	return key.Matches(msg, pushVisibleShortcutKey)
}

func mainFooterBindings(detailsShown, filtered, applyImmediately bool) []key.Binding {
	bindings := []key.Binding{
		helpShortcut,
		enterEditShortcut,
		newParameterShortcut,
		importShortcut,
		exportShortcut,
		detailsToggleShortcut(detailsShown),
		filterSlashShortcut,
		filterShortcut,
		columnsShortcut,
		sortShortcut,
		deleteOneShortcut,
		deleteVisibleShortcut(filtered),
	}

	if !applyImmediately {
		bindings = append(
			bindings,
			revertOneShortcut,
			revertVisibleShortcut(filtered),
			pushOneShortcut,
			pushVisibleShortcut(filtered),
		)
	}

	return append(bindings, quitShortcut)
}

func confirmPopupFooterBindings() []key.Binding {
	return []key.Binding{primaryConfirmShortcut, enterConfirmShortcut, cancelShortcut}
}

func importFileFooterBindings(enterAction string) []key.Binding {
	return []key.Binding{helpShortcut, primaryLoadShortcut, enterShortcut(enterAction), cancelShortcut}
}

func exportFileFooterBindings(enterAction string) []key.Binding {
	return []key.Binding{helpShortcut, primaryExportShortcut, enterShortcut(enterAction), cancelShortcut}
}

func selectorFooterBindings(enterAction string, options ...key.Binding) []key.Binding {
	bindings := []key.Binding{helpShortcut, primarySelectShortcut, enterShortcut(enterAction)}
	bindings = append(bindings, options...)

	return append(bindings, cancelShortcut)
}

func selectorFallbackFooterBindings(options ...key.Binding) []key.Binding {
	bindings := []key.Binding{helpShortcut, enterSelectShortcut}
	bindings = append(bindings, options...)

	return append(bindings, cancelShortcut)
}

func formatFooterBindings(enterAction string) []key.Binding {
	return selectorFooterBindings(enterAction, formatShortcutBindings()...)
}

func filePickerFooterBindings(parent key.Binding) []key.Binding {
	return []key.Binding{helpShortcut, enterShortcut("select/open"), keyShortcut("tab", "buttons"), parent, cancelShortcut}
}

func applyFooterBindings(enterAction string, extra ...key.Binding) []key.Binding {
	bindings := []key.Binding{helpShortcut, primaryApplyShortcut, enterShortcut(enterAction)}
	bindings = append(bindings, extra...)

	return append(bindings, cancelShortcut)
}

func overwriteFooterBindings(enterAction string) []key.Binding {
	return []key.Binding{helpShortcut, primaryOverwriteShortcut, enterShortcut(enterAction), cancelShortcut}
}

func editorActionPopupFooterBindings(hasPrimary bool, enterAction string, actions ...key.Binding) []key.Binding {
	bindings := []key.Binding{helpShortcut}
	if hasPrimary {
		bindings = append(bindings, primarySelectActionShortcut)
	}

	bindings = append(bindings, enterShortcut(enterAction))
	bindings = append(bindings, actions...)

	return append(bindings, cancelShortcut)
}

func randomValueFooterBindings(hasPrimary bool, enterAction string, includeTab bool) []key.Binding {
	bindings := []key.Binding{helpShortcut}
	if hasPrimary {
		bindings = append(bindings, primarySelectOptionShortcut)
	}

	bindings = append(bindings, enterShortcut(enterAction), randomBase64Shortcut, randomHexShortcut, randomUUIDShortcut, randomCustomShortcut)
	if includeTab {
		bindings = append(bindings, tabOptionsButtonsShortcut)
	}

	return append(bindings, cancelShortcut)
}

func fileActionFooterBindings(primaryAction, enterAction string, includeTab bool) []key.Binding {
	bindings := []key.Binding{helpShortcut, fileActionPrimaryShortcut(primaryAction), enterShortcut(enterAction)}
	if includeTab {
		bindings = append(bindings, tabInputButtonsShortcut)
	}

	return append(bindings, cancelShortcut)
}

func fileActionPrimaryShortcut(action string) key.Binding {
	switch action {
	case "load":
		return primaryLoadShortcut
	case "write":
		return primaryWriteShortcut
	case "generate":
		return primaryGenerateShortcut
	default:
		return primaryConfirmShortcut
	}
}

func inputConfirmFooterBindings(enterAction string) []key.Binding {
	return []key.Binding{helpShortcut, enterShortcut(enterAction), cancelShortcut}
}

func fileWriteConfirmFooterBindings(enterAction string, includeTab bool) []key.Binding {
	bindings := []key.Binding{helpShortcut, primaryConfirmContinueShortcut, enterShortcut(enterAction)}
	if includeTab {
		bindings = append(bindings, tabButtonsShortcut)
	}

	return append(bindings, cancelShortcut)
}

func unsavedChangesFooterBindings(enterAction string, includeTab bool) []key.Binding {
	bindings := []key.Binding{helpShortcut, primaryDiscardChangesShortcut, enterShortcut(enterAction)}
	if includeTab {
		bindings = append(bindings, tabButtonsShortcut)
	}

	return append(bindings, cancelShortcut)
}

func editorPopupFooterBindings(enterAction string, action key.Binding) []key.Binding {
	bindings := []key.Binding{helpShortcut, primarySaveShortcut, enterShortcut(enterAction)}
	if action.Help().Key != "" {
		bindings = append(bindings, action)
	}

	return append(bindings, cancelShortcut)
}

func importChildFooterBindings(enterAction string, extra ...key.Binding) []key.Binding {
	return applyFooterBindings(enterAction, extra...)
}

func selectFocusedOptionActionBindings(kind blockKind) []key.Binding {
	bindings := []key.Binding{enterShortcut("select focused option")}

	if extra, ok := selectFocusedOptionBindings[kind]; ok {
		bindings = append(bindings, extra...)
	}

	return append(bindings, cancelShortcut)
}

var selectFocusedOptionBindings = map[blockKind][]key.Binding{
	popupTypeSelect: {
		keyShortcut("e", "SecureString"),
		keyShortcut("s", "String"),
		keyShortcut("l", "StringList"),
	},
	popupTierSelect: {
		keyShortcut("i", "Intelligent-Tiering"),
		keyShortcut("s", "Standard"),
		keyShortcut("a", "Advanced"),
	},
	popupDataTypeSelect: {
		keyShortcut("t", "text"),
		keyShortcut("a", "aws:ec2:image"),
		keyShortcut("i", "aws:ssm:integration"),
	},
	popupOverwriteSelect: {
		keyShortcut("t", "true"),
		keyShortcut("f", "false"),
	},
}

func typeSelectorOptionBindings() []key.Binding {
	return []key.Binding{keyShortcut("e", "secure"), keyShortcut("s", "string"), keyShortcut("l", "list")}
}

func tierSelectorOptionBindings() []key.Binding {
	return []key.Binding{keyShortcut("i", "intelligent"), keyShortcut("s", "standard"), keyShortcut("a", "advanced")}
}

func dataTypeSelectorOptionBindings() []key.Binding {
	return []key.Binding{keyShortcut("t", "text"), keyShortcut("a", "AMI"), keyShortcut("i", "integration")}
}

func overwriteSelectorOptionBindings() []key.Binding {
	return []key.Binding{keyShortcut("t", "true"), keyShortcut("f", "false")}
}

func importFileActionBindings(enterAction string) []key.Binding {
	return []key.Binding{primaryLoadFileShortcut, enterShortcut(enterAction), tabFieldsButtonsShortcut, cancelShortcut}
}

func exportFileActionBindings(enterAction string) []key.Binding {
	return []key.Binding{primaryExportVisibleShortcut, enterShortcut(enterAction), tabFormButtonsShortcut, cancelShortcut}
}

func selectOptionActionBindings(enterAction string) []key.Binding {
	return []key.Binding{primarySelectOptionShortcut, enterShortcut(enterAction), tabOptionsButtonsShortcut, cancelShortcut}
}

func formatActionBindings(enterAction string) []key.Binding {
	bindings := []key.Binding{primarySelectOptionShortcut, enterShortcut(enterAction)}
	bindings = append(bindings, formatShortcutBindings()...)

	return append(bindings, tabOptionsButtonsShortcut, cancelShortcut)
}

func filePickerActionBindings() []key.Binding {
	return []key.Binding{filePickerSelectShortcut, tabListAndButtonsShortcut, cancelShortcut}
}

func exportOverwriteActionBindings() []key.Binding {
	return []key.Binding{primaryOverwriteFileShortcut, enterFocusedButtonShortcut, tabButtonsShortcut, cancelShortcut}
}

func editorPopupActionBindings(enterAction string) []key.Binding {
	return []key.Binding{primarySaveShortcut, enterShortcut(enterAction)}
}

func importChildActionBindings(enterAction string, tabAction key.Binding, extra ...key.Binding) []key.Binding {
	bindings := make([]key.Binding, 0, 2+len(extra)+2)
	bindings = append(bindings, primaryApplyValuesShortcut, enterShortcut(enterAction))
	bindings = append(bindings, extra...)
	bindings = append(bindings, tabAction, cancelShortcut)

	return bindings
}

func editorTextAreaFooterBindings(style keymapStyle, viInsertMode bool, field editField) []key.Binding {
	bindings := []key.Binding{helpShortcut}

	switch {
	case style == keymapVi && viInsertMode:
		bindings = append(bindings, primarySaveShortcut, keyShortcut("esc", "normal"))
	case style == keymapVi:
		bindings = append(bindings, keyShortcut("i", "insert"), primarySaveShortcut, backShortcut)
	default:
		bindings = append(bindings, editorFieldPrimaryBindings(field)...)
		bindings = append(bindings, backShortcut)
	}

	if isExpandableEditField(field) {
		bindings = append(bindings, lineNumbersShortcut)
	}

	if action := editorFieldActionShortcut(field); action.Help().Key != "" {
		bindings = append(bindings, action)
	}

	return bindings
}

func editorFieldPrimaryBindings(field editField) []key.Binding {
	switch field {
	case editFieldRegion:
		return []key.Binding{enterShortcut("choose region"), primarySaveShortcut}
	case editFieldType:
		return []key.Binding{enterShortcut("choose type"), primarySaveShortcut}
	case editFieldTier:
		return []key.Binding{enterShortcut("choose tier"), primarySaveShortcut}
	case editFieldDataType:
		return []key.Binding{enterShortcut("choose data type"), primarySaveShortcut}
	case editFieldOverwrite:
		return []key.Binding{enterShortcut("choose overwrite"), primarySaveShortcut}
	case editFieldValue, editFieldSSMPath, editFieldDescription, editFieldPolicies, editFieldFilePath:
		return []key.Binding{primarySaveShortcut}
	}

	return []key.Binding{primarySaveShortcut}
}

func editorFieldActionShortcut(field editField) key.Binding {
	switch field {
	case editFieldValue:
		return altValueShortcut
	case editFieldDescription:
		return altDescriptionShortcut
	case editFieldPolicies:
		return altPoliciesShortcut
	case editFieldSSMPath, editFieldRegion, editFieldType, editFieldTier, editFieldDataType, editFieldOverwrite, editFieldFilePath:
		return key.Binding{}
	}

	return key.Binding{}
}

func screenActionBindings(forScreen screen, style keymapStyle, viInsertMode, applyImmediately bool) []key.Binding {
	switch forScreen {
	case screenHelp:
		return nil
	case screenMain:
		return mainHelpActionBindings(applyImmediately)
	case screenTextArea:
		return textAreaHelpActionBindings(style, viInsertMode)
	case screenColumns:
		return []key.Binding{
			columnsToggleShortcut,
			columnsShowAllShortcut,
			columnsHideOptionalShortcut,
			backShortcut,
		}
	case screenConfirm:
		return []key.Binding{enterConfirmShortcut, backShortcut}
	case screenRegionSelect, screenTypeSelect:
		return []key.Binding{enterShortcut("choose option"), backShortcut}
	case screenLoading:
		return []key.Binding{quitShortcut}
	default:
		return []key.Binding{backShortcut}
	}
}

func mainHelpActionBindings(applyImmediately bool) []key.Binding {
	bindings := []key.Binding{
		shortcutWithDescription(enterEditShortcut, "edit value"),
		shortcutWithDescription(newParameterShortcut, "new parameter"),
		shortcutWithDescription(importShortcut, "import from file"),
		shortcutWithDescription(exportShortcut, "export to file"),
		shortcutWithDescription(detailsShortcut, "show/hide details"),
		keyAliasesShortcut([]string{"/", "f"}, "/ / f", "filter"),
		columnsShortcut,
		shortcutWithDescription(sortShortcut, "sort popup"),
		shortcutWithDescription(deleteOneShortcut, "delete selected value"),
		shortcutWithDescription(deleteVisibleShortcutKey, "delete filtered values"),
	}

	if !applyImmediately {
		bindings = append(
			bindings,
			shortcutWithDescription(revertOneShortcut, "revert current local change"),
			shortcutWithDescription(revertVisibleShortcutKey, "revert filtered local changes"),
			shortcutWithDescription(pushOneShortcut, "push current local change"),
			shortcutWithDescription(pushVisibleShortcutKey, "push filtered local changes"),
		)
	}

	return append(bindings, quitShortcut)
}

func textAreaHelpActionBindings(style keymapStyle, viInsertMode bool) []key.Binding {
	if style == keymapVi {
		if viInsertMode {
			return []key.Binding{
				viNormalModeShortcut,
				primarySaveShortcut,
				shortcutWithDescription(focusedFieldActionsShortcut, "value/description/policies actions popup"),
			}
		}

		return []key.Binding{
			viInsertModeShortcut,
			primarySaveShortcut,
			shortcutWithDescription(focusedFieldActionsShortcut, "value/description/policies actions popup"),
			backShortcut,
		}
	}

	return []key.Binding{
		primarySaveShortcut,
		shortcutWithDescription(focusedFieldActionsShortcut, "value/description/policies actions popup"),
		enterShortcut("expand/newline in Description/Policies/Value / choose selectors / next field"),
		backShortcut,
	}
}

func rowNavigationBindings(style keymapStyle) []key.Binding {
	if style == keymapVi {
		return []key.Binding{
			keyAliasesShortcut([]string{"up", "k", "shift+tab"}, "↑ / k / shift+tab", "previous row/option"),
			keyAliasesShortcut([]string{"down", "j", "tab"}, "↓ / j / tab", "next row/option"),
			keyShortcut("PgUp", "page up"),
			keyShortcut("PgDn", "page down"),
			keyAliasesShortcut([]string{"home", "g"}, "Home / gg", "first row/option"),
			keyAliasesShortcut([]string{"end", "G"}, "End / G", "last row/option"),
		}
	}

	return []key.Binding{
		keyAliasesShortcut([]string{"up", ctrlBind("p"), "shift+tab"}, "↑ / ctrl+p / shift+tab", "previous row/option"),
		keyAliasesShortcut([]string{"down", ctrlBind("n"), "tab"}, "↓ / ctrl+n / tab", "next row/option"),
		keyAliasesShortcut([]string{"pgup", "pageup", "alt+v"}, "PgUp / alt+v", "page up"),
		keyAliasesShortcut([]string{"pgdown", "pagedown", ctrlBind("v")}, "PgDn / ctrl+v", "page down"),
		keyAliasesShortcut([]string{"home", "alt+<"}, "Home / alt+<", "first row/option"),
		keyAliasesShortcut([]string{"end", "alt+>"}, "End / alt+>", "last row/option"),
	}
}

func optionNavigationBindings(style keymapStyle) []key.Binding {
	if style == keymapVi {
		return []key.Binding{
			keyAliasesShortcut([]string{"up", "k"}, "↑ / k", "previous option"),
			keyAliasesShortcut([]string{"down", "j"}, "↓ / j", "next option"),
			keyShortcut("PgUp", "page up"),
			keyShortcut("PgDn", "page down"),
			keyAliasesShortcut([]string{"home", "g"}, "Home / gg", "first option"),
			keyAliasesShortcut([]string{"end", "G"}, "End / G", "last option"),
		}
	}

	return []key.Binding{
		keyAliasesShortcut([]string{"up", ctrlBind("p")}, "↑ / ctrl+p", "previous option"),
		keyAliasesShortcut([]string{"down", ctrlBind("n")}, "↓ / ctrl+n", "next option"),
		keyAliasesShortcut([]string{"pgup", "pageup", "alt+v"}, "PgUp / alt+v", "page up"),
		keyAliasesShortcut([]string{"pgdown", "pagedown", ctrlBind("v")}, "PgDn / ctrl+v", "page down"),
		keyAliasesShortcut([]string{"home", "alt+<"}, "Home / alt+<", "first option"),
		keyAliasesShortcut([]string{"end", "alt+>"}, "End / alt+>", "last option"),
	}
}

func editorPopupNavigationBindings(style keymapStyle, buttonsFocused bool, field editField, viInsertMode bool) []key.Binding {
	if buttonsFocused {
		return editorButtonNavigationBindings(style)
	}

	if style == keymapVi {
		bindings := []key.Binding{
			keyAliasesShortcut([]string{"up", "k"}, "↑ / k", "previous field"),
			keyAliasesShortcut([]string{"down", "j"}, "↓ / j", "next field"),
		}

		if isEditableTextField(field) {
			if !viInsertMode {
				bindings = append(bindings, viWordNavigationShortcut)
			}
		}

		if isMultilineEditField(field) {
			bindings = append(
				bindings,
				shortcutWithDescription(viTextPageUpShortcut, "page up"),
				shortcutWithDescription(viTextPageDownShortcut, "page down"),
			)
		}

		return append(
			bindings,
			keyAliasesShortcut([]string{"home", "g"}, "Home / gg", "first field"),
			keyAliasesShortcut([]string{"end", "G"}, "End / G", "last field"),
		)
	}

	bindings := []key.Binding{
		keyAliasesShortcut([]string{"up", ctrlBind("p")}, "↑ / ctrl+p", "previous field"),
		keyAliasesShortcut([]string{"down", ctrlBind("n")}, "↓ / ctrl+n", "next field"),
	}

	if isEditableTextField(field) {
		bindings = append(bindings, emacsWordNavigationShortcut)
	}

	if isMultilineEditField(field) {
		bindings = append(
			bindings,
			shortcutWithDescription(emacsTextPageUpShortcut, "page up"),
			shortcutWithDescription(emacsTextPageDownShortcut, "page down"),
		)
	}

	return append(
		bindings,
		keyAliasesShortcut([]string{"home", "alt+<"}, "Home / alt+<", "first field"),
		keyAliasesShortcut([]string{"end", "alt+>"}, "End / alt+>", "last field"),
	)
}

func editorButtonNavigationBindings(style keymapStyle) []key.Binding {
	if style == keymapVi {
		return []key.Binding{
			keyShortcut("←", "previous button"),
			keyShortcut("→", "next button"),
			keyAliasesShortcut([]string{"up", "k"}, "↑ / k", "return to form"),
			keyAliasesShortcut([]string{"home", "g"}, "Home / gg", "first field"),
			keyAliasesShortcut([]string{"end", "G"}, "End / G", "cancel button"),
		}
	}

	return []key.Binding{
		keyShortcut("←", "previous button"),
		keyShortcut("→", "next button"),
		keyAliasesShortcut([]string{"up", ctrlBind("p")}, "↑ / ctrl+p", "return to form"),
		keyAliasesShortcut([]string{"home", "alt+<"}, "Home / alt+<", "first field"),
		keyAliasesShortcut([]string{"end", "alt+>"}, "End / alt+>", "cancel button"),
	}
}

func fieldNavigationBindings(style keymapStyle) []key.Binding {
	if style == keymapVi {
		return []key.Binding{
			keyAliasesShortcut([]string{"up", "k", "shift+tab"}, "↑ / k / shift+tab", "previous field"),
			keyAliasesShortcut([]string{"down", "j", "tab"}, "↓ / j / tab", "next field"),
		}
	}

	return []key.Binding{
		keyAliasesShortcut([]string{"up", ctrlBind("p"), "shift+tab"}, "↑ / ctrl+p / shift+tab", "previous field"),
		keyAliasesShortcut([]string{"down", ctrlBind("n"), "tab"}, "↓ / ctrl+n / tab", "next field"),
		shortcutWithDescription(emacsWordNavigationShortcut, "backward/forward word in inputs"),
	}
}

func textAreaNavigationSections(style keymapStyle) []shortcutSection {
	if style == keymapVi {
		return []shortcutSection{
			{
				title: "Mode",
				bindings: []key.Binding{
					shortcutWithDescription(viInsertModeShortcut, "enter insert mode"),
					shortcutWithDescription(viNormalModeShortcut, "leave insert mode / back from normal mode"),
				},
			},
			{
				title: "Navigation",
				bindings: []key.Binding{
					combinedShortcut(viTextBackwardCharShortcut, viTextForwardCharShortcut, "backward/forward character"),
					combinedShortcut(viTextNextLineShortcut, viTextPreviousLineShortcut, "next/previous line in Description/Policies/Value"),
					shortcutWithDescription(viTextPageDownShortcut, "page down in Description/Policies/Value"),
					shortcutWithDescription(viTextPageUpShortcut, "page up in Description/Policies/Value"),
					viWordNavigationShortcut,
					keyAliasesShortcut([]string{"0", "$"}, "0 / $", "start/end of line"),
					keyAliasesShortcut([]string{"g", "G"}, "gg / G", "start/end of text"),
					keyShortcut("tab", "next field"),
					keyShortcut("shift+tab", "previous field"),
					keyAliasesShortcut([]string{"pgup", "pageup", "pgdown", "pagedown"}, "PgUp / PgDn", "page in Description/Policies/Value"),
				},
			},
			{
				title: "Editing",
				bindings: []key.Binding{
					viTextDeleteCharShortcut,
					viTextDeleteToLineEnd,
					viDeleteNextWordShortcut,
					viDeletePrevWordShortcut,
					shortcutWithDescription(lineNumbersShortcut, "show/hide line numbers"),
				},
			},
		}
	}

	return []shortcutSection{
		{
			title: "Navigation",
			bindings: []key.Binding{
				keyShortcut("tab", "next field"),
				keyShortcut("shift+tab", "previous field"),
				combinedShortcut(emacsTextForwardCharShortcut, emacsTextBackwardCharShortcut, "forward/backward character"),
				combinedShortcut(emacsTextPreviousLineShortcut, emacsTextNextLineShortcut, "previous/next line"),
				shortcutWithDescription(emacsTextPageDownShortcut, "page down in Description/Policies/Value"),
				shortcutWithDescription(emacsTextPageUpShortcut, "page up in Description/Policies/Value"),
				combinedShortcut(emacsTextLineStartShortcut, emacsTextLineEndShortcut, "start/end of line"),
				combinedShortcut(emacsWordForwardShortcut, emacsWordBackwardShortcut, "forward/backward word"),
				keyAliasesShortcut([]string{"alt+<", "alt+>"}, "alt+< / alt+>", "start/end of text"),
				shortcutWithDescription(lineNumbersShortcut, "show/hide line numbers"),
			},
		},
		{
			title: "Editing",
			bindings: []key.Binding{
				emacsTextDeleteCharShortcut,
				emacsTextKillLineShortcut,
				emacsTextDeleteNextWord,
				emacsTextDeletePrevWord,
			},
		},
	}
}

func filePickerNavigationBindings(style keymapStyle) []key.Binding {
	if style == keymapVi {
		return []key.Binding{
			filePickerViPreviousShortcut,
			filePickerViNextShortcut,
			filePickerViPageUpShortcut,
			filePickerViPageDownShortcut,
			filePickerViFirstShortcut,
			filePickerViLastShortcut,
			filePickerViParentShortcut,
			filePickerViOpenShortcut,
		}
	}

	return []key.Binding{
		filePickerEmacsPreviousShortcut,
		filePickerEmacsNextShortcut,
		filePickerEmacsPageUpShortcut,
		filePickerEmacsPageDownShortcut,
		filePickerEmacsFirstShortcut,
		filePickerEmacsLastShortcut,
		filePickerEmacsParentShortcut,
		filePickerEmacsOpenShortcut,
	}
}

func formatShortcutBindings() []key.Binding {
	return []key.Binding{
		shortcutWithDescription(dotenvFormatShortcut, "Dotenv"),
		shortcutWithDescription(jsonFormatShortcut, "JSON"),
		shortcutWithDescription(yamlFormatShortcut, "YAML"),
	}
}

func sortFooterBindings(items []sortItem) []key.Binding {
	bindings := []key.Binding{
		helpShortcut,
		primaryCloseShortcut,
		enterFocusedControlShortcut,
		columnsToggleShortcut,
		sortDirectionShortcut,
	}

	for _, item := range items {
		bindings = append(bindings, keyShortcut(item.hotkey, strings.ToLower(item.label)))
	}

	return append(bindings, closeShortcut)
}

func renderSortShortcutSection(items []sortItem) string {
	if len(items) == 0 {
		return ""
	}

	bindings := make([]key.Binding, 0, len(items)+1)
	for _, item := range items {
		bindings = append(bindings, keyShortcut(item.hotkey, "sort by "+item.column.Label()))
	}

	return renderShortcutSections([]shortcutSection{{title: "Sort", bindings: append(bindings, sortDirectionShortcut)}})
}

func detailsToggleShortcut(detailsShown bool) key.Binding {
	if detailsShown {
		return keyShortcut("d", "hide details")
	}

	return keyShortcut("d", "show details")
}

func deleteVisibleShortcut(filtered bool) key.Binding {
	if filtered {
		return shortcutWithDescription(deleteVisibleShortcutKey, "delete filtered")
	}

	return shortcutWithDescription(deleteVisibleShortcutKey, "delete all")
}

func revertVisibleShortcut(filtered bool) key.Binding {
	if filtered {
		return shortcutWithDescription(revertVisibleShortcutKey, "revert filtered")
	}

	return shortcutWithDescription(revertVisibleShortcutKey, "revert all")
}

func pushVisibleShortcut(filtered bool) key.Binding {
	if filtered {
		return shortcutWithDescription(pushVisibleShortcutKey, "push filtered")
	}

	return shortcutWithDescription(pushVisibleShortcutKey, "push all")
}

var (
	revertOneShortcut = keyShortcut("r", "revert")
	pushOneShortcut   = keyShortcut("p", "push")

	columnsToggleShortcut       = keyAliasesShortcut([]string{" ", "space", "enter", ctrlBind("j")}, "space/enter", "toggle")
	columnsShowAllShortcut      = keyShortcut("a", "show all")
	columnsHideOptionalShortcut = keyShortcut("x", "hide all")
	sortDirectionShortcut       = keyShortcut("d", "direction")

	tabButtonsShortcut        = keyShortcut("tab", "move between buttons")
	tabControlsShortcut       = keyShortcut("tab", "move between controls")
	tabListButtonsShortcut    = keyShortcut("tab", "move between list and buttons")
	tabFormButtonsShortcut    = keyShortcut("tab", "move between form and buttons")
	tabFieldsButtonsShortcut  = keyShortcut("tab", "move through fields and buttons")
	tabRowsButtonsShortcut    = keyShortcut("tab", "move through rows and buttons")
	tabActionsButtonsShortcut = keyShortcut("tab", "move between actions and buttons")
	tabOptionsButtonsShortcut = keyShortcut("tab", "move between options and buttons")
	tabInputButtonsShortcut   = keyShortcut("tab", "move between input and buttons")
	tabListAndButtonsShortcut = keyShortcut("tab", "move between list and buttons")

	clearValueShortcut       = keyShortcut("c", "clear value")
	clearPoliciesShortcut    = keyShortcut("c", "clear policies")
	clearDescriptionShortcut = keyShortcut("c", "clear description")
	randomValueShortcut      = keyShortcut("r", "random value")
	loadFromFileShortcut     = keyShortcut("l", "load from file")
	writeToFileShortcut      = keyShortcut("w", "write to file")

	randomBase64Shortcut = keyShortcut("b", "base64 32 bytes")
	randomHexShortcut    = keyShortcut("x", "hex 32 bytes")
	randomUUIDShortcut   = keyShortcut("u", "uuid")
	randomCustomShortcut = keyShortcut("c", "custom length base64")
)
