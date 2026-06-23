// Package importer implements the import command.
package importer

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/app"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/biptec/aws-ssm-params/internal/textio"
)

// Options contains the complete runtime configuration for one import.
type Options struct {
	*app.Options

	Format          textio.FormatType
	FieldMappings   textio.FieldMappings
	Fields          textio.Fields
	KeyField        string
	BasePath        app.BasePath
	DefaultRegion   string
	DefaultType     ssm.ParameterType
	DefaultOptions  ssm.PutParameterOptions
	Policy          Policy
	ContinueOnError bool
	Summary         bool
}

// Run reads records, resolves their SSM names, and writes values to Parameter Store.
func Run(ctx context.Context, opts *Options, input io.Reader, summaryOutput io.Writer) error {
	r, err := newRunner(ctx, opts, input, summaryOutput)
	if err != nil {
		return err
	}

	return r.run(ctx)
}

// runner owns the mutable state and dependencies of one import invocation.
type runner struct {
	opts            *Options
	client          ssm.Client
	records         Records
	metadata        map[string]ssm.Metadata
	metadataErrors  map[string]error
	optionsResolver OptionsResolver
	policy          Policy
	continueOnError bool
	summaryEnabled  bool
	summaryOutput   io.Writer
	summary         Summary
	recordErrors    []string
	defaultType     ssm.ParameterType
}

// Summary contains the final per-operation import counters.
type Summary struct {
	Created int
	Updated int
	Skipped int
	Failed  int
}

func newRunner(ctx context.Context, opts *Options, input io.Reader, summaryOutput io.Writer) (*runner, error) {
	if defaultRegion := strings.TrimSpace(opts.DefaultRegion); defaultRegion != "" && opts.Region == "" {
		opts.Region = defaultRegion
		opts.Regions = []string{defaultRegion}
	}

	if opts.AllRegions {
		return nil, errors.New("all-regions mode is not supported for import")
	}

	if len(opts.Regions) > 1 {
		return nil, errors.New("import supports only one region")
	}

	if err := opts.EnsureRegions(ctx); err != nil {
		return nil, errors.WithStack(err)
	}

	if input == nil {
		return nil, errors.New("import input is required")
	}

	reader := textio.NewReader(opts.Format, input)

	parsedRecords, err := reader.Import(
		opts.FieldMappings.WithDefaults(),
		opts.KeyField,
	)
	if err != nil {
		return nil, errors.Wrap(err, "read import")
	}

	records, err := Records(parsedRecords).withBasePath(opts.BasePath)
	if err != nil {
		return nil, err
	}

	records = records.filter(opts.FilterGroups)
	if !opts.Fields.Allows(textio.FieldValue) {
		return nil, errors.New("import requires the value field")
	}

	client := ssm.NewClient(ssm.ClientConfig{
		Profile:        opts.Profile,
		Region:         opts.Region,
		WithDecryption: opts.WithDecryption,
		Logger:         opts.Logger,
	})
	metadata, metadataErrors := (&MetadataResolver{
		client: client, records: records, opts: opts, fields: opts.Fields,
	}).resolve(ctx)

	return &runner{
		opts:           opts,
		client:         client,
		records:        records,
		metadata:       metadata,
		metadataErrors: metadataErrors,
		optionsResolver: OptionsResolver{
			defaults: defaultOptionsForFields(opts.DefaultOptions, opts.Fields),
			fields:   opts.Fields,
		},
		policy:          opts.Policy,
		continueOnError: opts.ContinueOnError,
		summaryEnabled:  opts.Summary,
		summaryOutput:   summaryOutput,
		defaultType:     opts.DefaultType,
	}, nil
}

func (r *runner) run(ctx context.Context) error {
	for i := range r.records {
		if err := r.processRecord(ctx, &r.records[i]); err != nil {
			return err
		}
	}

	if r.summaryEnabled {
		r.writeSummary()
	}

	if len(r.recordErrors) > 0 {
		return fmt.Errorf(
			"import completed with %d error(s):\n%s",
			len(r.recordErrors),
			strings.Join(r.recordErrors, "\n"),
		)
	}

	return nil
}

func (r *runner) processRecord(ctx context.Context, record *textio.Record) error {
	region := recordRegion(record, r.opts, r.optionsResolver.fields)
	key := recordKey(region, record.Path)

	existing, exists := r.metadata[key]
	if err, ok := r.metadataErrors[key]; ok {
		if !errors.Is(err, ssm.ErrNotFound) {
			return r.handleRecordError(writeOperationUpdate, region, record.Path, err)
		}

		exists = false
	}

	operation, policyAction := r.policy.operation(exists)
	if strings.TrimSpace(record.Value) == "" {
		return r.handleRecordError(operation, region, record.Path, errors.New("import record value is required"))
	}

	shouldWrite, err := policyAction.resolve(operation, region, record.Path)
	if err != nil {
		return r.handleRecordError(operation, region, record.Path, err)
	}

	if !shouldWrite {
		logSkipped(r.opts.Logger, operation, policyAction, region, record.Path)
		r.summary.Skipped++

		return nil
	}

	parameterType, err := resolveType(r.defaultType, &existing, exists, record, r.optionsResolver.fields)
	if err != nil {
		return r.handleRecordError(operation, region, record.Path, err)
	}

	opts, err := r.optionsResolver.forRecord(record, &existing, exists)
	if err != nil {
		return r.handleRecordError(operation, region, record.Path, err)
	}

	opts.Overwrite = exists

	err = r.client.ForRegion(region).PutParameterWithOptions(
		ctx,
		record.Path,
		record.Value,
		parameterType,
		opts,
	)
	if err != nil {
		return r.handleRecordError(operation, region, record.Path, err)
	}

	if exists {
		r.summary.Updated++
	} else {
		r.summary.Created++
	}

	return nil
}

func (r *runner) handleRecordError(operation writeOperation, region, name string, err error) error {
	wrapped := errors.Wrap(err, name)
	logRecordError(r.opts.Logger, operation, region, name, wrapped)
	r.summary.Failed++

	r.recordErrors = append(r.recordErrors, wrapped.Error())
	if r.continueOnError {
		logContinueOnError(r.opts.Logger, region, name)
		return nil
	}

	return wrapped
}

func (r *runner) writeSummary() {
	_, _ = fmt.Fprintf(
		r.summaryOutput,
		"Import summary:\n  created: %d\n  updated: %d\n  skipped: %d\n  failed: %d\n",
		r.summary.Created,
		r.summary.Updated,
		r.summary.Skipped,
		r.summary.Failed,
	)
}
