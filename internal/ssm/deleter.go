package ssm

import (
	"context"
	"sort"
	"strings"

	"github.com/cockroachdb/errors"
)

// DeleteTarget identifies one parameter in one concrete AWS region.
type DeleteTarget struct {
	Name   string
	Region string
}

// Deleter groups targets by region and delegates AWS batch sizing to Client.
// Keeping regional routing here gives the TUI and non-interactive commands one
// deletion path.
type Deleter struct {
	client Client
}

// NewDeleter binds regional deletion orchestration to client.
func NewDeleter(client Client) *Deleter {
	return &Deleter{client: client}
}

// Delete removes targets grouped by region. Empty names are ignored, duplicate
// region/name pairs are deleted once, and processing stops on the first error.
func (deleter *Deleter) Delete(ctx context.Context, targets []DeleteTarget) error {
	pathsByRegion := make(map[string][]string)
	seen := make(map[string]bool, len(targets))

	for _, target := range targets {
		name := strings.TrimSpace(target.Name)
		region := strings.TrimSpace(target.Region)

		if name == "" {
			continue
		}

		key := region + "\x00" + name
		if seen[key] {
			continue
		}

		seen[key] = true

		pathsByRegion[region] = append(pathsByRegion[region], name)
	}

	regions := make([]string, 0, len(pathsByRegion))
	for region := range pathsByRegion {
		regions = append(regions, region)
	}

	sort.Strings(regions)

	for _, region := range regions {
		paths := pathsByRegion[region]
		if err := deleter.client.ForRegion(region).DeleteMany(ctx, paths); err != nil {
			return errors.Wrapf(err, "delete parameters from %s", region)
		}
	}

	return nil
}
