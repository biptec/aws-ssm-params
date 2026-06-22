package inventory

import "strings"

// Item describes one desired SSM parameter discovered from a paths file or project manifests.
// Path is the SSM name; Region is either a concrete AWS region or "*" for multi-region lookup;
// the remaining fields preserve source metadata so the UI can explain where the item came from.
type Item struct {
	Path       string
	Region     string
	Kind       string
	Source     string
	App        string
	Component  string
	SecretName string
}

// Items is an ordered collection of inventory entries.
type Items []Item

// SameIdentity reports whether two items identify the same regional SSM row.
func (item Item) SameIdentity(other Item) bool {
	return item.Path == other.Path && item.Region == other.Region
}

// Paths returns SSM names in collection order.
func (items Items) Paths() []string {
	paths := make([]string, 0, len(items))
	for _, item := range items {
		paths = append(paths, item.Path)
	}
	return paths
}

// CommonRegion returns the shared region, or empty when the collection contains mixed regions.
func (items Items) CommonRegion() string {
	if len(items) == 0 {
		return ""
	}
	region := items[0].Region
	for _, item := range items[1:] {
		if item.Region != region {
			return ""
		}
	}
	return region
}

// HasWildcardRegion reports whether any item requires expansion across regions.
func (items Items) HasWildcardRegion() bool {
	for _, item := range items {
		if item.Region == "*" {
			return true
		}
	}
	return false
}

// WithDefaultRegion returns a copy with region applied only to entries that do not already specify one.
func (items Items) WithDefaultRegion(region string) Items {
	if len(items) == 0 {
		return nil
	}
	out := append(Items(nil), items...)
	for i := range out {
		if out[i].Region == "" {
			out[i].Region = region
		}
	}
	return out
}

// UniqueByPath trims names and keeps the first non-empty item for each path.
func (items Items) UniqueByPath() Items {
	seen := make(map[string]bool, len(items))
	out := make(Items, 0, len(items))
	for _, item := range items {
		item.Path = strings.TrimSpace(item.Path)
		if item.Path == "" || seen[item.Path] {
			continue
		}
		seen[item.Path] = true
		out = append(out, item)
	}
	return out
}

// MergeDuplicates combines source metadata for entries that refer to the same path.
func (items Items) MergeDuplicates() Items {
	byPath := make(map[string]Item, len(items))
	for _, item := range items {
		if item.Path == "" {
			continue
		}
		if existing, ok := byPath[item.Path]; ok {
			existing.Kind = mergeMetadata(existing.Kind, item.Kind)
			existing.Source = mergeMetadata(existing.Source, item.Source)
			existing.App = mergeMetadata(existing.App, item.App)
			existing.Component = mergeMetadata(existing.Component, item.Component)
			existing.SecretName = mergeMetadata(existing.SecretName, item.SecretName)
			byPath[item.Path] = existing
			continue
		}
		byPath[item.Path] = item
	}
	out := make(Items, 0, len(byPath))
	for _, item := range byPath {
		out = append(out, item)
	}
	return out
}

func mergeMetadata(current, addition string) string {
	if current == "" {
		return addition
	}
	if addition == "" || strings.Contains(current, addition) {
		return current
	}
	return current + "," + addition
}
