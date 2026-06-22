package ui

import (
	"sort"
	"strconv"
	"strings"

	"github.com/biptec/aws-ssm-params/internal/natural"
)

type tableSortComponent struct {
	model model
}

type sortRule struct {
	column     columnName
	descending bool
}

type sortItem struct {
	hotkey string
	column columnName
	label  string
}

func parseInitialSortOptions(values []string) []sortRule {
	rules := make([]sortRule, 0, len(values))
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
		rules = withSortRule(rules, column, descending)
	}
	if len(rules) == 0 {
		return []sortRule{{column: columnPath}}
	}
	return rules
}

func primarySortRule(rules []sortRule) (columnName, bool) {
	if len(rules) == 0 {
		return columnPath, false
	}
	return rules[0].column, rules[0].descending
}

func columnByFieldName(name string) (columnName, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "name" || name == "path" {
		return columnPath, true
	}
	if name == "data-type" || name == "datatype" || name == "data_type" {
		return "", false
	}
	for _, column := range columnItems() {
		if name == string(column) || name == fieldForColumn(column) {
			return column, true
		}
	}
	return "", false
}

func sortItems() []sortItem {
	return []sortItem{
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

func (component tableSortComponent) popupSortItems() []sortItem {
	m := component.model
	visible := map[columnName]bool{columnPath: true}
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

func (component tableSortComponent) popupSortColumnByLetterHotkey(key string) (columnName, bool) {
	m := component.model
	for _, item := range m.popupSortItems() {
		if item.hotkey == key {
			return item.column, true
		}
	}
	return "", false
}

func (component tableSortComponent) visibleSortItems() []sortItem {
	m := component.model
	cols := []columnName{columnPath}
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
		items = append(items, sortItem{hotkey: hotkey, column: col, label: columnLabel(col)})
	}
	return items
}

func (component tableSortComponent) visibleSortColumnByHotkey(key string) (columnName, bool) {
	m := component.model
	for _, item := range m.visibleSortItems() {
		if item.hotkey == key {
			return item.column, true
		}
	}
	return "", false
}

func (component tableSortComponent) sortCursorForCurrentSort() int {
	m := component.model
	items := m.popupSortItems()
	primary, _ := primarySortRule(m.sortRules)
	for i, item := range items {
		if item.column == primary {
			return i
		}
	}
	return 0
}

func sortRuleIndex(rules []sortRule, column columnName) int {
	for i, rule := range rules {
		if rule.column == column {
			return i
		}
	}
	return -1
}

func sortRuleForColumn(rules []sortRule, column columnName) (sortRule, bool) {
	idx := sortRuleIndex(rules, column)
	if idx < 0 {
		return sortRule{}, false
	}
	return rules[idx], true
}

func withoutSortRule(rules []sortRule, column columnName) []sortRule {
	filtered := make([]sortRule, 0, len(rules))
	for _, rule := range rules {
		if rule.column != column {
			filtered = append(filtered, rule)
		}
	}
	return filtered
}

func withSortRule(rules []sortRule, column columnName, descending bool) []sortRule {
	if column == "" {
		return rules
	}
	updated := withoutSortRule(rules, column)
	updated = append(updated, sortRule{column: column, descending: descending})
	return updated
}

func (component tableSortComponent) sortRulesOrDefault() []sortRule {
	m := component.model
	if len(m.sortRules) == 0 {
		return []sortRule{{column: columnPath}}
	}
	return m.sortRules
}

func (component *tableSortComponent) setSortRules(rules []sortRule) {
	m := &component.model
	if len(rules) == 0 {
		rules = []sortRule{{column: columnPath}}
	}
	m.sortRules = append([]sortRule(nil), rules...)
	m.sortBy, m.sortDescending = primarySortRule(m.sortRules)
}

func (component *tableSortComponent) applySort(column columnName) {
	m := &component.model
	if column == "" {
		return
	}
	descending := false
	if column == m.sortBy {
		descending = !m.sortDescending
	}
	m.applySortWithRules([]sortRule{{column: column, descending: descending}})
}

func (component *tableSortComponent) toggleSortColumn(column columnName) {
	m := &component.model
	if column == "" {
		return
	}
	rules := m.sortRulesOrDefault()
	if sortRuleIndex(rules, column) >= 0 {
		rules = withoutSortRule(rules, column)
	} else {
		rules = withSortRule(rules, column, false)
	}
	m.applySortWithRules(rules)
}

func (component *tableSortComponent) toggleSortDirection(column columnName) {
	m := &component.model
	if column == "" {
		return
	}
	rules := m.sortRulesOrDefault()
	idx := sortRuleIndex(rules, column)
	if idx < 0 {
		rules = withSortRule(rules, column, true)
	} else {
		rules[idx].descending = !rules[idx].descending
	}
	m.applySortWithRules(rules)
}

func (component *tableSortComponent) applySortWithDirection(column columnName, descending bool) {
	m := &component.model
	if column == "" {
		return
	}
	m.applySortWithRules([]sortRule{{column: column, descending: descending}})
}

func (component *tableSortComponent) applySortWithRules(rules []sortRule) {
	m := &component.model
	var selectedKey string
	if len(m.visible()) > 0 && m.selected < len(m.visible()) {
		st := m.statuses[m.visible()[m.selected]]
		selectedKey = itemKey(st.Item.Region, st.Item.Path)
	}
	m.setSortRules(rules)
	rules = m.sortRulesOrDefault()
	sort.SliceStable(m.statuses, func(i, j int) bool {
		left := m.statuses[i]
		right := m.statuses[j]
		for _, rule := range rules {
			cmp := natural.Compare(m.tableCellValue(rule.column, 0, left), m.tableCellValue(rule.column, 0, right))
			if cmp == 0 {
				continue
			}
			if rule.descending {
				return cmp > 0
			}
			return cmp < 0
		}
		if cmp := natural.Compare(left.Item.Region, right.Item.Region); cmp != 0 {
			return cmp < 0
		}
		return natural.Compare(left.Item.Path, right.Item.Path) < 0
	})
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

func (component tableSortComponent) columnHeader(c columnName) string {
	m := component.model
	if c == columnIndex {
		return "#"
	}
	header := strings.ToUpper(columnLabel(c))
	if rule, ok := sortRuleForColumn(m.sortRules, c); ok {
		header += " " + sortDirectionArrow(rule.descending)
	}
	return header
}

func sortDirectionArrow(descending bool) string {
	if descending {
		return "↓"
	}
	return "↑"
}

func (component tableSortComponent) sortPopupLabel(item sortItem) string {
	m := component.model
	if rule, ok := sortRuleForColumn(m.sortRules, item.column); ok {
		return item.label + " " + sortDirectionArrow(rule.descending)
	}
	return item.label
}
