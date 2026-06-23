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
	ssmclient "github.com/biptec/aws-ssm-params/internal/ssm/client"
	"github.com/biptec/aws-ssm-params/internal/textio"
)

// Options contains the complete runtime configuration for one import.
type Options struct {
	*app.Options

	Format          textio.FormatType
	FieldMappings   textio.FieldMappings
	Fields          textio.Fields
	KeyField        string
	PathMappings    app.PathMappings
	DefaultRegion   string
	DefaultType     ssm.ParameterType
	DefaultOptions  ssm.PutParameterOptions
	Policy          Policy
	ContinueOnError bool
	Summary         bool
	DryRun          bool
}

type dependencies struct {
	newClient func(ssmclient.Config) ssmclient.Client
}

// Run reads records, resolves their SSM names, and writes values to Parameter Store.
func Run(ctx context.Context, opts *Options, input io.Reader, summaryOutput io.Writer) error {
	return runWithDependencies(ctx, opts, input, summaryOutput, dependencies{newClient: ssmclient.New})
}

func runWithDependencies(ctx context.Context, opts *Options, input io.Reader, summaryOutput io.Writer, deps dependencies) error {
	r, err := newRunner(ctx, opts, input, summaryOutput, deps)
	if err != nil {
		return err
	}

	return r.run(ctx)
}

// runner owns the mutable state and dependencies of one import invocation.
type runner struct {
	opts            *Options
	client          ssmclient.Client
	records         app.Records
	recordResolver  *recordResolver
	policy          Policy
	continueOnError bool
	summaryEnabled  bool
	summaryOutput   io.Writer
	dryRun          bool
	summary         Summary
	recordErrors    []string
}

// Summary contains the final per-operation import counters.
type Summary struct {
	Created int
	Updated int
	Skipped int
	Failed  int
}

func newRunner(ctx context.Context, opts *Options, input io.Reader, summaryOutput io.Writer, deps dependencies) (*runner, error) {
	if summaryOutput == nil {
		summaryOutput = io.Discard
	}

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

	records, err := app.Records(parsedRecords).MapNamesToAWS(opts.PathMappings)
	if err != nil {
		return nil, errors.Wrap(err, "resolve import parameter names")
	}

	records = records.Filter(opts.FilterGroups)
	if !opts.Fields.Allows(textio.FieldValue) {
		return nil, errors.New("import requires the value field")
	}

	client := deps.newClient(ssmclient.Config{
		Profile:        opts.Profile,
		Region:         opts.Region,
		WithDecryption: opts.WithDecryption,
		Logger:         opts.Logger,
	})
	recordResolver := newRecordResolver(ctx, client, records, opts)

	return &runner{
		opts:            opts,
		client:          client,
		records:         records,
		recordResolver:  recordResolver,
		policy:          opts.Policy,
		continueOnError: opts.ContinueOnError,
		summaryEnabled:  opts.Summary,
		summaryOutput:   summaryOutput,
		dryRun:          opts.DryRun,
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
	region, existing, exists, err := r.recordResolver.resolveExisting(record)
	if err != nil {
		return r.handleRecordError(writeOperationUpdate, region, record.Path, err)
	}

	operation, policyAction := r.policy.operation(exists)
	if strings.TrimSpace(record.Value) == "" {
		return r.handleRecordError(operation, region, record.Path, errors.New("import record value is required"))
	}

	shouldWrite, err := r.resolvePolicy(policyAction, operation, region, record.Path)
	if err != nil {
		return r.handleRecordError(operation, region, record.Path, err)
	}

	if !shouldWrite {
		logSkipped(r.opts.Logger, operation, policyAction, region, record.Path)
		r.summary.Skipped++

		return nil
	}

	parameterType, err := r.recordResolver.parameterType(&existing, exists, record)
	if err != nil {
		return r.handleRecordError(operation, region, record.Path, err)
	}

	opts, err := r.recordResolver.putOptions(record, &existing, exists)
	if err != nil {
		return r.handleRecordError(operation, region, record.Path, err)
	}

	opts.Overwrite = exists

	if r.dryRun {
		if err := r.writeDryRun(operation, region, record.Path); err != nil {
			return r.handleRecordError(operation, region, record.Path, err)
		}

		if exists {
			r.summary.Updated++
		} else {
			r.summary.Created++
		}

		return nil
	}

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

func (r *runner) resolvePolicy(action PolicyAction, operation writeOperation, region, name string) (bool, error) {
	if r.dryRun {
		return action.resolveDryRun(operation, name)
	}

	return action.resolve(operation, region, name)
}

func (r *runner) writeDryRun(operation writeOperation, region, name string) error {
	if region != "" {
		_, err := fmt.Fprintf(r.summaryOutput, "DRY-RUN: would %s parameter %s in %s\n", operation, name, region)
		return errors.Wrap(err, "write import dry-run output")
	}

	_, err := fmt.Fprintf(r.summaryOutput, "DRY-RUN: would %s parameter %s\n", operation, name)

	return errors.Wrap(err, "write import dry-run output")
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
	title := "Import summary"
	if r.dryRun {
		title = "Import dry-run summary"
	}

	_, _ = fmt.Fprintf(
		r.summaryOutput,
		"%s:\n  created: %d\n  updated: %d\n  skipped: %d\n  failed: %d\n",
		title,
		r.summary.Created,
		r.summary.Updated,
		r.summary.Skipped,
		r.summary.Failed,
	)
}
