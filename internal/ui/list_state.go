package ui

import (
	"strings"

	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/ssm"
)

type listStateComponent struct {
	model model
}

func (component listStateComponent) currentStatus() Status {
	m := component.model
	vis := m.visible()
	if len(vis) == 0 || m.selected < 0 || m.selected >= len(vis) {
		return Status{}
	}
	return m.statuses[vis[m.selected]]
}

func (component listStateComponent) currentItem() inventory.Item {
	m := component.model
	return m.currentStatus().Item
}

func (component listStateComponent) visible() []int {
	m := component.model
	return m.matchesFor(m.effectiveQuery)
}

func (component listStateComponent) matchesFor(query string) []int {
	m := component.model
	q := strings.ToLower(query)
	out := []int{}
	for i := range m.statuses {
		if q == "" || strings.Contains(strings.ToLower(m.statuses[i].Item.Path), q) {
			out = append(out, i)
		}
	}
	return out
}

// applySearchQuery updates the search query, validates it against visible rows, and keeps selection in range.
func (component *listStateComponent) applySearchQuery(query string) {
	m := &component.model
	m.query = query
	if query == "" {
		m.effectiveQuery = ""
		m.searchInvalid = false
		m.selected = 0
		return
	}
	if len(m.matchesFor(query)) > 0 {
		m.effectiveQuery = query
		m.searchInvalid = false
		m.selected = 0
		return
	}
	m.searchInvalid = true
	m.ensureSelection()
}

func (component listStateComponent) visiblePaths() []string {
	m := component.model
	vis := m.visible()
	out := make([]string, 0, len(vis))
	for _, idx := range vis {
		out = append(out, m.statuses[idx].Item.Path)
	}
	return out
}

func (component listStateComponent) visibleItems() []inventory.Item {
	m := component.model
	vis := m.visible()
	out := make([]inventory.Item, 0, len(vis))
	for _, idx := range vis {
		out = append(out, m.statuses[idx].Item)
	}
	return out
}

// ensureSelection clamps the selected row so it always points at a visible item when possible.
func (component *listStateComponent) ensureSelection() {
	m := &component.model
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
func (component *listStateComponent) move(delta int) {
	m := &component.model
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
func (component *listStateComponent) replaceStatus(path string, st Status) {
	m := &component.model
	fallback := -1
	for i := range m.statuses {
		if m.statuses[i].Item.Path != path {
			continue
		}
		if sameItem(m.statuses[i].Item, st.Item) {
			m.statuses[i] = st
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
		m.statuses[fallback] = st
		return
	}
	m.statuses = append(m.statuses, st)
	m.selected = len(m.statuses) - 1
}

func (component *listStateComponent) removeItemRows(items []inventory.Item) {
	m := &component.model
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
func (component *listStateComponent) markMissingItem(item inventory.Item) {
	m := &component.model
	for i := range m.statuses {
		if sameItem(m.statuses[i].Item, item) {
			m.statuses[i] = Status{Item: item, Type: ssm.DefaultParameterType.String()}
			return
		}
	}
}

// sameItem compares inventory identity fields that uniquely identify a row in the UI.
func sameItem(a, b inventory.Item) bool {
	return a.Path == b.Path && a.Region == b.Region
}
