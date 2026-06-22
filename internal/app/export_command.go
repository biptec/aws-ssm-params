package app

import (
	"io"
	"os"
	"strings"

	"github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/filter"
	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/biptec/aws-ssm-params/internal/textio"
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
	writer       textio.Writer
	items        inventory.Items
	regions      []string
	output       io.Writer
	keyField     string
	scalarField  string
	recordFields textio.Fields
	sortRules    exportSortRules
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
			return nil, errors.Wrap(err, "list AWS regions")
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
		writer:       textio.NewWriter(textio.FormatType(ctx.String("format")), output),
		items:        items,
		regions:      regions,
		output:       output,
		keyField:     keyField,
		scalarField:  scalarField,
		recordFields: exportRecordFields(fields, scalarField, keyField),
		sortRules:    parseExportSortRules(cfg.SortColumns),
	}, nil
}

func (command *exportCommand) run() error {
	statuses := command.loadStatuses()
	command.sortRules.sort(statuses)
	records := command.records(statuses)
	if command.scalarField != "" {
		return errors.Wrap(
			command.writer.ExportScalar(records, command.scalarField, command.keyField),
			"write scalar export",
		)
	}
	mappings := command.cfg.FieldMappings.WithDefaults().ForFields(command.recordFields)
	return errors.Wrap(command.writer.Export(records, mappings, command.keyField), "write export")
}

func (command *exportCommand) loadStatuses() ui.Statuses {
	includeValues := command.cfg.WithDecryption ||
		command.recordFields.RequiresValues() ||
		command.cfg.FilterGroups.HasField(filter.FieldValue) ||
		command.sortRules.requiresValues()

	var statuses ui.Statuses
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
		return statuses.Filter(command.cfg.FilterGroups)
	}
	return statuses
}

func (command *exportCommand) records(statuses ui.Statuses) textio.Records {
	records := make(textio.Records, 0, len(statuses))
	for i := range statuses {
		if statuses[i].Exists {
			records = append(records, exportRecordFromStatus(statuses[i], command.recordFields))
		}
	}
	return records
}
