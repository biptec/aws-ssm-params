package ui

import (
	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
)

type editorState struct {
	input                textinput.Model
	textArea             textarea.Model
	editPoliciesArea     textarea.Model
	editDescriptionArea  textarea.Model
	editPathInput        textinput.Model
	editDescriptionInput textinput.Model
	editFileInput        textinput.Model

	editField           editField
	editorButtonsFocused bool
	editorButtonCursor   int
	viInsertMode        bool
	editRegionOptions   []string
	pendingFileWrite    fileWriteConfirmation
	editRegion          string
	editType            ssm.ParameterType
	editTier            ssm.ParameterTier
	editDataType        ssm.ParameterDataType
	editOverwrite       bool
	editNewParameter    bool
	editInitialSnapshot editSnapshot
	typeReturnScreen    screen

	expandedFields  map[editField]bool
	showGutters     bool
	fileActionMode  string
	fileActionField editField
}

type editField int

const (
	editFieldValue editField = iota
	editFieldSSMPath
	editFieldRegion
	editFieldType
	editFieldTier
	editFieldDataType
	editFieldOverwrite
	editFieldDescription
	editFieldPolicies
	editFieldFilePath
)

type fileWriteConfirmation int

const (
	fileWriteConfirmationNone fileWriteConfirmation = iota
	fileWriteConfirmationSecure
	fileWriteConfirmationOverwrite
)

type actionItem struct{ label, value string }

type parameterTypeItem struct {
	hotkey      string
	label       string
	value       ssm.ParameterType
	description string
}

type parameterTierItem struct {
	hotkey      string
	label       string
	value       ssm.ParameterTier
	description string
}

type parameterDataTypeItem struct {
	hotkey      string
	label       string
	value       ssm.ParameterDataType
	description string
}

type overwriteItem struct {
	hotkey      string
	label       string
	value       bool
	description string
}

type editSnapshot struct {
	name          string
	region        string
	parameterType string
	tier          string
	dataType      string
	overwrite     bool
	newParameter  bool
	description   string
	policies      string
	value         string
}

const parameterNameValidationMessage = "Parameter name can consist of the following symbols and letters only: a-zA-Z0-9_.-/"

func parameterNameIsValid(name string) bool {
	if name == "" {
		return true
	}

	for _, r := range name {
		if !parameterNameRuneAllowed(r) {
			return false
		}
	}

	return true
}

func parameterNameRuneAllowed(r rune) bool {
	return r >= 'a' && r <= 'z' ||
		r >= 'A' && r <= 'Z' ||
		r >= '0' && r <= '9' ||
		r == '_' ||
		r == '.' ||
		r == '-' ||
		r == '/'
}

func (snapshot *editSnapshot) isZero() bool {
	return *snapshot == (editSnapshot{})
}

func (snapshot *editSnapshot) equal(other *editSnapshot) bool {
	return snapshot.name == other.name &&
		snapshot.region == other.region &&
		snapshot.parameterType == other.parameterType &&
		snapshot.tier == other.tier &&
		snapshot.dataType == other.dataType &&
		snapshot.overwrite == other.overwrite &&
		snapshot.newParameter == other.newParameter &&
		snapshot.description == other.description &&
		snapshot.policies == other.policies &&
		snapshot.value == other.value
}
