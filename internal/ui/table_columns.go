package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type tableState struct {
	columns        map[columnName]bool
	columnsDraft   map[columnName]bool
	columnCursor   int
	sortBy         columnName
	sortDescending bool
	sortRules      []sortRule
	sortCursor     int
}

type tableColumnsComponent struct {
	model model
}

type columnName string

const (
	columnIndex       columnName = "index"
	columnRegion      columnName = "region"
	columnDate        columnName = "date"
	columnType        columnName = "type"
	columnTier        columnName = "tier"
	columnVersion     columnName = "version"
	columnLength      columnName = "len"
	columnHash        columnName = "sha256"
	columnValue       columnName = "value"
	columnUser        columnName = "user"
	columnDescription columnName = "description"
	columnPath        columnName = "path"
)

func (component *tableColumnsComponent) openColumnsPopup() {
	m := &component.model
	m.columnCursor = 0
	m.columnsDraft = nil
	m.pushPopup(popupColumns)
}

// updateColumns handles the column visibility picker and returns to the screen that opened it.
func (component tableColumnsComponent) updateColumns(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	cols := m.allowedColumnItems()
	key := msg.String()
	if action, ok, consumed := (&m).handlePendingNavigationSequence(key); consumed {
		if ok {
			m.columnCursor = cursorFromNavigation(m.columnCursor, len(cols), action)
		}
		return m, nil
	}
	if action, ok := m.navigationAction(key); ok {
		m.columnCursor = cursorFromNavigation(m.columnCursor, len(cols), action)
		return m, nil
	}
	if m.keymapStyle() == keymapVi && key == "g" {
		m.pendingKeySequence = "g"
		return m, nil
	}
	switch key {
	case "ctrl+_", "ctrl+/":
		m.openShortcuts(screenColumns)
	case "q", "esc", "ctrl+g":
		m.screen = m.returnScreen
	case " ", "enter":
		if len(cols) > 0 {
			key := cols[m.columnCursor]
			m.columns[key] = !m.columns[key]
		}
	case "a":
		for _, c := range cols {
			m.columns[c] = true
		}
	case "x":
		for _, c := range cols {
			m.columns[c] = false
		}
	}
	return m, nil
}

func (component tableColumnsComponent) updateColumnsPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := component.model
	cols := m.allowedColumnItems()
	key := msg.String()
	if action, ok, consumed := (&m).handlePendingNavigationSequence(key); consumed {
		if ok {
			m.columnCursor = cursorFromNavigation(m.columnCursor, len(cols), action)
		}
		return m, nil
	}
	if action, ok := m.navigationAction(key); ok {
		m.columnCursor = cursorFromNavigation(m.columnCursor, len(cols), action)
		return m, nil
	}
	if m.keymapStyle() == keymapVi && key == "g" {
		m.pendingKeySequence = "g"
		return m, nil
	}
	switch key {
	case "ctrl+_", "ctrl+/":
		m.openPopupShortcuts(screenColumns, popupColumns)
	case "q", "esc", "ctrl+g", "enter", "ctrl+j":
		m.popPopup()
	case " ":
		if len(cols) > 0 {
			key := cols[m.columnCursor]
			m.columns[key] = !m.columns[key]
		}
	case "a":
		for _, c := range cols {
			m.columns[c] = true
		}
	case "x":
		for _, c := range cols {
			m.columns[c] = false
		}
	}
	return m, nil
}

// renderColumnsScreen renders the legacy full-screen table-column chooser.
// The main UI now opens the same content as a popup, but keeping this renderer
// makes the shortcuts context and focused tests straightforward.
func (component tableColumnsComponent) renderColumnsScreen() string {
	m := component.model
	return m.renderBox("Columns", m.columnOptionLines(), m.height)
}

func (component tableColumnsComponent) renderColumnsPopup() string {
	m := component.model
	return m.renderPopupBoxWithActions("Columns", m.columnOptionLines(), "Esc close")
}

func (component tableColumnsComponent) columnOptionLines() []string {
	m := component.model
	cols := m.allowedColumnItems()
	visible := m.columnsForRendering()
	lines := make([]string, 0, 2+len(cols))
	lines = append(lines, m.muted("# and NAME are always visible."), "")
	for i, c := range cols {
		checked := visible[c]
		lines = append(lines, m.multiSelectLine(columnLabel(c), checked, i == m.columnCursor))
	}
	return lines
}

// columnItems returns optional table columns in the order presented to the user.
func columnItems() []columnName {
	return []columnName{
		columnValue,
		columnType,
		columnRegion,
		columnDate,
		columnVersion,
		columnTier,
		columnLength,
		columnHash,
		columnUser,
		columnDescription,
	}
}

func (component tableColumnsComponent) columnAllowed(column columnName) bool {
	m := component.model
	return m.fieldAllowed(fieldForColumn(column))
}

func fieldForColumn(column columnName) string {
	switch column {
	case columnIndex:
		return string(column)
	case columnPath:
		return "name"
	case columnRegion:
		return "region"
	case columnDate:
		return "date"
	case columnType:
		return "type"
	case columnTier:
		return "tier"
	case columnVersion:
		return "version"
	case columnLength:
		return "len"
	case columnHash:
		return "sha256"
	case columnValue:
		return "value"
	case columnUser:
		return "user"
	case columnDescription:
		return "description"
	default:
		return string(column)
	}
}

func (component tableColumnsComponent) allowedColumnItems() []columnName {
	m := component.model
	items := columnItems()
	out := make([]columnName, 0, len(items))
	for _, column := range items {
		if m.columnAllowed(column) {
			out = append(out, column)
		}
	}
	return out
}

func columnLabel(c columnName) string {
	switch c {
	case columnIndex:
		return "Index"
	case columnPath:
		return "Name"
	case columnRegion:
		return "Region"
	case columnDate:
		return "Date"
	case columnType:
		return "Type"
	case columnTier:
		return "Tier"
	case columnVersion:
		return "Version"
	case columnLength:
		return "Len"
	case columnHash:
		return "SHA256"
	case columnValue:
		return "Value"
	case columnUser:
		return "User"
	case columnDescription:
		return "Description"
	default:
		return string(c)
	}
}

func (component tableColumnsComponent) columnsForRendering() map[columnName]bool {
	m := component.model
	return m.columns
}

func defaultColumnVisibility(selected []string) map[columnName]bool {
	columns := map[columnName]bool{}
	for _, column := range columnItems() {
		columns[column] = false
	}
	for _, name := range selected {
		if column, ok := optionalColumnByName(name); ok {
			columns[column] = true
		}
	}
	return columns
}

// ParseColumnOption validates a comma-separated list of optional TUI columns.
// The # and NAME columns are always visible and therefore are intentionally not accepted here.
func ParseColumnOption(value string) ([]string, error) {
	names := compactColumnNames(value)
	if len(names) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(names))
	seen := map[string]bool{}
	for _, name := range names {
		column, ok := optionalColumnByName(name)
		if !ok {
			return nil, fmt.Errorf("unsupported --show-column value %q; supported columns: %s", name, strings.Join(ValidColumnNames(), ","))
		}
		canonical := string(column)
		if !seen[canonical] {
			seen[canonical] = true
			out = append(out, canonical)
		}
	}
	return out, nil
}

// ValidColumnNames returns every column name accepted by --show-column.
func ValidColumnNames() []string {
	columns := append([]columnName{columnPath}, columnItems()...)
	out := make([]string, 0, len(columns))
	for _, column := range columns {
		out = append(out, string(column))
	}
	return out
}

func compactColumnNames(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.ToLower(strings.TrimSpace(part))
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func optionalColumnByName(name string) (columnName, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "name" || name == "path" {
		return columnPath, true
	}
	for _, column := range columnItems() {
		if name == string(column) {
			return column, true
		}
	}
	return "", false
}
