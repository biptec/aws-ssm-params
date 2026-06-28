package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/biptec/aws-ssm-params/internal/fileio"
	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeStatusBatchReplacesWildcardPendingRow(t *testing.T) {
	m := newModel(context.Background(), &fakeSSMClient{}, []inventory.Item{{Path: "/app/api/password", Region: "*"}}, &Options{})

	m.mergeStatusBatch([]Status{{Item: inventory.Item{Path: "/app/api/password", Region: "eu-north-1"}, Exists: true, Type: "SecureString"}})

	require.Len(t, m.statuses, 1)
	assert.False(t, m.statuses[0].Pending)
	assert.True(t, m.statuses[0].Exists)
	assert.Equal(t, "eu-north-1", m.statuses[0].Item.Region)
}

func TestUpdateLoadingAllowsQuitWhileLongLoadIsRunning(t *testing.T) {
	_, cmd := (model{modelState: &modelState{}}).updateLoading(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})

	require.NotNil(t, cmd)
	assert.IsType(t, tea.QuitMsg{}, cmd())
}

func TestUpdateLoadingIgnoresUnrelatedKeys(t *testing.T) {
	_, cmd := (model{modelState: &modelState{}}).updateLoading(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})

	assert.Nil(t, cmd)
}

func TestLoadingStartsWithCenteredSpinnerOverlay(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.width = 80
	m.height = 20

	view := m.View()

	assert.Equal(t, screenLoading, m.screen)
	assert.Contains(t, view, "Loading parameters")
	assert.Contains(t, view, "Loading parameters |")
	assert.NotContains(t, view, "Preparing AWS scan...")
}

func TestLoadingProgressKeepsStatusLineMessage(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.width = 90
	m.height = 20

	updated, cmd := m.Update(progressMsg{done: 2, total: 12, region: "eu-north-1"})
	m = updated.(model)

	assert.Equal(t, screenLoading, m.screen)
	assert.Equal(t, "Loading parameters 2/12 from eu-north-1 region...", m.busyMessage)
	view := m.View()
	assert.Contains(t, view, "Loading parameters |")
	assert.Contains(t, view, "Loading parameters 2/12 from eu-north-1 region...")
	assert.NotContains(t, view, "Progress: 2/12")
	assert.NotContains(t, view, "Region: eu-north-1")
	require.NotNil(t, cmd)
}

func TestStatusBatchKeepsLoadingOverlayUntilFinalLoad(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.width = 80
	m.height = 20

	updated, cmd := m.Update(statusBatchMsg{{Item: inventory.Item{Path: "/app/value", Region: "eu-north-1"}, Exists: true}})
	m = updated.(model)

	assert.Equal(t, screenLoading, m.screen)
	assert.Len(t, m.statuses, 1)
	require.NotNil(t, cmd)
}

func TestLoadingSpinnerTickAdvancesWhileLoading(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{})

	updated, cmd := m.Update(loadingTickMsg{})
	m = updated.(model)

	assert.Equal(t, 1, m.loadingSpinnerFrame)
	require.NotNil(t, cmd)
}

func TestNewModelStoresContextForAsyncLoad(t *testing.T) {
	type contextKey struct{}

	ctx := context.WithValue(context.Background(), contextKey{}, "value")

	m := newModel(ctx, nil, nil, &Options{})

	require.NotNil(t, m.contextProvider())
	assert.Equal(t, "value", m.contextProvider().Value(contextKey{}))
}

func TestStartMultilinePreservesExistingParameterType(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{})
	m.statuses = []Status{{Item: inventory.Item{Path: "/app/log-level", Region: "eu-north-1"}, Type: ssm.ParameterTypeString.String(), Value: "debug"}}

	updated, _ := m.startMultiline()
	actual := updated.(model)

	assert.Equal(t, screenTextArea, actual.screen)
	assert.Equal(t, ssm.ParameterTypeString, actual.editType)
}

func TestUpdateTypeSelectChangesEditTypeAndReturnsToEditor(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{})
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
	client := &fakeSSMClient{region: "eu-north-1", params: map[string]ssm.Parameter{}, metas: map[string]ssm.Metadata{}}
	item := inventory.Item{Path: "/app/hosts", Region: "eu-north-1"}

	msg := saveValueCmd(context.Background(), client, &item, item.Path, "api.example.com,www.example.com", ssm.ParameterTypeStringList, ssm.PutParameterOptions{Tier: ssm.ParameterTierStandard, DataType: ssm.DefaultParameterDataType, Overwrite: true}, "", false)()

	updated, ok := msg.(statusUpdatedMsg)
	require.True(t, ok)
	require.NoError(t, updated.err)

	stored := client.params[itemKey("eu-north-1", "/app/hosts")]
	assert.Equal(t, ssm.ParameterTypeStringList.String(), stored.Type)
	assert.Equal(t, "api.example.com,www.example.com", stored.Value)
}

func TestReplaceStatusPrefersMatchingRegion(t *testing.T) {
	m := model{modelState: &modelState{listState: listState{statuses: []Status{
		{Item: inventory.Item{Path: "/app/value", Region: "eu-north-1"}, Type: ssm.ParameterTypeSecureString.String(), Value: "eu"},
		{Item: inventory.Item{Path: "/app/value", Region: "us-east-1"}, Type: ssm.ParameterTypeSecureString.String(), Value: "us"},
	}}}}

	m.replaceStatus("/app/value", &Status{Item: inventory.Item{Path: "/app/value", Region: "us-east-1"}, Type: ssm.ParameterTypeString.String(), Value: "updated"})

	assert.Equal(t, "eu", m.statuses[0].Value)
	assert.Equal(t, "updated", m.statuses[1].Value)
	assert.Equal(t, ssm.ParameterTypeString.String(), m.statuses[1].Type)
}

func TestDisplayValueShowsSecureStringWhenDecrypted(t *testing.T) {
	m := model{modelState: &modelState{runtimeState: runtimeState{width: 100}}}

	assert.Equal(t, "secret", m.displayValue(&Status{Type: ssm.ParameterTypeSecureString.String(), Value: "secret"}, false))
	assert.Equal(t, "plain", m.displayValue(&Status{Type: ssm.ParameterTypeString.String(), Value: "plain"}, false))
	assert.Equal(t, "a,b", m.displayValue(&Status{Type: ssm.ParameterTypeStringList.String(), Value: "a,b"}, false))
}

func TestDisplayValueShowsEncryptedPlaceholderWithoutDecryption(t *testing.T) {
	m := model{modelState: &modelState{runtimeState: runtimeState{width: 100}}}

	assert.Equal(t, "(encrypted)", m.displayValue(&Status{Type: ssm.ParameterTypeSecureString.String()}, false))
}

func TestEncryptedPlaceholderUsesMutedValueStyleOnly(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)

	m := newModel(context.Background(), nil, nil, &Options{NoColor: false, ShowColumns: []string{"value"}})
	m.screen = screenMain
	m.width = 100
	m.height = 20
	m.statuses = []Status{{Item: inventory.Item{Path: "/app/secret", Region: "eu-north-1"}, Exists: true, Type: ssm.ParameterTypeSecureString.String()}}

	row := m.renderListRow(1, &m.statuses[0], false, m.tableColumns([]int{0}))

	assert.Contains(t, row, m.encryptedPlaceholder())
	assert.NotContains(t, row, "\x1b[38;5;45m")
}

func TestDisplayValueRendersMultilineAsSingleLinePreview(t *testing.T) {
	m := model{modelState: &modelState{runtimeState: runtimeState{width: 80}}}
	st := Status{Type: ssm.ParameterTypeString.String(), Value: "one\ntwo\nthree"}

	preview := m.displayValue(&st, true)

	assert.Equal(t, `one\ntwo\nthree...`, preview)
	assert.False(t, strings.Contains(preview, "\n"))
}

func TestOneLineValuePreviewTruncatesLongMultilineValues(t *testing.T) {
	preview := oneLineValuePreview("abcdefghij\nklmnop", 12)

	assert.Equal(t, `abcdefghi...`, preview)
}

func TestStartMultilineInitializesEditableFields(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{})
	m.statuses = []Status{{Item: inventory.Item{Path: "/app/value", Region: "eu-north-1"}, Type: ssm.ParameterTypeString.String(), Value: "hello"}}

	updated, _ := m.startMultiline()
	actual := updated.(model)

	assert.Equal(t, screenTextArea, actual.screen)
	assert.Equal(t, editFieldSSMPath, actual.editField)
	assert.True(t, actual.editPathInput.Focused())
	assert.Equal(t, "/app/value", actual.editPathInput.Value())
	assert.Equal(t, "", actual.editPathInput.Placeholder)
	assert.Equal(t, "", actual.editFileInput.Value())
	assert.Equal(t, "", actual.editFileInput.Placeholder)
	assert.Equal(t, "hello", actual.textArea.Value())
}

func TestStartMultilineShowsEncryptedSecureStringPlaceholderWithoutDecryption(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{})
	m.width = 80
	m.height = 24
	m.statuses = []Status{{Item: inventory.Item{Path: "/app/secret", Region: "eu-north-1"}, Exists: true, Type: ssm.ParameterTypeSecureString.String()}}

	updated, _ := m.startMultiline()
	actual := updated.(model)

	assert.Equal(t, screenTextArea, actual.screen)
	assert.Equal(t, editFieldSSMPath, actual.editField)
	assert.True(t, actual.editPathInput.Focused())
	assert.Empty(t, actual.textArea.Value())
	assert.Contains(t, actual.renderTextAreaScreen(), "(encrypted)")
}

func TestEncryptedSecureStringValueFieldBecomesEditableWhenFocused(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{})
	m.width = 80
	m.height = 24
	m.screen = screenTextArea
	m.editRegion = "eu-north-1"
	m.editType = ssm.ParameterTypeSecureString
	m.editTier = ssm.ParameterTierStandard
	m.editDataType = ssm.DefaultParameterDataType
	m.editField = editFieldDescription
	m.editDescriptionArea.Focus()
	m.statuses = []Status{{Item: inventory.Item{Path: "/app/secret", Region: "eu-north-1"}, Exists: true, Type: ssm.ParameterTypeSecureString.String()}}

	assert.Contains(t, m.renderTextAreaScreen(), "(encrypted)")

	updated, _ := m.updateTextArea(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)

	assert.Equal(t, editFieldValue, m.editField)
	assert.True(t, m.textArea.Focused())
	assert.NotContains(t, m.renderTextAreaScreen(), "(encrypted)")

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)

	assert.Equal(t, editFieldSSMPath, m.editField)
	assert.Contains(t, m.renderTextAreaScreen(), "(encrypted)")

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = updated.(model)

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("new-secret")})
	m = updated.(model)

	assert.Equal(t, "new-secret", m.textArea.Value())
	assert.NotContains(t, m.renderTextAreaScreen(), "(encrypted)")
}

func TestSavingChangedEncryptedSecureStringWithoutDecryptionRequiresValue(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{})
	m.screen = screenTextArea
	m.editRegion = "eu-north-1"
	m.editType = ssm.ParameterTypeSecureString
	m.editTier = ssm.ParameterTierStandard
	m.editDataType = ssm.DefaultParameterDataType
	m.statuses = []Status{{Item: inventory.Item{Path: "/app/secret", Region: "eu-north-1"}, Exists: true, Type: ssm.ParameterTypeSecureString.String()}}
	m.editPathInput.SetValue("/app/secret")
	m.editInitialSnapshot = m.currentEditSnapshot()
	m.editDescriptionArea.SetValue("changed")

	updated, cmd := m.saveValue("")
	actual := updated.(model)

	assert.Nil(t, cmd)
	assert.Equal(t, "Value is required.", actual.errMessage)
}

func TestSavingUnchangedEncryptedSecureStringWithoutDecryptionIsNoop(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{})
	m.screen = screenTextArea
	m.editRegion = "eu-north-1"
	m.editType = ssm.ParameterTypeSecureString
	m.editTier = ssm.ParameterTierStandard
	m.editDataType = ssm.DefaultParameterDataType
	m.statuses = []Status{{Item: inventory.Item{Path: "/app/secret", Region: "eu-north-1"}, Exists: true, Type: ssm.ParameterTypeSecureString.String()}}
	m.editPathInput.SetValue("/app/secret")
	m.editInitialSnapshot = m.currentEditSnapshot()

	updated, cmd := m.saveValue("")
	actual := updated.(model)

	assert.Nil(t, cmd)
	assert.Empty(t, actual.errMessage)
	assert.Equal(t, "No changes to save.", actual.message)
}

func TestSavingEncryptedSecureStringWithoutDecryptionAllowsReplacementValue(t *testing.T) {
	client := &fakeSSMClient{region: "eu-north-1", params: map[string]ssm.Parameter{}}
	m := newModel(context.Background(), client, nil, &Options{})
	m.screen = screenTextArea
	m.editRegion = "eu-north-1"
	m.editType = ssm.ParameterTypeSecureString
	m.editTier = ssm.ParameterTierStandard
	m.editDataType = ssm.DefaultParameterDataType
	m.statuses = []Status{{Item: inventory.Item{Path: "/app/secret", Region: "eu-north-1"}, Exists: true, Type: ssm.ParameterTypeSecureString.String()}}
	m.editPathInput.SetValue("/app/secret")

	updated, cmd := m.saveValue("new-secret")
	actual := updated.(model)

	require.NotNil(t, cmd)
	assert.Empty(t, actual.errMessage)

	msg := cmd()
	result, ok := msg.(statusUpdatedMsg)
	require.True(t, ok)
	assert.NoError(t, result.err)
	assert.Equal(t, "new-secret", client.params[itemKey("eu-north-1", "/app/secret")].Value)
}

func TestUpdateTextAreaTabsThroughInputsAndOpensSelectorsOnEnter(t *testing.T) {
	m := newModel(context.Background(), &fakeSSMClient{regions: []string{"eu-north-1", "us-east-1"}}, nil, &Options{Region: "eu-north-1"})
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
	assert.Contains(t, stripANSI(m.renderTextAreaScreen()), "eu-north-1 <")

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	assert.Equal(t, screenTextArea, m.screen)
	assert.Equal(t, popupRegionSelect, m.activePopup)
	assert.Equal(t, editFieldRegion, m.editField)
	assert.Equal(t, []string{"eu-north-1", "us-east-1"}, m.editRegionOptions)

	updated, _ = m.updateRegionSelectPopup(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	assert.Equal(t, screenTextArea, m.screen)
	assert.Equal(t, popupNone, m.activePopup)
	assert.Equal(t, editFieldRegion, m.editField)

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)
	assert.Equal(t, screenTextArea, m.screen)
	assert.Equal(t, editFieldType, m.editField)
	assert.Contains(t, stripANSI(m.renderTextAreaScreen()), m.normalizedEditType().String()+" <")

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	assert.Equal(t, screenTextArea, m.screen)
	assert.Equal(t, popupTypeSelect, m.activePopup)
	assert.Equal(t, editFieldType, m.editField)

	updated, _ = m.updateTypeSelectPopup(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	assert.Equal(t, screenTextArea, m.screen)
	assert.Equal(t, popupNone, m.activePopup)
	assert.Equal(t, editFieldType, m.editField)

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)
	assert.Equal(t, editFieldTier, m.editField)
	assert.Contains(t, stripANSI(m.renderTextAreaScreen()), m.normalizedEditTier().String()+" <")

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	assert.Equal(t, screenTextArea, m.screen)
	assert.Equal(t, popupTierSelect, m.activePopup)
	assert.Equal(t, editFieldTier, m.editField)

	updated, _ = m.updateTierSelectPopup(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	assert.Equal(t, screenTextArea, m.screen)
	assert.Equal(t, popupNone, m.activePopup)
	assert.Equal(t, editFieldTier, m.editField)

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)
	assert.Equal(t, editFieldDataType, m.editField)
	assert.Contains(t, stripANSI(m.renderTextAreaScreen()), m.normalizedEditDataType().String()+" <")

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	assert.Equal(t, screenTextArea, m.screen)
	assert.Equal(t, popupDataTypeSelect, m.activePopup)
	assert.Equal(t, editFieldDataType, m.editField)

	updated, _ = m.updateDataTypeSelectPopup(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	assert.Equal(t, screenTextArea, m.screen)
	assert.Equal(t, popupNone, m.activePopup)
	assert.Equal(t, editFieldDataType, m.editField)

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)
	assert.Equal(t, editFieldOverwrite, m.editField)
	assert.Contains(t, stripANSI(m.renderTextAreaScreen()), strconv.FormatBool(m.editOverwrite)+" <")

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	assert.Equal(t, screenTextArea, m.screen)
	assert.Equal(t, popupOverwriteSelect, m.activePopup)
	assert.Equal(t, editFieldOverwrite, m.editField)

	updated, _ = m.updateOverwriteSelectPopup(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	assert.Equal(t, screenTextArea, m.screen)
	assert.Equal(t, popupNone, m.activePopup)
	assert.Equal(t, editFieldOverwrite, m.editField)

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)
	assert.Equal(t, editFieldDescription, m.editField)

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)
	assert.Equal(t, editFieldValue, m.editField)

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = updated.(model)
	assert.Equal(t, editFieldDescription, m.editField)
}

func TestEditFieldsSupportArrowNavigationForSingleLineInputs(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.screen = screenTextArea
	m.editField = editFieldSSMPath
	m.editPathInput.Focus()

	updated, _ := m.updateTextArea(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	assert.Equal(t, editFieldRegion, m.editField)

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(model)
	assert.Equal(t, editFieldSSMPath, m.editField)
}

func TestEditFieldsSupportArrowNavigationInViNormalAndInsertModes(t *testing.T) {
	for _, insertMode := range []bool{false, true} {
		t.Run(fmt.Sprintf("insert=%v", insertMode), func(t *testing.T) {
			m := newModel(context.Background(), nil, nil, &Options{NoColor: true, Keymap: "vi"})
			m.screen = screenTextArea
			m.editField = editFieldSSMPath
			m.viInsertMode = insertMode
			m.editPathInput.Focus()

			updated, _ := m.updateTextArea(tea.KeyMsg{Type: tea.KeyDown})
			m = updated.(model)
			assert.Equal(t, editFieldRegion, m.editField)

			updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyUp})
			m = updated.(model)
			assert.Equal(t, editFieldSSMPath, m.editField)
		})
	}
}

func TestEditFieldsUseKeymapNavigationBetweenSingleLineInputs(t *testing.T) {
	tests := []struct {
		name   string
		keymap string
		down   tea.KeyMsg
		up     tea.KeyMsg
	}{
		{name: "emacs", keymap: "emacs", down: tea.KeyMsg{Type: tea.KeyCtrlN}, up: tea.KeyMsg{Type: tea.KeyCtrlP}},
		{name: "vi", keymap: "vi", down: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}, up: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newModel(context.Background(), nil, nil, &Options{NoColor: true, Keymap: tt.keymap})
			m.screen = screenTextArea
			m.editField = editFieldSSMPath
			m.viInsertMode = false
			m.editPathInput.Focus()

			updated, _ := m.updateTextArea(tt.down)
			m = updated.(model)
			assert.Equal(t, editFieldRegion, m.editField)

			updated, _ = m.updateTextArea(tt.up)
			m = updated.(model)
			assert.Equal(t, editFieldSSMPath, m.editField)
		})
	}
}

func TestCompactExpandableFieldsSupportVerticalFieldNavigation(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true, Keymap: "emacs"})
	m.width = 120
	m.height = 30
	m.screen = screenTextArea
	m.editField = editFieldDescription
	m.editDescriptionArea.SetValue("short description")
	m.editDescriptionArea.Focus()
	require.False(t, m.isCurrentExpandableFieldExpanded())

	updated, _ := m.updateTextArea(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	assert.Equal(t, editFieldValue, m.editField)

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = updated.(model)
	assert.Equal(t, editFieldDescription, m.editField)
}

func TestExpandedExpandableFieldsKeepKeymapNavigationInsideTextarea(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true, Keymap: "emacs"})
	m.width = 120
	m.height = 30
	m.screen = screenTextArea
	m.editField = editFieldValue
	m.textArea.SetValue("one\ntwo")
	m.textArea.Focus()
	require.True(t, m.isCurrentExpandableFieldExpanded())

	updated, _ := m.updateTextArea(tea.KeyMsg{Type: tea.KeyCtrlN})
	m = updated.(model)
	assert.Equal(t, editFieldValue, m.editField)
}

func TestEditFieldArrowsStayInsideMultilineTextarea(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.screen = screenTextArea
	m.editField = editFieldValue
	m.textArea.SetValue("one\ntwo")
	m.textArea.Focus()

	updated, _ := m.updateTextArea(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(model)
	assert.Equal(t, editFieldValue, m.editField)

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	assert.Equal(t, editFieldValue, m.editField)
}

func TestFileActionPopupLoadsValueFromFile(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{})
	path := t.TempDir() + "/value.txt"
	require.NoError(t, os.WriteFile(path, []byte("loaded-from-disk\nsecond-line"), 0o600))

	m.screen = screenTextArea
	m.editField = editFieldValue
	m.textArea.SetValue("old")

	actual, cmd := submitFileActionPopup(t, m, "load", path)

	assert.Nil(t, cmd)
	assert.Equal(t, "loaded-from-disk\nsecond-line", actual.textArea.Value())
	assert.Equal(t, "Loaded value from "+path, actual.message)
	assert.Empty(t, actual.errMessage)
	assert.Equal(t, editFieldValue, actual.editField)
	assert.Equal(t, popupNone, actual.activePopup)
}

func TestFileActionPopupWritesNonSecureValueToFile(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{})
	path := t.TempDir() + "/value.txt"
	m.screen = screenTextArea
	m.editType = ssm.ParameterTypeString
	m.textArea.SetValue("plain-value")

	actual, cmd := submitFileActionPopup(t, m, "write", path)

	assert.Nil(t, cmd)

	data, err := fileio.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "plain-value", string(data))
	assert.Equal(t, "Wrote value to "+path, actual.message)
	assert.Empty(t, actual.errMessage)
	assert.Equal(t, popupNone, actual.activePopup)
}

func TestFileActionPopupRequiresYForSecureStringFileWrite(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{})
	path := t.TempDir() + "/secret.txt"
	m.screen = screenTextArea
	m.editType = ssm.ParameterTypeSecureString
	m.textArea.SetValue("secret-value")

	m, cmd := submitFileActionPopup(t, m, "write", path)

	assert.Nil(t, cmd)
	assert.Equal(t, popupFileWriteConfirm, m.activePopup)
	assert.Equal(t, []popupKind{popupFileAction}, m.popupStack)
	assert.Equal(t, fileWriteConfirmationSecure, m.pendingFileWrite)
	assert.Empty(t, m.warningMessage)

	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err))

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(model)

	assert.Nil(t, cmd)

	data, err := fileio.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "secret-value", string(data))
	assert.Equal(t, fileWriteConfirmationNone, m.pendingFileWrite)
	assert.Equal(t, popupNone, m.activePopup)
}

func TestFileActionPopupReportsMissingFilePathForReadAndWrite(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{})
	m.screen = screenTextArea
	m.editField = editFieldValue
	m.textArea.SetValue("value")

	updated, cmd := submitFileActionPopup(t, m, "load", "")
	m = updated

	assert.Nil(t, cmd)
	assert.Equal(t, "File path is required.", m.errMessage)

	m.errMessage = ""
	updated, cmd = submitFileActionPopup(t, m, "write", "")
	m = updated

	assert.Nil(t, cmd)
	assert.Equal(t, "File path is required.", m.errMessage)
}

func TestFileActionPopupRequiresYBeforeOverwritingExistingFile(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{})
	path := t.TempDir() + "/value.txt"
	require.NoError(t, os.WriteFile(path, []byte("old"), 0o600))

	m.screen = screenTextArea
	m.editType = ssm.ParameterTypeString
	m.editField = editFieldValue
	m.textArea.SetValue("new")

	m, cmd := submitFileActionPopup(t, m, "write", path)
	assert.Nil(t, cmd)
	assert.Equal(t, popupFileWriteConfirm, m.activePopup)
	assert.Equal(t, []popupKind{popupFileAction}, m.popupStack)
	assert.Equal(t, fileWriteConfirmationOverwrite, m.pendingFileWrite)
	assert.Empty(t, m.warningMessage)

	data, err := fileio.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "old", string(data))

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(model)

	assert.Nil(t, cmd)
	assert.Equal(t, popupNone, m.activePopup)
	assert.Equal(t, fileWriteConfirmationNone, m.pendingFileWrite)

	data, err = fileio.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "new", string(data))
}

func TestPromptLineCountPreservesTrailingEmptyLines(t *testing.T) {
	assert.Equal(t, 1, promptLineCount(""))
	assert.Equal(t, 2, promptLineCount("one\n"))
	assert.Equal(t, 3, promptLineCount("one\n\n"))
	assert.Equal(t, 4, promptLineCount("\n\n\n"))
}

func TestRenderTextAreaScreenShowsAlignedSSMAndDescription(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.width = 120
	m.height = 30
	m.statuses = []Status{{Item: inventory.Item{Path: "/app/value", Region: "eu-north-1"}, Type: ssm.ParameterTypeSecureString.String(), Value: "secret"}}
	updated, _ := m.startMultiline()
	m = updated.(model)
	m.editDescriptionInput.SetValue("Example parameter")

	view := m.renderTextAreaScreen()

	assert.Contains(t, view, "Name:")
	assert.Contains(t, view, "/app/value")
	assert.Contains(t, view, "Region:")
	assert.Contains(t, view, "eu-north-1")
	assert.Contains(t, view, "Type:")
	assert.Contains(t, view, "SecureString")
	assert.Contains(t, view, "DataType:")
	assert.Contains(t, view, "Overwrite:")
	assert.Contains(t, view, "Description: Example parameter")
	assert.False(t, strings.Contains(view, "Policies:"))
	assert.Contains(t, view, "Value:")
}

func TestRenderTextAreaScreenDoesNotIndentValueWhenFilePathFocused(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
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
	assert.Contains(t, view, "Value:       test-value")
	assert.False(t, strings.Contains(view, "\n   1 │ test-value"))
}

func TestRenderRegionSelectScreenUsesLoadedFullRegionOptions(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true, Region: "eu-central-1"})
	m.width = 120
	m.height = 30
	m.screen = screenRegionSelect
	m.editRegion = "eu-central-1"
	m.editRegionOptions = []string{"eu-central-1", "us-east-1", "ap-southeast-1"}
	m.regionCursor = 2

	view := m.renderRegionSelectScreen()

	assert.Contains(t, view, "eu-central-1")
	assert.Contains(t, view, "us-east-1")
	assert.Contains(t, view, "ap-southeast-1")
	assert.True(t, strings.Index(view, "ap-southeast-1") < strings.Index(view, "eu-central-1"))
	assert.True(t, strings.Index(view, "eu-central-1") < strings.Index(view, "us-east-1"))
	assert.Contains(t, view, "> us-east-1")
}

func TestReplaceStatusWhenSSMPathChangesKeepsMatchingRegion(t *testing.T) {
	m := model{modelState: &modelState{listState: listState{statuses: []Status{
		{Item: inventory.Item{Path: "/old/path", Region: "eu-north-1"}, Type: ssm.ParameterTypeString.String(), Value: "eu"},
		{Item: inventory.Item{Path: "/old/path", Region: "us-east-1"}, Type: ssm.ParameterTypeString.String(), Value: "us"},
	}}}}

	m.replaceStatus("/old/path", &Status{Item: inventory.Item{Path: "/new/path", Region: "us-east-1"}, Type: ssm.ParameterTypeString.String(), Value: "updated"})

	assert.Equal(t, "/old/path", m.statuses[0].Item.Path)
	assert.Equal(t, "eu", m.statuses[0].Value)
	assert.Equal(t, "/new/path", m.statuses[1].Item.Path)
	assert.Equal(t, "updated", m.statuses[1].Value)
}

func TestOptionSelectorsSupportTabNavigation(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{})
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
	m := newModel(context.Background(), nil, nil, &Options{})
	dir := t.TempDir()
	path := dir + "/new-value.txt"
	m.screen = screenTextArea
	m.editType = ssm.ParameterTypeString
	m.textArea.SetValue("created-value")

	m, cmd := submitFileActionPopup(t, m, "write", path)

	assert.Nil(t, cmd)

	data, err := fileio.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "created-value", string(data))
	assert.Empty(t, m.errMessage)
}

func TestWriteValueExpandsHomeDirectory(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{})
	home := t.TempDir()
	t.Setenv("HOME", home)

	m.screen = screenTextArea
	m.editType = ssm.ParameterTypeString
	m.textArea.SetValue("home-value")

	m, cmd := submitFileActionPopup(t, m, "write", "~/new-value.txt")

	assert.Nil(t, cmd)

	data, err := fileio.ReadFile(home + "/new-value.txt")
	require.NoError(t, err)
	assert.Equal(t, "home-value", string(data))
	assert.Empty(t, m.errMessage)
}

func TestUpdateHandlesCtrlCQuitConfirmationEverywhere(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{})
	m.screen = screenTextArea
	m.editField = editFieldValue
	m.textArea.SetWidth(max(20, m.width-14))
	m.textArea.SetHeight(max(8, m.height-10))
	m.textArea.Focus()

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = updated.(model)

	assert.Nil(t, cmd)
	assert.True(t, m.pendingQuit)
	assert.Equal(t, "ctrl+c", m.pendingQuitKey)
	assert.Equal(t, `Are you sure you want to quit? Press "y" to confirm.`, m.warningMessage)

	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	_ = updated.(model)

	require.NotNil(t, cmd)
	assert.IsType(t, tea.QuitMsg{}, cmd())
}

func TestUpdateHandlesCtrlQQuitConfirmationEverywhere(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{})
	m.screen = screenHelp

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlQ})
	m = updated.(model)

	assert.Nil(t, cmd)
	assert.True(t, m.pendingQuit)
	assert.Equal(t, "ctrl+q", m.pendingQuitKey)
	assert.Equal(t, `Are you sure you want to quit? Press "y" to confirm.`, m.warningMessage)

	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	_ = updated.(model)

	require.NotNil(t, cmd)
	assert.IsType(t, tea.QuitMsg{}, cmd())
}

func TestTransientMessagesClearOnNextUserAction(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{})
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
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})

	withoutStatus := m.renderFooterWithStatus("q back")
	m.warningMessage = "warning"
	withStatus := m.renderFooterWithStatus("q back")

	assert.Equal(t, 3, countLines(withoutStatus))
	assert.Equal(t, 5, countLines(withStatus))
	assert.Contains(t, withStatus, "warning")
}

func TestEditorFooterKeepsHotkeysAtSameBottomOffset(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})

	withoutStatus := m.renderFooterWithStatus("ctrl+space save")
	m.warningMessage = "warning"
	withStatus := m.renderFooterWithStatus("ctrl+space save")

	assert.Equal(t, 2, hotkeyOffsetFromBottom(withoutStatus, "ctrl+space"))
	assert.Equal(t, 2, hotkeyOffsetFromBottom(withStatus, "ctrl+space"))
	assert.Contains(t, withStatus, "warning")
}

func TestMainScreenMessageIsRenderedOnlyInStatusArea(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.width = 120
	m.height = 40
	m.statuses = []Status{{Item: inventory.Item{Path: "/app/value", Region: "eu-north-1"}, Type: ssm.ParameterTypeString.String(), Value: "value"}}
	m.screen = screenMain
	m.message = "Updated /app/value"

	view := m.View()

	assert.Equal(t, 1, strings.Count(view, "Updated /app/value"))
	assert.Contains(t, view, "List of 1 Parameters")
	assert.False(t, strings.Contains(view, "Selected Parameter"))
	assert.True(t, countLines(view) <= m.height)
}

func TestEditScreenWithStatusDoesNotHideTopFields(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
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

	assert.Contains(t, view, "Name:")
	assert.Contains(t, view, "Region:")
	assert.True(t, countLines(view) <= m.height)
}

func TestTextAreaContentHeightShrinksOnlyWhenStatusMessageExists(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
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
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.height = 40

	withoutStatusContent := m
	withoutStatusContent.height = m.height - countLines(m.renderFooterWithStatus(mainFooterText(false, false)))
	withoutStatus := withoutStatusContent.listBodyHeight()

	m.message = "Updated /app/value"
	withStatusContent := m
	withStatusContent.height = m.height - countLines(m.renderFooterWithStatus(mainFooterText(false, false)))
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

	m := newModel(context.Background(), nil, nil, &Options{})
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
			m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
			m.width = 120
			m.height = 24
			m.screen = tt.screen
			m.loadingTitle = "Saving parameter..."

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
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
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
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.screen = screenTextArea
	m.editField = editFieldValue
	m.textArea.Focus()
	m.textArea.SetValue("one")

	updated, _ := m.updateTextArea(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model) // first Enter expands the compact one-line field
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
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.width = 120
	m.height = 32
	m.screen = screenTextArea
	m.editField = editFieldValue
	m.editTier = ssm.ParameterTierAdvanced
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
	m, _ = submitFileActionPopup(t, m, "load", "")
	withError := m.View()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
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
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.height = 40

	assert.Equal(t, m.listBlockHeight()-4, m.listBodyHeight())
}

func TestMainDetailsTogglePersistsAcrossNavigation(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
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
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true, ShowColumns: []string{"value"}})
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

	m.selected = len(m.statuses) - 1
	updated, _ = m.updateMain(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)
	assert.Equal(t, 0, m.selected)

	updated, _ = m.updateMain(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = updated.(model)
	assert.Equal(t, len(m.statuses)-1, m.selected)
}

func TestMainUpperXDeletesVisibleParameters(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.screen = screenMain
	m.statuses = []Status{
		{Item: inventory.Item{Path: "/app/one", Region: "eu-north-1"}, Exists: true},
		{Item: inventory.Item{Path: "/app/two", Region: "eu-north-1"}, Exists: true},
	}

	updated, _ := m.updateMain(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})
	m = updated.(model)
	assert.Equal(t, screenMain, m.screen)
	assert.Equal(t, popupConfirm, m.activePopup)
	assert.Equal(t, "DELETE ALL", m.confirmExpected)
	assert.Len(t, m.confirmItems, 2)
}

func TestMainColumnsHotkeyOpensPopupWithoutChangingScreen(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.screen = screenMain
	m.width = 100
	m.height = 30
	m.statuses = []Status{{Item: inventory.Item{Path: "/app/value", Region: "eu-north-1"}, Exists: true}}

	updated, _ := m.updateMain(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = updated.(model)

	assert.Equal(t, screenMain, m.screen)
	assert.Equal(t, popupColumns, m.activePopup)
	assert.Contains(t, m.View(), "Columns")
	assert.Contains(t, m.View(), "# and NAME are always visible.")
}

func TestMainImportHotkeyOpensPopupWithSelectedValues(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.screen = screenMain
	m.width = 120
	m.height = 40
	m.statuses = []Status{{Item: inventory.Item{Path: "/app/value", Region: "eu-north-1"}, Exists: true}}

	updated, _ := m.updateMain(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = updated.(model)

	assert.Equal(t, screenMain, m.screen)
	assert.Equal(t, popupImportFile, m.activePopup)

	view := m.View()
	assert.Contains(t, view, "Import from file")
	assert.Contains(t, view, "File path:")
	assert.Contains(t, view, "Key field:      none")
	assert.Contains(t, view, "Format:         dotenv")
	assert.NotContains(t, view, "> Key field")
}

func TestImportSelectorsUpdateParentPopupValues(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.screen = screenMain
	m.width = 120
	m.height = 40
	m.openImportPopup()

	m.importMainCursor = int(importMainFieldKeyField)
	updated, _ := m.updateImportFilePopup(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	assert.Equal(t, popupImportKeyField, m.activePopup)
	assert.Equal(t, []popupKind{popupImportFile}, m.popupStack)

	updated, _ = m.updateImportKeyFieldPopup(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	updated, _ = m.updateImportKeyFieldPopup(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)

	assert.Equal(t, popupImportFile, m.activePopup)
	assert.Equal(t, "name", m.importKeyField)
	assert.Contains(t, m.View(), "Key field:      name <")

	m.importMainCursor = int(importMainFieldFormat)
	updated, _ = m.updateImportFilePopup(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	assert.Equal(t, popupImportFormat, m.activePopup)
	assert.Equal(t, []popupKind{popupImportFile}, m.popupStack)

	updated, _ = m.updateImportFormatPopup(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	updated, _ = m.updateImportFormatPopup(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)

	assert.Equal(t, popupImportFile, m.activePopup)
	assert.Equal(t, "json", m.importFormat)
	assert.Contains(t, m.View(), "Format:         json <")
}

func TestImportRadioSelectorsMoveSelectedMarkerWithCursor(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.screen = screenMain
	m.width = 120
	m.height = 40
	m.openImportPopup()

	m.importMainCursor = int(importMainFieldFormat)
	updated, _ := m.updateImportFilePopup(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)

	updated, _ = m.updateImportFormatPopup(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)

	view := stripANSI(m.renderImportFormatPopup())
	assert.Contains(t, view, "> (*) json")
	assert.NotContains(t, view, "  (*) dotenv")
}

func TestImportFormNavigationMatchesEditorFieldMovement(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.screen = screenMain
	m.width = 120
	m.height = 40
	m.openImportPopup()
	m.importMainCursor = int(importMainFieldKeyField)
	m.focusImportMain()

	updated, _ := m.updateImportFilePopup(tea.KeyMsg{Type: tea.KeyEnd})
	m = updated.(model)
	assert.Equal(t, int(importMainFieldKeyField), m.importMainCursor)

	updated, _ = m.updateImportFilePopup(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	assert.Equal(t, int(importMainFieldFormat), m.importMainCursor)

	updated, _ = m.updateImportFilePopup(tea.KeyMsg{Type: tea.KeyHome})
	m = updated.(model)
	assert.Equal(t, int(importMainFieldFormat), m.importMainCursor)
}

func TestImportMapPathsBackspaceMovesToPreviousEmptyInput(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.openImportPopup()
	m.importMapPathRows = []importMapPathRow{
		newImportMapPathRow(&m.opts),
		newImportMapPathRow(&m.opts),
		newImportMapPathRow(&m.opts),
	}
	m.importMapPathRows[0].awsPath.SetValue("aws1")
	m.importMapPathRows[0].filePath.SetValue("file1")
	m.importMapPathRows[1].awsPath.SetValue("aws2")
	m.importMapPathsCursor = 3
	m.focusImportMapPath()

	updated, _ := m.updateImportMapPathsPopup(tea.KeyMsg{Type: tea.KeyBackspace})
	m = updated.(model)
	updated, _ = m.updateImportMapPathsPopup(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Z'}})
	m = updated.(model)

	assert.Equal(t, "aws2Z", m.importMapPathRows[1].awsPath.Value())
	assert.Empty(t, m.importMapPathRows[1].filePath.Value())

	m.importMapPathsCursor = 4
	m.focusImportMapPath()

	updated, _ = m.updateImportMapPathsPopup(tea.KeyMsg{Type: tea.KeyBackspace})
	m = updated.(model)
	updated, _ = m.updateImportMapPathsPopup(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Q'}})
	m = updated.(model)

	assert.Equal(t, "Q", m.importMapPathRows[1].filePath.Value())
}

func TestImportFormNavigationUsesConfiguredKeymap(t *testing.T) {
	vi := newModel(context.Background(), nil, nil, &Options{NoColor: true, Keymap: "vi"})
	vi.screen = screenMain
	vi.openImportPopup()

	updated, _ := vi.updateImportFilePopup(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	vi = updated.(model)
	assert.Equal(t, int(importMainFieldKeyField), vi.importMainCursor)

	updated, _ = vi.updateImportFilePopup(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	vi = updated.(model)
	assert.Equal(t, int(importMainFieldFilePath), vi.importMainCursor)

	emacs := newModel(context.Background(), nil, nil, &Options{NoColor: true, Keymap: "emacs"})
	emacs.screen = screenMain
	emacs.openImportPopup()

	updated, _ = emacs.updateImportFilePopup(tea.KeyMsg{Type: tea.KeyCtrlN})
	emacs = updated.(model)
	assert.Equal(t, int(importMainFieldKeyField), emacs.importMainCursor)

	updated, _ = emacs.updateImportFilePopup(tea.KeyMsg{Type: tea.KeyCtrlP})
	emacs = updated.(model)
	assert.Equal(t, int(importMainFieldFilePath), emacs.importMainCursor)
}

func TestImportShortcutsFollowFocusedElementAndKeymap(t *testing.T) {
	emacs := newModel(context.Background(), nil, nil, &Options{NoColor: true, Keymap: "emacs"})
	emacs.openImportPopup()
	emacs.importMainCursor = int(importMainFieldFilePath)
	emacs.openPopupShortcuts(screenMain, popupImportFile)

	text := emacs.shortcutsText()
	assert.Contains(t, text, "enter        load file")
	assert.Contains(t, text, "↑ / ctrl+p / shift+tab     previous field")
	assert.Contains(t, text, "↓ / ctrl+n / tab           next field")
	assert.NotContains(t, text, "Home / alt+<")

	vi := newModel(context.Background(), nil, nil, &Options{NoColor: true, Keymap: "vi"})
	vi.openImportPopup()
	vi.importMainCursor = int(importMainFieldKeyField)
	vi.openPopupShortcuts(screenMain, popupImportFile)

	text = vi.shortcutsText()
	assert.Contains(t, text, "enter        open focused child window")
	assert.Contains(t, text, "↑ / k / shift+tab          previous field")
	assert.Contains(t, text, "↓ / j / tab                next field")
	assert.NotContains(t, text, "Home / gg")
}

func TestImportPopupWidthAdaptsToInputContent(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.width = 140
	m.height = 40
	m.openImportPopup()

	compactWidth := lipgloss.Width(renderLines(m.renderImportFilePopup())[0])

	m.importFilePathInput.SetValue(strings.Repeat("a", 60))
	m.importFilePathInput.SetCursor(60)
	wideWidth := lipgloss.Width(renderLines(m.renderImportFilePopup())[0])

	assert.Less(t, compactWidth, 60)
	assert.Greater(t, wideWidth, compactWidth)
	assert.LessOrEqual(t, wideWidth, m.width)
}

func TestImportMapPathRowsUseCommonInputRenderingAndTrimTrailingEmptyRows(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.width = 120
	m.height = 40
	m.importMapPathRows = []importMapPathRow{newImportMapPathRow(&m.opts), newImportMapPathRow(&m.opts)}

	m.normalizeMapPathRows(&m.opts)

	require.Len(t, m.importMapPathRows, 1)
	assert.NotContains(t, m.renderImportMapPathsPopup(), "___")

	m.importMapPathRows[0].awsPath.SetValue("/app")
	m.normalizeMapPathRows(&m.opts)
	require.Len(t, m.importMapPathRows, 2)

	m.importMapPathRows[0].awsPath.SetValue("")
	m.normalizeMapPathRows(&m.opts)

	require.Len(t, m.importMapPathRows, 1)
}

func TestImportParentSummariesUseEmptyAndConfiguredValues(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true, Region: "eu-west-1"})
	m.width = 140
	m.height = 40
	m.openImportPopup()

	view := m.renderImportFilePopup()
	assert.Contains(t, view, "Map fields:     empty")
	assert.Contains(t, view, "Map paths:      empty")
	assert.Contains(t, view, "Defaults:       empty")

	m.importMapFieldInputs[0].SetValue("title")
	m.importMapPathRows[0].awsPath.SetValue("/app")
	m.importMapPathRows[0].filePath.SetValue("/file")
	m.importDefaultType = ssm.ParameterTypeString
	m.importDefaultDescription.SetValue("hello\nworld")

	view = m.renderImportFilePopup()
	assert.Contains(t, view, "Map fields:     name:title")
	assert.Contains(t, view, "Map paths:      /app:/file")
	assert.Contains(t, view, "Defaults:       type:String;description:hello world")
}

func TestImportDefaultShortcutsFollowFocusedElement(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.importDefaultsCursor = 1
	m.openPopupShortcuts(screenMain, popupImportDefaults)

	assert.Contains(t, m.shortcutsText(), "enter        choose focused option")

	m.importDefaultsCursor = 4

	assert.Contains(t, m.shortcutsText(), "enter        expand/newline in focused text area")

	m.importDefaultPolicies.SetValue("one\ntwo")

	assert.Contains(t, m.shortcutsText(), "enter        insert newline")
	assert.Contains(t, m.shortcutsText(), "alt+e        actions popup")
}

func TestImportDefaultTextAreaExpandsLikeEditorDescription(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.screen = screenMain
	m.width = 120
	m.height = 40
	m.openImportPopup()

	m.importMainCursor = int(importMainFieldDefaults)
	updated, _ := m.updateImportFilePopup(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	require.Equal(t, popupImportDefaults, m.activePopup)

	m.importDefaultsCursor = 5
	m.importDefaultDescription.SetValue("Some text")
	setTextAreaAbsPosition(&m.importDefaultDescription, len("Some text"))
	m.focusImportDefaults()

	view := m.renderImportDefaultsPopup()
	assert.Contains(t, view, "Description: Some text")
	assert.NotContains(t, view, "1 │ Some text")

	updated, _ = m.updateImportDefaultsPopup(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)

	assert.Equal(t, popupImportDefaults, m.activePopup)
	assert.True(t, m.importDefaultAreaExpanded(&m.importDefaultDescription))
	assert.Equal(t, "Some text\n", m.importDefaultDescription.Value())

	view = m.renderImportDefaultsPopup()
	assert.Contains(t, view, "Description:")
	assert.Contains(t, view, "1 │ Some text")
	assert.Contains(t, view, "2 │ █")
}

func TestMultilineAreaShowsTailWhenUnfocused(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	area := newImportTextArea(&m.opts)
	area.SetValue("one\ntwo\nthree\nfour\nfive")

	lines := m.formMultilineAreaLines(&area, 2, 20, false)
	text := strings.Join(lines, "\n")

	assert.Contains(t, text, "4 │ four")
	assert.Contains(t, text, "5 │ five")
	assert.NotContains(t, text, "1 │ one")
}

func TestImportDefaultTextareaLayoutKeepsUnfocusedAreasWhenSpaceAllows(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.width = 120
	m.height = 24
	m.importDefaultsCursor = 5
	m.importDefaultPolicies.SetValue("p1\np2\np3\np4\np5")
	m.importDefaultDescription.SetValue("d1\nd2\nd3\nd4\nd5\nd6")
	setTextAreaAbsPosition(&m.importDefaultDescription, len(m.importDefaultDescription.Value()))
	m.focusImportDefaults()

	view := m.renderImportDefaultsPopup()

	assert.Contains(t, view, "1 │ p1")
	assert.Contains(t, view, "5 │ p5")
	assert.Contains(t, view, "1 │ d1")
	assert.Contains(t, view, "6 │ d6")
}

func TestImportDefaultExpandedTextAreaNavigatesPastThreeLines(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.width = 120
	m.height = 40
	m.importDefaultsCursor = 4
	m.importDefaultPolicies.SetHeight(20)
	m.importDefaultPolicies.SetValue("one\ntwo\nthree\nfour\nfive")
	setTextAreaAbsPosition(&m.importDefaultPolicies, 0)
	m.focusImportDefaults()

	for i := 0; i < 4; i++ {
		updated, _ := m.updateImportDefaultsPopup(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(model)
	}

	line, _ := textAreaCursorLineOffset(&m.importDefaultPolicies)
	assert.Equal(t, 4, line)
}

func TestEditorTextareaLayoutKeepsUnfocusedAreasWhenSpaceAllows(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.width = 120
	m.height = 26
	m.screen = screenTextArea
	m.editField = editFieldValue
	m.editTier = ssm.ParameterTierAdvanced
	m.editPathInput.SetValue("/app/value")
	m.editDescriptionArea.SetValue("desc1\ndesc2\ndesc3\ndesc4")
	m.editPoliciesArea.SetValue("pol1\npol2\npol3\npol4")
	m.textArea.SetValue("value1\nvalue2\nvalue3\nvalue4\nvalue5")
	m.textArea.Focus()
	m.expandedFields = map[editField]bool{editFieldDescription: true, editFieldPolicies: true, editFieldValue: true}

	view := m.renderTextAreaScreen()

	assert.Contains(t, view, "1 │ desc1")
	assert.Contains(t, view, "3 │ desc3")
	assert.Contains(t, view, "4 │ desc4")
	assert.Contains(t, view, "1 │ pol1")
	assert.Contains(t, view, "3 │ pol3")
	assert.Contains(t, view, "4 │ pol4")
	assert.Contains(t, view, "5 │ value5")
}

func TestImportDefaultsReuseEditorSelectorsAndKeepPopupParents(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true, Region: "eu-central-1"})
	m.screen = screenMain
	m.width = 120
	m.height = 40
	m.openImportPopup()

	m.importMainCursor = int(importMainFieldDefaults)
	updated, _ := m.updateImportFilePopup(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	assert.Equal(t, popupImportDefaults, m.activePopup)
	assert.Equal(t, []popupKind{popupImportFile}, m.popupStack)

	view := m.View()
	assert.Contains(t, view, "Region:      none <")
	assert.Contains(t, view, "Type:        none")
	assert.Contains(t, view, "Tier:        none")
	assert.Contains(t, view, "DataType:    none")

	m.importDefaultsCursor = 1
	updated, _ = m.updateImportDefaultsPopup(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	assert.Equal(t, popupTypeSelect, m.activePopup)
	assert.Equal(t, []popupKind{popupImportFile, popupImportDefaults}, m.popupStack)
	assert.Contains(t, m.View(), "none")

	updated, _ = m.updateTypeSelectPopup(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(model)

	assert.Equal(t, popupImportDefaults, m.activePopup)
	assert.Equal(t, []popupKind{popupImportFile}, m.popupStack)
	assert.Equal(t, ssm.ParameterTypeString, m.importDefaultType)
	assert.Contains(t, m.View(), "Type:        String <")

	updated, _ = m.updateImportDefaultsPopup(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	assert.Equal(t, popupImportFile, m.activePopup)
	assert.Empty(t, m.popupStack)
}

func TestImportDefaultSelectorsExposeNoneAsDefaultChoice(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true, Region: "eu-central-1"})
	m.screen = screenMain
	m.width = 120
	m.height = 40
	m.openImportPopup()

	m.importMainCursor = int(importMainFieldDefaults)
	updated, _ := m.updateImportFilePopup(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)

	tests := []struct {
		cursor int
		open   func(model, tea.KeyMsg) (tea.Model, tea.Cmd)
		view   func(model) string
	}{
		{cursor: 0, open: model.updateRegionSelectPopup, view: model.renderRegionSelectPopup},
		{cursor: 1, open: model.updateTypeSelectPopup, view: model.renderTypeSelectPopup},
		{cursor: 2, open: model.updateTierSelectPopup, view: model.renderTierSelectPopup},
		{cursor: 3, open: model.updateDataTypeSelectPopup, view: model.renderDataTypeSelectPopup},
	}

	for _, tt := range tests {
		m.importDefaultsCursor = tt.cursor
		updated, _ = m.updateImportDefaultsPopup(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(model)

		assert.Contains(t, tt.view(m), "none")

		updated, _ = tt.open(m, tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(model)
		assert.Equal(t, popupImportDefaults, m.activePopup)
	}

	assert.Empty(t, m.importDefaultRegion)
	assert.False(t, m.importDefaultType.IsValid())
	assert.False(t, m.importDefaultTier.IsValid())
	assert.False(t, m.importDefaultDataType.IsValid())
}

func TestImportDefaultActionsPopupClearsTextAreasAndKeepsDefaultsOpen(t *testing.T) {
	tests := []struct {
		name        string
		cursor      int
		value       string
		expected    popupKind
		assertEmpty func(*testing.T, model)
	}{
		{
			name:     "policies",
			cursor:   4,
			value:    "policy",
			expected: popupPoliciesActions,
			assertEmpty: func(t *testing.T, m model) {
				t.Helper()
				assert.Empty(t, m.importDefaultPolicies.Value())
			},
		},
		{
			name:     "description",
			cursor:   5,
			value:    "description",
			expected: popupDescriptionActions,
			assertEmpty: func(t *testing.T, m model) {
				t.Helper()
				assert.Empty(t, m.importDefaultDescription.Value())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
			m.screen = screenMain
			m.width = 120
			m.height = 40
			m.openImportPopup()

			m.importMainCursor = int(importMainFieldDefaults)
			updated, _ := m.updateImportFilePopup(tea.KeyMsg{Type: tea.KeyEnter})
			m = updated.(model)

			m.importDefaultsCursor = tt.cursor
			if tt.cursor == 4 {
				m.importDefaultPolicies.SetValue(tt.value)
			} else {
				m.importDefaultDescription.SetValue(tt.value)
			}

			m.focusImportDefaults()
			updated, _ = m.updateImportDefaultsPopup(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}, Alt: true})
			m = updated.(model)

			assert.Equal(t, tt.expected, m.activePopup)
			assert.Equal(t, []popupKind{popupImportFile, popupImportDefaults}, m.popupStack)

			updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
			m = updated.(model)

			assert.Equal(t, popupImportDefaults, m.activePopup)
			assert.Equal(t, []popupKind{popupImportFile}, m.popupStack)
			tt.assertEmpty(t, m)
		})
	}
}

func TestColumnsPopupAppliesImmediatelyAndEscCloses(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.screen = screenMain
	m.activePopup = popupColumns
	m.columnCursor = 0

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(model)
	assert.True(t, m.columns[columnValue])
	assert.Equal(t, popupColumns, m.activePopup)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	assert.True(t, m.columns[columnValue])
	assert.Equal(t, popupNone, m.activePopup)
	assert.Equal(t, screenMain, m.screen)
}

func TestColumnsPopupFooterReplacesMainFooter(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.screen = screenMain
	m.width = 100
	m.height = 30
	m.activePopup = popupColumns
	m.statuses = []Status{{Item: inventory.Item{Path: "/app/value", Region: "eu-north-1"}, Exists: true}}

	view := m.View()

	assert.False(t, strings.Contains(view, "enter apply"))
	assert.Contains(t, strings.ToLower(view), "space toggle")
	assert.Contains(t, view, "x none")
	assert.Contains(t, view, "ctrl+/ help")
	assert.False(t, strings.Contains(view, "enter edit"))
}

func TestColumnsPopupShortcutsClosesToScreen(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.screen = screenMain
	m.activePopup = popupColumns

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlUnderscore})
	m = updated.(model)
	assert.Equal(t, screenMain, m.screen)
	assert.Equal(t, popupShortcuts, m.activePopup)
	assert.Empty(t, m.popupStack)
	assert.Equal(t, screenColumns, m.shortcutsFor)

	updated, _ = m.updateShortcutsPopup(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	assert.Equal(t, screenMain, m.screen)
	assert.Equal(t, popupNone, m.activePopup)
}

func TestSingleDeleteConfirmPopupUsesEnterEscWithoutTypedPhrase(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.screen = screenMain
	m.width = 100
	m.height = 30
	m.statuses = []Status{{Item: inventory.Item{Path: "/app/delete", Region: "eu-north-1"}, Exists: true}}

	updated, _ := m.updateMain(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(model)

	assert.Equal(t, screenMain, m.screen)
	assert.Equal(t, popupConfirm, m.activePopup)
	assert.Empty(t, m.confirmExpected)
	view := m.View()
	assert.Contains(t, view, "Delete selected parameter?")
	assert.Contains(t, view, "enter confirm")
	assert.Contains(t, view, "esc cancel")
	assert.False(t, strings.Contains(view, "Type DELETE"))
}

func TestSingleDeleteConfirmPopupEnterDeletesWithoutTypingPhrase(t *testing.T) {
	item := inventory.Item{Path: "/app/delete", Region: "eu-north-1"}
	client := &fakeSSMClient{region: "eu-north-1", params: map[string]ssm.Parameter{itemKey("eu-north-1", item.Path): {Name: item.Path, Value: "value", Type: ssm.ParameterTypeString.String()}}, metas: map[string]ssm.Metadata{}}
	m := newModel(context.Background(), client, nil, &Options{NoColor: true})
	m.screen = screenMain
	m.activePopup = popupConfirm
	m.confirmPrompt = "Delete selected parameter?"
	m.confirmExpected = ""
	m.confirmItems = []inventory.Item{item}

	updated, cmd := m.updateConfirmPopup(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)

	assert.Equal(t, screenMain, m.screen)
	assert.Equal(t, popupNone, m.activePopup)
	assert.Equal(t, "Deleting 1 parameter(s)...", m.busyMessage)
	require.NotNil(t, cmd)
	msg := cmd()
	deleteMsg, ok := msg.(deleteDoneMsg)
	require.True(t, ok)
	assert.NoError(t, deleteMsg.err)
}

func TestRegionAndTypeSelectorsOpenAsPopups(t *testing.T) {
	m := newModel(context.Background(), &fakeSSMClient{regions: []string{"us-east-1", "eu-central-1"}}, nil, &Options{NoColor: true, Region: "eu-central-1"})
	m.screen = screenTextArea
	m.editField = editFieldRegion
	m.editRegion = "eu-central-1"

	updated, _ := m.updateTextArea(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	assert.Equal(t, screenTextArea, m.screen)
	assert.Equal(t, popupRegionSelect, m.activePopup)
	assert.Equal(t, []string{"eu-central-1", "us-east-1"}, m.regionSelectOptions())

	updated, _ = m.updateRegionSelectPopup(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	assert.Equal(t, popupNone, m.activePopup)
	assert.Equal(t, editFieldRegion, m.editField)

	m.editField = editFieldType
	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	assert.Equal(t, screenTextArea, m.screen)
	assert.Equal(t, popupTypeSelect, m.activePopup)
}

func TestPopupTemplateAddsSharedPadding(t *testing.T) {
	lines := popupPaddedLines([]string{"content"})

	assert.Equal(t, []string{"", "  content  ", ""}, lines)
}

func TestMainFooterDetailsLabelIsDynamic(t *testing.T) {
	assert.Contains(t, mainFooterText(false, false), "ctrl+/ help")
	assert.Contains(t, mainFooterText(false, false), "d show details")
	assert.Contains(t, mainFooterText(true, false), "d hide details")
	assert.Contains(t, mainFooterText(false, false), "X delete all")
	assert.Contains(t, mainFooterText(false, true), "X delete filtered")
	assert.Contains(t, mainFooterText(false, false), "R revert all")
	assert.Contains(t, mainFooterText(false, true), "R revert filtered")
	assert.False(t, strings.Contains(mainFooterText(false, false), "r random"))
	assert.False(t, strings.Contains(mainFooterText(false, false), "v values"))
}

func TestViKeymapNavigatesMainRowsAndSupportsGG(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{Keymap: "vi", NoColor: true})
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
	m := newModel(context.Background(), nil, nil, &Options{})
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
	assert.Equal(t, screenTextArea, m.screen)
}

func TestRandomPopupInsertsIntoEditorWithoutSaving(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{})
	m.screen = screenTextArea
	m.editField = editFieldValue
	m.pushPopup(popupRandomValue)

	updated, cmd := m.updateRandomValuePopup(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	m = updated.(model)

	assert.Nil(t, cmd)
	assert.Equal(t, screenTextArea, m.screen)
	assert.NotEmpty(t, m.textArea.Value())
	assert.Contains(t, m.message, "Press Ctrl-Space to save")
	assert.Equal(t, popupNone, m.activePopup)
}

func TestShortcutsFollowSelectedKeymap(t *testing.T) {
	emacs := newModel(context.Background(), nil, nil, &Options{Keymap: "emacs"})
	emacs.shortcutsFor = screenMain
	assert.Contains(t, emacs.shortcutsText(), "ctrl+n")
	assert.False(t, strings.Contains(emacs.shortcutsText(), "↓ / j / tab"))

	vi := newModel(context.Background(), nil, nil, &Options{Keymap: "vi"})
	vi.shortcutsFor = screenMain
	assert.Contains(t, vi.shortcutsText(), "↓ / j / tab")
	assert.Contains(t, vi.shortcutsText(), "Home / gg")
}

func TestMainEnterEditsSelectedParameterAndEIsUnused(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{})
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
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.width = 120
	m.height = 32
	m.statuses = []Status{{Item: inventory.Item{Path: "/app/missing", Region: "eu-north-1"}, Exists: false, Type: ssm.ParameterTypeSecureString.String()}}

	compact := m.renderSelectedParameterBlock(false)
	expanded := m.renderSelectedParameterBlock(true)

	assert.Contains(t, compact, "Name:   /app/missing")
	assert.Contains(t, compact, "Region: -")
	assert.Contains(t, compact, "Type:   -")
	assert.Contains(t, compact, "Date:   -")
	assert.Contains(t, compact, "Value:  -")
	assert.False(t, strings.Contains(compact, "(hidden)"))

	assert.Contains(t, expanded, "Name:        /app/missing")
	assert.Contains(t, expanded, "Region:      -")
	assert.Contains(t, expanded, "Type:        -")
	assert.Contains(t, expanded, "Version:     -")
	assert.Contains(t, expanded, "Value:       -")
}

func TestSelectedParameterBlocksDoNotRenderStatusField(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.width = 120
	m.height = 32
	m.statuses = []Status{{Item: inventory.Item{Path: "/app/value", Region: "eu-north-1"}, Exists: true, Type: ssm.ParameterTypeString.String(), Value: "value"}}

	compact := m.renderSelectedParameterBlock(false)
	full := m.renderSelectedParameterBlock(true)

	assert.False(t, strings.Contains(compact, "Status:"))
	assert.False(t, strings.Contains(full, "Status:"))
}

func TestColumnsScreenDoesNotOfferStatusColumn(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.width = 120
	m.height = 32
	m.screen = screenColumns

	view := m.renderColumnsScreen()

	assert.False(t, strings.Contains(view, "Status"))
}

func TestSaveValueRejectsEmptyValueBeforeAWSRequest(t *testing.T) {
	m := newModel(context.Background(), &fakeSSMClient{}, nil, &Options{})
	m.width = 120
	m.height = 40
	m.returnScreen = screenMain
	m.statuses = []Status{{Item: inventory.Item{Path: "/app/value", Region: "eu-north-1"}, Exists: true, Type: ssm.ParameterTypeString.String(), Value: "old"}}

	updated, _ := m.startMultiline()
	m = updated.(model)
	m.textArea.SetValue("")
	m = m.focusEditField(editFieldValue)

	updated, cmd := m.updateTextArea(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(model)

	assert.Nil(t, cmd)
	assert.Equal(t, screenTextArea, m.screen)
	assert.Equal(t, "Value is required.", m.errMessage)
}

func TestTextAreaFooterUsesStableHotkeyOrderWithoutColons(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{})
	m.editField = editFieldValue

	footer := m.textAreaFooterText()

	assert.Contains(t, footer, "ctrl+/ help • ctrl+space save")
	assert.Contains(t, footer, "alt+e value actions")
	assert.False(t, strings.Contains(footer, "save AWS"))
	assert.False(t, strings.Contains(footer, "ctrl+o read file"))
	assert.False(t, strings.Contains(footer, "ctrl+t"))
	assert.False(t, strings.Contains(footer, ":"))
}

func TestViEditorStartsNormalAndInsertModeLabelsActiveTextField(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{Keymap: "vi", NoColor: true})
	m.width = 120
	m.height = 30
	m.statuses = []Status{{Item: inventory.Item{Path: "/app/value", Region: "eu-north-1"}, Exists: true, Type: ssm.ParameterTypeString.String(), Value: "value"}}

	updated, _ := m.startMultiline()
	m = updated.(model)
	assert.False(t, m.viInsertMode)
	assert.False(t, strings.Contains(m.renderTextAreaScreen(), "[INSERT]"))

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = updated.(model)
	assert.True(t, m.viInsertMode)
	assert.Contains(t, m.renderTextAreaScreen(), "Name [INSERT]:")

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	assert.False(t, m.viInsertMode)
	assert.Equal(t, screenTextArea, m.screen)
	assert.False(t, strings.Contains(m.renderTextAreaScreen(), "[INSERT]"))

	m = m.focusEditField(editFieldValue)
	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = updated.(model)
	assert.True(t, m.viInsertMode)
	assert.Contains(t, m.renderTextAreaScreen(), "Value [INSERT]:")
	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)

	m.editField = editFieldSSMPath
	m.editPathInput.Focus()
	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = updated.(model)
	assert.Contains(t, m.renderTextAreaScreen(), "Name [INSERT]:")
	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)

	m.editField = editFieldDescription
	m.editDescriptionInput.Focus()
	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = updated.(model)
	assert.Contains(t, m.renderTextAreaScreen(), "Description [INSERT]:")
	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	assert.Equal(t, screenMain, m.screen)
}

func TestViEditorInsertModeTypesAndNormalModeCommandsDoNotType(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{Keymap: "vi"})
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
	m := newModel(context.Background(), nil, nil, &Options{Keymap: "vi"})
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
	m := newModel(context.Background(), nil, nil, &Options{Keymap: "emacs"})
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
	m := newModel(context.Background(), nil, nil, &Options{Keymap: "emacs"})
	m.screen = screenTextArea
	m.editField = editFieldValue
	m.textArea.Focus()

	updated, _ := m.updateTextArea(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = updated.(model)
	assert.Equal(t, "?", m.textArea.Value())
	assert.Equal(t, screenTextArea, m.screen)

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyCtrlUnderscore})
	m = updated.(model)
	assert.Equal(t, screenTextArea, m.screen)
	assert.Equal(t, popupShortcuts, m.activePopup)
}

func TestTextAreaLineNumbersAreAlignedByTotalLineCount(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
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

func TestMainDefaultHidesSelectedParameterAndDetailsToggleShowsExpanded(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.width = 120
	m.height = 32
	m.screen = screenMain
	m.statuses = []Status{{Item: inventory.Item{Path: "/app/value", Region: "eu-north-1"}, Exists: true, Type: ssm.ParameterTypeString.String(), Value: "value", Version: 7, SHA256Prefix: "abcdef"}}

	defaultView := m.renderMainScreen()
	assert.Contains(t, defaultView, "List of 1 Parameters")
	assert.False(t, strings.Contains(defaultView, "Selected Parameter"))
	assert.False(t, strings.Contains(defaultView, "Version:"))

	updated, _ := m.updateMain(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = updated.(model)
	expanded := m.renderMainScreen()
	assert.True(t, m.selectedExpanded)
	assert.Contains(t, expanded, "Selected Parameter")
	assert.Contains(t, expanded, "Version:")
	assert.Contains(t, expanded, "SHA256:")

	updated, _ = m.updateMain(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = updated.(model)
	assert.False(t, m.selectedExpanded)
	assert.False(t, strings.Contains(m.renderMainScreen(), "Selected Parameter"))
}

func TestMainDetailsToggleDoesNothingVisuallyForEmptyList(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.width = 120
	m.height = 32
	m.screen = screenMain

	updated, _ := m.updateMain(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = updated.(model)

	assert.True(t, m.selectedExpanded)
	assert.Contains(t, m.renderMainScreen(), "List of 0 Parameters")
	assert.False(t, strings.Contains(m.renderMainScreen(), "Selected Parameter"))
}

func TestMainNewParameterOpensEditorFocusedOnSSMPath(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true, Region: "eu-north-1", Regions: []string{"eu-north-1"}})
	m.screen = screenMain

	updated, _ := m.updateMain(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = updated.(model)

	assert.Equal(t, screenTextArea, m.screen)
	assert.Equal(t, editFieldSSMPath, m.editField)
	assert.True(t, m.editPathInput.Focused())
	assert.Equal(t, "", m.editPathInput.Value())
	assert.Equal(t, ssm.DefaultParameterType, m.editType)
	assert.Equal(t, "eu-north-1", m.editRegion)
}

func TestNewParameterSaveValidatesPathAndValue(t *testing.T) {
	m := newModel(context.Background(), &fakeSSMClient{}, nil, &Options{NoColor: true, Region: "eu-north-1", Regions: []string{"eu-north-1"}})
	m.screen = screenMain
	updated, _ := m.updateMain(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = updated.(model)

	updated, cmd := m.updateTextArea(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(model)

	assert.Nil(t, cmd)
	assert.Equal(t, "Name is required.", m.errMessage)
	assert.Equal(t, screenTextArea, m.screen)

	m.editPathInput.SetValue("/app/new")
	updated, cmd = m.updateTextArea(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(model)

	assert.Nil(t, cmd)
	assert.Equal(t, "Value is required.", m.errMessage)
}

func TestReplaceStatusAppendsNewParameterAndSelectsIt(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{})
	m.statuses = []Status{{Item: inventory.Item{Path: "/app/old", Region: "eu-north-1"}, Exists: true}}

	m.replaceStatus("", &Status{Item: inventory.Item{Path: "/app/new", Region: "eu-north-1"}, Exists: true, Type: ssm.ParameterTypeString.String(), Value: "value"})

	require.Len(t, m.statuses, 2)
	assert.Equal(t, "/app/new", m.statuses[1].Item.Path)
	assert.Equal(t, 1, m.selected)
}

func TestCursorRenderingDoesNotInsertExtraCharacterInsideText(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: false})
	m.screen = screenTextArea
	m.editField = editFieldValue
	m.textArea.Focus()
	m.textArea.SetValue("hello world")
	(&m).setTextAreaCursorAbs(2)

	rows := m.renderTextAreaValueLines(1)
	require.Len(t, rows, 1)
	plain := stripANSI(rows[0])

	assert.Contains(t, plain, "1 │ hello world")
	assert.False(t, strings.Contains(plain, "he█llo"))
	assert.Equal(t, lipgloss.Width("1 │ hello world"), lipgloss.Width(strings.TrimRight(plain, " ")))
}

func TestNewParameterSaveCommandCreatesStatus(t *testing.T) {
	client := &fakeSSMClient{params: map[string]ssm.Parameter{}, metas: map[string]ssm.Metadata{}}
	m := newModel(context.Background(), client, nil, &Options{NoColor: true, Region: "eu-north-1", Regions: []string{"eu-north-1"}})
	m.screen = screenMain
	updated, _ := m.updateMain(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = updated.(model)
	m.editPathInput.SetValue("/app/new")
	m.textArea.SetValue("secret")

	updated, cmd := m.updateTextArea(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(model)

	require.NotNil(t, cmd)
	assert.Equal(t, screenTextArea, m.screen)
	assert.Equal(t, "Saving parameter...", m.busyMessage)

	msg := cmd()
	statusMsg, ok := msg.(statusUpdatedMsg)
	require.True(t, ok)
	require.NoError(t, statusMsg.err)
	assert.Equal(t, "/app/new", statusMsg.path)
	assert.Equal(t, "", statusMsg.oldPath)
	assert.True(t, statusMsg.status.Exists)
	assert.Equal(t, "secret", statusMsg.status.Value)
}

func TestPrintableQCanBeTypedInEditableFields(t *testing.T) {
	cases := []struct {
		name   string
		keymap string
		insert bool
		field  editField
	}{
		{name: "emacs name", keymap: "emacs", insert: true, field: editFieldSSMPath},
		{name: "emacs file path", keymap: "emacs", insert: true, field: editFieldFilePath},
		{name: "emacs value", keymap: "emacs", insert: true, field: editFieldValue},
		{name: "vi insert name", keymap: "vi", insert: true, field: editFieldSSMPath},
		{name: "vi insert file path", keymap: "vi", insert: true, field: editFieldFilePath},
		{name: "vi insert value", keymap: "vi", insert: true, field: editFieldValue},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			m := newModel(context.Background(), nil, nil, &Options{Keymap: tt.keymap})
			m.screen = screenTextArea
			m.returnScreen = screenMain
			m.editField = tt.field
			m.viInsertMode = tt.insert
			m = m.focusEditField(tt.field)

			updated, _ := m.updateTextArea(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
			m = updated.(model)

			assert.Equal(t, screenTextArea, m.screen)

			switch tt.field {
			case editFieldRegion, editFieldType, editFieldTier, editFieldDataType, editFieldOverwrite, editFieldDescription, editFieldPolicies:
			case editFieldSSMPath:
				assert.Equal(t, "q", m.editPathInput.Value())
			case editFieldFilePath:
				assert.Equal(t, "q", m.editFileInput.Value())
			case editFieldValue:
				assert.Equal(t, "q", m.textArea.Value())

			default:
			}
		})
	}
}

func TestViNormalModeQStillBacksOutOfEditor(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{Keymap: "vi"})
	m.screen = screenTextArea
	m.returnScreen = screenMain
	m.editField = editFieldValue
	m.viInsertMode = false
	m.textArea.Focus()

	updated, _ := m.updateTextArea(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m = updated.(model)

	assert.Equal(t, screenMain, m.screen)
}

func TestViInsertModeTypesOnAllEditableFieldsAndEscReturnsToNormal(t *testing.T) {
	fields := []editField{editFieldSSMPath, editFieldFilePath, editFieldValue}
	for _, field := range fields {
		m := newModel(context.Background(), nil, nil, &Options{Keymap: "vi"})
		m.screen = screenTextArea
		m.returnScreen = screenMain
		m.editField = field
		m.viInsertMode = false
		m = m.focusEditField(field)

		updated, _ := m.updateTextArea(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
		m = updated.(model)
		assert.True(t, m.viInsertMode)

		updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a', 'b', 'c', '?'}})
		m = updated.(model)

		switch field {
		case editFieldRegion, editFieldType, editFieldTier, editFieldDataType, editFieldOverwrite, editFieldDescription, editFieldPolicies:
		case editFieldSSMPath:
			assert.Equal(t, "abc?", m.editPathInput.Value())
		case editFieldFilePath:
			assert.Equal(t, "abc?", m.editFileInput.Value())
		case editFieldValue:
			assert.Equal(t, "abc?", m.textArea.Value())

		default:
		}

		updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyEsc})
		m = updated.(model)
		assert.False(t, m.viInsertMode)
		assert.Equal(t, screenTextArea, m.screen)
	}
}

func TestEditableTextInputsUseValueStyleInColorMode(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)

	m := newModel(context.Background(), nil, nil, &Options{NoColor: false, Keymap: "vi"})
	m.width = 120
	m.height = 30
	m.screen = screenTextArea
	m.editRegion = "eu-central-1"
	m.editType = ssm.ParameterTypeString
	m.editField = editFieldSSMPath
	m.viInsertMode = true
	m.editPathInput.SetValue("/app/path")
	m.editPathInput.Focus()
	m.editDescriptionInput.SetValue("Example parameter")

	view := m.renderTextAreaScreen()

	assert.Contains(t, view, "\x1b[38;5;254m/app/path")
	assert.Contains(t, view, "\x1b[38;5;254mExample parameter")
}

func TestNewParameterSaveKeepsNamesFileReadOnlyByDefault(t *testing.T) {
	pathsFile := filepath.Join(t.TempDir(), "paths.txt")
	require.NoError(t, os.WriteFile(pathsFile, []byte("/app/old\n"), 0o600))

	client := &fakeSSMClient{params: map[string]ssm.Parameter{}, metas: map[string]ssm.Metadata{}}
	m := newModel(context.Background(), client, nil, &Options{NoColor: true, Region: "eu-north-1", Regions: []string{"eu-north-1"}, NamesFile: pathsFile})
	m.screen = screenMain
	updated, _ := m.updateMain(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = updated.(model)
	m.editPathInput.SetValue("/app/new")
	m.textArea.SetValue("secret")

	updated, cmd := m.updateTextArea(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(model)

	require.NotNil(t, cmd)
	msg := cmd()
	statusMsg, ok := msg.(statusUpdatedMsg)
	require.True(t, ok)
	require.NoError(t, statusMsg.err)
	assert.Equal(t, "Updated /app/new", statusMsg.message)
	assert.Equal(t, "/app/old\n", readFileString(t, pathsFile))

	updatedModel, _ := m.Update(statusMsg)
	m = updatedModel.(model)
	assert.Contains(t, m.visiblePaths(), "/app/new")
}

func TestNewParameterSaveWithNamesFileUpdateAppendsPath(t *testing.T) {
	pathsFile := filepath.Join(t.TempDir(), "paths.txt")
	require.NoError(t, os.WriteFile(pathsFile, []byte("/app/old\n"), 0o600))

	client := &fakeSSMClient{params: map[string]ssm.Parameter{}, metas: map[string]ssm.Metadata{}}
	m := newModel(context.Background(), client, nil, &Options{NoColor: true, Region: "eu-north-1", Regions: []string{"eu-north-1"}, NamesFile: pathsFile, AllowNamesFileUpdate: true})
	m.screen = screenMain
	updated, _ := m.updateMain(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = updated.(model)
	m.editPathInput.SetValue("/app/new")
	m.textArea.SetValue("secret")

	_, cmd := m.updateTextArea(tea.KeyMsg{Type: tea.KeyCtrlS})
	require.NotNil(t, cmd)
	msg := cmd()
	statusMsg, ok := msg.(statusUpdatedMsg)
	require.True(t, ok)
	require.NoError(t, statusMsg.err)
	assert.Equal(t, "Updated /app/new and added it to "+pathsFile, statusMsg.message)
	assert.Equal(t, "path-file", statusMsg.status.Item.Kind)
	assert.Equal(t, pathsFile, statusMsg.status.Item.Source)
	assert.Equal(t, "/app/old\n/app/new\n", readFileString(t, pathsFile))
}

func TestNewParameterSaveWithNamesFileUpdateDoesNotDuplicateExistingEntry(t *testing.T) {
	pathsFile := filepath.Join(t.TempDir(), "paths.txt")
	require.NoError(t, os.WriteFile(pathsFile, []byte("/app/new # already tracked\n"), 0o600))

	client := &fakeSSMClient{params: map[string]ssm.Parameter{}, metas: map[string]ssm.Metadata{}}
	m := newModel(context.Background(), client, nil, &Options{NoColor: true, Region: "eu-north-1", Regions: []string{"eu-north-1"}, NamesFile: pathsFile, AllowNamesFileUpdate: true})
	m.screen = screenMain
	updated, _ := m.updateMain(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = updated.(model)
	m.editPathInput.SetValue("/app/new")
	m.textArea.SetValue("secret")

	_, cmd := m.updateTextArea(tea.KeyMsg{Type: tea.KeyCtrlS})
	require.NotNil(t, cmd)
	msg := cmd()
	statusMsg, ok := msg.(statusUpdatedMsg)
	require.True(t, ok)
	require.NoError(t, statusMsg.err)
	assert.Equal(t, "Updated /app/new", statusMsg.message)
	assert.Equal(t, "/app/new # already tracked\n", readFileString(t, pathsFile))
}

func readFileString(t *testing.T, file string) string {
	t.Helper()

	data, err := fileio.ReadFile(file)
	require.NoError(t, err)

	return string(data)
}

func submitFileActionPopup(t *testing.T, m model, mode, path string) (model, tea.Cmd) {
	t.Helper()

	m.activePopup = popupFileAction
	m.fileActionMode = mode
	m.input.SetValue(path)
	m.input.Focus()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	actual, ok := updated.(model)
	require.True(t, ok)

	return actual, cmd
}

func TestNewParameterEditorDoesNotRenderPlaceholders(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true, Keymap: "vi", Region: "eu-central-1", Regions: []string{"eu-central-1"}})
	m.width = 120
	m.height = 30
	m.screen = screenMain
	updated, _ := m.updateMain(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = updated.(model)

	assert.Equal(t, "", m.editPathInput.Placeholder)
	assert.Equal(t, "", m.editFileInput.Placeholder)
	assert.False(t, strings.Contains(m.renderTextAreaScreen(), "/app/env/service/NAME"))
}

func TestViInsertLabelsUseLabelStyleForEditableFields(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)

	cases := []struct {
		name  string
		field editField
		label string
	}{
		{name: "name", field: editFieldSSMPath, label: "Name [INSERT]:"},
		{name: "description", field: editFieldDescription, label: "Description [INSERT]:"},
		{name: "value", field: editFieldValue, label: "Value [INSERT]:"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newModel(context.Background(), nil, nil, &Options{NoColor: false, Keymap: "vi", Region: "eu-central-1"})
			m.width = 120
			m.height = 30
			m.screen = screenTextArea
			m.editRegion = "eu-central-1"
			m.editType = ssm.ParameterTypeString
			m.editPathInput.SetValue("/app/path")
			m.editFileInput.SetValue("")
			m.textArea.SetValue("value")
			m.editField = tc.field
			m.viInsertMode = true
			m = m.focusEditField(tc.field)

			view := m.renderTextAreaScreen()

			assert.Contains(t, view, "\x1b[38;5;214m"+tc.label)
		})
	}
}

func TestEditTextInputLineKeepsColorWhenTerminalIsNarrow(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)

	m := newModel(context.Background(), nil, nil, &Options{NoColor: false, Keymap: "vi", Region: "eu-central-1"})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 86, Height: 24})
	m = updated.(model)
	m.screen = screenTextArea
	m.editRegion = "eu-central-1"
	m.editType = ssm.ParameterTypeString
	m.editField = editFieldSSMPath
	m.viInsertMode = true
	m.editPathInput.SetValue("/app-infra/stage/biptec/website/STRIPE_API_KEY")
	m = m.focusEditField(editFieldSSMPath)

	view := m.renderTextAreaScreen()

	assert.Contains(t, view, "\x1b[38;5;214mName [INSERT]:")
	assert.Contains(t, view, "\x1b[38;5;254m")
}

func TestParseColumnOptionAcceptsOnlyAWSBackedOptionalColumns(t *testing.T) {
	columns, err := ParseColumnOption("region,type,value,sha256")
	require.NoError(t, err)
	assert.Equal(t, []string{"region", "type", "value", "sha256"}, columns)

	unsupported := []string{"source", "app", "component", "secret", "kind", "index"}
	for _, name := range unsupported {
		t.Run(name, func(t *testing.T) {
			columns, err := ParseColumnOption(name)
			assert.Nil(t, columns)
			require.Error(t, err)
			assert.ErrorContains(t, err, "unsupported --show-column value")
		})
	}
}

func TestInitialColumnsUseIndexAndPathAsBaseColumns(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.width = 100
	m.height = 30
	m.statuses = []Status{{Item: inventory.Item{Path: "/app/path", Region: "eu-central-1"}, Exists: true, Type: ssm.ParameterTypeString.String(), Value: "value"}}

	cols := m.tableColumns(m.visible())

	require.Len(t, cols, 2)
	assert.Equal(t, columnIndex, cols[0].key)
	assert.Equal(t, columnPath, cols[1].key)
}

func TestColumnsOptionEnablesOptionalColumnsAfterIndexAndPath(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true, ShowColumns: []string{"region", "type", "value"}})
	m.width = 120
	m.height = 30
	m.statuses = []Status{{Item: inventory.Item{Path: "/app/path", Region: "eu-central-1"}, Exists: true, Type: ssm.ParameterTypeString.String(), Value: "value"}}

	cols := m.tableColumns(m.visible())

	require.Len(t, cols, 5)
	assert.Equal(t, columnIndex, cols[0].key)
	assert.Equal(t, columnPath, cols[1].key)
	assert.Equal(t, columnValue, cols[2].key)
	assert.Equal(t, columnType, cols[3].key)
	assert.Equal(t, columnRegion, cols[4].key)
}

func TestColumnChooserShowsOnlyAWSBackedOptionalColumns(t *testing.T) {
	labels := make([]string, 0, len(columnItems()))
	for _, column := range columnItems() {
		labels = append(labels, string(column))
	}

	assert.Equal(t, []string{"value", "type", "region", "date", "version", "tier", "len", "sha256", "user", "description"}, labels)
}

func TestDeleteWithoutNamesFileRemovesRowsFromUI(t *testing.T) {
	item := inventory.Item{Path: "/app/delete", Region: "eu-north-1"}
	m := newModel(context.Background(), &fakeSSMClient{}, nil, &Options{NoColor: true, Region: "eu-north-1"})
	m.returnScreen = screenMain
	m.statuses = []Status{{Item: item, Exists: true}, {Item: inventory.Item{Path: "/app/keep", Region: "eu-north-1"}, Exists: true}}

	msg := deleteCmd(context.Background(), m.client, []inventory.Item{item}, "", false)()
	deleteMsg, ok := msg.(deleteDoneMsg)
	require.True(t, ok)
	assert.True(t, deleteMsg.removeRows)
	updated, _ := m.Update(deleteMsg)
	m = updated.(model)

	assert.False(t, stringSliceContains(m.visiblePaths(), "/app/delete"))
	assert.True(t, stringSliceContains(m.visiblePaths(), "/app/keep"))
}

func TestDeleteWithReadOnlyNamesFileMarksRowsMissing(t *testing.T) {
	pathsFile := filepath.Join(t.TempDir(), "paths.txt")
	require.NoError(t, os.WriteFile(pathsFile, []byte("/app/delete\n"), 0o600))

	item := inventory.Item{Path: "/app/delete", Region: "eu-north-1"}
	m := newModel(context.Background(), &fakeSSMClient{}, nil, &Options{NoColor: true, Region: "eu-north-1", NamesFile: pathsFile})
	m.returnScreen = screenMain
	m.statuses = []Status{{Item: item, Exists: true}}

	msg := deleteCmd(context.Background(), m.client, []inventory.Item{item}, pathsFile, false)()
	deleteMsg, ok := msg.(deleteDoneMsg)
	require.True(t, ok)
	assert.False(t, deleteMsg.removeRows)
	updated, _ := m.Update(deleteMsg)
	m = updated.(model)

	require.Len(t, m.statuses, 1)
	assert.Equal(t, "/app/delete", m.statuses[0].Item.Path)
	assert.False(t, m.statuses[0].Exists)
	assert.Equal(t, "/app/delete\n", readFileString(t, pathsFile))
}

func TestDeleteWithNamesFileUpdateRemovesPathAndRow(t *testing.T) {
	pathsFile := filepath.Join(t.TempDir(), "paths.txt")
	require.NoError(t, os.WriteFile(pathsFile, []byte("/app/delete\n/app/keep\n"), 0o600))

	item := inventory.Item{Path: "/app/delete", Region: "eu-north-1"}
	m := newModel(context.Background(), &fakeSSMClient{}, nil, &Options{NoColor: true, Region: "eu-north-1", NamesFile: pathsFile, AllowNamesFileUpdate: true})
	m.returnScreen = screenMain
	m.statuses = []Status{{Item: item, Exists: true}, {Item: inventory.Item{Path: "/app/keep", Region: "eu-north-1"}, Exists: true}}

	msg := deleteCmd(context.Background(), m.client, []inventory.Item{item}, pathsFile, true)()
	deleteMsg, ok := msg.(deleteDoneMsg)
	require.True(t, ok)
	assert.True(t, deleteMsg.removeRows)
	updated, _ := m.Update(deleteMsg)
	m = updated.(model)

	assert.False(t, stringSliceContains(m.visiblePaths(), "/app/delete"))
	assert.True(t, stringSliceContains(m.visiblePaths(), "/app/keep"))
	assert.Equal(t, "/app/keep\n", readFileString(t, pathsFile))
}

func TestSortPopupShortcutsShowLetterHotkeysOnlyInSortContext(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true, ShowColumns: []string{"value", "type"}})
	m.screen = screenMain
	m.width = 120
	m.height = 40
	m.activePopup = popupSort

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlUnderscore})
	m = updated.(model)

	assert.Equal(t, popupShortcuts, m.activePopup)
	assert.Empty(t, m.popupStack)
	view := m.View()
	assert.Contains(t, view, "Sort")
	assert.Contains(t, view, "n            sort by Name")
	assert.Contains(t, view, "v            sort by Value")
	assert.False(t, strings.Contains(view, "1            sort by Name"))
}

func TestValueActionsPopupAcceptsFooterHotkeys(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.screen = screenTextArea
	m.activePopup = popupValueActions
	m.editField = editFieldValue
	m.textArea.SetValue("value")
	m.valueActionCursor = 0

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = updated.(model)

	assert.Equal(t, popupRandomValue, m.activePopup)
	assert.Equal(t, "value", m.textArea.Value())
}

func TestFileActionPopupConfirmsSecureWriteWarningWithY(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	path := t.TempDir() + "/secret.txt"
	m.screen = screenTextArea
	m.activePopup = popupFileAction
	m.fileActionMode = "write"
	m.editType = ssm.ParameterTypeSecureString
	m.textArea.SetValue("secret")
	m.input.SetValue(path)
	m.input.Focus()

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)

	assert.Nil(t, cmd)
	assert.Equal(t, popupFileWriteConfirm, m.activePopup)
	assert.Equal(t, []popupKind{popupFileAction}, m.popupStack)
	assert.Equal(t, fileWriteConfirmationSecure, m.pendingFileWrite)

	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(model)

	assert.Nil(t, cmd)
	assert.Equal(t, popupNone, m.activePopup)
	assert.Equal(t, fileWriteConfirmationNone, m.pendingFileWrite)
	assert.Equal(t, "secret", readFileString(t, path))
}

func TestFileActionPopupUsesCompactInputWithVisibleCursor(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.screen = screenTextArea
	m.activePopup = popupFileAction
	m.fileActionMode = "write"
	m.width = 208
	m.height = 69
	m.input.SetValue("")
	m.input.Focus()

	view := m.View()

	assert.Contains(t, view, "Write to file")
	assert.Contains(t, view, "File path: █")
	assert.Contains(t, view, "enter write")
	assert.Contains(t, view, "esc cancel")
	assert.False(t, strings.Contains(view, "..."))
}

func TestSaveValueCmdWritesSelectedParameterTier(t *testing.T) {
	client := &fakeSSMClient{region: "eu-north-1", params: map[string]ssm.Parameter{}, metas: map[string]ssm.Metadata{}}
	item := inventory.Item{Path: "/app/hosts", Region: "eu-north-1"}

	msg := saveValueCmd(context.Background(), client, &item, item.Path, "plain", ssm.ParameterTypeString, ssm.PutParameterOptions{Tier: ssm.ParameterTierAdvanced, DataType: ssm.DefaultParameterDataType, Overwrite: true}, "", false)()

	updated, ok := msg.(statusUpdatedMsg)
	require.True(t, ok)
	require.NoError(t, updated.err)

	storedMeta := client.metas[itemKey("eu-north-1", "/app/hosts")]
	assert.Equal(t, ssm.ParameterTierAdvanced.String(), storedMeta.Tier)
}

func TestEditorShowsPoliciesOnlyForAdvancedTier(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.screen = screenTextArea
	m.width = 120
	m.height = 30
	m.editField = editFieldValue
	m.editPathInput.SetValue("/app/value")
	m.editRegion = "eu-north-1"
	m.editType = ssm.ParameterTypeString
	m.editDataType = ssm.DefaultParameterDataType
	m.editOverwrite = true
	m.textArea.SetValue("value")
	m.editTier = ssm.ParameterTierStandard

	assert.False(t, strings.Contains(m.renderTextAreaScreen(), "Policies:"))

	m.editTier = ssm.ParameterTierAdvanced
	assert.Contains(t, m.renderTextAreaScreen(), "Policies:")
}

func TestSaveValueOmitsPoliciesUnlessTierIsAdvanced(t *testing.T) {
	client := &fakeSSMClient{region: "eu-north-1", params: map[string]ssm.Parameter{}, metas: map[string]ssm.Metadata{}}
	m := newModel(context.Background(), client, nil, &Options{NoColor: true, Region: "eu-north-1"})
	m.screen = screenTextArea
	m.editPathInput.SetValue("/app/value")
	m.editRegion = "eu-north-1"
	m.editType = ssm.ParameterTypeString
	m.editTier = ssm.ParameterTierStandard
	m.editDataType = ssm.DefaultParameterDataType
	m.editOverwrite = true
	m.editPoliciesArea.SetValue("policy")

	_, cmd := m.saveValue("value")
	msg := cmd()
	updated, ok := msg.(statusUpdatedMsg)
	require.True(t, ok)
	require.NoError(t, updated.err)
	assert.Empty(t, client.metas[itemKey("eu-north-1", "/app/value")].Policies)
}

func TestSaveValueClearsExistingPoliciesWhenAdvancedPoliciesEmptied(t *testing.T) {
	client := &fakeSSMClient{region: "eu-north-1", params: map[string]ssm.Parameter{}, metas: map[string]ssm.Metadata{}, putOpts: map[string]ssm.PutParameterOptions{}}
	m := newModel(context.Background(), client, nil, &Options{NoColor: true, Region: "eu-north-1"})
	item := inventory.Item{Path: "/app/value", Region: "eu-north-1"}
	m.screen = screenTextArea
	m.statuses = []Status{{Item: item, Exists: true, Type: ssm.ParameterTypeString.String(), Tier: ssm.ParameterTierAdvanced.String(), DataType: ssm.DefaultParameterDataType.String(), Value: "old", Policies: `[{"Type":"Expiration","Version":"1.0"}]`}}
	m.editPathInput.SetValue(item.Path)
	m.editRegion = "eu-north-1"
	m.editType = ssm.ParameterTypeString
	m.editTier = ssm.ParameterTierAdvanced
	m.editDataType = ssm.DefaultParameterDataType
	m.editOverwrite = true
	m.editPoliciesArea.SetValue("")

	_, cmd := m.saveValue("value")
	msg := cmd()
	updated, ok := msg.(statusUpdatedMsg)
	require.True(t, ok)
	require.NoError(t, updated.err)
	assert.Equal(t, "[{}]", client.putOpts[itemKey("eu-north-1", "/app/value")].Policies)
	assert.True(t, client.putOpts[itemKey("eu-north-1", "/app/value")].PoliciesSet)
	assert.Equal(t, "[{}]", client.metas[itemKey("eu-north-1", "/app/value")].Policies)
}

func TestWrappedMultilineCursorRendersAndMovesAcrossVisualRows(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.width = 40
	m.height = 20
	m.screen = screenTextArea
	m.editField = editFieldValue
	m.textArea.Focus()
	wrapWidth := m.multilineContentWidth()
	longLine := strings.Repeat("d", wrapWidth+5)
	m.textArea.SetValue(longLine + "\nnext")
	m.setTextAreaCursorAbs(wrapWidth + 3)

	view := strings.Join(m.renderMultilineFieldLines(editFieldValue, &m.textArea, 4), "\n")
	assert.Contains(t, view, "| "+strings.Repeat("d", 3)+"█")

	m.setTextAreaCursorAbs(0)
	m.moveActiveTextLine(1)
	assert.Equal(t, wrapWidth, m.textAreaCursorAbs())
	m.moveActiveTextLine(1)
	assert.Equal(t, len(longLine)+1, m.textAreaCursorAbs())
}

func TestTextAreaOldValueActionHotkeysAreDisabled(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.screen = screenTextArea
	m.editField = editFieldValue
	m.textArea.Focus()
	m.textArea.SetValue("keep")

	keys := []tea.KeyMsg{
		{Type: tea.KeyCtrlK},
		{Type: tea.KeyCtrlW},
		{Type: tea.KeyCtrlR},
		{Type: tea.KeyCtrlV},
	}
	for _, key := range keys {
		updated, cmd := m.updateTextArea(key)
		m = updated.(model)

		assert.Nil(t, cmd)
		assert.Equal(t, "keep", m.textArea.Value())
		assert.Equal(t, popupNone, m.activePopup)
		assert.Equal(t, screenTextArea, m.screen)
	}
}

func TestOverwriteFieldIsShownOnlyForNewParameters(t *testing.T) {
	existing := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	existing.width = 120
	existing.height = 30
	existing.statuses = []Status{{Item: inventory.Item{Path: "/app/value", Region: "eu-north-1"}, Exists: true, Type: ssm.ParameterTypeString.String(), Value: "value"}}
	updated, _ := existing.startMultiline()
	existing = updated.(model)
	assert.False(t, existing.shouldShowOverwriteField())
	assert.False(t, strings.Contains(existing.renderTextAreaScreen(), "Overwrite:"))

	created := newModel(context.Background(), nil, nil, &Options{NoColor: true, Region: "eu-north-1"})
	created.width = 120
	created.height = 30
	updated, _ = created.startNewParameter(screenMain)
	created = updated.(model)
	assert.True(t, created.shouldShowOverwriteField())
	assert.False(t, created.editOverwrite)
	assert.Contains(t, created.renderTextAreaScreen(), "Overwrite:")
	assert.Contains(t, created.renderTextAreaScreen(), "false")
}

func TestSaveValueUsesOverwriteOnlyForNewParameters(t *testing.T) {
	existingClient := &fakeSSMClient{region: "eu-north-1", params: map[string]ssm.Parameter{}, metas: map[string]ssm.Metadata{}, putOpts: map[string]ssm.PutParameterOptions{}}
	existing := newModel(context.Background(), existingClient, nil, &Options{NoColor: true, Region: "eu-north-1"})
	existing.screen = screenTextArea
	existing.statuses = []Status{{Item: inventory.Item{Path: "/app/existing", Region: "eu-north-1"}, Exists: true, Type: ssm.ParameterTypeString.String(), Value: "old"}}
	existing.editPathInput.SetValue("/app/existing")
	existing.editRegion = "eu-north-1"
	existing.editType = ssm.ParameterTypeString
	existing.editTier = ssm.ParameterTierStandard
	existing.editDataType = ssm.DefaultParameterDataType
	existing.editOverwrite = false
	_, cmd := existing.saveValue("new")
	msg := cmd()
	statusMsg, ok := msg.(statusUpdatedMsg)
	require.True(t, ok)
	require.NoError(t, statusMsg.err)
	assert.True(t, existingClient.putOpts[itemKey("eu-north-1", "/app/existing")].Overwrite)

	newClient := &fakeSSMClient{region: "eu-north-1", params: map[string]ssm.Parameter{}, metas: map[string]ssm.Metadata{}, putOpts: map[string]ssm.PutParameterOptions{}}
	created := newModel(context.Background(), newClient, nil, &Options{NoColor: true, Region: "eu-north-1"})
	created.screen = screenTextArea
	created.editPathInput.SetValue("/app/new")
	created.editRegion = "eu-north-1"
	created.editType = ssm.ParameterTypeString
	created.editTier = ssm.ParameterTierStandard
	created.editDataType = ssm.DefaultParameterDataType
	created.editOverwrite = false
	_, cmd = created.saveValue("value")
	msg = cmd()
	statusMsg, ok = msg.(statusUpdatedMsg)
	require.True(t, ok)
	require.NoError(t, statusMsg.err)
	assert.False(t, newClient.putOpts[itemKey("eu-north-1", "/app/new")].Overwrite)
}

func TestSortHotkeyTogglesDirectionAndHeaderArrow(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true, ShowColumns: []string{"version"}})
	m.screen = screenMain
	m.statuses = []Status{
		{Item: inventory.Item{Path: "/v1", Region: "eu-central-1"}, Exists: true, Version: 1},
		{Item: inventory.Item{Path: "/v18", Region: "eu-central-1"}, Exists: true, Version: 18},
		{Item: inventory.Item{Path: "/v2", Region: "eu-central-1"}, Exists: true, Version: 2},
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = updated.(model)
	assert.False(t, m.sortDescending)
	assert.Equal(t, []string{"/v1", "/v2", "/v18"}, m.visiblePaths())
	assert.Contains(t, m.renderListBlock(), "VERSION ↑")

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = updated.(model)
	assert.True(t, m.sortDescending)
	assert.Equal(t, []string{"/v18", "/v2", "/v1"}, m.visiblePaths())
	assert.Contains(t, m.renderListBlock(), "VERSION ↓")
}

func TestPopupShowsInternalActionsAndBottomHotkeys(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true, ShowColumns: []string{"value"}})
	m.screen = screenMain
	m.activePopup = popupSort
	m.width = 100
	m.height = 30

	view := m.View()

	assert.Contains(t, view, "Esc close")
	assert.False(t, strings.Contains(view, "enter sort/toggle"))
	assert.Contains(t, strings.ToLower(view), "space toggle")
	assert.Contains(t, view, "d direction")
}

func TestColumnsPopupAppliesLiveAndEscKeepsChanges(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.screen = screenMain
	m.width = 100
	m.height = 30
	m.statuses = []Status{{Item: inventory.Item{Path: "/app/value", Region: "eu-north-1"}, Exists: true, Value: "secret"}}
	m.openColumnsPopup()
	m.columnCursor = 0

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(model)
	assert.True(t, strings.Contains(m.View(), "VALUE"))
	assert.True(t, m.columns[columnValue])

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	assert.True(t, strings.Contains(m.View(), "VALUE"))
	assert.True(t, m.columns[columnValue])
}

func TestDefaultSortArrowIsShownForName(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.screen = screenMain
	m.width = 100
	m.height = 30
	m.statuses = []Status{{Item: inventory.Item{Path: "/b", Region: "eu-north-1"}, Exists: true}, {Item: inventory.Item{Path: "/a", Region: "eu-north-1"}, Exists: true}}
	m.applySortWithDirection(columnPath, false)

	view := m.View()

	assert.Contains(t, view, "NAME ↑")
	assert.Equal(t, "/a", m.statuses[0].Item.Path)
}

func TestNewParameterShowsOverwriteDefaultFalseFromExistingSelection(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true, Region: "eu-north-1"})
	m.width = 120
	m.height = 30
	m.screen = screenMain
	m.statuses = []Status{{Item: inventory.Item{Path: "/app/existing", Region: "eu-north-1"}, Exists: true, Value: "old", Type: ssm.ParameterTypeString.String()}}

	updated, _ := m.updateMain(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = updated.(model)

	assert.True(t, m.shouldShowOverwriteField())
	assert.False(t, m.editOverwrite)
	assert.Contains(t, m.renderTextAreaScreen(), "Overwrite:")
	assert.Contains(t, m.renderTextAreaScreen(), "false")
}

func TestExpandableFieldCompactsExpandsAndTogglesGutters(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.width = 120
	m.height = 30
	m.screen = screenTextArea
	m.editField = editFieldValue
	m.textArea.Focus()
	m.textArea.SetValue("short")

	assert.Contains(t, m.renderTextAreaScreen(), "Value:       short")
	assert.False(t, strings.Contains(m.renderTextAreaScreen(), "1 │ short"))

	updated, _ := m.updateTextArea(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	assert.Contains(t, m.renderTextAreaScreen(), "1 │ short")

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyCtrlL})
	m = updated.(model)
	view := m.renderTextAreaScreen()
	assert.Contains(t, view, "short")
	assert.False(t, strings.Contains(view, "1 │ short"))
}

func TestPoliciesPrettyPrintAndNormalizePolicyText(t *testing.T) {
	raw := `[{"PolicyStatus":"Pending","PolicyText":"{\"Type\":\"Expiration\",\"Version\":\"1.0\",\"Attributes\":{\"Timestamp\":\"2030-01-01T00:00:00Z\"}}","PolicyType":"Expiration"}]`

	pretty := prettyPoliciesForEditor(raw)
	assert.Contains(t, pretty, `"Type": "Expiration"`)
	assert.Contains(t, pretty, `"Timestamp": "2030-01-01T00:00:00Z"`)
	assert.False(t, strings.Contains(pretty, `"PolicyText"`))
	assert.False(t, strings.Contains(pretty, `\"Type\"`))

	normalized := normalizePoliciesForAWS(pretty)
	assert.Equal(t, `[{"Attributes":{"Timestamp":"2030-01-01T00:00:00Z"},"Type":"Expiration","Version":"1.0"}]`, normalized)
}

func TestEmacsCtrlKDeletesToRealLineEnd(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true, Keymap: "emacs"})
	m.screen = screenTextArea
	m.editField = editFieldValue
	m.textArea.Focus()
	m.textArea.SetValue("alpha beta\ngamma")
	m.setTextAreaCursorAbs(len("alpha "))

	updated, _ := m.updateTextArea(tea.KeyMsg{Type: tea.KeyCtrlK})
	m = updated.(model)

	assert.Equal(t, "alpha \ngamma", m.textArea.Value())
	assert.Equal(t, len("alpha "), m.textAreaCursorAbs())

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyCtrlK})
	m = updated.(model)
	assert.Equal(t, "alpha gamma", m.textArea.Value())
}

func TestViDDeletesToRealLineEnd(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true, Keymap: "vi"})
	m.screen = screenTextArea
	m.editField = editFieldValue
	m.viInsertMode = false
	m.textArea.Focus()
	m.textArea.SetValue("alpha beta\ngamma")
	m.setTextAreaCursorAbs(len("alpha "))

	updated, _ := m.updateTextArea(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'D'}})
	m = updated.(model)
	assert.Equal(t, "alpha \ngamma", m.textArea.Value())

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'D'}})
	m = updated.(model)
	assert.Equal(t, "alpha gamma", m.textArea.Value())
}

func TestHiddenGuttersRenderTextAtLeftEdge(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.width = 100
	m.height = 30
	m.screen = screenTextArea
	m.editField = editFieldValue
	m.textArea.Focus()
	m.textArea.SetValue("[\n  {\n    \"a\": 1\n  }\n]")
	m.expandedFields[editFieldValue] = true
	m.showGutters = false

	lines := renderLines(stripANSI(m.renderTextAreaScreen()))
	assert.True(t, strings.HasPrefix(lines[9], "["), "expected first multiline row to start at column 0, got %q", lines[9])
	assert.True(t, strings.HasPrefix(lines[10], "  {"), "expected JSON indentation only, got %q", lines[10])
}

func TestConfirmPopupInputPrefixIsNotLabelStyled(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{})
	m.activePopup = popupConfirm
	m.confirmPrompt = "Delete visible parameter(s)?"
	m.confirmExpected = "DELETE ALL"
	m.input.Focus()

	view := m.renderConfirmPopup()
	assert.Contains(t, view, "Type ")

	styledTypePrefix := labelStyle.Render("Type ")
	if styledTypePrefix != "Type " {
		assert.False(t, strings.Contains(view, styledTypePrefix))
	}
}

func stringSliceContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}

	return false
}

func TestMainDigitHotkeySortsByValue(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true, ShowColumns: []string{"value"}})
	m.screen = screenMain
	m.statuses = []Status{
		{Item: inventory.Item{Path: "/b", Region: "eu-central-1"}, Exists: true, Value: "zebra", Type: ssm.ParameterTypeString.String()},
		{Item: inventory.Item{Path: "/a", Region: "eu-central-1"}, Exists: true, Value: "alpha", Type: ssm.ParameterTypeString.String()},
	}
	m.selected = 0

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = updated.(model)

	assert.Equal(t, columnValue, m.sortBy)
	assert.Equal(t, "/a", m.statuses[0].Item.Path)
	assert.Equal(t, "/b", m.statuses[1].Item.Path)
}

func TestApplySortUsesNaturalNumericOrder(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.statuses = []Status{
		{Item: inventory.Item{Path: "/v18", Region: "eu-central-1"}, Exists: true, Version: 18},
		{Item: inventory.Item{Path: "/v2", Region: "eu-central-1"}, Exists: true, Version: 2},
		{Item: inventory.Item{Path: "/v1", Region: "eu-central-1"}, Exists: true, Version: 1},
	}

	m.applySort(columnVersion)

	assert.Equal(t, "/v1", m.statuses[0].Item.Path)
	assert.Equal(t, "/v2", m.statuses[1].Item.Path)
	assert.Equal(t, "/v18", m.statuses[2].Item.Path)
}

func TestSortPopupTogglesColumnWithLetter(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true, ShowColumns: []string{"type"}})
	m.screen = screenMain
	m.activePopup = popupSort
	m.statuses = []Status{
		{Item: inventory.Item{Path: "/b", Region: "eu-central-1"}, Exists: true, Type: ssm.ParameterTypeSecureString.String()},
		{Item: inventory.Item{Path: "/a", Region: "eu-central-1"}, Exists: true, Type: ssm.ParameterTypeString.String()},
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	m = updated.(model)

	assert.Equal(t, columnPath, m.sortBy)
	assert.Equal(t, popupSort, m.activePopup)
	require.Len(t, m.sortRules, 2)
	assert.Equal(t, columnPath, m.sortRules[0].column)
	assert.Equal(t, columnType, m.sortRules[1].column)
	assert.Equal(t, "/a", m.statuses[0].Item.Path)
	assert.Equal(t, "/b", m.statuses[1].Item.Path)
}

func TestSortPopupNavigationMovesFocusAndSpaceApplies(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true, ShowColumns: []string{"value"}})
	m.screen = screenMain
	m.activePopup = popupSort
	m.setSortRules([]sortRule{{column: columnPath}})
	m.sortCursor = 0
	m.statuses = []Status{
		{Item: inventory.Item{Path: "/b", Region: "eu-north-1"}, Exists: true, Value: "2"},
		{Item: inventory.Item{Path: "/a", Region: "eu-north-1"}, Exists: true, Value: "1"},
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)

	assert.Equal(t, columnPath, m.sortBy)
	assert.Equal(t, 1, m.sortCursor)
	assert.Equal(t, popupSort, m.activePopup)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(model)

	assert.Equal(t, columnPath, m.sortBy)
	require.Len(t, m.sortRules, 2)
	assert.Equal(t, columnValue, m.sortRules[1].column)
	assert.Equal(t, "/a", m.statuses[0].Item.Path)
	assert.Equal(t, popupSort, m.activePopup)
}

func TestSortPopupRendersCheckboxSelectionWithoutInlineHotkeys(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true, ShowColumns: []string{"value", "user"}})
	m.screen = screenMain
	m.activePopup = popupSort
	m.setSortRules([]sortRule{{column: columnValue}})
	m.sortCursor = 1
	m.width = 100
	m.height = 30

	view := m.View()

	assert.Contains(t, view, "Sort By")
	assert.Contains(t, view, "> [x] Value")
	assert.Contains(t, view, "[ ] Name")
	assert.Contains(t, view, "[ ] User")
	assert.False(t, strings.Contains(view, "[ ] Type"))
	assert.False(t, strings.Contains(view, "v  Value"))
	assert.False(t, strings.Contains(view, "enter sort/toggle"))
	assert.Contains(t, strings.ToLower(view), "space toggle")
	assert.Contains(t, view, "d direction")
	assert.Contains(t, view, "n name")
	assert.Contains(t, view, "esc close")
}

func TestMultiSortUsesSelectedColumnOrderAndDirections(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.statuses = []Status{
		{Item: inventory.Item{Path: "/a", Region: "eu-north-1"}, Exists: true, Type: ssm.ParameterTypeString.String()},
		{Item: inventory.Item{Path: "/c", Region: "eu-north-1"}, Exists: true, Type: ssm.ParameterTypeSecureString.String()},
		{Item: inventory.Item{Path: "/b", Region: "eu-north-1"}, Exists: true, Type: ssm.ParameterTypeString.String()},
	}

	m.applySortWithRules([]sortRule{{column: columnType}, {column: columnPath, descending: true}})

	assert.Equal(t, []string{"/c", "/b", "/a"}, m.visiblePaths())
	assert.Equal(t, columnType, m.sortBy)
	assert.False(t, m.sortDescending)
}

func TestSortPopupDirectionAppliesToFocusedColumn(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true, ShowColumns: []string{"type", "value"}})
	m.screen = screenMain
	m.activePopup = popupSort
	m.statuses = []Status{
		{Item: inventory.Item{Path: "/a", Region: "eu-north-1"}, Exists: true, Type: ssm.ParameterTypeString.String(), Value: "2"},
		{Item: inventory.Item{Path: "/b", Region: "eu-north-1"}, Exists: true, Type: ssm.ParameterTypeSecureString.String(), Value: "1"},
	}
	m.sortCursor = 2 // Type when Name and Value are also visible.

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = updated.(model)

	require.Len(t, m.sortRules, 2)
	assert.Equal(t, columnPath, m.sortRules[0].column)
	assert.Equal(t, columnType, m.sortRules[1].column)
	assert.True(t, m.sortRules[1].descending)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(model)

	require.Len(t, m.sortRules, 3)
	assert.Equal(t, columnValue, m.sortRules[2].column)
	assert.False(t, m.sortRules[2].descending)
}

func TestBulkDeleteConfirmRendersInlinePhraseInputAndButtons(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.screen = screenMain
	m.width = 100
	m.height = 30
	m.startConfirm("Delete 2 visible parameter(s)?", "DELETE ALL", []inventory.Item{{Path: "/a"}, {Path: "/b"}}, screenMain)
	m.input.SetValue("DELETE ALL")

	view := m.View()

	assert.Contains(t, view, "Type DELETE ALL to confirm: DELETE ALL")
	assert.Contains(t, view, "enter confirm")
	assert.Contains(t, view, "esc cancel")
	assert.False(t, strings.Contains(view, "Type DELETE ALL to confirm:\n"))
}

func TestPopupActionLineStylesKeysSeparatelyFromDescriptions(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	m := newModel(context.Background(), nil, nil, &Options{})

	styled := m.popupActionLine("Enter apply   Esc cancel")

	assert.Equal(t, "Enter apply   Esc cancel", stripANSI(styled))
	assert.Contains(t, styled, "\x1b[")
}

func TestCompactExpandableCursorDoesNotInsertExtraCell(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{})
	m.width = 80
	m.height = 20
	m.screen = screenTextArea
	m.editField = editFieldValue
	m.textArea.Focus()
	m.textArea.SetValue("abcdef")
	m.setTextAreaCursorAbs(3)

	plain := stripANSI(m.singleLineAreaView(editFieldValue, &m.textArea, 11))

	assert.Equal(t, "abcdef", plain)
}

func TestExpandableFieldCollapsesAfterEditedBackToOneLine(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.width = 120
	m.height = 30
	m.screen = screenTextArea
	m.editField = editFieldValue
	m.textArea.Focus()
	m.textArea.SetValue("shortx")
	m.setTextAreaCursorAbs(len("shortx"))
	m.expandedFields[editFieldValue] = true

	assert.Contains(t, m.renderTextAreaScreen(), "1 │ shortx")

	updated, _ := m.updateTextArea(tea.KeyMsg{Type: tea.KeyBackspace})
	m = updated.(model)
	view := m.renderTextAreaScreen()

	assert.Contains(t, view, "Value:       short")
	assert.False(t, strings.Contains(view, "1 │ short"))
}

func TestPopupTransitionsReplaceParentPopup(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.screen = screenTextArea
	m.activePopup = popupValueActions
	m.editField = editFieldValue

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = updated.(model)
	assert.Equal(t, popupFileAction, m.activePopup)
	assert.Empty(t, m.popupStack)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	assert.Equal(t, popupNone, m.activePopup)
}

func TestPoliciesActionsPopupClearsPolicies(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.screen = screenTextArea
	m.editField = editFieldPolicies
	m.editTier = ssm.ParameterTierAdvanced
	m.editPoliciesArea.SetValue("policy")
	m.editPoliciesArea.Focus()

	updated, _ := m.updateTextArea(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}, Alt: true})
	m = updated.(model)
	assert.Equal(t, popupPoliciesActions, m.activePopup)
	assert.Contains(t, m.textAreaFooterText(), "alt+e policies actions")

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = updated.(model)
	assert.Equal(t, popupNone, m.activePopup)
	assert.Empty(t, m.editPoliciesArea.Value())
}

func TestPoliciesActionsLoadPrettyPoliciesFromFile(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.screen = screenTextArea
	m.editField = editFieldPolicies
	m.editTier = ssm.ParameterTierAdvanced
	m.activePopup = popupPoliciesActions
	raw := `[{"PolicyText":"{\"Type\":\"Expiration\",\"Version\":\"1.0\"}","PolicyType":"Expiration"}]`
	path := t.TempDir() + "/policies.json"
	require.NoError(t, os.WriteFile(path, []byte(raw), 0o600))

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = updated.(model)
	assert.Equal(t, popupFileAction, m.activePopup)
	assert.Equal(t, editFieldPolicies, m.fileActionField)
	m.input.SetValue(path)
	m.input.Focus()

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)

	assert.Equal(t, popupNone, m.activePopup)
	assert.False(t, strings.Contains(m.editPoliciesArea.Value(), `"PolicyText"`))
	assert.Contains(t, m.editPoliciesArea.Value(), `"Type": "Expiration"`)
}

func TestUnsavedEditorExitRequiresConfirmation(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.width = 120
	m.height = 40
	m.screen = screenMain
	m.returnScreen = screenMain
	m.statuses = []Status{{Item: inventory.Item{Path: "/app/value", Region: "eu-north-1"}, Type: ssm.ParameterTypeString.String(), Value: "old", Exists: true}}
	updated, _ := m.startMultiline()
	m = updated.(model)
	m.textArea.SetValue("changed")

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	assert.Equal(t, screenTextArea, m.screen)
	assert.Equal(t, popupUnsavedChanges, m.activePopup)
	assert.Contains(t, m.renderUnsavedChangesPopup(), "Unsaved changes. Discard unsaved changes?")
	assert.False(t, strings.Contains(m.renderUnsavedChangesPopup(), "Unsaved changes.\nDiscard"))

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	assert.Equal(t, screenTextArea, m.screen)
	assert.Equal(t, popupNone, m.activePopup)

	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	assert.Equal(t, screenMain, m.screen)
}

func TestTextAreaPageDownKeepsCursorVisible(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true, Keymap: "emacs"})
	m.width = 100
	m.height = 20
	m.screen = screenTextArea
	m.editField = editFieldValue
	m.textArea.Focus()

	lines := make([]string, 0, 40)
	for i := 0; i < 40; i++ {
		lines = append(lines, fmt.Sprintf("line-%02d", i))
	}

	m.textArea.SetValue(strings.Join(lines, "\n"))
	m.expandedFields[editFieldValue] = true

	updated, _ := m.updateTextArea(tea.KeyMsg{Type: tea.KeyPgDown})
	m = updated.(model)
	view := m.renderTextAreaScreen()
	assert.Contains(t, view, "█")
	assert.True(t, m.textAreaCursorAbs() > 0)

	m = newModel(context.Background(), nil, nil, &Options{NoColor: true, Keymap: "vi"})
	m.width = 100
	m.height = 20
	m.screen = screenTextArea
	m.editField = editFieldValue
	m.viInsertMode = false
	m.textArea.Focus()
	m.textArea.SetValue(strings.Join(lines, "\n"))
	m.expandedFields[editFieldValue] = true
	updated, _ = m.updateTextArea(tea.KeyMsg{Type: tea.KeyCtrlF})
	m = updated.(model)
	assert.Contains(t, m.renderTextAreaScreen(), "█")
	assert.True(t, m.textAreaCursorAbs() > 0)
}

func TestFileActionPopupKeepsInputFocusedAfterWriteError(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	path := filepath.Join(t.TempDir(), "missing", "value.txt")
	m.screen = screenTextArea
	m.activePopup = popupFileAction
	m.fileActionMode = "write"
	m.fileActionField = editFieldValue
	m.editType = ssm.ParameterTypeString
	m.textArea.SetValue("new")
	m.input.SetValue(path)
	m.input.Focus()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	assert.Equal(t, popupFileAction, m.activePopup)
	assert.NotEmpty(t, m.errMessage)
	assert.True(t, m.input.Focused())

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = updated.(model)
	assert.Equal(t, path+"2", m.input.Value())
}

func TestFileActionPopupKeepsInputFocusedAfterLoadError(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	path := filepath.Join(t.TempDir(), "missing.txt")
	m.screen = screenTextArea
	m.activePopup = popupFileAction
	m.fileActionMode = "load"
	m.fileActionField = editFieldValue
	m.input.SetValue(path)
	m.input.Focus()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	assert.Equal(t, popupFileAction, m.activePopup)
	assert.NotEmpty(t, m.errMessage)
	assert.True(t, m.input.Focused())

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = updated.(model)
	assert.Equal(t, path+"2", m.input.Value())
}

func TestFileWriteConfirmEscReturnsToFilePathPopup(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	path := t.TempDir() + "/value.txt"
	require.NoError(t, os.WriteFile(path, []byte("old"), 0o600))

	m.screen = screenTextArea
	m.width = 120
	m.height = 40
	m.activePopup = popupFileAction
	m.fileActionMode = "write"
	m.fileActionField = editFieldValue
	m.editType = ssm.ParameterTypeString
	m.textArea.SetValue("new")
	m.input.SetValue(path)
	m.input.Focus()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	assert.Equal(t, popupFileWriteConfirm, m.activePopup)
	assert.Contains(t, m.renderFileWriteConfirmPopup(), "File already exists. Overwrite it?")
	assert.False(t, strings.Contains(m.renderFileWriteConfirmPopup(), "File already exists.\nOverwrite it?"))
	view := m.View()
	assert.Contains(t, view, "File already exists. Overwrite it?")

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	assert.Equal(t, popupFileAction, m.activePopup)
	assert.Equal(t, fileWriteConfirmationNone, m.pendingFileWrite)
	assert.Equal(t, path, m.input.Value())
	assert.True(t, m.input.Focused())

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = updated.(model)
	assert.Equal(t, path+"2", m.input.Value())
	assert.Equal(t, "old", readFileString(t, path))
}

func TestFileWriteConfirmShowsSecondConfirmAfterSecureConfirm(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	path := t.TempDir() + "/secret.txt"
	require.NoError(t, os.WriteFile(path, []byte("old"), 0o600))

	m.screen = screenTextArea
	m.width = 120
	m.height = 40
	m.activePopup = popupFileAction
	m.fileActionMode = "write"
	m.fileActionField = editFieldValue
	m.editType = ssm.ParameterTypeSecureString
	m.textArea.SetValue("secret")
	m.input.SetValue(path)
	m.input.Focus()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	assert.Equal(t, fileWriteConfirmationSecure, m.pendingFileWrite)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	assert.Equal(t, popupFileWriteConfirm, m.activePopup)
	assert.Equal(t, []popupKind{popupFileAction}, m.popupStack)
	assert.Equal(t, fileWriteConfirmationOverwrite, m.pendingFileWrite)
	assert.Contains(t, m.renderFileWriteConfirmPopup(), "File already exists. Overwrite it?")
	view := m.View()
	assert.Contains(t, view, "File already exists. Overwrite it?")

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	assert.Equal(t, popupNone, m.activePopup)
	assert.Equal(t, "secret", readFileString(t, path))
}

func TestEnterOnCompactExpandableFieldCreatesNewLine(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.width = 120
	m.height = 30
	m.screen = screenTextArea
	m.editField = editFieldValue
	m.textArea.Focus()
	m.textArea.SetValue("short")
	m.setTextAreaCursorAbs(len("short"))

	updated, _ := m.updateTextArea(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)

	assert.Equal(t, "short\n", m.textArea.Value())
	line, offset := textAreaCursorLineOffset(&m.textArea)
	assert.Equal(t, 1, line)
	assert.Equal(t, 0, offset)

	view := m.renderTextAreaScreen()
	assert.Contains(t, view, "1 │ short")
	assert.Contains(t, view, "2 │ █")
}

func TestFieldsOptionLimitsColumnsDetailsAndEditor(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true, Fields: []string{"name", "type"}, ShowColumns: []string{"type", "value", "region"}})
	m.width = 120
	m.height = 40
	m.screen = screenMain
	m.statuses = []Status{{Item: inventory.Item{Path: "/app/value", Region: "eu-north-1"}, Exists: true, Type: ssm.ParameterTypeString.String(), Value: "value", Tier: "Advanced", Description: "desc", Modified: "date"}}
	m.selectedExpanded = true

	list := m.renderListBlock()
	assert.Contains(t, list, "NAME")
	assert.Contains(t, list, "TYPE")
	assert.False(t, strings.Contains(list, "VALUE"))
	assert.False(t, strings.Contains(list, "REGION"))

	details := m.renderSelectedParameterBlock(true)
	assert.Contains(t, details, "Name:")
	assert.Contains(t, details, "Type:")
	assert.False(t, strings.Contains(details, "Value:"))
	assert.False(t, strings.Contains(details, "Region:"))
	assert.False(t, strings.Contains(details, "Description:"))

	columns := m.renderColumnsPopup()
	assert.Contains(t, columns, "Type")
	assert.False(t, strings.Contains(columns, "Value"))
	assert.False(t, strings.Contains(columns, "Region"))

	m.screen = screenTextArea
	m.editField = editFieldSSMPath
	m.editPathInput.SetValue("/app/value")
	m.editType = ssm.ParameterTypeString
	editor := m.renderTextAreaScreen()
	assert.Contains(t, editor, "Name:")
	assert.Contains(t, editor, "Type:")
	assert.False(t, strings.Contains(editor, "Value:"))
	assert.False(t, strings.Contains(editor, "Region:"))
	assert.Equal(t, []editField{editFieldSSMPath, editFieldType}, m.editFieldOrder())
}

func TestOverlayPopupLinePreservesTextOutsidePopupBounds(t *testing.T) {
	row := overlayPopupLine("0123456789abcdefghij", "POP", 8, 3, 20)

	assert.Equal(t, "01234567POPbcdefghij", row)
}

func TestOverlayPopupLinePreservesANSIOutsidePopupBounds(t *testing.T) {
	row := overlayPopupLine("\x1b[31mABCDE\x1b[0m", "X", 2, 1, 5)

	assert.Equal(t, "ABXDE", stripANSI(row))
	assert.Contains(t, row, "\x1b[31mAB")
	assert.Contains(t, row, "\x1b[31mDE")
}

func TestOverlayPopupOnBodyOnlyReplacesPopupRectangle(t *testing.T) {
	m := newModel(context.Background(), nil, nil, &Options{NoColor: true})
	m.width = 20
	m.height = 5
	body := strings.Join([]string{
		"00000000000000000000",
		"11111111111111111111",
		"22222222222222222222",
		"33333333333333333333",
		"44444444444444444444",
	}, "\n")
	popup := strings.Join([]string{"ABC", "DEF", "GHI"}, "\n")

	view := m.overlayPopupOnBody(body, popup)
	lines := strings.Split(view, "\n")

	require.Len(t, lines, 5)
	assert.Equal(t, "00000000000000000000", lines[0])
	assert.Equal(t, "11111111ABC111111111", lines[1])
	assert.Equal(t, "22222222DEF222222222", lines[2])
	assert.Equal(t, "33333333GHI333333333", lines[3])
	assert.Equal(t, "44444444444444444444", lines[4])
}
