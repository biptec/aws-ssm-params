package ui

import (
	"strconv"
	"strings"
)

type tableSorter struct {
	*tableState
	*listState
	applyImmediately bool
	columnAllowedFn  func(columnName) bool
	cellValueFn      func(columnName, int, *Status) string
}

func newTableSorter(m *model) tableSorter {
	return tableSorter{
		tableState:       &m.tableState,
		listState:        &m.listState,
		applyImmediately: m.opts.ApplyImmediately,
		columnAllowedFn:  m.columnAllowed,
		cellValueFn:      m.tableCellValue,
	}
}

func (component *tableSorter) openSortPopup() {
	m := component
	m.sortCursor = m.sortCursorForCurrentSort()
	m.sortButtonsFocused = false
}

func (component tableSorter) columnAllowed(column columnName) bool {
	return component.columnAllowedFn(column)
}

func (component tableSorter) tableCellValue(column columnName, index int, status *Status) string {
	return component.cellValueFn(column, index, status)
}

type sortRule struct {
	column     columnName
	descending bool
}

func (rule sortRule) directionArrow() string {
	if rule.descending {
		return "↓"
	}

	return "↑"
}

type sortRules []sortRule

func (rules sortRules) primary() (columnName, bool) {
	if len(rules) == 0 {
		return columnPath, false
	}

	return rules[0].column, rules[0].descending
}

func (rules sortRules) index(column columnName) int {
	for i, rule := range rules {
		if rule.column == column {
			return i
		}
	}

	return -1
}

func (rules sortRules) find(column columnName) (sortRule, bool) {
	index := rules.index(column)
	if index < 0 {
		return sortRule{}, false
	}

	return rules[index], true
}

func (rules sortRules) without(column columnName) sortRules {
	filtered := make(sortRules, 0, len(rules))
	for _, rule := range rules {
		if rule.column != column {
			filtered = append(filtered, rule)
		}
	}

	return filtered
}

func (rules sortRules) with(column columnName, descending bool) sortRules {
	if column == "" {
		return rules
	}

	return append(rules.without(column), sortRule{column: column, descending: descending})
}

func (rules sortRules) orDefault() sortRules {
	if len(rules) == 0 {
		return sortRules{{column: columnPath}}
	}

	return rules
}

type sortItem struct {
	hotkey string
	column columnName
	label  string
}

func parseInitialSortOptions(values []string) sortRules {
	rules := make(sortRules, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}

		parts := strings.Split(value, ":")
		field := strings.TrimSpace(parts[0])

		column, ok := columnByFieldName(field)
		if !ok {
			continue
		}

		descending := false

		if len(parts) > 1 {
			switch strings.ToLower(strings.TrimSpace(parts[1])) {
			case "desc", "descending":
				descending = true
			}
		}

		rules = rules.with(column, descending)
	}

	if len(rules) == 0 {
		return sortRules{{column: columnPath}}
	}

	return rules
}

func columnByFieldName(name string) (columnName, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "state" {
		return columnState, true
	}

	if name == "name" || name == "path" {
		return columnPath, true
	}

	if name == "data-type" || name == "datatype" || name == "data_type" {
		return "", false
	}

	for _, column := range columnItems() {
		if name == string(column) || name == column.Field() {
			return column, true
		}
	}

	return "", false
}

func sortItems() []sortItem {
	return []sortItem{
		{hotkey: "m", column: columnState, label: "State"},
		{hotkey: "n", column: columnPath, label: "Name"},
		{hotkey: "v", column: columnValue, label: "Value"},
		{hotkey: "t", column: columnType, label: "Type"},
		{hotkey: "r", column: columnRegion, label: "Region"},
		{hotkey: "a", column: columnDate, label: "Date"},
		{hotkey: "o", column: columnVersion, label: "Version"},
		{hotkey: "i", column: columnTier, label: "Tier"},
		{hotkey: "z", column: columnLength, label: "Len"},
		{hotkey: "s", column: columnHash, label: "SHA256"},
		{hotkey: "u", column: columnUser, label: "User"},
		{hotkey: "e", column: columnDescription, label: "Description"},
	}
}

func (component tableSorter) popupSortItems() []sortItem {
	m := component

	visible := map[columnName]bool{columnPath: true}
	if m.hasLocalChanges() && !m.applyImmediately {
		visible[columnState] = true
	}

	for _, col := range columnItems() {
		if m.columnAllowed(col) && m.columns[col] {
			visible[col] = true
		}
	}

	items := make([]sortItem, 0, len(visible))
	for _, item := range sortItems() {
		if visible[item.column] {
			items = append(items, item)
		}
	}

	return items
}

func (component tableSorter) popupSortColumnByLetterHotkey(key string) (columnName, bool) {
	m := component
	for _, item := range m.popupSortItems() {
		if item.hotkey == key {
			return item.column, true
		}
	}

	return "", false
}

func (component tableSorter) visibleSortItems() []sortItem {
	m := component

	cols := []columnName{columnPath}
	if m.hasLocalChanges() && !m.applyImmediately {
		cols = []columnName{columnState, columnPath}
	}

	for _, col := range columnItems() {
		if m.columnAllowed(col) && m.columns[col] {
			cols = append(cols, col)
		}
	}

	items := make([]sortItem, 0, len(cols))
	for i, col := range cols {
		n := i + 1
		if n > 10 {
			break
		}

		hotkey := strconv.Itoa(n)
		if n == 10 {
			hotkey = "0"
		}

		items = append(items, sortItem{hotkey: hotkey, column: col, label: col.Label()})
	}

	return items
}

func (component tableSorter) visibleSortColumnByHotkey(key string) (columnName, bool) {
	m := component
	for _, item := range m.visibleSortItems() {
		if item.hotkey == key {
			return item.column, true
		}
	}

	return "", false
}

func (component tableSorter) sortCursorForCurrentSort() int {
	m := component
	items := m.popupSortItems()

	primary, _ := m.sortRules.primary()
	for i, item := range items {
		if item.column == primary {
			return i
		}
	}

	return 0
}

func (component tableSorter) sortRulesOrDefault() sortRules {
	return component.sortRules.orDefault()
}

func (component *tableSorter) setSortRules(rules sortRules) {
	m := component
	rules = rules.orDefault()
	m.sortRules = append(sortRules(nil), rules...)
	m.sortBy, m.sortDescending = m.sortRules.primary()
}

func (component *tableSorter) applySort(column columnName) {
	m := component

	if column == "" {
		return
	}

	descending := false
	if column == m.sortBy {
		descending = !m.sortDescending
	}

	m.applySortWithRules(sortRules{{column: column, descending: descending}})
}

func (component *tableSorter) toggleSortColumn(column columnName) {
	m := component

	if column == "" {
		return
	}

	rules := m.sortRulesOrDefault()
	if rules.index(column) >= 0 {
		rules = rules.without(column)
	} else {
		rules = rules.with(column, false)
	}

	m.applySortWithRules(rules)
}

func (component *tableSorter) toggleSortDirection(column columnName) {
	m := component

	if column == "" {
		return
	}

	rules := m.sortRulesOrDefault()

	idx := rules.index(column)
	if idx < 0 {
		rules = rules.with(column, true)
	} else {
		rules[idx].descending = !rules[idx].descending
	}

	m.applySortWithRules(rules)
}

func (component *tableSorter) applySortWithDirection(column columnName, descending bool) {
	m := component

	if column == "" {
		return
	}

	m.applySortWithRules(sortRules{{column: column, descending: descending}})
}

func (component *tableSorter) applySortWithRules(rules sortRules) {
	m := component

	var selectedKey string

	if len(m.visible()) > 0 && m.selected < len(m.visible()) {
		st := m.statuses[m.visible()[m.selected]]
		selectedKey = itemKey(st.Item.Region, st.Item.Path)
	}

	m.setSortRules(rules)
	rules = m.sortRulesOrDefault()

	orders := make([]statusOrder, 0, len(rules))
	for _, rule := range rules {
		currentRule := rule
		orders = append(orders, statusOrder{
			value: func(status *Status) string {
				return m.tableCellValue(currentRule.column, 0, status)
			},
			descending: currentRule.descending,
		})
	}

	m.statuses.sort(orders)

	if selectedKey != "" {
		for idx, visIdx := range m.visible() {
			st := m.statuses[visIdx]
			if itemKey(st.Item.Region, st.Item.Path) == selectedKey {
				m.selected = idx
				return
			}
		}
	}

	m.ensureSelection()
}

func (component tableSorter) columnHeader(c columnName) string {
	m := component

	if c == columnIndex {
		return "#"
	}

	if c == columnState {
		header := "STS"
		if rule, ok := m.sortRules.find(c); ok {
			header += " " + rule.directionArrow()
		}

		return header
	}

	header := strings.ToUpper(c.Label())
	if rule, ok := m.sortRules.find(c); ok {
		header += " " + rule.directionArrow()
	}

	return header
}

func (component tableSorter) sortPopupLabel(item sortItem) string {
	m := component
	if rule, ok := m.sortRules.find(item.column); ok {
		return item.label + " " + rule.directionArrow()
	}

	return item.label
}
