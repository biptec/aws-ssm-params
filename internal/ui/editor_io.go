package ui

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	ssmclient "github.com/biptec/aws-ssm-params/internal/ssm/client"
	tea "github.com/charmbracelet/bubbletea"
)

type editorIOComponent struct {
	model model
}

func (component *editorIOComponent) openFileWriteConfirmation(kind fileWriteConfirmation) {
	m := &component.model
	m.pendingFileWrite = kind

	m.warningMessage = ""
	if m.activePopup == popupFileWriteConfirm {
		return
	}

	if m.activePopup != popupFileAction {
		m.activePopup = popupFileAction
	}

	m.pushNestedPopup(popupFileWriteConfirm)
}

// loadValueFromFile reads the path from the edit screen and replaces the active file-action field with that file content.
func (component editorIOComponent) loadValueFromFile() (tea.Model, tea.Cmd) {
	m := component.model

	path := strings.TrimSpace(component.fileActionPath())
	if path == "" {
		m.errMessage = "File path is required."
		m.message = ""
		m.warningMessage = ""

		return m, nil
	}

	expandedPath, err := expandLocalPath(path)
	if err != nil {
		m.errMessage = err.Error()
		m.message = ""
		m.warningMessage = ""

		return m, nil
	}

	data, err := backendFor(m).readFile(expandedPath)
	if err != nil {
		m.errMessage = err.Error()
		m.message = ""
		m.warningMessage = ""

		return m, nil
	}

	switch m.fileActionField {
	case editFieldPolicies:
		if m.importDefaultsInPopupStack() {
			m.importDefaultPolicies.SetValue(prettyPoliciesForEditor(string(data)))
			m.importDefaultsCursor = 4
			m.focusImportDefaults()
		} else {
			m.editPoliciesArea.SetValue(prettyPoliciesForEditor(string(data)))
			m = m.focusEditField(editFieldPolicies)
		}

		m.message = "Loaded policies from " + path
	case editFieldDescription:
		if m.importDefaultsInPopupStack() {
			m.importDefaultDescription.SetValue(string(data))
			m.importDefaultsCursor = 5
			m.focusImportDefaults()
		} else {
			m.editDescriptionArea.SetValue(string(data))
			m = m.focusEditField(editFieldDescription)
		}

		m.message = "Loaded description from " + path
	case editFieldValue,
		editFieldSSMPath,
		editFieldRegion,
		editFieldType,
		editFieldTier,
		editFieldDataType,
		editFieldOverwrite,
		editFieldFilePath:
		m.textArea.SetValue(string(data))
		m = m.focusEditField(editFieldValue)
		m.message = "Loaded value from " + path
	}

	m.errMessage = ""
	m.warningMessage = ""

	return m, nil
}

// writeValueToFile writes the current active file-action field to the path from the edit screen.
// SecureString value writes and overwrite operations require explicit confirmation to reduce accidental local writes.
func (component editorIOComponent) writeValueToFile(secureConfirmed, overwriteConfirmed bool) (tea.Model, tea.Cmd) {
	m := component.model

	path := strings.TrimSpace(component.fileActionPath())
	if path == "" {
		m.errMessage = "File path is required."
		m.message = ""
		m.warningMessage = ""
		m.pendingFileWrite = fileWriteConfirmationNone

		return m, nil
	}

	expandedPath, err := expandLocalPath(path)
	if err != nil {
		m.errMessage = err.Error()
		m.message = ""
		m.warningMessage = ""
		m.pendingFileWrite = fileWriteConfirmationNone

		return m, nil
	}

	if m.fileActionField == editFieldValue && m.normalizedEditType() == ssm.ParameterTypeSecureString && !secureConfirmed {
		m.errMessage = ""
		m.message = ""
		m.warningMessage = ""
		m.openFileWriteConfirmation(fileWriteConfirmationSecure)

		return m, nil
	}

	if _, err := backendFor(m).statFile(expandedPath); err == nil && !overwriteConfirmed {
		m.errMessage = ""
		m.message = ""
		m.warningMessage = ""
		m.openFileWriteConfirmation(fileWriteConfirmationOverwrite)

		return m, nil
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		m.errMessage = err.Error()
		m.message = ""
		m.warningMessage = ""
		m.pendingFileWrite = fileWriteConfirmationNone

		return m, nil
	}

	contents := m.fileActionContents()
	if err := backendFor(m).writeFile(expandedPath, []byte(contents), 0o600); err != nil {
		m.errMessage = err.Error()
		m.message = ""
		m.warningMessage = ""
		m.pendingFileWrite = fileWriteConfirmationNone

		return m, nil
	}

	m.errMessage = ""

	m.warningMessage = ""
	switch m.fileActionField {
	case editFieldPolicies:
		m.message = "Wrote policies to " + path
	case editFieldDescription:
		m.message = "Wrote description to " + path
	case editFieldValue,
		editFieldSSMPath,
		editFieldRegion,
		editFieldType,
		editFieldTier,
		editFieldDataType,
		editFieldOverwrite,
		editFieldFilePath:
		m.message = "Wrote value to " + path
	}

	m.pendingFileWrite = fileWriteConfirmationNone

	return m, nil
}

func (component editorIOComponent) fileActionContents() string {
	m := component.model

	switch m.fileActionField {
	case editFieldPolicies:
		if m.importDefaultsInPopupStack() {
			return m.importDefaultPolicies.Value()
		}

		return m.editPoliciesArea.Value()
	case editFieldDescription:
		if m.importDefaultsInPopupStack() {
			return m.importDefaultDescription.Value()
		}

		return m.editDescriptionArea.Value()
	case editFieldValue,
		editFieldSSMPath,
		editFieldRegion,
		editFieldType,
		editFieldTier,
		editFieldDataType,
		editFieldOverwrite,
		editFieldFilePath:
		return m.textArea.Value()
	}

	return m.textArea.Value()
}

func (component editorIOComponent) fileActionPath() string {
	m := component.model
	if m.importDefaultsInPopupStack() {
		return m.input.Value()
	}

	return m.editFileInput.Value()
}

// startConfirm initializes a confirmation screen for one or more items.
func (component *editorIOComponent) startConfirm(prompt, expected string, items inventory.Items, ret screen) {
	m := &component.model
	m.confirmPrompt = prompt
	m.confirmExpected = expected
	m.confirmItems = items
	m.confirmAction = confirmActionDelete
	m.confirmButtonCursor = importActionPrimary
	m.confirmFocus = confirmFocusPrimaryButton
	m.confirmStatusIndexes = nil
	m.confirmStateFilterOrder = nil
	m.confirmStateFilterSelected = nil
	m.returnScreen = ret
	m.input.SetValue("")
	m.input.Placeholder = ""
	m.input.Blur()
	m.errMessage = ""
	m.pushPopup(popupConfirm)
}

func (component *editorIOComponent) startPushConfirm(prompt string, indexes []int, ret screen, withStateFilters bool) {
	m := &component.model
	m.confirmPrompt = prompt
	m.confirmExpected = ""
	m.confirmItems = nil
	m.confirmAction = confirmActionPush
	m.confirmButtonCursor = importActionPrimary
	m.confirmFocus = confirmFocusPrimaryButton

	m.confirmStatusIndexes = append([]int(nil), indexes...)
	if withStateFilters {
		m.confirmStateFilterOrder = component.confirmPushStateFilterOrder(indexes)
	} else {
		m.confirmStateFilterOrder = nil
	}

	m.confirmStateFilterSelected = map[parameterState]bool{}
	for _, state := range m.confirmStateFilterOrder {
		m.confirmStateFilterSelected[state] = true
	}

	m.returnScreen = ret
	m.input.SetValue("")
	m.input.Placeholder = ""
	m.input.Blur()
	m.errMessage = ""
	m.pushPopup(popupConfirm)
}

func (component editorIOComponent) confirmPushStateFilterOrder(indexes []int) []parameterState {
	m := component.model
	seen := map[parameterState]bool{}
	order := []parameterState{}

	for _, state := range []parameterState{parameterStateNew, parameterStateModified, parameterStateDeleted} {
		for _, idx := range indexes {
			if idx < 0 || idx >= len(m.statuses) {
				continue
			}

			if m.statuses[idx].pendingOperation() == state && !seen[state] {
				seen[state] = true
				order = append(order, state)
			}
		}
	}

	return order
}

func (component editorIOComponent) startRandomFromPopup(kind string) (tea.Model, tea.Cmd) {
	m := component.model
	if kind == "base64-custom" {
		m.fileActionMode = "random-custom"
		m.input.SetValue("32")
		m.input.Placeholder = ""
		m.input.Focus()
		m.pushEditorChildPopup(popupFileAction)

		return m, nil
	}

	return m.generateRandomValueIntoEditor(kind)
}

func (component editorIOComponent) generateRandomValueIntoEditor(kind string) (tea.Model, tea.Cmd) {
	m := component.model

	value, err := m.randomValue(kind)
	if err != nil {
		m.errMessage = err.Error()
		return m, nil
	}

	m.textArea.SetValue(value)
	m = m.focusEditField(editFieldValue)
	m.message = "Random value inserted. Press Ctrl-Space to save."
	m.errMessage = ""

	m.warningMessage = ""
	if m.editorPopupActiveOrStack() {
		m.returnToEditorPopup()
	} else {
		m.screen = screenTextArea
		m.clearPopupStack()
	}

	return m, nil
}

func (component editorIOComponent) randomValue(kind string) (string, error) {
	m := component.model
	return backendFor(m).randomValue(kind, m.input.Value())
}

// saveValue captures the current item/region and switches to the loading screen while the save command runs.
func (component editorIOComponent) saveValue(value string) (tea.Model, tea.Cmd) {
	m := component.model
	item := m.currentItem()

	if m.screen == screenTextArea || m.editorPopupActiveOrStack() {
		newPath := strings.TrimSpace(m.editPathInput.Value())
		if newPath == "" {
			m.errMessage = "Name is required."
			m.message = ""

			return m, nil
		}

		if !parameterNameIsValid(newPath) {
			m.errMessage = parameterNameValidationMessage
			m.message = ""
			m.warningMessage = ""

			return m, nil
		}

		item.Path = newPath
	}

	if value == "" {
		if !m.editNewParameter && !m.editorHasUnsavedChanges() {
			m.message = "No changes to save."
			m.errMessage = ""
			m.warningMessage = ""

			return m, nil
		}

		m.errMessage = "Value is required."
		m.message = ""
		m.warningMessage = ""

		return m, nil
	}

	if strings.TrimSpace(m.editRegion) == "" {
		m.errMessage = "Region is required."
		m.message = ""
		m.warningMessage = ""

		return m, nil
	}

	if !m.normalizedEditType().IsValid() {
		m.errMessage = "Type is required."
		m.message = ""
		m.warningMessage = ""

		return m, nil
	}

	if !m.normalizedEditTier().IsValid() {
		m.errMessage = "Tier is required."
		m.message = ""
		m.warningMessage = ""

		return m, nil
	}

	if !m.normalizedEditDataType().IsValid() {
		m.errMessage = "DataType is required."
		m.message = ""
		m.warningMessage = ""

		return m, nil
	}

	item.Region = m.editRegion
	policies := ""
	localPolicies := ""
	policiesSet := false

	if m.shouldShowPoliciesField() {
		rawPolicies := strings.TrimSpace(m.editPoliciesArea.Value())

		policies = normalizePoliciesForAWS(rawPolicies)

		localPolicies = policies
		if strings.TrimSpace(policies) == "[{}]" {
			policiesSet = true
			localPolicies = ""
		}

		if rawPolicies == "" && strings.TrimSpace(m.currentStatus().Policies) != "" {
			policies = "[{}]"
			policiesSet = true
			localPolicies = ""
		}
	}

	overwrite := true
	if m.shouldShowOverwriteField() {
		overwrite = m.editOverwrite
	}

	description := strings.TrimSpace(m.editDescriptionArea.Value())

	opts := ssm.PutParameterOptions{Description: description, Tier: m.normalizedEditTier(), DataType: m.normalizedEditDataType(), Policies: policies, PoliciesSet: policiesSet, Overwrite: overwrite}
	local := Status{Item: item, Exists: true, Type: m.normalizedEditType().String(), Tier: m.normalizedEditTier().String(), DataType: m.normalizedEditDataType().String(), Policies: localPolicies, Description: description, Value: value}

	if m.editNewParameter {
		local.applyLocalCreate(m.normalizedEditType(), opts)
		m.replaceStatusByKey(itemKey(item.Region, item.Path), &local)
		m.applySortWithRules(m.sortRulesOrDefault())
		m.selectItem(&item)

		if m.opts.ApplyImmediately {
			m.discardEditorChanges()
			return m.applyImmediatelyDirtyStatuses(m.dirtyStatusIndexes(), "Saving parameter...")
		}

		m.message = "Created local change for " + item.Path + ". Press p to push."
		m.errMessage = ""
		m.warningMessage = ""
		m.discardEditorChanges()

		return m, nil
	}

	idx := m.currentStatusIndex()
	if idx < 0 {
		m.errMessage = "No parameter selected."
		m.message = ""
		m.warningMessage = ""

		return m, nil
	}

	current := m.statuses[idx]
	if current.pendingOperation() == parameterStateDeleted {
		m.errMessage = "Revert deleted parameter before editing it."
		m.message = ""
		m.warningMessage = ""

		return m, nil
	}

	if current.pendingOperation() == parameterStateNew {
		local.applyLocalCreate(m.normalizedEditType(), opts)
	} else {
		base := current.cloudStatus()
		local.Version = base.Version
		local.Modified = base.Modified
		local.User = base.User

		cloud := current.Cloud
		if cloud.isZero() {
			cloud = current.snapshot()
		}

		local.applyLocalModification(&cloud, m.normalizedEditType(), opts)
	}

	m.statuses[idx] = local
	m.applySortWithRules(m.sortRulesOrDefault())
	m.selectItem(&item)

	if m.opts.ApplyImmediately {
		if !local.HasLocalChanges() {
			m.message = "No changes to save."
			m.errMessage = ""
			m.warningMessage = ""
			m.discardEditorChanges()

			return m, nil
		}

		m.discardEditorChanges()

		return m.applyImmediatelyDirtyStatuses(m.dirtyStatusIndexes(), "Saving parameter...")
	}

	if local.State == parameterStateClean {
		m.message = "No local changes."
	} else {
		m.message = "Saved local change for " + item.Path + ". Press p to push."
	}

	m.errMessage = ""
	m.warningMessage = ""
	m.discardEditorChanges()

	return m, nil
}

func (m model) applyImmediatelyDirtyStatuses(indexes []int, busyMessage string) (tea.Model, tea.Cmd) {
	statuses := m.dirtyStatuses(indexes)
	if len(statuses) == 0 {
		m.message = "No changes to apply."
		m.errMessage = ""
		m.warningMessage = ""

		return m, nil
	}

	m.busyMessage = busyMessage
	m.loadingTitle = ""
	m.clearPopupStack()
	m.screen = m.returnScreen

	return m, pushLocalChangesCmdWithBackend(m.contextProvider(), backendFor(m), statuses, m.opts.NamesFile, m.opts.AllowNamesFileUpdate)
}

// saveValueCmd writes one SSM parameter to Parameter Store and reloads its fresh status for the UI.
// Wildcard items must be converted to a concrete region before saving, otherwise the command returns an inline error.
func saveValueCmd(ctx context.Context, client ssmclient.Client, item *inventory.Item, oldPath, value string, parameterType ssm.ParameterType, opts ssm.PutParameterOptions, pathsFile string, allowNamesFileUpdate bool) tea.Cmd {
	return saveValueCmdWithBackend(ctx, newDefaultBackend(client), item, oldPath, value, parameterType, opts, pathsFile, allowNamesFileUpdate)
}

func saveValueCmdWithBackend(ctx context.Context, backend uiBackend, item *inventory.Item, oldPath, value string, parameterType ssm.ParameterType, opts ssm.PutParameterOptions, pathsFile string, allowNamesFileUpdate bool) tea.Cmd {
	return func() tea.Msg {
		return backend.saveParameter(ctx, item, oldPath, value, parameterType, opts, pathsFile, allowNamesFileUpdate)
	}
}

// deleteCmd groups selected items by concrete region and deletes them from SSM.
// Wildcard missing rows are skipped because they do not represent a real parameter in one AWS region.
func deleteCmd(ctx context.Context, client ssmclient.Client, items inventory.Items, pathsFile string, allowNamesFileUpdate bool) tea.Cmd {
	return deleteCmdWithBackend(ctx, newDefaultBackend(client), items, pathsFile, allowNamesFileUpdate)
}

func deleteCmdWithBackend(ctx context.Context, backend uiBackend, items inventory.Items, pathsFile string, allowNamesFileUpdate bool) tea.Cmd {
	return func() tea.Msg {
		return backend.deleteParameters(ctx, items, pathsFile, allowNamesFileUpdate)
	}
}

func pushLocalChangesCmdWithBackend(ctx context.Context, backend uiBackend, statuses []Status, pathsFile string, allowNamesFileUpdate bool) tea.Cmd {
	return func() tea.Msg {
		results := make([]pushResult, 0, len(statuses))
		for i := range statuses {
			results = append(results, pushOneLocalChange(ctx, backend, &statuses[i], pathsFile, allowNamesFileUpdate))
		}

		return pushDoneMsg{results: results}
	}
}

func pushOneLocalChange(ctx context.Context, backend uiBackend, status *Status, pathsFile string, allowNamesFileUpdate bool) pushResult {
	localKey := itemKey(status.Item.Region, status.Item.Path)
	cloud := status.cloudStatus()
	cloudKey := itemKey(cloud.Item.Region, cloud.Item.Path)
	operation := status.pendingOperation()

	result := pushResult{localKey: localKey, cloudKey: cloudKey, operation: operation, item: status.Item}
	switch operation {
	case parameterStateNew:
		msg := backend.saveParameter(ctx, &status.Item, "", status.Value, status.pushType(), status.pushOptions(), pathsFile, allowNamesFileUpdate)
		if msg.err != nil {
			result.err = msg.err
			return result
		}

		result.status = msg.status
		result.warning = msg.warning

		return result
	case parameterStateModified:
		msg := backend.saveParameter(ctx, &status.Item, cloud.Item.Path, status.Value, status.pushType(), status.pushOptions(), pathsFile, allowNamesFileUpdate)
		if msg.err != nil {
			result.err = msg.err
			return result
		}

		if !cloud.Item.SameIdentity(&status.Item) {
			deleteMsg := backend.deleteParameters(ctx, inventory.Items{cloud.Item}, pathsFile, allowNamesFileUpdate)
			if deleteMsg.err != nil {
				result.err = deleteMsg.err
				result.warning = joinWarnings(msg.warning, deleteMsg.warning)

				return result
			}

			result.warning = joinWarnings(msg.warning, deleteMsg.warning)
		} else {
			result.warning = msg.warning
		}

		result.status = msg.status

		return result
	case parameterStateDeleted:
		deleteItem := cloud.Item
		if deleteItem.Path == "" {
			deleteItem = status.Item
		}

		msg := backend.deleteParameters(ctx, inventory.Items{deleteItem}, pathsFile, allowNamesFileUpdate)
		if msg.err != nil {
			result.err = msg.err
			return result
		}

		result.item = deleteItem
		result.removeRow = msg.removeRows
		result.warning = msg.warning

		return result
	case parameterStateClean, parameterStateError:
		result.err = errors.New("no local change to push")
		return result
	}

	return result
}

func joinWarnings(left, right string) string {
	switch {
	case left == "":
		return right
	case right == "":
		return left
	default:
		return left + "; " + right
	}
}

func expandLocalPath(path string) (string, error) {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", errors.Wrap(err, "resolve user home directory")
		}

		return home, nil
	}

	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", errors.Wrap(err, "resolve user home directory")
		}

		return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
	}

	return path, nil
}
