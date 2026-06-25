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
	items := valueActions()

	lines := make([]string, 0, len(items))
	for i, item := range items {
		lines = append(lines, m.singleSelectLine(item.label, i == m.valueActionCursor, i == m.valueActionCursor))
	}

	return m.renderPopupBoxWithActions("Value Actions", lines, "Enter select   Esc cancel")
}

func (component popupViewComponent) renderPoliciesActionsPopup() string {
	m := component.model
	items := policiesActions()

	lines := make([]string, 0, len(items))
	for i, item := range items {
		lines = append(lines, m.singleSelectLine(item.label, i == m.valueActionCursor, i == m.valueActionCursor))
	}

	return m.renderPopupBoxWithActions("Policies Actions", lines, "Enter select   Esc cancel")
}

func (component popupViewComponent) renderDescriptionActionsPopup() string {
	m := component.model
	items := descriptionActions()

	lines := make([]string, 0, len(items))
	for i, item := range items {
		lines = append(lines, m.singleSelectLine(item.label, i == m.valueActionCursor, i == m.valueActionCursor))
	}

	return m.renderPopupBoxWithActions("Description Actions", lines, "Enter select   Esc cancel")
}

func (component popupViewComponent) renderFileActionPopup() string {
	m := component.model

	title := "Load from file"

	switch m.fileActionField {
	case editFieldPolicies:
		title = "Load policies from file"
	case editFieldDescription:
		title = "Load description from file"
	case editFieldValue,
		editFieldSSMPath,
		editFieldRegion,
		editFieldType,
		editFieldTier,
		editFieldDataType,
		editFieldOverwrite,
		editFieldFilePath:
	}

	label := "File path:"
	inputWidth := 48

	switch m.fileActionMode {
	case "write":
		title = "Write to file"

		switch m.fileActionField {
		case editFieldPolicies:
			title = "Write policies to file"
		case editFieldDescription:
			title = "Write description to file"
		case editFieldValue,
			editFieldSSMPath,
			editFieldRegion,
			editFieldType,
			editFieldTier,
			editFieldDataType,
			editFieldOverwrite,
			editFieldFilePath:
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

	lines := []string{m.popupInputLine(label, &m.input, inputWidth)}

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
		_, checked := m.sortRules.find(item.column)
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
		lines = append(lines, "", m.popupInputLinePlainPrefix(prefix, &m.input, max(len(m.confirmExpected)+1, 18)))
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
	if m.importSelectorActive() {
		lines := append(m.regionSelectLines(), "", m.importActionButtonsLine("Select"))

		return m.renderPopupBox("Region", lines)
	}

	return m.renderPopupBoxWithActions("Region", m.regionSelectLines(), "Enter select   Esc cancel")
}

func (component popupViewComponent) regionSelectLines() []string {
	m := component.model
	regions := m.regionSelectOptions()

	if m.importSelectorActive() {
		regions = m.importDefaultRegionOptions()
	}

	lines := make([]string, 0, 2+len(regions))
	lines = append(lines, m.muted("Choose region for saving this value:"), "")

	for i, region := range regions {
		label := region
		if label == "" && m.importSelectorActive() {
			label = m.muted(nonePlaceholderText)
		} else if label == "" {
			label = "none"
		}

		focused := i == m.regionCursor && (!m.importSelectorActive() || !m.importButtonsFocused)
		lines = append(lines, m.singleSelectLine(label, i == m.regionCursor, focused))
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
	if m.importSelectorActive() {
		lines := append(m.typeSelectLines(), "", m.importActionButtonsLine("Select"))

		return m.renderPopupBox("Parameter Type", lines)
	}

	return m.renderPopupBoxWithActions("Parameter Type", m.typeSelectLines(), "Enter select   Esc cancel")
}

func (component popupViewComponent) renderTierSelectPopup() string {
	m := component.model
	if m.importSelectorActive() {
		lines := append(m.tierSelectLines(), "", m.importActionButtonsLine("Select"))

		return m.renderPopupBox("Parameter Tier", lines)
	}

	return m.renderPopupBoxWithActions("Parameter Tier", m.tierSelectLines(), "Enter select   Esc cancel")
}

func (component popupViewComponent) renderDataTypeSelectPopup() string {
	m := component.model
	if m.importSelectorActive() {
		lines := append(m.dataTypeSelectLines(), "", m.importActionButtonsLine("Select"))

		return m.renderPopupBox("Data Type", lines)
	}

	return m.renderPopupBoxWithActions("Data Type", m.dataTypeSelectLines(), "Enter select   Esc cancel")
}

func (component popupViewComponent) renderOverwriteSelectPopup() string {
	m := component.model
	return m.renderPopupBoxWithActions("Overwrite", m.overwriteSelectLines(), "Enter select   Esc cancel")
}

func (component popupViewComponent) typeSelectLines() []string {
	m := component.model
	typeItems := parameterTypeItems()

	if m.importSelectorActive() {
		typeItems = importParameterTypeItems()
	}

	lines := make([]string, 0, 2+len(typeItems))
	lines = append(lines, m.muted("Choose how this value should be stored in AWS SSM Parameter Store:"), "")

	for i, it := range typeItems {
		row := optionLabelWithDescription(m.importOptionalSelectorLabel(it.label), it.description)
		focused := i == m.typeCursor && (!m.importSelectorActive() || !m.importButtonsFocused)
		lines = append(lines, m.singleSelectLine(row, i == m.typeCursor, focused))
	}

	return lines
}

func (component popupViewComponent) tierSelectLines() []string {
	m := component.model
	tierItems := parameterTierItems()

	if m.importSelectorActive() {
		tierItems = importParameterTierItems()
	}

	lines := make([]string, 0, 2+len(tierItems))
	lines = append(lines, m.muted("Choose the AWS SSM storage tier for this parameter:"), "")

	for i, it := range tierItems {
		row := optionLabelWithDescription(m.importOptionalSelectorLabel(it.label), it.description)
		focused := i == m.tierCursor && (!m.importSelectorActive() || !m.importButtonsFocused)
		lines = append(lines, m.singleSelectLine(row, i == m.tierCursor, focused))
	}

	return lines
}

func (component popupViewComponent) dataTypeSelectLines() []string {
	m := component.model
	dataTypeItems := parameterDataTypeItems()

	if m.importSelectorActive() {
		dataTypeItems = importParameterDataTypeItems()
	}

	lines := make([]string, 0, 2+len(dataTypeItems))
	lines = append(lines, m.muted("Choose AWS SSM value validation data type:"), "")

	for i, it := range dataTypeItems {
		row := optionLabelWithDescription(m.importOptionalSelectorLabel(it.label), it.description)
		focused := i == m.dataTypeCursor && (!m.importSelectorActive() || !m.importButtonsFocused)
		lines = append(lines, m.singleSelectLine(row, i == m.dataTypeCursor, focused))
	}

	return lines
}

func optionLabelWithDescription(label, description string) string {
	if strings.TrimSpace(description) == "" {
		return label
	}

	return fmt.Sprintf("%s — %s", label, description)
}

func (m model) importOptionalSelectorLabel(label string) string {
	if label == "" && m.importSelectorActive() {
		return m.muted(nonePlaceholderText)
	}

	return label
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
