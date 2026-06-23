// Package export implements the export command.
package export

import (
	"context"
	"io"

	"github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/app"
	"github.com/biptec/aws-ssm-params/internal/filter"
	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/biptec/aws-ssm-params/internal/textio"
	"github.com/biptec/aws-ssm-params/internal/ui"
)

// Options contains the complete runtime configuration for one export.
type Options struct {
	Config        app.Config
	Format        textio.FormatType
	FieldMappings textio.FieldMappings
	Fields        textio.Fields
	SortColumns   []string
	KeyField      string
	BasePath      app.BasePath
	ScalarField   string
}

// Run loads statuses for the requested inventory and writes existing parameter values.
func Run(ctx context.Context, options Options, output io.Writer) error {
	command, err := newCommand(ctx, options, output)
	if err != nil {
		return err
	}
	return command.run()
}

// Command owns the state and dependencies of one export invocation.
// Pure formatting and sorting helpers remain package functions; orchestration lives here.
type Command struct {
	ctx          context.Context
	cfg          app.Config
	client       ssm.Client
	writer       textio.Writer
	items        inventory.Items
	regions      []string
	keyField     string
	scalarField  string
	recordFields textio.Fields
	sortRules    SortRules
	basePath     app.BasePath
	fieldMaps    textio.FieldMappings
}

func newCommand(ctx context.Context, options Options, output io.Writer) (*Command, error) {
	cfg := options.Config
	items, err := app.PrepareItems(ctx, &cfg)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	client := app.NewClient(cfg)
	regions := append([]string(nil), cfg.Regions...)
	if cfg.AllRegions {
		regions, err = client.ListRegions(ctx)
		if err != nil {
			return nil, errors.Wrap(err, "list AWS regions")
		}
	}

	fields := fieldsForOptions(options.Fields)
	return &Command{
		ctx:          ctx,
		cfg:          cfg,
		client:       client,
		writer:       textio.NewWriter(options.Format, output),
		items:        items,
		regions:      regions,
		keyField:     options.KeyField,
		scalarField:  options.ScalarField,
		recordFields: recordFields(fields, options.ScalarField, options.KeyField),
		sortRules:    parseSortRules(options.SortColumns),
		basePath:     options.BasePath,
		fieldMaps:    options.FieldMappings,
	}, nil
}

func (command *Command) run() error {
	statuses := command.loadStatuses()
	command.sortRules.sort(statuses)
	records, err := command.records(statuses)
	if err != nil {
		return errors.WithStack(err)
	}
	if command.scalarField != "" {
		return errors.Wrap(
			command.writer.ExportScalar(records, command.scalarField, command.keyField),
			"write scalar export",
		)
	}
	mappings := command.fieldMaps.WithDefaults().ForFields(command.recordFields)
	return errors.Wrap(command.writer.Export(records, mappings, command.keyField), "write export")
}

func (command *Command) loadStatuses() ui.Statuses {
	includeValues := command.cfg.WithDecryption ||
		command.recordFields.RequiresValues() ||
		command.cfg.FilterGroups.HasField(filter.FieldValue) ||
		command.sortRules.requiresValues()

	var statuses ui.Statuses
	if len(command.cfg.FilterGroups) > 0 && len(command.items) == 0 {
		statuses = ui.LoadFilteredStatusesBatchForRegions(
			command.ctx,
			command.client,
			command.cfg.FilterGroups,
			includeValues,
			command.regions,
			nil,
		)
	} else {
		statuses = ui.LoadStatusesForRegions(
			command.ctx,
			command.client,
			command.items,
			includeValues,
			command.regions,
		)
	}
	if len(command.items) > 0 {
		return statuses.Filter(command.cfg.FilterGroups)
	}
	return statuses
}

func (command *Command) records(statuses ui.Statuses) (textio.Records, error) {
	records := make(textio.Records, 0, len(statuses))
	for i := range statuses {
		if !statuses[i].Exists {
			continue
		}
		record := recordFromStatus(statuses[i], command.recordFields)
		path, err := command.basePath.Relativize(record.Path)
		if err != nil {
			return nil, errors.Wrap(err, "make export parameter name relative")
		}
		record.Path = path
		records = append(records, record)
	}
	return records, nil
}
