package ui

import (
	"fmt"
	"strings"

	"github.com/biptec/aws-ssm-params/internal/inventory"
	tea "github.com/charmbracelet/bubbletea"
)

type mainScreenComponent struct {
	model model
}

// updateMain handles navigation and actions on the main parameter table.
// It also owns filter mode, where printable keys update the active local filter instead of triggering table shortcuts.
func (component mainScreenComponent) updateMain(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	m.message = ""

	key := msg.String()

	if m.filterMode {
		switch {
		case isHelpKeyMsg(msg):
			m.openShortcuts(screenMain)
			return m, nil
		case isEscapeCloseKeyMsg(msg):
			m.closeFilterMode()
			return m, nil
		case isEnterKeyMsg(msg):
			m.closeFilterMode()
			return m, nil
		default:
			before := m.filterInput.Value()

			cmd := m.updateTextInput(&m.filterInput, msg)
			if after := m.filterInput.Value(); after != before {
				m.applyFilterQuery(after)
			}

			return m, cmd
		}
	}

	if action, ok, consumed := (&m).handlePendingNavigationSequence(key); consumed {
		if ok {
			m.applyMainNavigation(action)
		}

		m.ensureSelection()

		return m, nil
	}

	if action, ok := m.navigationAction(key); ok {
		m.applyMainNavigation(action)
		m.ensureSelection()

		return m, nil
	}

	if m.keymapStyle() == keymapVi && isViFirstNavigationPrefixString(key) {
		m.pendingKeySequence = firstBindingKey(viFirstNavigationPrefixShortcut)
		return m, nil
	}

	if isHelpKeyMsg(msg) {
		m.openShortcuts(screenMain)
		return m, nil
	}

	if isEnterKeyMsg(msg) {
		if len(m.visible()) == 0 {
			return m, nil
		}

		return m.startMultiline()
	}

	switch {
	case isQuitKeyMsg(msg):
		return m, tea.Quit
	case isNewParameterShortcutMsg(msg):
		return m.startNewParameter(screenMain)
	case isImportShortcutMsg(msg):
		m.openImportPopup()
	case isExportShortcutMsg(msg):
		m.openExportPopup()
	case isFilterShortcutMsg(msg):
		m.openFilterMode()
	case isDetailsShortcutMsg(msg):
		m.selectedExpanded = !m.selectedExpanded
	case isColumnsShortcutMsg(msg):
		m.openColumnsPopup()
	case isSortShortcutMsg(msg):
		m.openSortPopup()
	case isSortColumnShortcut(key):
		if col, ok := m.visibleSortColumnByHotkey(key); ok {
			m.applySort(col)
		}
	case isDeleteOneShortcutMsg(msg):
		if len(m.visible()) > 0 {
			items := inventory.Items{m.currentItem()}
			if m.opts.ApplyImmediately {
				m.startConfirm("Delete selected parameter?", "", items, screenMain)
			} else {
				changed := m.applyLocalDeleteItems(items)
				m.message = fmt.Sprintf("Marked %d parameter(s) for deletion. Press p to push.", changed)
				m.ensureSelection()
			}
		}
	case isDeleteVisibleShortcutMsg(msg):
		items := m.visibleItems()
		if len(items) > 0 {
			scope := m.mainListScope()
			if m.opts.ApplyImmediately {
				m.startConfirm(fmt.Sprintf("Delete %d visible parameter(s)?", len(items)), "", items, screenMain)
			} else {
				changed := m.applyLocalDeleteItems(items)
				m.message = fmt.Sprintf("Marked %d parameter(s) for deletion. Press P to push %s.", changed, scope)
				m.ensureSelection()
			}
		}
	case isRevertOneShortcutMsg(msg):
		if m.opts.ApplyImmediately {
			return m, nil
		}

		operation, ok := m.revertCurrentLocalChange()
		if !ok {
			m.message = "No local change to revert."
			m.errMessage = ""
			m.warningMessage = ""

			return m, nil
		}

		m.message = fmt.Sprintf("Reverted %s local change.", operation)
		m.errMessage = ""
		m.warningMessage = ""
		m.applySortWithRules(m.sortRulesOrDefault())
	case isRevertVisibleShortcutMsg(msg):
		if m.opts.ApplyImmediately {
			return m, nil
		}

		indexes := m.visibleDirtyStatusIndexes()
		if len(indexes) == 0 {
			m.message = "No visible local changes to revert."
			m.errMessage = ""
			m.warningMessage = ""

			return m, nil
		}

		scope := m.mainListScope()
		changed := m.revertLocalChanges(indexes)
		m.message = fmt.Sprintf("Reverted %d %s local change(s).", changed, scope)
		m.errMessage = ""
		m.warningMessage = ""
		m.applySortWithRules(m.sortRulesOrDefault())
	case isPushOneShortcutMsg(msg):
		if m.opts.ApplyImmediately {
			return m, nil
		}

		indexes := m.currentDirtyStatusIndexes()
		if len(indexes) == 0 {
			m.message = "No local change to push."
			m.errMessage = ""
			m.warningMessage = ""

			return m, nil
		}

		m.startPushConfirm("Push selected local change?", indexes, screenMain, false)
	case isPushVisibleShortcutMsg(msg):
		if m.opts.ApplyImmediately {
			return m, nil
		}

		indexes := m.visibleDirtyStatusIndexes()
		if len(indexes) == 0 {
			m.message = "No visible local changes to push."
			m.errMessage = ""
			m.warningMessage = ""

			return m, nil
		}

		m.startPushConfirm(fmt.Sprintf("Push %d %s local change(s)?", len(indexes), m.mainListScope()), indexes, screenMain, true)
	}

	m.ensureSelection()

	return m, nil
}

func isSortColumnShortcut(key string) bool {
	return len(key) == 1 && key[0] >= '0' && key[0] <= '9'
}

func (m model) mainListScope() string {
	if m.mainListFiltered() {
		return "filtered"
	}

	return "all"
}

func (component *mainScreenComponent) applyMainNavigation(action navigationAction) {
	m := &component.model

	switch action {
	case navNone:
		return
	case navPrevious:
		m.move(-1)
	case navNext:
		m.move(1)
	case navPageUp:
		m.move(-pageSize(m.listBodyHeight()))
	case navPageDown:
		m.move(pageSize(m.listBodyHeight()))
	case navFirst:
		m.selected = 0
	case navLast:
		vis := m.visible()
		if len(vis) > 0 {
			m.selected = len(vis) - 1
		}

	default:
	}
}

// updateLoading handles shortcuts that must remain available while long SSM scans are running.
// The footer advertises q quit on the loading screen, while ctrl+c is handled globally with confirmation.

func (component mainScreenComponent) renderMainScreen() string {
	m := component.model
	if !m.selectedExpanded || m.currentStatus().Item.Path == "" {
		return m.renderListBlock()
	}

	return m.renderSelectedParameterBlock(true) + "\n" + m.renderListBlock()
}

// renderTextAreaScreen renders the unified editor for multiline values plus editable metadata/file fields.
func (component mainScreenComponent) renderHelpScreen() string {
	m := component.model
	shortcutLines := strings.Split(m.shortcutsText(), "\n")

	lines := make([]string, 0, len(shortcutLines))
	for _, line := range shortcutLines {
		if line == "" {
			lines = append(lines, "")
			continue
		}

		lines = append(lines, "  "+line)
	}

	return m.renderBox("Shortcuts", lines, m.height)
}

func (component mainScreenComponent) renderShortcutsPopup() string {
	m := component.model
	lines := strings.Split(m.shortcutsText(), "\n")

	return m.renderPopupBoxWithActions("Shortcuts", lines, renderFooter(closeShortcut))
}

// renderLoading renders a centered loading overlay while the initial background scan is running.
func (component mainScreenComponent) renderLoading() string {
	m := component.model
	bodyLines := make([]string, max(1, m.height))
	body := strings.Join(bodyLines, "\n")

	return m.overlayPopupOnBody(body, m.renderLoadingPopup())
}

func (component mainScreenComponent) renderLoadingPopup() string {
	m := component.model

	title := strings.TrimSpace(m.loadingTitle)
	if title == "" {
		title = "Loading parameters"
	}

	spinner := loadingSpinnerFrames[m.loadingSpinnerFrame%len(loadingSpinnerFrames)]

	return m.renderPopupBox("Loading", []string{fmt.Sprintf("%s %s", title, spinner)})
}
