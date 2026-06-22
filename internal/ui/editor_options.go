package ui

import (
	"encoding/json"
	"strings"

	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/ssm"
)

type editorOptions struct {
	opts Options
	editorState
	current Status
}

func newEditorOptions(m model) editorOptions {
	return editorOptions{opts: m.opts, editorState: m.editorState, current: m.currentStatus()}
}

func (m editorOptions) currentStatus() Status {
	return m.current
}

func (m editorOptions) currentItem() inventory.Item {
	return m.current.Item
}

func (m editorOptions) fieldAllowed(field string) bool {
	if field == "name" || len(m.opts.Fields) == 0 {
		return true
	}
	for _, candidate := range m.opts.Fields {
		if candidate == field {
			return true
		}
	}
	return false
}

func (m editorOptions) editFieldAllowed(field editField) bool {
	switch field {
	case editFieldFilePath:
		return true
	case editFieldSSMPath:
		return true
	case editFieldRegion:
		return m.fieldAllowed("region")
	case editFieldType:
		return m.fieldAllowed("type")
	case editFieldTier:
		return m.fieldAllowed("tier")
	case editFieldDataType:
		return m.fieldAllowed("data-type")
	case editFieldDescription:
		return m.fieldAllowed("description")
	case editFieldPolicies:
		return m.fieldAllowed("policies")
	case editFieldValue:
		return m.fieldAllowed("value")
	case editFieldOverwrite:
		return m.fieldAllowed("value")
	default:
		return true
	}
}

// randomItems returns supported random value generator choices.
func randomItems() []actionItem {
	return []actionItem{{"base64 32 bytes", "base64-32"}, {"hex 32 bytes", "hex-32"}, {"uuid", "uuid"}, {"custom length base64", "base64-custom"}}
}

// itemPaths extracts SSM names for loading/progress displays.
func itemPaths(items []inventory.Item) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Path)
	}
	return out
}

// parameterTypeItems returns the AWS SSM parameter types exposed in the TUI.
func parameterTypeItems() []parameterTypeItem {
	return []parameterTypeItem{
		{hotkey: "e", label: ssm.ParameterTypeSecureString.String(), value: ssm.ParameterTypeSecureString, description: "encrypted value; best default for secrets"},
		{hotkey: "s", label: ssm.ParameterTypeString.String(), value: ssm.ParameterTypeString, description: "plain text scalar value"},
		{hotkey: "l", label: ssm.ParameterTypeStringList.String(), value: ssm.ParameterTypeStringList, description: "comma-separated plain text list"},
	}
}

// parameterTierItems returns the AWS SSM parameter tiers exposed in the TUI.
func parameterTierItems() []parameterTierItem {
	return []parameterTierItem{
		{hotkey: "i", label: ssm.ParameterTierIntelligentTiering.String(), value: ssm.ParameterTierIntelligentTiering, description: "AWS chooses Standard or Advanced as needed"},
		{hotkey: "s", label: ssm.ParameterTierStandard.String(), value: ssm.ParameterTierStandard, description: "default tier for most parameters"},
		{hotkey: "a", label: ssm.ParameterTierAdvanced.String(), value: ssm.ParameterTierAdvanced, description: "larger values and higher parameter limits"},
	}
}

// parameterDataTypeItems returns AWS SSM parameter data types exposed in the TUI.
func parameterDataTypeItems() []parameterDataTypeItem {
	return []parameterDataTypeItem{
		{hotkey: "t", label: ssm.ParameterDataTypeText.String(), value: ssm.ParameterDataTypeText, description: "ordinary text; AWS default"},
		{hotkey: "a", label: ssm.ParameterDataTypeEC2Image.String(), value: ssm.ParameterDataTypeEC2Image, description: "validate that the value is an AMI id"},
		{hotkey: "i", label: ssm.ParameterDataTypeSSMIntegration.String(), value: ssm.ParameterDataTypeSSMIntegration, description: "for AWS SSM service integrations"},
	}
}

// overwriteItems returns the choices for AWS SSM --overwrite.
func overwriteItems() []overwriteItem {
	return []overwriteItem{
		{hotkey: "t", label: "true", value: true, description: "update the parameter if it already exists"},
		{hotkey: "f", label: "false", value: false, description: "let AWS return an error if it already exists"},
	}
}

// initialEditType chooses the type shown when opening an editor.
// Existing parameters preserve their AWS type, while missing/new parameters default to SecureString.
func (m editorOptions) initialEditType() ssm.ParameterType {
	current := m.currentStatus().Type
	if parameterType, err := ssm.ParseParameterType(current); err == nil {
		return parameterType
	}
	return ssm.DefaultParameterType
}

// normalizedEditType returns a valid parameter type even if edit state has not been initialized yet.
func (m editorOptions) normalizedEditType() ssm.ParameterType {
	if m.editType.IsValid() {
		return m.editType
	}
	return ssm.DefaultParameterType
}

func indexOfParameterType(items []parameterTypeItem, value ssm.ParameterType) int {
	for i, item := range items {
		if item.value == value {
			return i
		}
	}
	return 0
}

func parameterTypeIndexByHotkey(items []parameterTypeItem, key string) (int, bool) {
	for i, item := range items {
		if item.hotkey == key {
			return i, true
		}
	}
	return 0, false
}

func (m editorOptions) initialEditTier() ssm.ParameterTier {
	current := m.currentStatus().Tier
	if tier, err := ssm.ParseParameterTier(current); err == nil {
		return tier
	}
	return ssm.DefaultParameterTier
}

func (m editorOptions) normalizedEditTier() ssm.ParameterTier {
	if m.editTier.IsValid() {
		return m.editTier
	}
	return ssm.DefaultParameterTier
}

func (m editorOptions) shouldShowPoliciesField() bool {
	return m.editFieldAllowed(editFieldPolicies) && m.normalizedEditTier() == ssm.ParameterTierAdvanced
}

func (m editorOptions) shouldShowOverwriteField() bool {
	return m.editFieldAllowed(editFieldOverwrite) && (m.editNewParameter || !m.currentStatus().Exists)
}

func indexOfParameterTier(items []parameterTierItem, value ssm.ParameterTier) int {
	for i, item := range items {
		if item.value == value {
			return i
		}
	}
	return 0
}

func parameterTierIndexByHotkey(items []parameterTierItem, key string) (int, bool) {
	for i, item := range items {
		if item.hotkey == key {
			return i, true
		}
	}
	return 0, false
}

func (m editorOptions) initialEditDataType() ssm.ParameterDataType {
	current := m.currentStatus().DataType
	if dataType, err := ssm.ParseParameterDataType(current); err == nil {
		return dataType
	}
	return ssm.DefaultParameterDataType
}

func (m editorOptions) normalizedEditDataType() ssm.ParameterDataType {
	if m.editDataType.IsValid() {
		return m.editDataType
	}
	return ssm.DefaultParameterDataType
}

func indexOfParameterDataType(items []parameterDataTypeItem, value ssm.ParameterDataType) int {
	for i, item := range items {
		if item.value == value {
			return i
		}
	}
	return 0
}

func parameterDataTypeIndexByHotkey(items []parameterDataTypeItem, key string) (int, bool) {
	for i, item := range items {
		if item.hotkey == key {
			return i, true
		}
	}
	return 0, false
}

func indexOfOverwrite(items []overwriteItem, value bool) int {
	for i, item := range items {
		if item.value == value {
			return i
		}
	}
	return 0
}

func overwriteIndexByHotkey(items []overwriteItem, key string) (int, bool) {
	for i, item := range items {
		if item.hotkey == key {
			return i, true
		}
	}
	return 0, false
}

// initialEditRegion chooses the default concrete region when editing a parameter.
// For wildcard rows it prefers the first configured region so saving never targets "*" accidentally.
func (m editorOptions) initialEditRegion() string {
	item := m.currentItem()
	if item.Region != "" && item.Region != "*" {
		return item.Region
	}
	regions := m.regionOptions()
	if len(regions) > 0 {
		return regions[0]
	}
	if m.opts.Region != "all regions" {
		return m.opts.Region
	}
	return ""
}

// regionOptions returns the concrete regions available for saving the current value.
func (m editorOptions) regionOptions() []string {
	if len(m.opts.Regions) > 0 {
		return append([]string(nil), m.opts.Regions...)
	}
	if m.opts.Region != "" && m.opts.Region != "all regions" && m.opts.Region != "-" {
		return []string{m.opts.Region}
	}
	return nil
}

func prettyPoliciesForEditor(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return raw
	}
	decoded = canonicalPoliciesForEditor(decoded)
	out, err := json.MarshalIndent(decoded, "", "  ")
	if err != nil {
		return raw
	}
	return string(out)
}

func normalizePoliciesForAWS(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return raw
	}
	decoded = canonicalPoliciesForEditor(decoded)
	out, err := json.Marshal(decoded)
	if err != nil {
		return raw
	}
	return string(out)
}

func canonicalPoliciesForEditor(value any) any {
	switch v := value.(type) {
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, canonicalPolicyItem(item))
		}
		return out
	default:
		return canonicalPolicyItem(v)
	}
}

func canonicalPolicyItem(value any) any {
	v, ok := value.(map[string]any)
	if !ok {
		return value
	}
	policyText, ok := v["PolicyText"]
	if !ok {
		return value
	}
	switch text := policyText.(type) {
	case string:
		var decoded any
		if err := json.Unmarshal([]byte(text), &decoded); err == nil {
			return canonicalPoliciesForEditor(decoded)
		}
	case map[string]any, []any:
		return canonicalPoliciesForEditor(text)
	}
	return value
}
