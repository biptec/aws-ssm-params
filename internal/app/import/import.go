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
	Config          app.Config
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
func Run(ctx context.Context, options Options, input io.Reader, summaryOutput io.Writer) error {
	r, err := newRunner(ctx, options, input, summaryOutput)
	if err != nil {
		return err
	}
	return r.run()
}

// runner owns the mutable state and dependencies of one import invocation.
type runner struct {
	ctx             context.Context
	cfg             app.Config
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

func newRunner(ctx context.Context, options Options, input io.Reader, summaryOutput io.Writer) (*runner, error) {
	cfg := options.Config
	if defaultRegion := strings.TrimSpace(options.DefaultRegion); defaultRegion != "" && cfg.Region == "" {
		cfg.Region = defaultRegion
		cfg.Regions = []string{defaultRegion}
	}
	if cfg.AllRegions {
		return nil, errors.New("all-regions mode is not supported for import")
	}
	if len(cfg.Regions) > 1 {
		return nil, errors.New("import supports only one region")
	}
	if err := app.EnsureRegions(ctx, &cfg); err != nil {
		return nil, errors.WithStack(err)
	}
	if input == nil {
		return nil, errors.New("import input is required")
	}
	reader := textio.NewReader(options.Format, input)
	parsedRecords, err := reader.Import(
		options.FieldMappings.WithDefaults(),
		options.KeyField,
	)
	if err != nil {
		return nil, errors.Wrap(err, "read import")
	}
	records, err := Records(parsedRecords).withBasePath(options.BasePath)
	if err != nil {
		return nil, err
	}
	records = records.filter(cfg.FilterGroups)
	if !options.Fields.Allows(textio.FieldValue) {
		return nil, errors.New("import requires the value field")
	}

	client := ssm.NewClient(ssm.ClientConfig{
		Profile:        cfg.Profile,
		Region:         cfg.Region,
		WithDecryption: cfg.WithDecryption,
		Logger:         cfg.Logger,
	})
	metadata, metadataErrors := (MetadataResolver{
		ctx: ctx, client: client, records: records, cfg: cfg, fields: options.Fields,
	}).resolve()
	return &runner{
		ctx:            ctx,
		cfg:            cfg,
		client:         client,
		records:        records,
		metadata:       metadata,
		metadataErrors: metadataErrors,
		optionsResolver: OptionsResolver{
			defaults: defaultOptionsForFields(options.DefaultOptions, options.Fields),
			fields:   options.Fields,
		},
		policy:          options.Policy,
		continueOnError: options.ContinueOnError,
		summaryEnabled:  options.Summary,
		summaryOutput:   summaryOutput,
		defaultType:     options.DefaultType,
	}, nil
}

func (r *runner) run() error {
	for i := range r.records {
		if err := r.processRecord(r.records[i]); err != nil {
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

func (r *runner) processRecord(record textio.Record) error {
	region := recordRegion(record, r.cfg, r.optionsResolver.fields)
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
		logSkipped(r.cfg.Logger, operation, policyAction, region, record.Path)
		r.summary.Skipped++
		return nil
	}

	parameterType, err := resolveType(r.defaultType, existing, exists, record, r.optionsResolver.fields)
	if err != nil {
		return r.handleRecordError(operation, region, record.Path, err)
	}
	options, err := r.optionsResolver.forRecord(record, existing, exists)
	if err != nil {
		return r.handleRecordError(operation, region, record.Path, err)
	}
	options.Overwrite = exists
	err = r.client.ForRegion(region).PutParameterWithOptions(
		r.ctx,
		record.Path,
		record.Value,
		parameterType,
		options,
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
	logRecordError(r.cfg.Logger, operation, region, name, wrapped)
	r.summary.Failed++
	r.recordErrors = append(r.recordErrors, wrapped.Error())
	if r.continueOnError {
		logContinueOnError(r.cfg.Logger, region, name)
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
