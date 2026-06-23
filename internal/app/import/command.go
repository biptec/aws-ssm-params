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
	command, err := newCommand(ctx, options, input, summaryOutput)
	if err != nil {
		return err
	}
	return command.run()
}

// Command owns the mutable state and dependencies of one import invocation.
type Command struct {
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

func newCommand(ctx context.Context, options Options, input io.Reader, summaryOutput io.Writer) (*Command, error) {
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

	client := app.NewClient(cfg)
	metadata, metadataErrors := (MetadataResolver{
		ctx: ctx, client: client, records: records, cfg: cfg, fields: options.Fields,
	}).resolve()
	return &Command{
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

func (command *Command) run() error {
	for i := range command.records {
		if err := command.processRecord(command.records[i]); err != nil {
			return err
		}
	}
	if command.summaryEnabled {
		command.writeSummary()
	}
	if len(command.recordErrors) > 0 {
		return fmt.Errorf(
			"import completed with %d error(s):\n%s",
			len(command.recordErrors),
			strings.Join(command.recordErrors, "\n"),
		)
	}
	return nil
}

func (command *Command) processRecord(record textio.Record) error {
	region := recordRegion(record, command.cfg, command.optionsResolver.fields)
	key := recordKey(region, record.Path)
	existing, exists := command.metadata[key]
	if err, ok := command.metadataErrors[key]; ok {
		if !errors.Is(err, ssm.ErrNotFound) {
			return command.handleRecordError(writeOperationUpdate, region, record.Path, err)
		}
		exists = false
	}

	operation, policyAction := command.policy.operation(exists)
	if strings.TrimSpace(record.Value) == "" {
		return command.handleRecordError(operation, region, record.Path, errors.New("import record value is required"))
	}
	shouldWrite, err := policyAction.resolve(operation, region, record.Path)
	if err != nil {
		return command.handleRecordError(operation, region, record.Path, err)
	}
	if !shouldWrite {
		logSkipped(command.cfg.Logger, operation, policyAction, region, record.Path)
		command.summary.Skipped++
		return nil
	}

	parameterType, err := resolveType(command.defaultType, existing, exists, record, command.optionsResolver.fields)
	if err != nil {
		return command.handleRecordError(operation, region, record.Path, err)
	}
	options, err := command.optionsResolver.forRecord(record, existing, exists)
	if err != nil {
		return command.handleRecordError(operation, region, record.Path, err)
	}
	options.Overwrite = exists
	err = command.client.ForRegion(region).PutParameterWithOptions(
		command.ctx,
		record.Path,
		record.Value,
		parameterType,
		options,
	)
	if err != nil {
		return command.handleRecordError(operation, region, record.Path, err)
	}
	if exists {
		command.summary.Updated++
	} else {
		command.summary.Created++
	}
	return nil
}

func (command *Command) handleRecordError(operation writeOperation, region, name string, err error) error {
	wrapped := errors.Wrap(err, name)
	logRecordError(command.cfg.Logger, operation, region, name, wrapped)
	command.summary.Failed++
	command.recordErrors = append(command.recordErrors, wrapped.Error())
	if command.continueOnError {
		logContinueOnError(command.cfg.Logger, region, name)
		return nil
	}
	return wrapped
}

func (command *Command) writeSummary() {
	_, _ = fmt.Fprintf(
		command.summaryOutput,
		"Import summary:\n  created: %d\n  updated: %d\n  skipped: %d\n  failed: %d\n",
		command.summary.Created,
		command.summary.Updated,
		command.summary.Skipped,
		command.summary.Failed,
	)
}
