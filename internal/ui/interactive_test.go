package ui

import (
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
	assert.Equal(t, editFieldFilePath, m.editField)
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

func TestUpdateTextAreaCyclesThroughEditableFields(t *testing.T) {
	m := newModel(nil, nil, Options{})
	m.screen = screenTextArea
	m.editField = editFieldValue
	m.textArea.Focus()

	for _, expected := range []editField{editFieldSSMPath, editFieldRegion, editFieldType, editFieldFilePath, editFieldValue} {
		updated, _ := m.updateTextArea(tea.KeyMsg{Type: tea.KeyTab})
		m = updated.(model)
		assert.Equal(t, expected, m.editField)
	}

	updated, _ := m.updateTextArea(tea.KeyMsg{Type: tea.KeyShiftTab})
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

func TestUpdateTextAreaRequiresSecondCtrlWForSecureStringFileWrite(t *testing.T) {
	m := newModel(nil, nil, Options{})
	path := t.TempDir() + "/secret.txt"
	m.screen = screenTextArea
	m.editType = ssm.ParameterTypeSecureString
	m.editFileInput.SetValue(path)
	m.textArea.SetValue("secret-value")

	updated, cmd := m.updateTextArea(tea.KeyMsg{Type: tea.KeyCtrlW})
	m = updated.(model)

	assert.Nil(t, cmd)
	assert.True(t, m.confirmWriteSecure)
	assert.Contains(t, m.message, "Press ctrl+w again")
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err))

	updated, cmd = m.updateTextArea(tea.KeyMsg{Type: tea.KeyCtrlW})
	m = updated.(model)

	assert.Nil(t, cmd)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "secret-value", string(data))
	assert.False(t, m.confirmWriteSecure)
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

func TestTextAreaFooterUsesStableColonSeparatedHotkeys(t *testing.T) {
	m := newModel(nil, nil, Options{})
	m.editField = editFieldRegion

	footer := m.textAreaFooterText()

	assert.Contains(t, footer, "ctrl+s: save • tab: next field • shift+tab: previous field")
	assert.Contains(t, footer, "enter: choose region")
	assert.False(t, strings.Contains(footer, "save AWS"))
	assert.False(t, strings.Contains(footer, "ctrl+r"))
	assert.False(t, strings.Contains(footer, "ctrl+t"))
}
