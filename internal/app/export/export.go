// Package export implements the export command.
package export

import (
	"context"
	"io"
	"strings"

	"github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/app"
	"github.com/biptec/aws-ssm-params/internal/filter"
	"github.com/biptec/aws-ssm-params/internal/inventory"
	ssmclient "github.com/biptec/aws-ssm-params/internal/ssm/client"
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
type runner struct {
	opts         *Options
	client       ssmclient.Client
	writer       textio.Writer
	items        inventory.Items
	regions      []string
	keyField     string
	scalarField  string
	recordFields textio.Fields
	statusSort   ui.StatusSort
	basePath     app.BasePath
	fieldMaps    textio.FieldMappings
}

func newRunner(ctx context.Context, opts *Options, output io.Writer) (*runner, error) {
	items, err := opts.PrepareItems(ctx)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	client := ssmclient.New(ssmclient.Config{
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

	return &runner{
		opts:         opts,
		client:       client,
		writer:       textio.NewWriter(opts.Format, output),
		items:        items,
		regions:      regions,
		keyField:     opts.KeyField,
		scalarField:  opts.ScalarField,
		recordFields: opts.recordFields(),
		statusSort:   ui.ParseStatusSort(opts.SortColumns),
		basePath:     opts.BasePath,
		fieldMaps:    opts.FieldMappings,
	}, nil
}

func (r *runner) run(ctx context.Context) error {
	statuses := r.loadStatuses(ctx)
	statuses.Sort(r.statusSort)

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
		r.statusSort.RequiresValues()

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

		record := r.record(&statuses[i])

		path, err := r.basePath.Relativize(record.Path)
		if err != nil {
			return nil, errors.Wrap(err, "make export parameter name relative")
		}

		record.Path = path
		records = append(records, record)
	}

	return records, nil
}

func (r *runner) record(status *ui.Status) textio.Record {
	record := textio.Record{Path: status.Item.Path, Fields: r.recordFields}
	if r.recordFields.Contains(textio.FieldRegion) {
		record.Region = status.Item.Region
	}

	if r.recordFields.Contains(textio.FieldType) {
		record.Type = status.Type
	}

	if r.recordFields.Contains(textio.FieldTier) {
		record.Tier = status.Tier
	}

	if r.recordFields.Contains(textio.FieldDataType) {
		record.DataType = status.DataType
	}

	if r.recordFields.Contains(textio.FieldPolicies) {
		record.Policies = status.Policies
	}

	if r.recordFields.Contains(textio.FieldDescription) {
		record.Description = status.Description
	}

	if r.recordFields.Contains(textio.FieldValue) && status.Exists {
		record.Value = status.Value
	}

	if r.recordFields.Contains(textio.FieldDate) {
		record.Date = status.Modified
	}

	if r.recordFields.Contains(textio.FieldVersion) {
		record.Version = status.Version
	}

	if r.recordFields.Contains(textio.FieldLen) {
		record.Len = status.Length
	}

	if r.recordFields.Contains(textio.FieldSHA256) {
		record.SHA256 = status.SHA256Prefix
	}

	if r.recordFields.Contains(textio.FieldUser) {
		record.User = status.User
	}

	return record
}

func (opts *Options) recordFields() textio.Fields {
	fields := append(textio.Fields(nil), opts.Fields...)
	if len(fields) == 0 {
		fields = append(fields, exportFields...)
	}

	return fields.With(
		strings.TrimSpace(opts.ScalarField),
		strings.TrimSpace(opts.KeyField),
	)
}

var exportFields = textio.Fields{
	textio.FieldName,
	textio.FieldRegion,
	textio.FieldType,
	textio.FieldTier,
	textio.FieldDataType,
	textio.FieldPolicies,
	textio.FieldDescription,
	textio.FieldValue,
	textio.FieldDate,
	textio.FieldVersion,
	textio.FieldLen,
	textio.FieldSHA256,
	textio.FieldUser,
}
