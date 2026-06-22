package app

import (
	"fmt"
	"io"
	"os"
	"strings"

	crerr "github.com/cockroachdb/errors"

	outputfmt "github.com/biptec/aws-ssm-params/internal/format"
	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/biptec/aws-ssm-params/internal/ui"
)

// Export loads statuses for the requested inventory and writes existing parameter values to stdout.
func Export(ctx *CLIContext) error {
	command, err := newExportCommand(ctx, os.Stdout)
	if err != nil {
		return err
	}
	return command.run()
}

// exportCommand owns the state and dependencies of one export invocation.
// Pure formatting and sorting helpers remain package functions; orchestration lives here.
type exportCommand struct {
	ctx          *CLIContext
	cfg          Config
	client       ssm.Client
	items        []inventory.Item
	regions      []string
	output       io.Writer
	format       string
	keyField     string
	scalarField  string
	recordFields []string
}

func newExportCommand(ctx *CLIContext, output io.Writer) (*exportCommand, error) {
	cfg, err := ConfigFromCLI(ctx)
	if err != nil {
		return nil, err
	}
	items, err := PrepareItems(ctx.Context, &cfg)
	if err != nil {
		return nil, err
	}
	client := NewClient(cfg)
	regions := append([]string(nil), cfg.Regions...)
	if cfg.AllRegions {
		regions, err = client.ListRegions(ctx.Context)
		if err != nil {
			return nil, crerr.Wrap(err, "list AWS regions")
		}
	}

	scalarField, err := scalarExportField(ctx, cfg)
	if err != nil {
		return nil, err
	}
	keyField := strings.TrimSpace(ctx.String("key-field"))
	if err := validateKeyFieldOutputFields(keyField, cfg.Fields); err != nil {
		return nil, err
	}
	fields := exportFields(cfg)
	return &exportCommand{
		ctx:          ctx,
		cfg:          cfg,
		client:       client,
		items:        items,
		regions:      regions,
		output:       output,
		format:       ctx.String("format"),
		keyField:     keyField,
		scalarField:  scalarField,
		recordFields: exportRecordFields(fields, scalarField, keyField),
	}, nil
}

func (command *exportCommand) run() error {
	statuses := command.loadStatuses()
	sortStatusesForExport(statuses, command.cfg.SortColumns)
	records := command.records(statuses)
	if command.scalarField != "" {
		return command.writeScalar(records)
	}
	return command.writeRecords(records)
}

func (command *exportCommand) loadStatuses() []ui.Status {
	includeValues := command.cfg.WithDecryption ||
		includeValuesForFields(command.recordFields) ||
		includeValuesForFilterGroups(command.cfg.FilterGroups) ||
		includeValuesForSortColumns(command.cfg.SortColumns)

	var statuses []ui.Status
	if len(command.cfg.FilterGroups) > 0 && len(command.items) == 0 {
		statuses = ui.LoadFilteredStatusesBatchForRegions(
			command.ctx.Context,
			command.client,
			command.cfg.FilterGroups,
			includeValues,
			command.regions,
			nil,
		)
	} else {
		statuses = ui.LoadStatusesForRegions(
			command.ctx.Context,
			command.client,
			command.items,
			includeValues,
			command.regions,
		)
	}
	if len(command.items) > 0 {
		return ui.FilterStatusesByGroups(statuses, command.cfg.FilterGroups)
	}
	return statuses
}

func (command *exportCommand) records(statuses []ui.Status) []outputfmt.Record {
	records := make([]outputfmt.Record, 0, len(statuses))
	for i := range statuses {
		if statuses[i].Exists {
			records = append(records, exportRecordFromStatus(statuses[i], command.recordFields))
		}
	}
	return records
}

func (command *exportCommand) writeScalar(records []outputfmt.Record) error {
	switch command.format {
	case "dotenv":
		return crerr.Wrap(outputfmt.ExportScalarLines(command.output, records, command.scalarField), "export scalar")
	case "json":
		return crerr.Wrap(outputfmt.ExportJSONScalar(command.output, records, command.scalarField, command.keyField), "export scalar JSON")
	case "yaml", "yml":
		return crerr.Wrap(outputfmt.ExportYAMLScalar(command.output, records, command.scalarField, command.keyField), "export scalar YAML")
	default:
		return fmt.Errorf("unsupported format: %s", command.format)
	}
}

func (command *exportCommand) writeRecords(records []outputfmt.Record) error {
	mappings := exportFieldMappings(command.recordFields, command.cfg.FieldMappings)
	switch command.format {
	case "dotenv":
		return crerr.Wrap(outputfmt.ExportDotenv(command.output, records), "export dotenv")
	case "json":
		return crerr.Wrap(outputfmt.ExportJSONMapped(command.output, records, mappings, command.keyField), "export JSON")
	case "yaml", "yml":
		return crerr.Wrap(outputfmt.ExportYAMLMapped(command.output, records, mappings, command.keyField), "export YAML")
	default:
		return fmt.Errorf("unsupported format: %s", command.format)
	}
}
