// Package app contains runtime configuration and shared application services.
package app

import (
	"log/slog"

	"github.com/biptec/aws-ssm-params/internal/filter"
	"github.com/biptec/aws-ssm-params/internal/inventory"
)

// Config contains runtime settings shared by all application commands.
// It is independent of any CLI, environment-variable, or configuration-file adapter.
type Config struct {
	Logger         *slog.Logger
	FilterGroups   filter.Groups
	InventoryItems inventory.Items
	Region         string
	Regions        []string
	Profile        string
	AllRegions     bool
	WithDecryption bool
}
