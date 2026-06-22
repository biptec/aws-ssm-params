package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	crerr "github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/fileio"
	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/randomx"
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
	data, err := fileio.ReadFile(expandedPath)
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
	if _, err := os.Stat(expandedPath); err == nil && !overwriteConfirmed && !m.opts.NoConfirmOverwriteFile {
		m.errMessage = ""
		m.message = ""
		m.warningMessage = ""
		m.openFileWriteConfirmation(fileWriteConfirmationOverwrite)
		return m, nil
	} else if err != nil && !os.IsNotExist(err) {
		m.errMessage = err.Error()
		m.message = ""
		m.warningMessage = ""
		m.pendingFileWrite = fileWriteConfirmationNone
		return m, nil
	}
	contents := m.fileActionContents()
	if err := fileio.WriteFile(expandedPath, []byte(contents), 0o600); err != nil {
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
func (component *editorIOComponent) startConfirm(prompt, expected string, items []inventory.Item, ret screen) {
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
	switch kind {
	case "base64-32":
		value, err := randomx.Base64(32)
		return value, crerr.Wrap(err, "generate base64 random value")
	case "hex-32":
		value, err := randomx.Hex(32)
		return value, crerr.Wrap(err, "generate hex random value")
	case "uuid":
		value, err := randomx.UUID()
		return value, crerr.Wrap(err, "generate UUID random value")
	case "base64-custom":
		n, err := strconv.Atoi(strings.TrimSpace(m.input.Value()))
		if err != nil || n <= 0 {
			return "", errors.New("invalid byte length")
		}
		value, err := randomx.Base64(n)
		return value, crerr.Wrap(err, "generate custom base64 random value")
	default:
		return "", errors.New("unknown random value generator")
	}
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
	return m, saveValueCmd(m.ctx, m.client, item, oldPath, value, m.normalizedEditType(), ssm.PutParameterOptions{Description: description, Tier: m.normalizedEditTier(), DataType: m.normalizedEditDataType(), Policies: policies, PoliciesSet: policiesSet, Overwrite: overwrite}, m.opts.NamesFile, m.opts.AllowNamesFileUpdate)
}

// saveValueCmd writes one SSM parameter to Parameter Store and reloads its fresh status for the UI.
// Wildcard items must be converted to a concrete region before saving, otherwise the command returns an inline error.
func saveValueCmd(ctx context.Context, client ssm.Client, item inventory.Item, oldPath, value string, parameterType ssm.ParameterType, opts ssm.PutParameterOptions, pathsFile string, allowNamesFileUpdate bool) tea.Cmd {
	return func() tea.Msg {
		if item.Region == "*" {
			return statusUpdatedMsg{path: item.Path, oldPath: oldPath, err: fmt.Errorf("cannot save %s without a concrete AWS region", item.Path)}
		}
		regionalClient := client.ForRegion(item.Region)
		if err := regionalClient.PutParameterWithOptions(ctx, item.Path, value, parameterType, opts); err != nil {
			return statusUpdatedMsg{path: item.Path, oldPath: oldPath, err: err}
		}
		appendedToNamesFile := false
		if pathsFile != "" && allowNamesFileUpdate {
			appended, err := inventory.AppendPathIfMissing(pathsFile, item.Path)
			if err != nil {
				st := LoadStatuses(ctx, regionalClient, []inventory.Item{item}, true)[0]
				return statusUpdatedMsg{path: item.Path, oldPath: oldPath, status: st, message: "Updated " + item.Path, warning: fmt.Sprintf("Could not append %s to %s: %v", item.Path, pathsFile, err)}
			}
			if appended {
				appendedToNamesFile = true
				item.Kind = "path-file"
				item.Source = pathsFile
			}
		}
		st := LoadStatuses(ctx, regionalClient, []inventory.Item{item}, true)[0]
		message := "Updated " + item.Path
		if appendedToNamesFile {
			message += " and added it to " + pathsFile
		}
		return statusUpdatedMsg{path: item.Path, oldPath: oldPath, status: st, message: message}
	}
}

// deleteCmd groups selected items by concrete region and deletes them from SSM.
// Wildcard missing rows are skipped because they do not represent a real parameter in one AWS region.
func deleteCmd(ctx context.Context, client ssm.Client, items []inventory.Item, pathsFile string, allowNamesFileUpdate bool) tea.Cmd {
	return func() tea.Msg {
		byRegion := map[string][]string{}
		for _, item := range items {
			if item.Region == "*" {
				continue
			}
			byRegion[item.Region] = append(byRegion[item.Region], item.Path)
		}
		for region, paths := range byRegion {
			if err := client.ForRegion(region).DeleteMany(ctx, paths); err != nil {
				return deleteDoneMsg{items: items, err: err}
			}
		}

		removeRows := pathsFile == ""
		if pathsFile != "" && allowNamesFileUpdate {
			if _, err := inventory.RemovePathsIfPresent(pathsFile, itemPaths(items)); err != nil {
				return deleteDoneMsg{items: items, warning: fmt.Sprintf("Could not update %s after delete: %v", pathsFile, err)}
			}
			removeRows = true
		}
		return deleteDoneMsg{items: items, removeRows: removeRows}
	}
}

func expandLocalPath(path string) (string, error) {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", crerr.Wrap(err, "resolve user home directory")
		}
		return home, nil
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", crerr.Wrap(err, "resolve user home directory")
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
	}
	return path, nil
}
