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
	path := strings.TrimSpace(m.editFileInput.Value())
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
	if m.fileActionField == editFieldPolicies {
		m.editPoliciesArea.SetValue(prettyPoliciesForEditor(string(data)))
		m = m.focusEditField(editFieldPolicies)
		m.message = "Loaded policies from " + path
	} else {
		m.textArea.SetValue(string(data))
		m = m.focusEditField(editFieldValue)
		m.message = "Loaded value from " + path
	}
	m.errMessage = ""
	m.warningMessage = ""
	return m, nil
}

// writeValueToFile writes the current active file-action field to the path from the edit screen.
// SecureString value writes and overwrite operations require explicit y confirmation to reduce accidental local writes.
func (component editorIOComponent) writeValueToFile(secureConfirmed, overwriteConfirmed bool) (tea.Model, tea.Cmd) {
	m := component.model
	path := strings.TrimSpace(m.editFileInput.Value())
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
	if m.fileActionField != editFieldPolicies && m.normalizedEditType() == ssm.ParameterTypeSecureString && !secureConfirmed && !m.opts.NoConfirmWriteSecureValue {
		m.errMessage = ""
		m.message = ""
		m.warningMessage = ""
		m.openFileWriteConfirmation(fileWriteConfirmationSecure)
		return m, nil
	}
	if _, err := backendFor(m).statFile(expandedPath); err == nil && !overwriteConfirmed && !m.opts.NoConfirmOverwriteFile {
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
	if m.fileActionField == editFieldPolicies {
		m.message = "Wrote policies to " + path
	} else {
		m.message = "Wrote value to " + path
	}
	m.pendingFileWrite = fileWriteConfirmationNone
	return m, nil
}

func (component editorIOComponent) fileActionContents() string {
	m := component.model
	if m.fileActionField == editFieldPolicies {
		return m.editPoliciesArea.Value()
	}
	return m.textArea.Value()
}

// startConfirm initializes a confirmation screen for one or more items.
func (component *editorIOComponent) startConfirm(prompt, expected string, items inventory.Items, ret screen) {
	m := &component.model
	m.confirmPrompt = prompt
	m.confirmExpected = expected
	m.confirmItems = items
	m.returnScreen = ret
	m.input.SetValue("")
	m.input.Placeholder = ""
	m.input.Focus()
	m.errMessage = ""
	m.pushPopup(popupConfirm)
}

func (component editorIOComponent) startRandomFromPopup(kind string) (tea.Model, tea.Cmd) {
	m := component.model
	if kind == "base64-custom" {
		m.fileActionMode = "random-custom"
		m.input.SetValue("32")
		m.input.Placeholder = ""
		m.input.Focus()
		m.pushPopup(popupFileAction)
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
	m.screen = screenTextArea
	m = m.focusEditField(editFieldValue)
	m.message = "Random value inserted. Press Ctrl-s to save."
	m.errMessage = ""
	m.warningMessage = ""
	m.clearPopupStack()
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
	oldPath := item.Path
	if m.screen == screenTextArea {
		newPath := strings.TrimSpace(m.editPathInput.Value())
		if newPath == "" {
			m.errMessage = "Name is required."
			m.message = ""
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
	policiesSet := false
	if m.shouldShowPoliciesField() {
		rawPolicies := strings.TrimSpace(m.editPoliciesArea.Value())
		policies = normalizePoliciesForAWS(rawPolicies)
		if strings.TrimSpace(policies) == "[{}]" {
			policiesSet = true
		}
		if rawPolicies == "" && strings.TrimSpace(m.currentStatus().Policies) != "" {
			policies = "[{}]"
			policiesSet = true
		}
	}
	overwrite := true
	if m.shouldShowOverwriteField() {
		overwrite = m.editOverwrite
	}
	m.busyMessage = "Saving parameter..."
	m.loadingTitle = ""
	m.loadingLines = nil
	description := strings.TrimSpace(m.editDescriptionArea.Value())
	if description == "" {
		description = strings.TrimSpace(m.editDescriptionInput.Value())
	}
	return m, saveValueCmdWithBackend(m.ctx, backendFor(m), item, oldPath, value, m.normalizedEditType(), ssm.PutParameterOptions{Description: description, Tier: m.normalizedEditTier(), DataType: m.normalizedEditDataType(), Policies: policies, PoliciesSet: policiesSet, Overwrite: overwrite}, m.opts.NamesFile, m.opts.AllowNamesFileUpdate)
}

// saveValueCmd writes one SSM parameter to Parameter Store and reloads its fresh status for the UI.
// Wildcard items must be converted to a concrete region before saving, otherwise the command returns an inline error.
func saveValueCmd(ctx context.Context, client ssm.Client, item inventory.Item, oldPath, value string, parameterType ssm.ParameterType, opts ssm.PutParameterOptions, pathsFile string, allowNamesFileUpdate bool) tea.Cmd {
	return saveValueCmdWithBackend(ctx, newDefaultBackend(client), item, oldPath, value, parameterType, opts, pathsFile, allowNamesFileUpdate)
}

func saveValueCmdWithBackend(ctx context.Context, backend uiBackend, item inventory.Item, oldPath, value string, parameterType ssm.ParameterType, opts ssm.PutParameterOptions, pathsFile string, allowNamesFileUpdate bool) tea.Cmd {
	return func() tea.Msg {
		return backend.saveParameter(ctx, item, oldPath, value, parameterType, opts, pathsFile, allowNamesFileUpdate)
	}
}

// deleteCmd groups selected items by concrete region and deletes them from SSM.
// Wildcard missing rows are skipped because they do not represent a real parameter in one AWS region.
func deleteCmd(ctx context.Context, client ssm.Client, items inventory.Items, pathsFile string, allowNamesFileUpdate bool) tea.Cmd {
	return deleteCmdWithBackend(ctx, newDefaultBackend(client), items, pathsFile, allowNamesFileUpdate)
}

func deleteCmdWithBackend(ctx context.Context, backend uiBackend, items inventory.Items, pathsFile string, allowNamesFileUpdate bool) tea.Cmd {
	return func() tea.Msg {
		return backend.deleteParameters(ctx, items, pathsFile, allowNamesFileUpdate)
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
