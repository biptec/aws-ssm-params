// Package importer implements the import command.
package importer

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/app"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/biptec/aws-ssm-params/internal/textio"
)

// Run reads dotenv, JSON, or YAML data, resolves each record to an SSM name, and writes values to Parameter Store.
func Run(ctx *app.CLIContext) error {
	command, err := newCommand(ctx, nil, os.Stderr)
	if err != nil {
		return err
	}
	return command.run()
}

// Command owns the mutable state and dependencies of one import invocation.
type Command struct {
	ctx             *app.CLIContext
	cfg             app.Config
	client          ssm.Client
	records         Records
	metadata        map[string]ssm.Metadata
	metadataErrors  map[string]error
	optionsResolver OptionsResolver
	policy          writePolicy
	continueOnError bool
	summaryEnabled  bool
	summaryOutput   io.Writer
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

func newCommand(ctx *app.CLIContext, input io.Reader, summaryOutput io.Writer) (*Command, error) {
	cfg, err := app.ConfigFromCLI(ctx)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	if defaultRegion := strings.TrimSpace(ctx.String("default-region")); defaultRegion != "" && cfg.Region == "" {
		cfg.Region = defaultRegion
		cfg.Regions = []string{defaultRegion}
	}
	if cfg.AllRegions {
		return nil, errors.New("--all-regions is not supported for import; specify --region")
	}
	if len(cfg.Regions) > 1 {
		return nil, errors.New("multiple --region values are only supported for tui and export")
	}
	if err := app.EnsureRegions(ctx.Context, &cfg); err != nil {
		return nil, errors.WithStack(err)
	}
	if input == nil {
		input, err = stdinReader()
		if err != nil {
			return nil, err
		}
	}
	reader := textio.NewReader(textio.FormatType(ctx.String("format")), input)
	parsedRecords, err := reader.Import(
		cfg.FieldMappings.WithDefaults(),
		strings.TrimSpace(ctx.String("key-field")),
	)
	if err != nil {
		return nil, errors.Wrap(err, "read import")
	}
	records, err := Records(parsedRecords).withBasePath(ctx.String("base-path"))
	if err != nil {
		return nil, err
	}
	records = records.filter(cfg.FilterGroups)
	if err := cfg.RequireField(textio.FieldValue, "import"); err != nil {
		return nil, errors.WithStack(err)
	}
	defaultOptions, err := defaultOptions(ctx, cfg)
	if err != nil {
		return nil, err
	}
	policy, err := parseWritePolicy(ctx)
	if err != nil {
		return nil, err
	}

	client := app.NewClient(cfg)
	metadata, metadataErrors := (MetadataResolver{ctx: ctx.Context, client: client, records: records, cfg: cfg}).resolve()
	return &Command{
		ctx:             ctx,
		cfg:             cfg,
		client:          client,
		records:         records,
		metadata:        metadata,
		metadataErrors:  metadataErrors,
		optionsResolver: OptionsResolver{defaults: defaultOptions, cfg: cfg},
		policy:          policy,
		continueOnError: ctx.Bool("continue-on-error"),
		summaryEnabled:  ctx.Bool("summary"),
		summaryOutput:   summaryOutput,
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
	region := recordRegion(record, command.cfg)
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
		logSkipped(command.cfg.Logger, "import", operation, policyAction, region, record.Path)
		command.summary.Skipped++
		return nil
	}

	parameterType, err := resolveType(command.ctx.String("default-type"), existing, exists, record, command.cfg)
	if err != nil {
		return command.handleRecordError(operation, region, record.Path, err)
	}
	options, err := command.optionsResolver.forRecord(record, existing, exists)
	if err != nil {
		return command.handleRecordError(operation, region, record.Path, err)
	}
	options.Overwrite = exists
	err = command.client.ForRegion(region).PutParameterWithOptions(
		command.ctx.Context,
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
	logRecordError(command.cfg.Logger, "import", operation, region, name, wrapped)
	command.summary.Failed++
	command.recordErrors = append(command.recordErrors, wrapped.Error())
	if command.continueOnError {
		logContinueOnError(command.cfg.Logger, "import", region, name)
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

func stdinReader() (io.Reader, error) {
	info, err := os.Stdin.Stat()
	if err == nil && info.Mode()&os.ModeCharDevice != 0 {
		return nil, errors.New("import requires data from stdin")
	}
	return os.Stdin, nil
}
