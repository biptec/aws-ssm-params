package inventory

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
