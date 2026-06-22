package filter

// MatchAny reports whether a record matches at least one group. No groups means match all.
func MatchAny(groups []Group, record Record) bool {
	if len(groups) == 0 {
		return true
	}
	for _, group := range groups {
		if group.Match(record) {
			return true
		}
	}
	return false
}

// GroupsHaveField reports whether any group targets field.
func GroupsHaveField(groups []Group, field string) bool {
	for _, group := range groups {
		if group.HasField(field) {
			return true
		}
	}
	return false
}
