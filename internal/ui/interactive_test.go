package ui

import (
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
	m := model{screen: screenTextArea, editType: ssm.ParameterTypeSecureString}
	updated, _ := m.startTypeSelect(screenTextArea)
	m = updated.(model)

	updated, _ = m.updateTypeSelect(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(model)
	updated, _ = m.updateTypeSelect(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)

	assert.Equal(t, screenTextArea, m.screen)
	assert.Equal(t, ssm.ParameterTypeString, m.editType)
}

func TestSaveValueCmdWritesSelectedParameterType(t *testing.T) {
	client := fakeSSMClient{region: "eu-north-1", params: map[string]ssm.Parameter{}, metas: map[string]ssm.Metadata{}}
	item := inventory.Item{Path: "/app/hosts", Region: "eu-north-1"}

	msg := saveValueCmd(client, item, "api.example.com,www.example.com", ssm.ParameterTypeStringList)()

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
