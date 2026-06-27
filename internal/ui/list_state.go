package ui

import (
	"strings"

	paramfilter "github.com/biptec/aws-ssm-params/internal/filter"
	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/charmbracelet/bubbles/textinput"
)

type listState struct {
	statuses Statuses

	selected         int
	selectedExpanded bool

	filterMode            bool
	filterQuery           string
	filterInput           textinput.Model
	effectiveFilter       string
	effectiveFilterGroups paramfilter.Groups
	filterInvalid         bool
	filterError           string
}

func (m *listState) currentStatus() Status {
	vis := m.visible()
	if len(vis) == 0 || m.selected < 0 || m.selected >= len(vis) {
		return Status{}
	}

	return m.statuses[vis[m.selected]]
}

func (m *listState) currentStatusIndex() int {
	vis := m.visible()
	if len(vis) == 0 || m.selected < 0 || m.selected >= len(vis) {
		return -1
	}

	return vis[m.selected]
}

func (m *listState) currentItem() inventory.Item {
	return m.currentStatus().Item
}

func (m *listState) visible() []int {
	return m.matchesForFilter(m.effectiveFilterGroups)
}

func (m *listState) matchesForFilter(groups paramfilter.Groups) []int {
	out := []int{}

	for i := range m.statuses {
		if groups.Match(tuiFilterRecord(m.statuses[i].FilterRecord())) {
			out = append(out, i)
		}
	}

	return out
}

// applyFilterQuery parses the TUI filter expression and applies it to the already-loaded rows.
func (m *listState) applyFilterQuery(query string) {
	m.filterQuery = query
	if query == "" {
		m.effectiveFilter = ""
		m.effectiveFilterGroups = nil
		m.filterInvalid = false
		m.filterError = ""
		m.selected = 0

		return
	}

	groups, err := paramfilter.ParseGroups([]string{strings.ToLower(tuiFilterExpression(query))})
	if err != nil {
		m.filterInvalid = true
		m.filterError = err.Error()
		m.ensureSelection()

		return
	}

	if len(m.matchesForFilter(groups)) == 0 {
		m.filterInvalid = true
		m.filterError = "filter has no matches"
		m.ensureSelection()

		return
	}

	m.effectiveFilter = query
	m.effectiveFilterGroups = groups
	m.filterInvalid = false
	m.filterError = ""
	m.selected = 0
}

func tuiFilterExpression(query string) string {
	conditions := strings.Split(query, ";")
	for i := range conditions {
		conditions[i] = tuiFilterConditionExpression(conditions[i])
	}

	return strings.Join(conditions, ";")
}

func tuiFilterRecord(record *paramfilter.Record) *paramfilter.Record {
	return &paramfilter.Record{
		Name:        strings.ToLower(record.Name),
		Region:      strings.ToLower(record.Region),
		Type:        strings.ToLower(record.Type),
		Tier:        strings.ToLower(record.Tier),
		DataType:    strings.ToLower(record.DataType),
		Description: strings.ToLower(record.Description),
		Policies:    strings.ToLower(record.Policies),
		Value:       strings.ToLower(record.Value),
	}
}

func tuiFilterConditionExpression(condition string) string {
	condition = strings.TrimSpace(condition)
	if condition == "" || tuiFilterHasExplicitField(condition) {
		return condition
	}

	return tuiFilterPrefix(condition) + condition + tuiFilterSuffix(condition)
}

func tuiFilterHasExplicitField(condition string) bool {
	idx := strings.Index(condition, ":")
	if idx <= 0 {
		return false
	}

	_, ok := paramfilter.CanonicalField(condition[:idx])

	return ok
}

func tuiFilterPrefix(pattern string) string {
	switch {
	case strings.HasPrefix(pattern, "**"):
		return ""
	case strings.HasPrefix(pattern, "*") && !strings.HasPrefix(pattern, "*("):
		return "*"
	default:
		return "**"
	}
}

func tuiFilterSuffix(pattern string) string {
	switch {
	case strings.HasSuffix(pattern, "**"):
		return ""
	case strings.HasSuffix(pattern, "*"):
		return "*"
	default:
		return "**"
	}
}

func (m *listState) visiblePaths() []string {
	vis := m.visible()

	out := make([]string, 0, len(vis))
	for _, idx := range vis {
		out = append(out, m.statuses[idx].Item.Path)
	}

	return out
}

func (m *listState) visibleItems() inventory.Items {
	vis := m.visible()

	out := make(inventory.Items, 0, len(vis))
	for _, idx := range vis {
		out = append(out, m.statuses[idx].Item)
	}

	return out
}

func (m *listState) isFiltered() bool {
	return m.effectiveFilter != "" || len(m.visible()) < len(m.statuses)
}

func (m *listState) openFilterMode() {
	m.filterMode = true
	m.filterQuery = m.effectiveFilter
	m.filterInput.SetValue(m.filterQuery)
	m.filterInput.SetCursor(len([]rune(m.filterQuery)))
	m.filterInput.Focus()
	m.filterInvalid = false
	m.filterError = ""
}

func (m *listState) closeFilterMode() {
	m.filterMode = false
	if m.filterInvalid {
		m.filterQuery = m.effectiveFilter
		m.filterInput.SetValue(m.effectiveFilter)
		m.filterInvalid = false
		m.filterError = ""
	}

	m.filterInput.Blur()
}

// ensureSelection clamps the selected row so it always points at a visible item when possible.
func (m *listState) ensureSelection() {
	vis := m.visible()
	if len(vis) == 0 {
		m.selected = 0
		return
	}

	if m.selected < 0 {
		m.selected = 0
	}

	if m.selected >= len(vis) {
		m.selected = len(vis) - 1
	}
}

// move changes the selected row by delta within the currently visible result set.
func (m *listState) move(delta int) {
	vis := m.visible()
	if len(vis) == 0 {
		return
	}

	if delta == 1 {
		m.selected = nextCursor(m.selected, len(vis))
		return
	}

	if delta == -1 {
		m.selected = previousCursor(m.selected, len(vis))
		return
	}

	m.selected = max(0, min(len(vis)-1, m.selected+delta))
}

// replaceStatus updates the status list after saving a value.
// It prefers the exact path+region row so multi-region screens do not replace the wrong regional value;
// when a wildcard missing row was saved to a concrete region, it replaces that wildcard row as a fallback.
func (m *listState) replaceStatus(path string, st *Status) {
	fallback := -1

	for i := range m.statuses {
		if m.statuses[i].Item.Path != path {
			continue
		}

		if m.statuses[i].Item.SameIdentity(&st.Item) {
			m.statuses[i] = *st
			return
		}

		if m.statuses[i].Item.Region == st.Item.Region {
			fallback = i
			continue
		}

		if fallback < 0 || m.statuses[i].Item.Region == "*" {
			fallback = i
		}
	}

	if fallback >= 0 {
		m.statuses[fallback] = *st
		return
	}

	m.statuses = append(m.statuses, *st)
	m.selected = len(m.statuses) - 1
}

func (m *listState) replaceStatusByKey(key string, st *Status) {
	for i := range m.statuses {
		if itemKey(m.statuses[i].Item.Region, m.statuses[i].Item.Path) == key {
			m.statuses[i] = *st
			return
		}
	}

	m.statuses = append(m.statuses, *st)
}

func (m *listState) selectItem(item inventory.Item) {
	for selected, idx := range m.visible() {
		if m.statuses[idx].Item.SameIdentity(&item) {
			m.selected = selected
			return
		}
	}

	m.ensureSelection()
}

func (m *listState) hasLocalChanges() bool {
	for i := range m.statuses {
		if m.statuses[i].HasLocalChanges() {
			return true
		}
	}

	return false
}

func (m *listState) dirtyStatusIndexes() []int {
	out := []int{}
	for i := range m.statuses {
		if m.statuses[i].HasLocalChanges() {
			out = append(out, i)
		}
	}

	return out
}

func (m *listState) visibleDirtyStatusIndexes() []int {
	out := []int{}
	for _, idx := range m.visible() {
		if idx >= 0 && idx < len(m.statuses) && m.statuses[idx].HasLocalChanges() {
			out = append(out, idx)
		}
	}

	return out
}

func (m *listState) currentDirtyStatusIndexes() []int {
	idx := m.currentStatusIndex()
	if idx < 0 || !m.statuses[idx].HasLocalChanges() {
		return nil
	}

	return []int{idx}
}

func (m *listState) dirtyStatuses(indexes []int) []Status {
	out := make([]Status, 0, len(indexes))
	for _, idx := range indexes {
		if idx < 0 || idx >= len(m.statuses) || !m.statuses[idx].HasLocalChanges() {
			continue
		}

		out = append(out, m.statuses[idx])
	}

	return out
}

func (m *listState) dirtyStatusIndexesByState(indexes []int, allowed map[parameterState]bool) []int {
	out := make([]int, 0, len(indexes))
	for _, idx := range indexes {
		if idx < 0 || idx >= len(m.statuses) || !m.statuses[idx].HasLocalChanges() {
			continue
		}

		if allowed[m.statuses[idx].PendingOperation()] {
			out = append(out, idx)
		}
	}

	return out
}

func (m *listState) applyLocalDeleteItems(items inventory.Items) int {
	if len(items) == 0 {
		return 0
	}

	targets := map[string]bool{}
	for _, item := range items {
		targets[itemKey(item.Region, item.Path)] = true
	}

	changed := 0
	kept := m.statuses[:0]
	for i := range m.statuses {
		status := m.statuses[i]
		key := itemKey(status.Item.Region, status.Item.Path)
		if !targets[key] {
			kept = append(kept, status)
			continue
		}

		if status.PendingOperation() == parameterStateNew {
			changed++
			continue
		}

		if status.PendingOperation() != parameterStateDeleted {
			status.applyLocalDelete()
			changed++
		}

		kept = append(kept, status)
	}

	m.statuses = kept
	m.ensureSelection()

	return changed
}

func (m *listState) revertCurrentLocalChange() (parameterState, bool) {
	idx := m.currentStatusIndex()
	if idx < 0 || !m.statuses[idx].HasLocalChanges() {
		return parameterStateClean, false
	}

	operation, ok := m.revertLocalChangeAt(idx)
	m.ensureSelection()

	return operation, ok
}

func (m *listState) revertLocalChanges(indexes []int) int {
	changed := 0
	for i := len(indexes) - 1; i >= 0; i-- {
		if _, ok := m.revertLocalChangeAt(indexes[i]); ok {
			changed++
		}
	}

	m.ensureSelection()

	return changed
}

func (m *listState) revertLocalChangeAt(idx int) (parameterState, bool) {
	if idx < 0 || idx >= len(m.statuses) || !m.statuses[idx].HasLocalChanges() {
		return parameterStateClean, false
	}

	status := m.statuses[idx]
	operation := status.PendingOperation()
	if operation == parameterStateNew {
		m.statuses = append(m.statuses[:idx], m.statuses[idx+1:]...)
		return operation, true
	}

	if !status.Cloud.isZero() {
		m.statuses[idx] = status.Cloud.status()
		return operation, true
	}

	status.clearLocalState()
	m.statuses[idx] = status

	return operation, true
}

func (m *listState) markPushError(localKey, cloudKey string, operation parameterState, err error) {
	for i := range m.statuses {
		key := itemKey(m.statuses[i].Item.Region, m.statuses[i].Item.Path)
		if key != localKey && key != cloudKey {
			continue
		}

		m.statuses[i].applyPushError(operation, err)
		return
	}
}

func (m *listState) removeItemRows(items inventory.Items) {
	targets := map[string]bool{}
	for _, item := range items {
		targets[itemKey(item.Region, item.Path)] = true
	}

	kept := m.statuses[:0]
	for i := range m.statuses {
		if targets[itemKey(m.statuses[i].Item.Region, m.statuses[i].Item.Path)] {
			continue
		}

		kept = append(kept, m.statuses[i])
	}

	m.statuses = kept
}

// markMissingItem updates the UI after deletion by replacing matching concrete rows with a missing status.
func (m *listState) markMissingItem(item *inventory.Item) {
	for i := range m.statuses {
		if m.statuses[i].Item.SameIdentity(item) {
			m.statuses[i] = Status{Item: *item, Type: ssm.DefaultParameterType.String()}
			return
		}
	}
}
