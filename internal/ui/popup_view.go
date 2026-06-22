package ui

import (
	"fmt"
	"strings"
)

type popupViewComponent struct {
	model model
}

func (component popupViewComponent) renderSortPopup() string {
	m := component.model
	return m.renderPopupBoxWithActions("Sort By", m.sortOptionLines(), "Space toggle   D direction   Esc close")
}

func (component popupViewComponent) renderValueActionsPopup() string {
	m := component.model
	items := valueActionItems()
	lines := make([]string, 0, len(items))
	for i, item := range items {
		lines = append(lines, m.singleSelectLine(item.label, i == m.valueActionCursor, i == m.valueActionCursor))
	}
	return m.renderPopupBoxWithActions("Value Actions", lines, "Enter select   Esc cancel")
}

func (component popupViewComponent) renderPoliciesActionsPopup() string {
	m := component.model
	items := policiesActionItems()
	lines := make([]string, 0, len(items))
	for i, item := range items {
		lines = append(lines, m.singleSelectLine(item.label, i == m.valueActionCursor, i == m.valueActionCursor))
	}
	return m.renderPopupBoxWithActions("Policies Actions", lines, "Enter select   Esc cancel")
}

func (component popupViewComponent) renderFileActionPopup() string {
	m := component.model
	title := "Load from file"
	if m.fileActionField == editFieldPolicies {
		title = "Load policies from file"
	}
	label := "File path:"
	inputWidth := 48
	switch m.fileActionMode {
	case "write":
		title = "Write to file"
		if m.fileActionField == editFieldPolicies {
			title = "Write policies to file"
		}
	case "random-custom":
		title = "Random Value"
		label = "Byte length:"
		inputWidth = 12
	}
	button := "load"
	switch m.fileActionMode {
	case "write":
		button = "write"
	case "random-custom":
		button = "generate"
	}
	lines := []string{m.popupInputLine(label, m.input, inputWidth)}
	return m.renderPopupBoxWithActions(title, lines, "Enter "+button+"   Esc cancel")
}

func (component popupViewComponent) renderFileWriteConfirmPopup() string {
	m := component.model
	message := "Confirm file write?"
	switch m.pendingFileWrite {
	case fileWriteConfirmationNone:
	case fileWriteConfirmationSecure:
		message = "This is a SecureString value. Write it to a local file in plain text?"
	case fileWriteConfirmationOverwrite:
		message = "File already exists. Overwrite it?"

	default:
	}
	return m.renderPopupBoxWithActions("Confirm", []string{message}, "Enter yes   Esc cancel")
}

func (component popupViewComponent) renderUnsavedChangesPopup() string {
	m := component.model
	return m.renderPopupBoxWithActions("Confirm", []string{"Unsaved changes. Discard unsaved changes?"}, "Enter discard   Esc cancel")
}

func (component popupViewComponent) renderRandomValuePopup() string {
	m := component.model
	items := randomItems()
	lines := make([]string, 0, len(items))
	for i, item := range items {
		lines = append(lines, m.singleSelectLine(item.label, i == m.randomCursor, i == m.randomCursor))
	}
	return m.renderPopupBoxWithActions("Random Value", lines, "Enter select   Esc cancel")
}

func (component popupViewComponent) sortOptionLines() []string {
	m := component.model
	items := m.popupSortItems()
	lines := make([]string, 0, len(items))
	if len(items) > 0 && m.sortCursor >= len(items) {
		m.sortCursor = len(items) - 1
	}
	for i, item := range items {
		_, checked := sortRuleForColumn(m.sortRules, item.column)
		lines = append(lines, m.multiSelectLine(m.sortPopupLabel(item), checked, i == m.sortCursor))
	}
	return lines
}

// renderConfirmScreen renders the destructive-action confirmation prompt and input field.
func (component popupViewComponent) renderConfirmScreen() string {
	m := component.model
	confirmLines := strings.Split(m.confirmPrompt, "\n")
	lines := make([]string, 0, len(confirmLines)+2)
	for _, line := range confirmLines {
		lines = append(lines, "  "+line)
	}
	lines = append(lines, "", "  > "+m.input.View())
	return m.renderBox("Confirm", lines, m.height)
}

func (component popupViewComponent) renderConfirmPopup() string {
	m := component.model
	confirmLines := strings.Split(m.confirmPrompt, "\n")
	lines := make([]string, 0, len(confirmLines)+2)
	for _, line := range confirmLines {
		if strings.TrimSpace(line) == "" {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, line)
	}
	if m.confirmExpected != "" {
		prefix := "Type " + m.value(m.confirmExpected) + " to confirm: "
		lines = append(lines, "", m.popupInputLinePlainPrefix(prefix, m.input, max(len(m.confirmExpected)+1, 18)))
	}
	return m.renderPopupBoxWithActions("Confirm", lines, "Enter confirm   Esc cancel")
}

// renderRegionSelectScreen renders the region picker used before saving wildcard/all-regions items.
func (component popupViewComponent) renderRegionSelectScreen() string {
	m := component.model
	regions := m.regionSelectOptions()
	lines := make([]string, 0, 2+len(regions))
	lines = append(lines, "  "+m.muted("Choose region for saving this value:"), "")
	for i, region := range regions {
		row := region
		if i == m.regionCursor {
			row = m.selectedMarker() + m.selectedRow(row)
		} else {
			row = "  " + row
		}
		lines = append(lines, "  "+row)
	}
	return m.renderBox("Region", lines, m.height)
}

func (component popupViewComponent) renderRegionSelectPopup() string {
	m := component.model
	return m.renderPopupBoxWithActions("Region", m.regionSelectLines(), "Enter select   Esc cancel")
}

func (component popupViewComponent) regionSelectLines() []string {
	m := component.model
	regions := m.regionSelectOptions()
	lines := make([]string, 0, 2+len(regions))
	lines = append(lines, m.muted("Choose region for saving this value:"), "")
	for i, region := range regions {
		lines = append(lines, m.singleSelectLine(region, i == m.regionCursor, i == m.regionCursor))
	}
	return lines
}

// renderTypeSelectScreen renders the AWS SSM parameter type picker used by value editors.
func (component popupViewComponent) renderTypeSelectScreen() string {
	m := component.model
	typeItems := parameterTypeItems()
	lines := make([]string, 0, 2+len(typeItems))
	lines = append(lines, "  "+m.muted("Choose how this value should be stored in AWS SSM Parameter Store:"), "")
	for i, it := range typeItems {
		row := fmt.Sprintf("%s — %s", it.label, it.description)
		if i == m.typeCursor {
			row = m.selectedMarker() + m.selectedRow(row)
		} else {
			row = "  " + row
		}
		lines = append(lines, "  "+row)
	}
	return m.renderBox("Parameter Type", lines, m.height)
}

func (component popupViewComponent) renderTypeSelectPopup() string {
	m := component.model
	return m.renderPopupBoxWithActions("Parameter Type", m.typeSelectLines(), "Enter select   Esc cancel")
}

func (component popupViewComponent) renderTierSelectPopup() string {
	m := component.model
	return m.renderPopupBoxWithActions("Parameter Tier", m.tierSelectLines(), "Enter select   Esc cancel")
}

func (component popupViewComponent) renderDataTypeSelectPopup() string {
	m := component.model
	return m.renderPopupBoxWithActions("Data Type", m.dataTypeSelectLines(), "Enter select   Esc cancel")
}

func (component popupViewComponent) renderOverwriteSelectPopup() string {
	m := component.model
	return m.renderPopupBoxWithActions("Overwrite", m.overwriteSelectLines(), "Enter select   Esc cancel")
}

func (component popupViewComponent) typeSelectLines() []string {
	m := component.model
	typeItems := parameterTypeItems()
	lines := make([]string, 0, 2+len(typeItems))
	lines = append(lines, m.muted("Choose how this value should be stored in AWS SSM Parameter Store:"), "")
	for i, it := range typeItems {
		row := fmt.Sprintf("%s — %s", it.label, it.description)
		lines = append(lines, m.singleSelectLine(row, i == m.typeCursor, i == m.typeCursor))
	}
	return lines
}

func (component popupViewComponent) tierSelectLines() []string {
	m := component.model
	tierItems := parameterTierItems()
	lines := make([]string, 0, 2+len(tierItems))
	lines = append(lines, m.muted("Choose the AWS SSM storage tier for this parameter:"), "")
	for i, it := range tierItems {
		row := fmt.Sprintf("%s — %s", it.label, it.description)
		lines = append(lines, m.singleSelectLine(row, i == m.tierCursor, i == m.tierCursor))
	}
	return lines
}

func (component popupViewComponent) dataTypeSelectLines() []string {
	m := component.model
	dataTypeItems := parameterDataTypeItems()
	lines := make([]string, 0, 2+len(dataTypeItems))
	lines = append(lines, m.muted("Choose AWS SSM value validation data type:"), "")
	for i, it := range dataTypeItems {
		row := fmt.Sprintf("%s — %s", it.label, it.description)
		lines = append(lines, m.singleSelectLine(row, i == m.dataTypeCursor, i == m.dataTypeCursor))
	}
	return lines
}

func (component popupViewComponent) overwriteSelectLines() []string {
	m := component.model
	overwriteItems := overwriteItems()
	lines := make([]string, 0, 2+len(overwriteItems))
	lines = append(lines, m.muted("Choose whether AWS SSM may overwrite an existing parameter:"), "")
	for i, it := range overwriteItems {
		row := fmt.Sprintf("%s — %s", it.label, it.description)
		lines = append(lines, m.singleSelectLine(row, i == m.overwriteCursor, i == m.overwriteCursor))
	}
	return lines
}

// renderHelpScreen renders the full shortcut reference.
