package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	crerr "github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/biptec/aws-ssm-params/internal/textio"
)

// Import reads dotenv, JSON, or YAML data, resolves each record to an SSM name, and writes values to Parameter Store.
func Import(ctx *CLIContext) error {
	command, err := newImportCommand(ctx, nil, os.Stderr)
	if err != nil {
		return err
	}
	return command.run()
}

// importCommand owns the mutable state and dependencies of one import invocation.
type importCommand struct {
	ctx             *CLIContext
	cfg             Config
	client          ssm.Client
	records         importRecords
	metadata        map[string]ssm.Metadata
	metadataErrors  map[string]error
	optionsResolver importOptionsResolver
	policy          writePolicy
	continueOnError bool
	summaryEnabled  bool
	summaryOutput   io.Writer
	summary         importSummary
	recordErrors    []string
}

type importSummary struct {
	Created int
	Updated int
	Skipped int
	Failed  int
}

func newImportCommand(ctx *CLIContext, input io.Reader, summaryOutput io.Writer) (*importCommand, error) {
	cfg, err := ConfigFromCLI(ctx)
	if err != nil {
		return nil, err
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
	if err := ensureRegions(ctx.Context, &cfg); err != nil {
		return nil, err
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
		return nil, crerr.Wrap(err, "read import")
	}
	records, err := importRecords(parsedRecords).withRootPath(ctx.String("root-path"))
	if err != nil {
		return nil, err
	}
	records = records.filter(cfg.FilterGroups)
	if err := cfg.requireField("value", "import"); err != nil {
		return nil, err
	}
	defaultOptions, err := importDefaultOptions(ctx, cfg)
	if err != nil {
		return nil, err
	}
	policy, err := parseWritePolicy(ctx)
	if err != nil {
		return nil, err
	}

	client := NewClient(cfg)
	metadata, metadataErrors := (importMetadataResolver{ctx: ctx.Context, client: client, records: records, cfg: cfg}).resolve()
	return &importCommand{
		ctx:             ctx,
		cfg:             cfg,
		client:          client,
		records:         records,
		metadata:        metadata,
		metadataErrors:  metadataErrors,
		optionsResolver: importOptionsResolver{defaults: defaultOptions, cfg: cfg},
		policy:          policy,
		continueOnError: ctx.Bool("continue-on-error"),
		summaryEnabled:  ctx.Bool("summary"),
		summaryOutput:   summaryOutput,
	}, nil
}

func (command *importCommand) run() error {
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

func (command *importCommand) processRecord(record textio.Record) error {
	region := recordRegion(record, command.cfg)
	key := recordKey(region, record.Path)
	existing, exists := command.metadata[key]
	if err, ok := command.metadataErrors[key]; ok {
		if !crerr.Is(err, ssm.ErrNotFound) {
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

	parameterType, err := resolveImportType(command.ctx.String("default-type"), existing, exists, record, command.cfg)
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

func (command *importCommand) handleRecordError(operation writeOperation, region, name string, err error) error {
	wrapped := crerr.Wrap(err, name)
	logRecordError(command.cfg.Logger, "import", operation, region, name, wrapped)
	command.summary.Failed++
	command.recordErrors = append(command.recordErrors, wrapped.Error())
	if command.continueOnError {
		logContinueOnError(command.cfg.Logger, "import", region, name)
		return nil
	}
	return wrapped
}

func (command *importCommand) writeSummary() {
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
