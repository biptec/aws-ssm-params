package ui

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateLoadingAllowsQuitWhileLongLoadIsRunning(t *testing.T) {
	_, cmd := model{}.updateLoading(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})

	require.NotNil(t, cmd)
	assert.IsType(t, tea.QuitMsg{}, cmd())
}

func TestUpdateLoadingIgnoresUnrelatedKeys(t *testing.T) {
	_, cmd := model{}.updateLoading(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})

	assert.Nil(t, cmd)
}

func TestStartMultilinePreservesExistingParameterType(t *testing.T) {
	m := newModel(nil, nil, Options{})
	m.statuses = []Status{{Item: inventory.Item{Path: "/app/log-level", Region: "eu-north-1"}, Type: ssm.ParameterTypeString.String(), Value: "debug"}}

	updated, _ := m.startMultiline(screenMain)
	actual := updated.(model)

	assert.Equal(t, screenTextArea, actual.screen)
	assert.Equal(t, ssm.ParameterTypeString, actual.editType)
}

func TestUpdateTypeSelectChangesEditTypeAndReturnsToEditor(t *testing.T) {
	m := newModel(nil, nil, Options{})
	m.screen = screenTextArea
	m.editType = ssm.ParameterTypeSecureString
	m.editField = editFieldType
	updated, _ := m.startTypeSelect(screenTextArea)
	m = updated.(model)

	updated, _ = m.updateTypeSelect(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	updated, _ = m.updateTypeSelect(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)

	assert.Equal(t, screenTextArea, m.screen)
	assert.Equal(t, ssm.ParameterTypeString, m.editType)
	assert.Equal(t, editFieldType, m.editField)
}

func TestSaveValueCmdWritesSelectedParameterType(t *testing.T) {
	client := fakeSSMClient{region: "eu-north-1", params: map[string]ssm.Parameter{}, metas: map[string]ssm.Metadata{}}
	item := inventory.Item{Path: "/app/hosts", Region: "eu-north-1"}

	msg := saveValueCmd(client, item, item.Path, "api.example.com,www.example.com", ssm.ParameterTypeStringList)()

	updated, ok := msg.(statusUpdatedMsg)
	require.True(t, ok)
	require.NoError(t, updated.err)
	stored := client.params[itemKey("eu-north-1", "/app/hosts")]
	assert.Equal(t, ssm.ParameterTypeStringList.String(), stored.Type)
	assert.Equal(t, "api.example.com,www.example.com", stored.Value)
}

func TestReplaceStatusPrefersMatchingRegion(t *testing.T) {
	m := model{statuses: []Status{
		{Item: inventory.Item{Path: "/app/value", Region: "eu-north-1"}, Type: ssm.ParameterTypeSecureString.String(), Value: "eu"},
		{Item: inventory.Item{Path: "/app/value", Region: "us-east-1"}, Type: ssm.ParameterTypeSecureString.String(), Value: "us"},
	}}

	m.replaceStatus("/app/value", Status{Item: inventory.Item{Path: "/app/value", Region: "us-east-1"}, Type: ssm.ParameterTypeString.String(), Value: "updated"})

	assert.Equal(t, "eu", m.statuses[0].Value)
	assert.Equal(t, "updated", m.statuses[1].Value)
	assert.Equal(t, ssm.ParameterTypeString.String(), m.statuses[1].Type)
}

func TestDisplayValueOnlyHidesSecureStringByDefault(t *testing.T) {
	m := model{width: 100}

	assert.Equal(t, "(hidden)", m.displayValue(Status{Type: ssm.ParameterTypeSecureString.String(), Value: "secret"}, false))
	assert.Equal(t, "plain", m.displayValue(Status{Type: ssm.ParameterTypeString.String(), Value: "plain"}, false))
	assert.Equal(t, "a,b", m.displayValue(Status{Type: ssm.ParameterTypeStringList.String(), Value: "a,b"}, false))

	m.revealValues = true
	assert.Equal(t, "secret", m.displayValue(Status{Type: ssm.ParameterTypeSecureString.String(), Value: "secret"}, false))
}

func TestDisplayValueRendersMultilineAsSingleLinePreview(t *testing.T) {
	m := model{width: 80}
	st := Status{Type: ssm.ParameterTypeString.String(), Value: "one\ntwo\nthree"}

	preview := m.displayValue(st, true)

	assert.Equal(t, `one\ntwo\nthree...`, preview)
	assert.False(t, strings.Contains(preview, "\n"))
}

func TestOneLineValuePreviewTruncatesLongMultilineValues(t *testing.T) {
	preview := oneLineValuePreview("abcdefghij\nklmnop", 12)

	assert.Equal(t, `abcdefghi...`, preview)
}

func TestStartMultilineInitializesEditableFields(t *testing.T) {
	m := newModel(nil, nil, Options{})
	m.statuses = []Status{{Item: inventory.Item{Path: "/app/value", Region: "eu-north-1"}, Type: ssm.ParameterTypeString.String(), Value: "hello"}}

	updated, _ := m.startMultiline(screenMain)
	actual := updated.(model)

	assert.Equal(t, screenTextArea, actual.screen)
	assert.Equal(t, editFieldValue, actual.editField)
	assert.Equal(t, "/app/value", actual.editPathInput.Value())
	assert.Equal(t, "", actual.editPathInput.Placeholder)
	assert.Equal(t, "", actual.editFileInput.Value())
	assert.Equal(t, "", actual.editFileInput.Placeholder)
	assert.Equal(t, "hello", actual.textArea.Value())
}

func TestUpdateTextAreaTabsThroughInputsAndOpensSelectorsOnEnter(t *testing.T) {
	m := newModel(fakeSSMClient{regions: []string{"eu-north-1", "us-east-1"}}, nil, Options{Region: "eu-north-1"})
	m.screen = screenTextArea
	m.editField = editFieldValue
	m.editRegion = "eu-north-1"
	m.textArea.Focus()

	updated, _ := m.updateTextArea(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)
	assert.Equal(t, screenTextArea, m.screen)
	assert.Equal(t, editFieldSSMPath, m.editField)

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)
	assert.Equal(t, screenTextArea, m.screen)
	assert.Equal(t, editFieldRegion, m.editField)
	assert.Empty(t, m.editRegionOptions)
	assert.Contains(t, m.renderTextAreaScreen(), "eu-north-1 ⌵")

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	assert.Equal(t, screenRegionSelect, m.screen)
	assert.Equal(t, editFieldRegion, m.editField)
	assert.Equal(t, []string{"eu-north-1", "us-east-1"}, m.editRegionOptions)

	updated, _ = m.updateRegionSelect(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	assert.Equal(t, screenTextArea, m.screen)
	assert.Equal(t, editFieldRegion, m.editField)

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)
	assert.Equal(t, screenTextArea, m.screen)
	assert.Equal(t, editFieldType, m.editField)
	assert.Contains(t, m.renderTextAreaScreen(), m.normalizedEditType().String()+" ⌵")

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	assert.Equal(t, screenTypeSelect, m.screen)
	assert.Equal(t, editFieldType, m.editField)

	updated, _ = m.updateTypeSelect(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	assert.Equal(t, screenTextArea, m.screen)
	assert.Equal(t, editFieldType, m.editField)

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)
	assert.Equal(t, editFieldFilePath, m.editField)

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)
	assert.Equal(t, editFieldValue, m.editField)

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = updated.(model)
	assert.Equal(t, editFieldFilePath, m.editField)
}

func TestUpdateTextAreaLoadsValueFromFile(t *testing.T) {
	m := newModel(nil, nil, Options{})
	path := t.TempDir() + "/value.txt"
	require.NoError(t, os.WriteFile(path, []byte("from-file\nsecond-line"), 0600))
	m.screen = screenTextArea
	m.editFileInput.SetValue(path)
	m.textArea.SetValue("old")

	updated, cmd := m.updateTextArea(tea.KeyMsg{Type: tea.KeyCtrlO})
	actual := updated.(model)

	assert.Nil(t, cmd)
	assert.Equal(t, "from-file\nsecond-line", actual.textArea.Value())
	assert.Equal(t, "Loaded value from "+path, actual.message)
	assert.Empty(t, actual.errMessage)
	assert.Equal(t, editFieldValue, actual.editField)
}

func TestUpdateTextAreaWritesNonSecureValueToFile(t *testing.T) {
	m := newModel(nil, nil, Options{})
	path := t.TempDir() + "/value.txt"
	m.screen = screenTextArea
	m.editType = ssm.ParameterTypeString
	m.editFileInput.SetValue(path)
	m.textArea.SetValue("plain-value")

	updated, cmd := m.updateTextArea(tea.KeyMsg{Type: tea.KeyCtrlW})
	actual := updated.(model)

	assert.Nil(t, cmd)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "plain-value", string(data))
	assert.Equal(t, "Wrote value to "+path, actual.message)
	assert.Empty(t, actual.errMessage)
}

func TestUpdateTextAreaRequiresYForSecureStringFileWrite(t *testing.T) {
	m := newModel(nil, nil, Options{})
	path := t.TempDir() + "/secret.txt"
	m.screen = screenTextArea
	m.editType = ssm.ParameterTypeSecureString
	m.editFileInput.SetValue(path)
	m.textArea.SetValue("secret-value")

	updated, cmd := m.updateTextArea(tea.KeyMsg{Type: tea.KeyCtrlW})
	m = updated.(model)

	assert.Nil(t, cmd)
	assert.Equal(t, fileWriteConfirmationSecure, m.pendingFileWrite)
	assert.Contains(t, m.warningMessage, `Press "y"`)
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err))

	updated, cmd = m.updateTextArea(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(model)

	assert.Nil(t, cmd)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "secret-value", string(data))
	assert.Equal(t, fileWriteConfirmationNone, m.pendingFileWrite)
}

func TestUpdateTextAreaReportsMissingFilePathForReadAndWrite(t *testing.T) {
	m := newModel(nil, nil, Options{})
	m.screen = screenTextArea
	m.editField = editFieldSSMPath
	m.textArea.SetValue("value")

	updated, cmd := m.updateTextArea(tea.KeyMsg{Type: tea.KeyCtrlO})
	m = updated.(model)
	assert.Nil(t, cmd)
	assert.Equal(t, "File path is required.", m.errMessage)

	m.errMessage = ""
	updated, cmd = m.updateTextArea(tea.KeyMsg{Type: tea.KeyCtrlW})
	m = updated.(model)
	assert.Nil(t, cmd)
	assert.Equal(t, "File path is required.", m.errMessage)
}

func TestUpdateTextAreaRequiresYBeforeOverwritingExistingFile(t *testing.T) {
	m := newModel(nil, nil, Options{})
	path := t.TempDir() + "/value.txt"
	require.NoError(t, os.WriteFile(path, []byte("old"), 0600))
	m.screen = screenTextArea
	m.editType = ssm.ParameterTypeString
	m.editField = editFieldValue
	m.editFileInput.SetValue(path)
	m.textArea.SetValue("new")

	updated, cmd := m.updateTextArea(tea.KeyMsg{Type: tea.KeyCtrlW})
	m = updated.(model)
	assert.Nil(t, cmd)
	assert.Equal(t, fileWriteConfirmationOverwrite, m.pendingFileWrite)
	assert.Contains(t, m.warningMessage, `Press "y"`)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "old", string(data))

	updated, cmd = m.updateTextArea(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(model)
	assert.Nil(t, cmd)
	assert.Equal(t, fileWriteConfirmationNone, m.pendingFileWrite)

	data, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "new", string(data))
}

func TestPromptLineCountPreservesTrailingEmptyLines(t *testing.T) {
	assert.Equal(t, 1, promptLineCount(""))
	assert.Equal(t, 2, promptLineCount("one\n"))
	assert.Equal(t, 3, promptLineCount("one\n\n"))
	assert.Equal(t, 4, promptLineCount("\n\n\n"))
}

func TestRenderTextAreaScreenShowsAlignedSSMAndFilePath(t *testing.T) {
	m := newModel(nil, nil, Options{NoColor: true})
	m.width = 120
	m.height = 30
	m.statuses = []Status{{Item: inventory.Item{Path: "/app/value", Region: "eu-north-1"}, Type: ssm.ParameterTypeSecureString.String(), Value: "secret"}}
	updated, _ := m.startMultiline(screenMain)
	m = updated.(model)
	m.editFileInput.SetValue("/tmp/value.txt")

	view := m.renderTextAreaScreen()

	assert.Contains(t, view, "SSM path:  /app/value")
	assert.Contains(t, view, "Region:    eu-north-1")
	assert.Contains(t, view, "Type:      SecureString")
	assert.Contains(t, view, "File path: /tmp/value.txt")
	assert.Contains(t, view, "Value:")
}

func TestRenderTextAreaScreenDoesNotIndentValueWhenFilePathFocused(t *testing.T) {
	m := newModel(nil, nil, Options{NoColor: true})
	m.width = 120
	m.height = 30
	m.screen = screenTextArea
	m.editField = editFieldFilePath
	m.editType = ssm.ParameterTypeString
	m.editRegion = "eu-north-1"
	m.editPathInput.SetValue("/app/value")
	m.editFileInput.Focus()
	m.textArea.SetValue("test-value")

	view := m.renderTextAreaScreen()

	assert.Contains(t, view, "Value:")
	assert.Contains(t, view, "1 │ test-value")
	assert.False(t, strings.Contains(view, "\n   1 │ test-value"))
}

func TestRenderRegionSelectScreenUsesLoadedFullRegionOptions(t *testing.T) {
	m := newModel(nil, nil, Options{NoColor: true, Region: "eu-central-1"})
	m.width = 120
	m.height = 30
	m.screen = screenRegionSelect
	m.editRegion = "eu-central-1"
	m.editRegionOptions = []string{"eu-central-1", "us-east-1", "ap-southeast-1"}
	m.regionCursor = 1

	view := m.renderRegionSelectScreen()

	assert.Contains(t, view, "eu-central-1")
	assert.Contains(t, view, "us-east-1")
	assert.Contains(t, view, "ap-southeast-1")
	assert.Contains(t, view, "| us-east-1")
}

func TestReplaceStatusWhenSSMPathChangesKeepsMatchingRegion(t *testing.T) {
	m := model{statuses: []Status{
		{Item: inventory.Item{Path: "/old/path", Region: "eu-north-1"}, Type: ssm.ParameterTypeString.String(), Value: "eu"},
		{Item: inventory.Item{Path: "/old/path", Region: "us-east-1"}, Type: ssm.ParameterTypeString.String(), Value: "us"},
	}}

	m.replaceStatus("/old/path", Status{Item: inventory.Item{Path: "/new/path", Region: "us-east-1"}, Type: ssm.ParameterTypeString.String(), Value: "updated"})

	assert.Equal(t, "/old/path", m.statuses[0].Item.Path)
	assert.Equal(t, "eu", m.statuses[0].Value)
	assert.Equal(t, "/new/path", m.statuses[1].Item.Path)
	assert.Equal(t, "updated", m.statuses[1].Value)
}

func TestOptionSelectorsSupportTabNavigation(t *testing.T) {
	m := newModel(nil, nil, Options{})
	m.editRegionOptions = []string{"eu-central-1", "us-east-1", "ap-southeast-1"}
	m.regionCursor = 0

	updated, _ := m.updateRegionSelect(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)
	assert.Equal(t, 1, m.regionCursor)

	updated, _ = m.updateRegionSelect(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = updated.(model)
	assert.Equal(t, 0, m.regionCursor)

	m.typeCursor = 0
	updated, _ = m.updateTypeSelect(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)
	assert.Equal(t, 1, m.typeCursor)

	updated, _ = m.updateTypeSelect(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = updated.(model)
	assert.Equal(t, 0, m.typeCursor)
}

func TestWriteValueCreatesNewFileInExistingDirectory(t *testing.T) {
	m := newModel(nil, nil, Options{})
	dir := t.TempDir()
	path := dir + "/new-value.txt"
	m.screen = screenTextArea
	m.editType = ssm.ParameterTypeString
	m.editFileInput.SetValue(path)
	m.textArea.SetValue("created-value")

	updated, cmd := m.updateTextArea(tea.KeyMsg{Type: tea.KeyCtrlW})
	m = updated.(model)

	assert.Nil(t, cmd)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "created-value", string(data))
	assert.Empty(t, m.errMessage)
}

func TestWriteValueExpandsHomeDirectory(t *testing.T) {
	m := newModel(nil, nil, Options{})
	home := t.TempDir()
	t.Setenv("HOME", home)
	m.screen = screenTextArea
	m.editType = ssm.ParameterTypeString
	m.editFileInput.SetValue("~/new-value.txt")
	m.textArea.SetValue("home-value")

	updated, cmd := m.updateTextArea(tea.KeyMsg{Type: tea.KeyCtrlW})
	m = updated.(model)

	assert.Nil(t, cmd)
	data, err := os.ReadFile(home + "/new-value.txt")
	require.NoError(t, err)
	assert.Equal(t, "home-value", string(data))
	assert.Empty(t, m.errMessage)
}

func TestUpdateHandlesCtrlCQuitConfirmationEverywhere(t *testing.T) {
	m := newModel(nil, nil, Options{})
	m.screen = screenTextArea
	m.editField = editFieldValue
	m.textArea.Focus()

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = updated.(model)

	assert.Nil(t, cmd)
	assert.True(t, m.pendingQuit)
	assert.Equal(t, "ctrl+c", m.pendingQuitKey)
	assert.Equal(t, "Press Ctrl-c again to quit.", m.warningMessage)

	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	_ = updated.(model)
	require.NotNil(t, cmd)
	assert.IsType(t, tea.QuitMsg{}, cmd())
}

func TestUpdateHandlesCtrlQQuitConfirmationEverywhere(t *testing.T) {
	m := newModel(nil, nil, Options{})
	m.screen = screenHelp

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlQ})
	m = updated.(model)

	assert.Nil(t, cmd)
	assert.True(t, m.pendingQuit)
	assert.Equal(t, "ctrl+q", m.pendingQuitKey)
	assert.Equal(t, "Press Ctrl-q again to quit.", m.warningMessage)

	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlQ})
	_ = updated.(model)
	require.NotNil(t, cmd)
	assert.IsType(t, tea.QuitMsg{}, cmd())
}

func TestTransientMessagesClearOnNextUserAction(t *testing.T) {
	m := newModel(nil, nil, Options{})
	m.screen = screenTextArea
	m.editField = editFieldValue
	m.warningMessage = "warning"
	m.errMessage = "error"
	m.message = "message"

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)

	assert.Empty(t, m.warningMessage)
	assert.Empty(t, m.errMessage)
	assert.Empty(t, m.message)
}

func TestFooterStatusLineIsDynamic(t *testing.T) {
	m := newModel(nil, nil, Options{NoColor: true})

	withoutStatus := m.renderFooterWithStatus("q back")
	m.warningMessage = "warning"
	withStatus := m.renderFooterWithStatus("q back")

	assert.Equal(t, 3, countLines(withoutStatus))
	assert.Equal(t, 5, countLines(withStatus))
	assert.Contains(t, withStatus, "warning")
}

func TestEditorFooterKeepsHotkeysAtSameBottomOffset(t *testing.T) {
	m := newModel(nil, nil, Options{NoColor: true})

	withoutStatus := m.renderFooterWithStatus("ctrl+s save")
	m.warningMessage = "warning"
	withStatus := m.renderFooterWithStatus("ctrl+s save")

	assert.Equal(t, 2, hotkeyOffsetFromBottom(withoutStatus, "ctrl+s"))
	assert.Equal(t, 2, hotkeyOffsetFromBottom(withStatus, "ctrl+s"))
	assert.Contains(t, withStatus, "warning")
}

func TestMainScreenMessageIsRenderedOnlyInStatusArea(t *testing.T) {
	m := newModel(nil, nil, Options{NoColor: true})
	m.width = 120
	m.height = 40
	m.statuses = []Status{{Item: inventory.Item{Path: "/app/value", Region: "eu-north-1"}, Type: ssm.ParameterTypeString.String(), Value: "value"}}
	m.screen = screenMain
	m.message = "Updated /app/value"

	view := m.View()

	assert.Equal(t, 1, strings.Count(view, "Updated /app/value"))
	assert.Contains(t, view, "Selected Parameter")
	assert.True(t, countLines(view) <= m.height)
}

func TestEditScreenWithStatusDoesNotHideTopFields(t *testing.T) {
	m := newModel(nil, nil, Options{NoColor: true})
	m.width = 120
	m.height = 20
	m.screen = screenTextArea
	m.editField = editFieldValue
	m.editRegion = "eu-north-1"
	m.editType = ssm.ParameterTypeSecureString
	m.editPathInput.SetValue("/app/value")
	m.textArea.SetValue(strings.Repeat("line\n", 20))
	m.warningMessage = `This is a SecureString value. Press "y" to write it to a local file.`

	view := m.View()

	assert.Contains(t, view, "SSM path:")
	assert.Contains(t, view, "Region:")
	assert.True(t, countLines(view) <= m.height)
}

func TestTextAreaContentHeightShrinksOnlyWhenStatusMessageExists(t *testing.T) {
	m := newModel(nil, nil, Options{NoColor: true})
	m.height = 30
	m.screen = screenTextArea
	m.editField = editFieldValue

	withoutStatusContent := m
	withoutStatusContent.height = m.height - countLines(m.renderFooterWithStatus(m.textAreaFooterText()))
	withoutStatus := withoutStatusContent.textAreaBodyHeight()

	m.warningMessage = `File already exists. Press "y" to overwrite it.`
	withStatusContent := m
	withStatusContent.height = m.height - countLines(m.renderFooterWithStatus(m.textAreaFooterText()))
	withStatus := withStatusContent.textAreaBodyHeight()

	assert.True(t, withoutStatus > withStatus)
}

func TestMainContentListHeightUsesStatusSpaceOnlyWhenMessageExists(t *testing.T) {
	m := newModel(nil, nil, Options{NoColor: true})
	m.height = 40

	withoutStatusContent := m
	withoutStatusContent.height = m.height - countLines(m.renderFooterWithStatus(mainFooterText(false)))
	withoutStatus := withoutStatusContent.listBodyHeight()

	m.message = "Updated /app/value"
	withStatusContent := m
	withStatusContent.height = m.height - countLines(m.renderFooterWithStatus(mainFooterText(false)))
	withStatus := withStatusContent.listBodyHeight()

	assert.True(t, withoutStatus > withStatus)
}

func hotkeyOffsetFromBottom(view, hotkey string) int {
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		if strings.Contains(line, hotkey) {
			return len(lines) - i
		}
	}
	return -1
}
func TestOptionNavigationWrapsAround(t *testing.T) {
	assert.Equal(t, 0, nextCursor(2, 3))
	assert.Equal(t, 2, previousCursor(0, 3))

	m := newModel(nil, nil, Options{})
	m.editRegionOptions = []string{"eu-central-1", "us-east-1", "ap-southeast-1"}
	m.regionCursor = 2
	updated, _ := m.updateRegionSelect(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)
	assert.Equal(t, 0, m.regionCursor)

	m.typeCursor = 0
	updated, _ = m.updateTypeSelect(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = updated.(model)
	assert.Equal(t, len(parameterTypeItems())-1, m.typeCursor)
}

func TestCommonBottomLayoutKeepsLoadingAndHelpFootersStable(t *testing.T) {
	screens := []struct {
		name   string
		screen screen
		hotkey string
	}{
		{name: "loading", screen: screenLoading, hotkey: "ctrl+/ help"},
		{name: "help", screen: screenHelp, hotkey: "esc back"},
	}

	for _, tt := range screens {
		t.Run(tt.name, func(t *testing.T) {
			m := newModel(nil, nil, Options{NoColor: true})
			m.width = 120
			m.height = 24
			m.screen = tt.screen
			m.loadingTitle = "Saving parameter..."
			m.loadingLines = []string{"/app/value"}

			withoutStatus := m.View()
			m.warningMessage = `Are you sure you want to quit? Press "y" to confirm.`
			withStatus := m.View()

			assert.Equal(t, hotkeyOffsetFromBottom(withoutStatus, tt.hotkey), hotkeyOffsetFromBottom(withStatus, tt.hotkey))
			assert.True(t, countLines(withoutStatus) <= m.height)
			assert.True(t, countLines(withStatus) <= m.height)
		})
	}
}

func TestRenderTextAreaDoesNotAddFakeRowsWhenHeightChanges(t *testing.T) {
	m := newModel(nil, nil, Options{NoColor: true})
	m.width = 120
	m.height = 32
	m.screen = screenTextArea
	m.editField = editFieldValue
	m.editRegion = "eu-north-1"
	m.editType = ssm.ParameterTypeString
	m.editPathInput.SetValue("/app/value")
	m.textArea.SetValue("one\ntwo")

	withoutStatus := m.View()
	m.warningMessage = `File already exists. Press "y" to overwrite it.`
	withStatus := m.View()
	m.warningMessage = ""
	afterStatus := m.View()

	assert.Equal(t, 1, strings.Count(withoutStatus, "1 │ one"))
	assert.Equal(t, 1, strings.Count(withoutStatus, "2 │ two"))
	assert.Equal(t, 1, strings.Count(withStatus, "1 │ one"))
	assert.Equal(t, 1, strings.Count(withStatus, "2 │ two"))
	assert.Equal(t, 1, strings.Count(afterStatus, "1 │ one"))
	assert.Equal(t, 1, strings.Count(afterStatus, "2 │ two"))
}

func TestBackspaceRemovesEmptyTextareaRows(t *testing.T) {
	m := newModel(nil, nil, Options{NoColor: true})
	m.screen = screenTextArea
	m.editField = editFieldValue
	m.textArea.Focus()
	m.textArea.SetValue("one")

	updated, _ := m.updateTextArea(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	require.Equal(t, 3, m.textArea.LineCount())

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyBackspace})
	m = updated.(model)
	assert.Equal(t, 2, m.textArea.LineCount())

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyBackspace})
	m = updated.(model)
	assert.Equal(t, 1, m.textArea.LineCount())
}

func TestTextAreaFilePathErrorDoesNotCreateFakePromptRows(t *testing.T) {
	m := newModel(nil, nil, Options{NoColor: true})
	m.width = 120
	m.height = 32
	m.screen = screenTextArea
	m.editField = editFieldValue
	m.editPathInput.SetValue("/app/value")
	m.editRegion = "eu-north-1"
	m.editType = ssm.ParameterTypeString
	m.textArea.Focus()

	valueLines := make([]string, 40)
	for i := range valueLines {
		valueLines[i] = fmt.Sprintf("line-%02d", i+1)
	}
	m.textArea.SetValue(strings.Join(valueLines, "\n"))

	before := m.View()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	m = updated.(model)
	withError := m.View()
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)
	afterClear := m.View()

	assert.Contains(t, withError, "File path is required.")
	assert.Equal(t, 0, blankValuePromptRows(before))
	assert.Equal(t, 0, blankValuePromptRows(afterClear))
	assert.Equal(t, m.textArea.LineCount(), len(valueLines))
}

func blankValuePromptRows(view string) int {
	count := 0
	for _, line := range strings.Split(view, "\n") {
		if strings.TrimSpace(line) == ">" {
			count++
		}
	}
	return count
}

func TestMainListUsesAvailableRowAboveFooter(t *testing.T) {
	m := newModel(nil, nil, Options{NoColor: true})
	m.height = 40

	assert.Equal(t, m.listBlockHeight()-4, m.listBodyHeight())
}

func TestMainDetailsTogglePersistsAcrossNavigation(t *testing.T) {
	m := newModel(nil, nil, Options{NoColor: true})
	m.width = 120
	m.height = 32
	m.screen = screenMain
	m.statuses = []Status{
		{Item: inventory.Item{Path: "/app/value", Region: "eu-north-1"}, Exists: true, Type: ssm.ParameterTypeString.String(), Value: "value", Version: 7, SHA256Prefix: "abcdef"},
		{Item: inventory.Item{Path: "/app/other", Region: "eu-north-1"}, Exists: true, Type: ssm.ParameterTypeString.String(), Value: "other", Version: 8, SHA256Prefix: "123456"},
	}

	updated, _ := m.updateMain(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = updated.(model)
	assert.True(t, m.selectedExpanded)
	assert.Contains(t, m.renderMainScreen(), "Version:")

	updated, _ = m.updateMain(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	assert.True(t, m.selectedExpanded)
	assert.Contains(t, m.renderMainScreen(), "Version:")

	updated, _ = m.updateMain(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = updated.(model)
	assert.False(t, m.selectedExpanded)
	assert.False(t, strings.Contains(m.renderMainScreen(), "Version:"))
}

func TestMainTabAndShiftTabMoveRows(t *testing.T) {
	m := newModel(nil, nil, Options{NoColor: true})
	m.screen = screenMain
	m.statuses = []Status{
		{Item: inventory.Item{Path: "/app/one", Region: "eu-north-1"}, Exists: true},
		{Item: inventory.Item{Path: "/app/two", Region: "eu-north-1"}, Exists: true},
		{Item: inventory.Item{Path: "/app/three", Region: "eu-north-1"}, Exists: true},
	}

	updated, _ := m.updateMain(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)
	assert.Equal(t, 1, m.selected)
	assert.False(t, m.selectedExpanded)

	updated, _ = m.updateMain(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = updated.(model)
	assert.Equal(t, 0, m.selected)
}

func TestMainUpperXDeletesVisibleParameters(t *testing.T) {
	m := newModel(nil, nil, Options{NoColor: true})
	m.screen = screenMain
	m.statuses = []Status{
		{Item: inventory.Item{Path: "/app/one", Region: "eu-north-1"}, Exists: true},
		{Item: inventory.Item{Path: "/app/two", Region: "eu-north-1"}, Exists: true},
	}

	updated, _ := m.updateMain(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})
	m = updated.(model)
	assert.Equal(t, screenConfirm, m.screen)
	assert.Equal(t, "DELETE ALL", m.confirmExpected)
	assert.Len(t, m.confirmItems, 2)
}

func TestMainFooterDetailsLabelIsDynamic(t *testing.T) {
	assert.Contains(t, mainFooterText(false), "ctrl+/ help")
	assert.Contains(t, mainFooterText(false), "d show details")
	assert.Contains(t, mainFooterText(true), "d hide details")
	assert.Contains(t, mainFooterText(false), "X delete visible")
	assert.False(t, strings.Contains(mainFooterText(false), "r random"))
	assert.False(t, strings.Contains(mainFooterText(false), "v values"))
}

func TestViKeymapNavigatesMainRowsAndSupportsGG(t *testing.T) {
	m := newModel(nil, nil, Options{Keymap: "vi", NoColor: true})
	m.screen = screenMain
	m.statuses = []Status{
		{Item: inventory.Item{Path: "/app/one", Region: "eu-north-1"}, Exists: true},
		{Item: inventory.Item{Path: "/app/two", Region: "eu-north-1"}, Exists: true},
		{Item: inventory.Item{Path: "/app/three", Region: "eu-north-1"}, Exists: true},
	}

	updated, _ := m.updateMain(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(model)
	assert.Equal(t, 1, m.selected)

	updated, _ = m.updateMain(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	m = updated.(model)
	assert.Equal(t, 2, m.selected)

	updated, _ = m.updateMain(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = updated.(model)
	updated, _ = m.updateMain(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = updated.(model)
	assert.Equal(t, 0, m.selected)
}

func TestMainRandomShortcutIsMovedToEditor(t *testing.T) {
	m := newModel(nil, nil, Options{})
	m.screen = screenMain
	m.statuses = []Status{{Item: inventory.Item{Path: "/app/value", Region: "eu-north-1"}, Exists: true}}

	updated, _ := m.updateMain(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = updated.(model)
	assert.Equal(t, screenMain, m.screen)

	m.screen = screenTextArea
	m.editField = editFieldValue
	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = updated.(model)
	assert.Equal(t, screenTextArea, m.screen)

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = updated.(model)
	assert.Equal(t, screenRandom, m.screen)
	assert.Equal(t, screenTextArea, m.returnScreen)
}

func TestGeneratedRandomPreviewInsertsIntoEditorWithoutSaving(t *testing.T) {
	m := newModel(nil, nil, Options{})
	m.screen = screenRandomPreview
	m.returnScreen = screenTextArea
	m.generatedValue = "generated-value"

	updated, cmd := m.updateRandomPreview(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)

	assert.Nil(t, cmd)
	assert.Equal(t, screenTextArea, m.screen)
	assert.Equal(t, "generated-value", m.textArea.Value())
	assert.Contains(t, m.message, "Press Ctrl-s to save")
}

func TestShortcutsFollowSelectedKeymap(t *testing.T) {
	emacs := newModel(nil, nil, Options{Keymap: "emacs"})
	emacs.shortcutsFor = screenMain
	assert.Contains(t, emacs.shortcutsText(), "ctrl+n")
	assert.False(t, strings.Contains(emacs.shortcutsText(), "↓ / j / tab"))

	vi := newModel(nil, nil, Options{Keymap: "vi"})
	vi.shortcutsFor = screenMain
	assert.Contains(t, vi.shortcutsText(), "↓ / j / tab")
	assert.Contains(t, vi.shortcutsText(), "Home / gg")
}

func TestMainEnterEditsSelectedParameterAndEIsUnused(t *testing.T) {
	m := newModel(nil, nil, Options{})
	m.screen = screenMain
	m.statuses = []Status{{Item: inventory.Item{Path: "/app/value", Region: "eu-north-1"}, Exists: true, Type: ssm.ParameterTypeString.String(), Value: "value"}}

	updated, _ := m.updateMain(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	assert.Equal(t, screenTextArea, m.screen)

	m.screen = screenMain
	updated, _ = m.updateMain(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = updated.(model)
	assert.Equal(t, screenMain, m.screen)
}

func TestMissingSelectedParameterShowsOnlyPathAndDashes(t *testing.T) {
	m := newModel(nil, nil, Options{NoColor: true})
	m.width = 120
	m.height = 32
	m.statuses = []Status{{Item: inventory.Item{Path: "/app/missing", Region: "eu-north-1"}, Exists: false, Type: ssm.ParameterTypeSecureString.String()}}

	compact := m.renderSelectedParameterBlock(false)
	expanded := m.renderSelectedParameterBlock(true)

	assert.Contains(t, compact, "Path:   /app/missing")
	assert.Contains(t, compact, "Region: -")
	assert.Contains(t, compact, "Type:   -")
	assert.Contains(t, compact, "Date:   -")
	assert.Contains(t, compact, "Value:  -")
	assert.False(t, strings.Contains(compact, "(hidden)"))

	assert.Contains(t, expanded, "Path:        /app/missing")
	assert.Contains(t, expanded, "Region:      -")
	assert.Contains(t, expanded, "Type:        -")
	assert.Contains(t, expanded, "Version:     -")
	assert.Contains(t, expanded, "Value:       -")
}

func TestSelectedParameterBlocksDoNotRenderStatusField(t *testing.T) {
	m := newModel(nil, nil, Options{NoColor: true})
	m.width = 120
	m.height = 32
	m.statuses = []Status{{Item: inventory.Item{Path: "/app/value", Region: "eu-north-1"}, Exists: true, Type: ssm.ParameterTypeString.String(), Value: "value"}}

	compact := m.renderSelectedParameterBlock(false)
	full := m.renderSelectedParameterBlock(true)

	assert.False(t, strings.Contains(compact, "Status:"))
	assert.False(t, strings.Contains(full, "Status:"))
}

func TestColumnsScreenDoesNotOfferStatusColumn(t *testing.T) {
	m := newModel(nil, nil, Options{NoColor: true})
	m.width = 120
	m.height = 32
	m.screen = screenColumns

	view := m.renderColumnsScreen()

	assert.False(t, strings.Contains(view, "Status"))
}

func TestSaveValueRejectsEmptyValueBeforeAWSRequest(t *testing.T) {
	m := newModel(fakeSSMClient{}, nil, Options{})
	m.screen = screenTextArea
	m.returnScreen = screenMain
	m.editPathInput.SetValue("/app/value")
	m.editRegion = "eu-north-1"
	m.editType = ssm.ParameterTypeString
	m.textArea.SetValue("")
	m.statuses = []Status{{Item: inventory.Item{Path: "/app/value", Region: "eu-north-1"}, Type: ssm.ParameterTypeString.String(), Value: "old"}}

	updated, cmd := m.updateTextArea(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(model)

	assert.Nil(t, cmd)
	assert.Equal(t, screenTextArea, m.screen)
	assert.Equal(t, "Value cannot be empty.", m.errMessage)
}

func TestTextAreaFooterUsesStableHotkeyOrderWithoutColons(t *testing.T) {
	m := newModel(nil, nil, Options{})
	m.editField = editFieldValue

	footer := m.textAreaFooterText()

	assert.Contains(t, footer, "ctrl+/ help • ctrl+s save")
	assert.Contains(t, footer, "ctrl+r random")
	assert.False(t, strings.Contains(footer, "save AWS"))
	assert.Contains(t, footer, "ctrl+o read file")
	assert.False(t, strings.Contains(footer, "ctrl+t"))
	assert.False(t, strings.Contains(footer, ":"))
}

func TestViEditorStartsNormalAndInsertModeLabelsActiveTextField(t *testing.T) {
	m := newModel(nil, nil, Options{Keymap: "vi", NoColor: true})
	m.width = 120
	m.height = 30
	m.statuses = []Status{{Item: inventory.Item{Path: "/app/value", Region: "eu-north-1"}, Exists: true, Type: ssm.ParameterTypeString.String(), Value: "value"}}

	updated, _ := m.startMultiline(screenMain)
	m = updated.(model)
	assert.False(t, m.viInsertMode)
	assert.False(t, strings.Contains(m.renderTextAreaScreen(), "[INSERT]"))

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = updated.(model)
	assert.True(t, m.viInsertMode)
	assert.Contains(t, m.renderTextAreaScreen(), "Value [INSERT]:")

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	assert.False(t, m.viInsertMode)
	assert.Equal(t, screenTextArea, m.screen)
	assert.False(t, strings.Contains(m.renderTextAreaScreen(), "[INSERT]"))

	m.editField = editFieldSSMPath
	m.editPathInput.Focus()
	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = updated.(model)
	assert.Contains(t, m.renderTextAreaScreen(), "SSM path [INSERT]:")
	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)

	m.editField = editFieldFilePath
	m.editFileInput.Focus()
	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = updated.(model)
	assert.Contains(t, m.renderTextAreaScreen(), "File path [INSERT]:")
	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	assert.Equal(t, screenMain, m.screen)
}

func TestViEditorInsertModeTypesAndNormalModeCommandsDoNotType(t *testing.T) {
	m := newModel(nil, nil, Options{Keymap: "vi"})
	m.screen = screenTextArea
	m.returnScreen = screenMain
	m.editField = editFieldValue
	m.viInsertMode = false
	m.textArea.Focus()
	m.textArea.SetValue("")

	updated, _ := m.updateTextArea(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = updated.(model)
	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h', 'j', 'k', 'l', 'x', 'w', 'b', '?'}})
	m = updated.(model)
	assert.Equal(t, "hjklxwb?", m.textArea.Value())

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	before := m.textArea.Value()
	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = updated.(model)
	assert.Equal(t, before, m.textArea.Value())
}

func TestViEditorNormalModeNavigatesAndDeletesValue(t *testing.T) {
	m := newModel(nil, nil, Options{Keymap: "vi"})
	m.screen = screenTextArea
	m.returnScreen = screenMain
	m.editField = editFieldValue
	m.viInsertMode = false
	m.textArea.Focus()
	m.textArea.SetValue("abc def\nsecond")
	(&m).setTextAreaCursorAbs(0)

	updated, _ := m.updateTextArea(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = updated.(model)
	assert.Equal(t, 1, (&m).textAreaCursorAbs())

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	m = updated.(model)
	assert.Equal(t, 4, (&m).textAreaCursorAbs())

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	m = updated.(model)
	assert.Equal(t, len([]rune(m.textArea.Value())), (&m).textAreaCursorAbs())

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = updated.(model)
	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = updated.(model)
	assert.Equal(t, 0, (&m).textAreaCursorAbs())

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(model)
	assert.Equal(t, "bc def\nsecond", m.textArea.Value())

	m.textArea.SetValue("abc def")
	(&m).setTextAreaCursorAbs(0)
	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = updated.(model)
	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	m = updated.(model)
	assert.Equal(t, "def", m.textArea.Value())
}

func TestEmacsValueNavigationDoesNotMutateText(t *testing.T) {
	m := newModel(nil, nil, Options{Keymap: "emacs"})
	m.screen = screenTextArea
	m.editField = editFieldValue
	m.textArea.Focus()
	m.textArea.SetValue("abc")
	(&m).setTextAreaCursorAbs(1)

	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyCtrlF},
		{Type: tea.KeyCtrlB},
		{Type: tea.KeyRight},
		{Type: tea.KeyLeft},
	} {
		before := m.textArea.Value()
		updated, _ := m.updateTextArea(key)
		m = updated.(model)
		assert.Equal(t, before, m.textArea.Value())
	}
}

func TestQuestionMarkCanBeTypedAndCtrlSlashOpensShortcuts(t *testing.T) {
	m := newModel(nil, nil, Options{Keymap: "emacs"})
	m.screen = screenTextArea
	m.editField = editFieldValue
	m.textArea.Focus()

	updated, _ := m.updateTextArea(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = updated.(model)
	assert.Equal(t, "?", m.textArea.Value())
	assert.Equal(t, screenTextArea, m.screen)

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyCtrlUnderscore})
	m = updated.(model)
	assert.Equal(t, screenHelp, m.screen)
}

func TestTextAreaLineNumbersAreAlignedByTotalLineCount(t *testing.T) {
	m := newModel(nil, nil, Options{NoColor: true})
	m.width = 120
	m.height = 140
	m.screen = screenTextArea
	m.editField = editFieldValue
	m.textArea.Focus()

	lines := make([]string, 100)
	for i := range lines {
		lines[i] = "x"
	}
	m.textArea.SetValue(strings.Join(lines, "\n"))

	valueRows := strings.Join(m.renderTextAreaValueLines(100), "\n")
	assert.Contains(t, valueRows, "  1 │ x")
	assert.Contains(t, valueRows, "100 │ x")
	assert.False(t, strings.Contains(valueRows, "> x"))
}
