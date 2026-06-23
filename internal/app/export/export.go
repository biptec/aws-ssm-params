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
	*app.Options

	Format        textio.FormatType
	FieldMappings textio.FieldMappings
	Fields        textio.Fields
	SortColumns   []string
	KeyField      string
	BasePath      app.BasePath
	ScalarField   string
}

// Run loads statuses for the requested inventory and writes existing parameter values.
func Run(ctx context.Context, opts *Options, output io.Writer) error {
	r, err := newRunner(ctx, opts, output)
	if err != nil {
		return err
	}

	return r.run(ctx)
}

// runner owns the state and dependencies of one export invocation.
// Pure formatting and sorting helpers remain package functions; orchestration lives here.
type runner struct {
	opts         *Options
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

func newRunner(ctx context.Context, opts *Options, output io.Writer) (*runner, error) {
	items, err := opts.PrepareItems(ctx)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	client := ssm.NewClient(ssm.ClientConfig{
		Profile:        opts.Profile,
		Region:         opts.Region,
		WithDecryption: opts.WithDecryption,
		Logger:         opts.Logger,
	})

	regions := append([]string(nil), opts.Regions...)
	if opts.AllRegions {
		regions, err = client.ListRegions(ctx)
		if err != nil {
			return nil, errors.Wrap(err, "list AWS regions")
		}
	}

	fields := fieldsForOptions(opts.Fields)

	return &runner{
		opts:         opts,
		client:       client,
		writer:       textio.NewWriter(opts.Format, output),
		items:        items,
		regions:      regions,
		keyField:     opts.KeyField,
		scalarField:  opts.ScalarField,
		recordFields: recordFields(fields, opts.ScalarField, opts.KeyField),
		sortRules:    parseSortRules(opts.SortColumns),
		basePath:     opts.BasePath,
		fieldMaps:    opts.FieldMappings,
	}, nil
}

func (r *runner) run(ctx context.Context) error {
	statuses := r.loadStatuses(ctx)
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

func (r *runner) loadStatuses(ctx context.Context) ui.Statuses {
	includeValues := r.opts.WithDecryption ||
		r.recordFields.RequiresValues() ||
		r.opts.FilterGroups.HasField(filter.FieldValue) ||
		r.sortRules.requiresValues()

	var statuses ui.Statuses
	if len(r.opts.FilterGroups) > 0 && len(r.items) == 0 {
		statuses = ui.LoadFilteredStatusesBatchForRegions(
			ctx,
			r.client,
			r.opts.FilterGroups,
			includeValues,
			r.regions,
			nil,
		)
	} else {
		statuses = ui.LoadStatusesForRegions(
			ctx,
			r.client,
			r.items,
			includeValues,
			r.regions,
		)
	}

	if len(r.items) > 0 {
		return statuses.Filter(r.opts.FilterGroups)
	}

	return statuses
}

func (r *runner) records(statuses ui.Statuses) (textio.Records, error) {
	records := make(textio.Records, 0, len(statuses))
	for i := range statuses {
		if !statuses[i].Exists {
			continue
		}

		record := recordFromStatus(&statuses[i], r.recordFields)

		path, err := r.basePath.Relativize(record.Path)
		if err != nil {
			return nil, errors.Wrap(err, "make export parameter name relative")
		}

		record.Path = path
		records = append(records, record)
	}

	return records, nil
}
