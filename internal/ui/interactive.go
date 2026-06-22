package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

type interactiveComponent struct {
	model model
}

// Update is the Bubble Tea state machine.
// It handles window changes, async loader/save/delete results, and keypresses, then delegates screen-specific input
// to smaller update helpers so each view owns its own shortcuts and transitions.
func (component interactiveComponent) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m := component.model
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.Width = max(20, msg.Width-12)
		m.editPathInput.Width = max(20, msg.Width-18)
		m.editDescriptionInput.Width = max(20, msg.Width-18)
		m.editFileInput.Width = max(20, msg.Width-18)
		m.textArea.SetWidth(max(20, msg.Width-14))
		m.textArea.SetHeight(max(8, msg.Height-10))
		m.editPoliciesArea.SetWidth(max(20, msg.Width-14))
		m.editPoliciesArea.SetHeight(max(1, msg.Height-10))
		m.editDescriptionArea.SetWidth(max(20, msg.Width-14))
		m.editDescriptionArea.SetHeight(max(1, msg.Height-10))
		return m, nil

	case progressMsg:
		m.loadingTitle = "Loading parameters"
		m.loadingLines = nil
		if msg.region != "" {
			m.busyMessage = fmt.Sprintf("Loading parameters %d/%d from %s region...", msg.done, msg.total, msg.region)
		} else {
			m.busyMessage = fmt.Sprintf("Loading parameters %d/%d...", msg.done, msg.total)
		}
		return m, waitForLoad(m.loadCh)

	case loadingTickMsg:
		if m.screen == screenLoading {
			m.loadingSpinnerFrame = (m.loadingSpinnerFrame + 1) % len(loadingSpinnerFrames)
			return m, tickLoadingSpinner()
		}
		return m, nil

	case statusBatchMsg:
		m.mergeStatusBatch([]Status(msg))
		return m, waitForLoad(m.loadCh)

	case loadedMsg:
		m.statuses = []Status(msg)
		m.applySortWithRules(m.sortRulesOrDefault())
		m.screen = screenMain
		m.busyMessage = ""
		m.loadingTitle = ""
		m.loadingLines = nil
		m.ensureSelection()
		return m, nil

	case statusUpdatedMsg:
		m.busyMessage = ""
		if msg.err != nil {
			m.errMessage = msg.err.Error()
			m.screen = m.returnScreen
			return m, nil
		}
		matchPath := msg.oldPath
		if matchPath == "" {
			matchPath = msg.path
		}
		m.replaceStatus(matchPath, msg.status)
		m.ensureSelection()
		m.message = msg.message
		m.warningMessage = msg.warning
		m.errMessage = ""
		m.screen = m.returnScreen
		return m, nil

	case deleteDoneMsg:
		m.busyMessage = ""
		if msg.err != nil {
			m.errMessage = msg.err.Error()
			m.screen = m.returnScreen
			return m, nil
		}
		if msg.removeRows {
			m.removeItemRows(msg.items)
		} else {
			for _, item := range msg.items {
				m.markMissingItem(item)
			}
		}
		m.message = fmt.Sprintf("Deleted %d parameter(s)", len(msg.items))
		m.warningMessage = msg.warning
		m.errMessage = ""
		m.screen = m.returnScreen
		m.ensureSelection()
		return m, nil

	case tea.KeyMsg:
		key := msg.String()
		if m.pendingQuit && key == "y" {
			return m, tea.Quit
		}
		if key == "ctrl+c" || key == "ctrl+q" {
			m.message = ""
			m.errMessage = ""
			m.warningMessage = quitConfirmationMessage(key)
			m.pendingQuit = true
			m.pendingQuitKey = key
			m.pendingFileWrite = fileWriteConfirmationNone
			return m, nil
		}
		fileWriteConfirmKey := m.pendingFileWrite != fileWriteConfirmationNone && (key == "y" || key == "enter" || key == "ctrl+j" || key == "esc" || key == "q" || key == "ctrl+g" || m.activePopup == popupFileWriteConfirm)
		if !fileWriteConfirmKey {
			m.clearTransientStatus()
		}
		if m.activePopup != popupNone {
			switch m.activePopup {
			case popupNone:
			case popupColumns:
				return m.updateColumnsPopup(msg)
			case popupShortcuts:
				return m.updateShortcutsPopup(msg)
			case popupConfirm:
				return m.updateConfirmPopup(msg)
			case popupSort:
				return m.updateSortPopup(msg)
			case popupRegionSelect:
				return m.updateRegionSelectPopup(msg)
			case popupTypeSelect:
				return m.updateTypeSelectPopup(msg)
			case popupTierSelect:
				return m.updateTierSelectPopup(msg)
			case popupDataTypeSelect:
				return m.updateDataTypeSelectPopup(msg)
			case popupOverwriteSelect:
				return m.updateOverwriteSelectPopup(msg)
			case popupValueActions:
				return m.updateValueActionsPopup(msg)
			case popupPoliciesActions:
				return m.updatePoliciesActionsPopup(msg)
			case popupFileAction:
				return m.updateFileActionPopup(msg)
			case popupFileWriteConfirm:
				return m.updateFileWriteConfirmPopup(msg)
			case popupUnsavedChanges:
				return m.updateUnsavedChangesPopup(msg)
			case popupRandomValue:
				return m.updateRandomValuePopup(msg)

			default:
			}
		}

		switch m.screen {
		case screenMain:
			return m.updateMain(msg)
		case screenTextArea:
			return m.updateTextArea(msg)
		case screenColumns:
			return m.updateColumns(msg)
		case screenConfirm:
			return m.updateConfirm(msg)
		case screenRegionSelect:
			return m.updateRegionSelect(msg)
		case screenTypeSelect:
			return m.updateTypeSelect(msg)
		case screenHelp:
			return m.updateHelp(msg)
		case screenLoading:
			return m.updateLoading(msg)
		}
	}

	if m.screen == screenTextArea {
		var cmd tea.Cmd
		if m.editField == editFieldPolicies {
			m.editPoliciesArea, cmd = m.editPoliciesArea.Update(msg)
		} else {
			m.textArea, cmd = m.textArea.Update(msg)
		}
		return m, cmd
	}
	return m, nil
}

// View renders the active screen plus a fixed footer with the hotkeys valid for that screen.
// Keeping footer text here ensures rendered shortcuts stay aligned with the screen selected by Update.
func (component interactiveComponent) View() string {
	m := component.model
	switch m.screen {
	case screenLoading:
		return m.renderPage("ctrl+/ help • esc quit", func(content model) string { return content.renderLoading() })
	case screenMain:
		footer := mainFooterText(m.selectedExpanded && m.currentStatus().Item.Path != "")
		if m.searchMode {
			footer = searchFooterText()
		}
		return m.renderPage(footer, func(content model) string { return content.renderMainScreen() })
	case screenTextArea:
		return m.renderPage(m.textAreaFooterText(), func(content model) string { return content.renderTextAreaScreen() })
	case screenColumns:
		return m.renderPage("ctrl+/ help • space/enter toggle • a show all • x hide all • esc back", func(content model) string { return content.renderColumnsScreen() })
	case screenConfirm:
		return m.renderPage("ctrl+/ help • enter confirm • esc back", func(content model) string { return content.renderConfirmScreen() })
	case screenRegionSelect:
		return m.renderPage("ctrl+/ help • enter choose • esc back", func(content model) string { return content.renderRegionSelectScreen() })
	case screenTypeSelect:
		return m.renderPage("ctrl+/ help • enter choose • esc back", func(content model) string { return content.renderTypeSelectScreen() })
	case screenHelp:
		return m.renderPage("esc back", func(content model) string { return content.renderHelpScreen() })
	default:
		return ""
	}
}

func (component interactiveComponent) renderPage(footerText string, renderBody func(model) string) string {
	m := component.model
	if m.activePopup != popupNone {
		footerText = m.popupFooterText(m.activePopup)
	}
	bottom := m.renderFooterWithStatus(footerText)
	content := m
	if m.height > 0 {
		content.height = max(1, m.height-countLines(bottom))
	}
	body := renderBody(content)
	body = content.renderPopupStack(body)
	return m.renderFullscreen(body, bottom)
}

func (component interactiveComponent) updateLoading(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	switch msg.String() {
	case "ctrl+_", "ctrl+/":
		m.openShortcuts(screenLoading)
		return m, nil
	case "q", "esc":
		return m, tea.Quit
	}
	return m, nil
}
