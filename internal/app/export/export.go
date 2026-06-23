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
	r, err := newRunner(ctx, options, output)
	if err != nil {
		return err
	}
	return r.run()
}

// runner owns the state and dependencies of one export invocation.
// Pure formatting and sorting helpers remain package functions; orchestration lives here.
type runner struct {
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

func newRunner(ctx context.Context, options Options, output io.Writer) (*runner, error) {
	cfg := options.Config
	items, err := app.PrepareItems(ctx, &cfg)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	client := ssm.NewClient(ssm.ClientConfig{
		Profile:        cfg.Profile,
		Region:         cfg.Region,
		WithDecryption: cfg.WithDecryption,
		Logger:         cfg.Logger,
	})
	regions := append([]string(nil), cfg.Regions...)
	if cfg.AllRegions {
		regions, err = client.ListRegions(ctx)
		if err != nil {
			return nil, errors.Wrap(err, "list AWS regions")
		}
	}

	fields := fieldsForOptions(options.Fields)
	return &runner{
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

func (r *runner) run() error {
	statuses := r.loadStatuses()
	r.sortRules.sort(statuses)
	records, err := r.records(statuses)
	if err != nil {
		return errors.WithStack(err)
	}
	if r.scalarField != "" {
		return errors.Wrap(
			r.writer.ExportScalar(records, r.scalarField, r.keyField),
			"write scalar export",
		)
	}
	mappings := r.fieldMaps.WithDefaults().ForFields(r.recordFields)
	return errors.Wrap(r.writer.Export(records, mappings, r.keyField), "write export")
}

func (r *runner) loadStatuses() ui.Statuses {
	includeValues := r.cfg.WithDecryption ||
		r.recordFields.RequiresValues() ||
		r.cfg.FilterGroups.HasField(filter.FieldValue) ||
		r.sortRules.requiresValues()

	var statuses ui.Statuses
	if len(r.cfg.FilterGroups) > 0 && len(r.items) == 0 {
		statuses = ui.LoadFilteredStatusesBatchForRegions(
			r.ctx,
			r.client,
			r.cfg.FilterGroups,
			includeValues,
			r.regions,
			nil,
		)
	} else {
		statuses = ui.LoadStatusesForRegions(
			r.ctx,
			r.client,
			r.items,
			includeValues,
			r.regions,
		)
	}
	if len(r.items) > 0 {
		return statuses.Filter(r.cfg.FilterGroups)
	}
	return statuses
}

func (r *runner) records(statuses ui.Statuses) (textio.Records, error) {
	records := make(textio.Records, 0, len(statuses))
	for i := range statuses {
		if !statuses[i].Exists {
			continue
		}
		record := recordFromStatus(statuses[i], r.recordFields)
		path, err := r.basePath.Relativize(record.Path)
		if err != nil {
			return nil, errors.Wrap(err, "make export parameter name relative")
		}
		record.Path = path
		records = append(records, record)
	}
	return records, nil
}
