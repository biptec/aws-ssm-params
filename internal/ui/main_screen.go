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
// It also owns search mode, where printable keys update the active filter instead of triggering table shortcuts.
func (component mainScreenComponent) updateMain(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	m.message = ""

	key := msg.String()
	if m.searchMode {
		switch key {
		case "ctrl+_", "ctrl+/":
			m.openShortcuts(screenMain)
			return m, nil
		case "esc", "ctrl+g":
			m.searchMode = false
			if m.searchInvalid {
				m.query = m.effectiveQuery
				m.searchInvalid = false
			}

			return m, nil
		case "backspace":
			if m.query != "" {
				m.applySearchQuery(m.query[:len(m.query)-1])
			}

			return m, nil
		case "enter":
			m.searchMode = false
			if m.searchInvalid {
				m.query = m.effectiveQuery
				m.searchInvalid = false
			}

			return m, nil
		default:
			if len(msg.String()) == 1 {
				m.applySearchQuery(m.query + msg.String())
			}

			return m, nil
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

	if m.keymapStyle() == keymapVi && key == "g" {
		m.pendingKeySequence = "g"
		return m, nil
	}

	switch key {
	case "q", "esc":
		return m, tea.Quit
	case "enter", "ctrl+j":
		if len(m.visible()) == 0 {
			return m, nil
		}

		return m.startMultiline()
	case "n":
		return m.startNewParameter(screenMain)
	case "i":
		m.openImportPopup()
	case "/":
		m.searchMode = true
		m.query = m.effectiveQuery
		m.searchInvalid = false
	case "d":
		m.selectedExpanded = !m.selectedExpanded
	case "c":
		m.openColumnsPopup()
	case "s":
		m.openSortPopup()
	case "0", "1", "2", "3", "4", "5", "6", "7", "8", "9":
		if col, ok := m.visibleSortColumnByHotkey(key); ok {
			m.applySort(col)
		}
	case "x":
		if len(m.visible()) > 0 {
			items := inventory.Items{m.currentItem()}
			if m.opts.NoConfirmDeleteOne {
				changed := m.applyLocalDeleteItems(items)
				m.message = fmt.Sprintf("Marked %d parameter(s) for deletion. Press p to push.", changed)
				m.ensureSelection()

				return m, nil
			}

			m.startConfirm("Delete selected parameter?", "", items, screenMain)
		}
	case "X":
		items := m.visibleItems()
		if len(items) > 0 {
			scope := m.mainListScope()
			if m.opts.NoConfirmDeleteAll {
				changed := m.applyLocalDeleteItems(items)
				m.message = fmt.Sprintf("Marked %d parameter(s) for deletion. Press P to push %s.", changed, scope)
				m.ensureSelection()

				return m, nil
			}

			m.startConfirm(fmt.Sprintf("Delete %d visible parameter(s)?", len(items)), "", items, screenMain)
		}
	case "r":
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
	case "R":
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
	case "p":
		indexes := m.currentDirtyStatusIndexes()
		if len(indexes) == 0 {
			m.message = "No local change to push."
			m.errMessage = ""
			m.warningMessage = ""
			return m, nil
		}

		m.startPushConfirm("Push selected local change?", indexes, screenMain, false)
	case "P":
		indexes := m.visibleDirtyStatusIndexes()
		if len(indexes) == 0 {
			m.message = "No visible local changes to push."
			m.errMessage = ""
			m.warningMessage = ""
			return m, nil
		}

		m.startPushConfirm(fmt.Sprintf("Push %d %s local change(s)?", len(indexes), m.mainListScope()), indexes, screenMain, true)
	case "ctrl+_", "ctrl+/":
		m.openShortcuts(screenMain)
	}

	m.ensureSelection()

	return m, nil
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

	return m.renderPopupBoxWithActions("Shortcuts", lines, "Esc close")
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
