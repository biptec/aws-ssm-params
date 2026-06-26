package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/charmbracelet/lipgloss"
)

type tableViewComponent struct {
	model model
}

const (
	cleanSelectedParameterDetailsHeight = 13
	dirtySelectedParameterDetailsHeight = 18
)

type editableDetailField struct {
	label string
	cloud string
	local string
}

// renderSelectedParameterBlock renders the compact or expanded selected-parameter summary shown above the main table.
// Missing parameters only have an expected name, so every field except Name is displayed as a dash.
func (component tableViewComponent) renderSelectedParameterBlock(full bool) string {
	m := component.model

	st := m.currentStatus()
	if st.Item.Path == "" {
		return m.renderBox("Selected Parameter", []string{"No parameters found."}, 8)
	}

	if full && m.hasLocalChanges() {
		title := component.selectedParameterDetailsTitle(&st)
		lines := component.editableSelectedParameterLines(&st)

		return m.renderBox(title, lines, dirtySelectedParameterDetailsHeight)
	}

	fields := m.selectedParameterFields(&st, full)

	labelWidth := 6
	if full {
		labelWidth = 11
	}

	lines := m.renderFieldPairs(fields, labelWidth)

	height := len(lines) + 2
	if full {
		height = cleanSelectedParameterDetailsHeight
	}

	return m.renderBox("Selected Parameter", lines, height)
}

func (component tableViewComponent) selectedParameterFields(st *Status, full bool) [][2]string {
	m := component.model

	if st.isMissing() {
		if full {
			return m.filterSelectedParameterFields([][2]string{{"Name", st.Item.Path}, {"Region", "-"}, {"Type", "-"}, {"Tier", "-"}, {"DataType", "-"}, {"Policies", "-"}, {"Version", "-"}, {"Len", "-"}, {"SHA256", "-"}, {"Description", "-"}, {"User", "-"}, {"Date", "-"}, {"Value", "-"}})
		}

		return m.filterSelectedParameterFields([][2]string{{"Name", st.Item.Path}, {"Region", "-"}, {"Type", "-"}, {"Date", "-"}, {"Value", "-"}})
	}

	value := m.displayValue(st, full)

	fields := [][2]string{{"Name", st.Item.Path}, {"Region", st.RegionLabel(m.opts.Region)}, {"Type", valueOrDash(st.Type)}, {"Date", valueOrDash(st.Modified)}, {"Value", value}}
	if full {
		detailWidth := max(20, m.boxInnerWidth()-18)
		fields = [][2]string{{"Name", st.Item.Path}, {"Region", st.RegionLabel(m.opts.Region)}, {"Type", valueOrDash(st.Type)}, {"Tier", valueOrDash(st.Tier)}, {"DataType", valueOrDash(st.DataType)}, {"Policies", oneLineValuePreview(st.Policies, detailWidth)}, {"Version", intOrDash(st.Version)}, {"Len", intOrDash(int64(st.Length))}, {"SHA256", valueOrDash(st.SHA256Prefix)}, {"Description", oneLineValuePreview(st.Description, detailWidth)}, {"User", valueOrDash(st.User)}, {"Date", valueOrDash(st.Modified)}, {"Value", value}}
		if st.Error != "" {
			fields = append(fields, [2]string{"Error", st.Error})
		}
		if st.PushError != "" {
			fields = append(fields, [2]string{"Push error", st.PushError})
		}
	}

	return m.filterSelectedParameterFields(fields)
}

func (component tableViewComponent) selectedParameterDetailsTitle(st *Status) string {
	switch st.PendingOperation() {
	case parameterStateNew:
		return "New Parameter"
	case parameterStateModified:
		return "Modified Parameter"
	case parameterStateDeleted:
		return "Deleted Parameter"
	default:
		return "Selected Parameter"
	}
}

func (component tableViewComponent) editableSelectedParameterLines(st *Status) []string {
	m := component.model
	fields := component.editableDetailFields(st)
	lines := make([]string, 0, len(fields)*2)

	valueWidth := max(4, m.boxInnerWidth()-15)
	for _, field := range fields {
		localOnly := st.PendingOperation() == parameterStateNew
		changed := field.cloud != field.local && st.HasLocalChanges()
		cloudValue := component.renderEditableDetailValue(field.cloud, valueWidth, changed || localOnly, !localOnly)
		lines = append(lines, "  "+m.fieldLine(field.label, cloudValue, 11))

		if changed {
			localValue := component.renderEditableLocalDetailValue(field, valueWidth)
			lines = append(lines, "  "+strings.Repeat(" ", 13)+localValue)
			continue
		}

		lines = append(lines, "")
	}

	return lines
}

func (component tableViewComponent) editableDetailFields(st *Status) []editableDetailField {
	m := component.model
	cloud := st.cloudStatus()
	local := st.localStatus()
	if st.PendingOperation() == parameterStateDeleted {
		local = Status{}
	}

	if st.PendingOperation() == parameterStateNew || !st.HasLocalChanges() {
		cloud = *st
		local = *st
	}

	fields := []editableDetailField{
		{label: "Name", cloud: cloud.Item.Path, local: local.Item.Path},
		{label: "Region", cloud: cloud.Item.Region, local: local.Item.Region},
		{label: "Type", cloud: cloud.Type, local: local.Type},
		{label: "Tier", cloud: cloud.Tier, local: local.Tier},
		{label: "DataType", cloud: cloud.DataType, local: local.DataType},
		{label: "Policies", cloud: cloud.Policies, local: local.Policies},
		{label: "Description", cloud: cloud.Description, local: local.Description},
		{label: "Value", cloud: component.editableDetailValue(&cloud), local: component.editableDetailValue(&local)},
	}

	filtered := make([]editableDetailField, 0, len(fields))
	for _, field := range fields {
		if m.detailFieldAllowed(field.label) {
			filtered = append(filtered, field)
		}
	}

	return filtered
}

func (component tableViewComponent) editableDetailValue(st *Status) string {
	if !st.Exists {
		return st.Value
	}

	if component.model.shouldDisplayEncryptedValue(st) {
		return encryptedPlaceholderText
	}

	return st.Value
}

func (component tableViewComponent) renderEditableLocalDetailValue(field editableDetailField, width int) string {
	if !editableDetailValuePresent(field.local) && editableDetailValuePresent(field.cloud) {
		return component.model.muted("(deleted)")
	}

	return component.renderEditableDetailValue(field.local, width, true, false)
}

func (component tableViewComponent) renderEditableDetailValue(value string, width int, changed, cloud bool) string {
	m := component.model
	if value == "" || value == "-" {
		return m.muted("(none)")
	}

	if value == encryptedPlaceholderText {
		return m.encryptedPlaceholder()
	}

	value = detailOneLinePreview(value, width)
	if changed {
		if cloud {
			return m.diffCloudValue(value)
		}

		return m.diffLocalValue(value)
	}

	return m.value(value)
}

func editableDetailValuePresent(value string) bool {
	return value != "" && value != "-"
}

func (component tableViewComponent) filterSelectedParameterFields(fields [][2]string) [][2]string {
	m := component.model
	if len(m.opts.Fields) == 0 {
		return fields
	}

	out := make([][2]string, 0, len(fields))
	for _, pair := range fields {
		if m.detailFieldAllowed(pair[0]) {
			out = append(out, pair)
		}
	}

	return out
}

func (component tableViewComponent) detailFieldAllowed(label string) bool {
	m := component.model

	switch strings.ToLower(strings.TrimSpace(label)) {
	case "name":
		return true
	case "region":
		return m.opts.Fields.Allows("region")
	case "type":
		return m.opts.Fields.Allows("type")
	case "tier":
		return m.opts.Fields.Allows("tier")
	case "datatype", "data type", "data-type":
		return m.opts.Fields.Allows("data-type")
	case "policies":
		return m.opts.Fields.Allows("policies")
	case "version":
		return m.opts.Fields.Allows("version")
	case "len":
		return m.opts.Fields.Allows("len")
	case "sha256":
		return m.opts.Fields.Allows("sha256")
	case "description":
		return m.opts.Fields.Allows("description")
	case "user":
		return m.opts.Fields.Allows("user")
	case "date":
		return m.opts.Fields.Allows("date")
	case "value":
		return m.opts.Fields.Allows("value")
	default:
		return true
	}
}

// displayValue returns the user-facing value for selected blocks and VALUE table cells.
// SecureString values are shown when decrypted; otherwise the UI renders an encrypted placeholder.
func (component tableViewComponent) displayValue(st *Status, full bool) string {
	m := component.model

	if st.Pending {
		return "-"
	}

	if st.Item.Path != "" && st.isMissing() {
		return "-"
	}

	if m.shouldDisplayEncryptedValue(st) {
		return encryptedPlaceholderText
	}

	width := max(20, m.boxInnerWidth()-22)
	if full {
		width = max(20, m.boxInnerWidth()-18)
	}

	return oneLineValuePreview(st.Value, width)
}

func oneLineValuePreview(value string, width int) string {
	if value == "" {
		return "-"
	}

	if width < 4 {
		width = 4
	}

	normalized := strings.ReplaceAll(value, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	multiline := strings.Contains(normalized, "\n")
	preview := strings.ReplaceAll(normalized, "\n", `\n`)

	suffix := ""
	if multiline {
		suffix = "..."
	}

	available := width - len(suffix)
	if available < 1 {
		available = 1
	}

	runes := []rune(preview)
	if len(runes) > available {
		return string(runes[:available]) + "..."
	}

	return preview + suffix
}

func detailOneLinePreview(value string, width int) string {
	if value == "" {
		return ""
	}

	return oneLineValuePreview(value, width)
}

func (component tableViewComponent) shouldDisplayEncryptedValue(st *Status) bool {
	m := component.model
	return !st.Pending && st.HasSensitiveValue() && st.Value == "" && !m.opts.ShowSecureValues
}

func (component tableViewComponent) encryptedValueLocked() bool {
	m := component.model
	return !m.editNewParameter && m.currentStatus().Exists && m.normalizedEditType() == ssm.ParameterTypeSecureString && !m.opts.ShowSecureValues
}

func (component tableViewComponent) shouldShowEncryptedEditPlaceholder() bool {
	m := component.model
	return m.encryptedValueLocked() && m.editField != editFieldValue && m.textArea.Value() == ""
}

// renderListBlock renders the main table, including dynamic columns, scrolling, search/filter status, and messages.
func (component tableViewComponent) renderListBlock() string {
	m := component.model
	vis := m.visible()
	title := fmt.Sprintf("List of %d Parameters", len(vis))

	columns := m.tableColumns(vis)
	header := m.renderListHeader(columns)
	divider := strings.Repeat("─", m.boxInnerWidth())
	lines := []string{"  " + header, m.divider(divider)}

	bodyHeight := m.listBodyHeight()

	start := 0
	if m.selected >= bodyHeight {
		start = m.selected - bodyHeight + 1
	}

	end := min(len(vis), start+bodyHeight)
	for row := start; row < end; row++ {
		st := &m.statuses[vis[row]]
		lines = append(lines, m.renderListRow(row+1, st, row == m.selected, columns))
	}

	for len(lines) < 2+bodyHeight {
		lines = append(lines, "")
	}

	if m.searchMode || m.effectiveQuery != "" {
		lines = append(lines, m.divider(divider))
		if m.searchMode {
			lines = append(lines, m.searchLine())
		} else {
			lines = append(lines, m.filteredLine())
		}
	}

	return m.renderBox(title, lines, m.listBlockHeight())
}

type tableColumn struct {
	key    columnName
	header string
	width  int
}

// tableColumns calculates visible table columns and their widths from the current data.
// It shrinks wide columns until the table fits inside the box without moving headers away from row values.
func (component tableViewComponent) tableColumns(vis []int) []tableColumn {
	m := component.model
	keys := []columnName{columnIndex, columnPath}
	if m.hasLocalChanges() {
		keys = []columnName{columnIndex, columnState, columnPath}
	}

	visibleColumns := m.columnsForRendering()
	for _, key := range columnItems() {
		if m.columnAllowed(key) && visibleColumns[key] {
			keys = append(keys, key)
		}
	}

	cols := make([]tableColumn, 0, len(keys))
	for _, key := range keys {
		header := m.columnHeader(key)

		width := lipgloss.Width(header)
		if key == columnIndex {
			width = max(width, len(strconv.Itoa(max(1, len(vis)))))
		}

		for row, idx := range vis {
			st := &m.statuses[idx]
			value := m.tableCellValue(key, row+1, st)
			width = max(width, lipgloss.Width(value))
		}

		cols = append(cols, tableColumn{key: key, header: header, width: width})
	}

	available := max(20, m.boxInnerWidth()-2)
	for tableColumnsWidth(cols) > available {
		idx := widestShrinkableColumn(cols)
		if idx < 0 {
			break
		}

		cols[idx].width--
	}

	return cols
}

// tableColumnsWidth returns the visible width of the table with two spaces between columns.
func tableColumnsWidth(cols []tableColumn) int {
	if len(cols) == 0 {
		return 0
	}

	total := 2 * (len(cols) - 1)
	for _, col := range cols {
		total += col.width
	}

	return total
}

// widestShrinkableColumn finds the widest column that can still shrink without going below its minimum width.
func widestShrinkableColumn(cols []tableColumn) int {
	idx := -1
	width := -1

	for i, col := range cols {
		minWidth := columnMinWidth(col.key, col.header)
		if col.width <= minWidth {
			continue
		}

		if col.width > width {
			idx = i
			width = col.width
		}
	}

	return idx
}

// columnMinWidth protects important columns from becoming unreadably narrow during terminal-width fitting.
func columnMinWidth(key columnName, header string) int {
	switch key {
	case columnIndex, columnState, columnRegion, columnType, columnTier, columnVersion, columnLength, columnHash:
		return lipgloss.Width(header)
	case columnPath:
		return max(lipgloss.Width(header), 20)
	case columnDate:
		return max(lipgloss.Width(header), 20)
	case columnValue, columnUser, columnDescription:
		return max(lipgloss.Width(header), 12)
	default:
		return lipgloss.Width(header)
	}
}

// renderListHeader pads and styles the table header row.
func (component tableViewComponent) renderListHeader(cols []tableColumn) string {
	m := component.model

	parts := make([]string, 0, len(cols))
	for _, col := range cols {
		parts = append(parts, pad(col.header, col.width))
	}

	s := strings.Join(parts, "  ")
	if m.opts.NoColor {
		return s
	}

	return tableHeaderStyle.Render(s)
}

// renderListRow renders one status row with selection and status-based coloring.
func (component tableViewComponent) renderListRow(index int, st *Status, selected bool, cols []tableColumn) string {
	m := component.model

	if selected {
		parts := make([]string, 0, len(cols))
		for _, col := range cols {
			if col.key == columnState {
				parts = append(parts, m.renderListCell(col, index, st))
			} else {
				parts = append(parts, m.selectedListCell(col, index, st))
			}
		}

		return m.selectedMarker() + strings.Join(parts, "  ")
	}

	parts := make([]string, 0, len(cols))
	for _, col := range cols {
		parts = append(parts, m.renderListCell(col, index, st))
	}

	row := strings.Join(parts, "  ")

	plain := stripANSI(row)
	if styled := m.rowText(st, plain, false); styled != plain {
		return "  " + styled
	}

	return "  " + row
}

func (component tableViewComponent) selectedListCell(col tableColumn, index int, st *Status) string {
	m := component.model

	value := truncateInline(m.tableCellValue(col.key, index, st), col.width)
	if col.key == columnValue && m.shouldDisplayEncryptedValue(st) {
		value = encryptedPlaceholderText
	}

	return m.selectedRow(pad(value, col.width))
}

func (component tableViewComponent) renderListCell(col tableColumn, index int, st *Status) string {
	m := component.model

	value := truncateInline(m.tableCellValue(col.key, index, st), col.width)
	if col.key == columnValue && m.shouldDisplayEncryptedValue(st) {
		value = m.encryptedPlaceholder()
	}

	if col.key == columnState {
		return pad(m.stateValue(st.State), col.width)
	}

	return pad(value, col.width)
}

// rowText applies row-level styling based on selection and status severity.
func (component tableViewComponent) rowText(st *Status, row string, selected bool) string {
	m := component.model
	if selected {
		return m.selectedRow(row)
	}

	label := st.DisplayLabel()
	if label == "ERROR" {
		if m.opts.NoColor {
			return row
		}

		return lipgloss.NewStyle().Foreground(errFg).Render(row)
	}

	if label == "LOADING" || label == "MISSING" {
		if m.opts.NoColor {
			return row
		}

		return lipgloss.NewStyle().Foreground(missFg).Render(row)
	}

	if label == "EMPTY" {
		if m.opts.NoColor {
			return row
		}

		return lipgloss.NewStyle().Foreground(emptyFg).Render(row)
	}

	return row
}

// tableCellValue returns the raw display value for one dynamic table column.
func (component tableViewComponent) tableCellValue(key columnName, index int, st *Status) string {
	m := component.model

	switch key {
	case columnIndex:
		return strconv.Itoa(index)
	case columnState:
		return st.StateLabel()
	case columnRegion:
		return st.RegionLabel(m.opts.Region)
	case columnDate:
		return valueOrDash(st.Modified)
	case columnType:
		return valueOrDash(st.Type)
	case columnTier:
		return valueOrDash(st.Tier)
	case columnVersion:
		return intOrDash(st.Version)
	case columnLength:
		return intOrDash(int64(st.Length))
	case columnHash:
		return valueOrDash(st.SHA256Prefix)
	case columnValue:
		return m.displayValue(st, true)
	case columnUser:
		return valueOrDash(st.User)
	case columnDescription:
		return valueOrDash(st.Description)
	case columnPath:
		return st.Item.Path
	default:
		return "-"
	}
}

func (component tableViewComponent) boxInnerWidth() int {
	m := component.model
	return max(40, m.width-2)
}

func (component tableViewComponent) listBlockHeight() int {
	m := component.model
	// Main page content layout: optional selected parameter block + dynamic list block.
	return max(8, m.height-m.selectedParameterBlockHeight())
}

func (component tableViewComponent) selectedParameterBlockHeight() int {
	m := component.model
	if !m.selectedExpanded {
		return 0
	}

	st := m.currentStatus()
	if st.Item.Path == "" {
		return 0
	}

	if m.hasLocalChanges() {
		return dirtySelectedParameterDetailsHeight
	}

	return cleanSelectedParameterDetailsHeight
}

func (component tableViewComponent) listBodyHeight() int {
	m := component.model
	// Top/bottom border + header + header divider + optional filter/search lines.
	reserved := 4
	if m.searchMode || m.effectiveQuery != "" {
		reserved += 2
	}

	return max(3, m.listBlockHeight()-reserved)
}
