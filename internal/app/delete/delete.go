// Package deleter implements the non-interactive delete command.
package deleter

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/app"
	"github.com/biptec/aws-ssm-params/internal/prompt"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/biptec/aws-ssm-params/internal/textio"
)

// Options contains the complete runtime configuration for one delete command.
type Options struct {
	*app.Options

	Format        textio.FormatType
	FieldMappings textio.FieldMappings
	KeyField      string
	BasePath      app.BasePath
	NoConfirm     bool
	DryRun        bool
}

type dependencies struct {
	newClient    func(ssm.ClientConfig) ssm.Client
	openTerminal func() (*prompt.Terminal, error)
}

// runner owns the normalized deletion candidates and command dependencies.
type runner struct {
	opts         *Options
	client       ssm.Client
	records      app.Records
	output       io.Writer
	openTerminal func() (*prompt.Terminal, error)
}

type confirmationDecision string

const (
	decisionYes     confirmationDecision = "yes"
	decisionSkip    confirmationDecision = "skip"
	decisionCancel  confirmationDecision = "cancel"
	decisionDetails confirmationDecision = "details"
)

// Run parses stdin, applies optional filters, and deletes the resulting
// region/name identities from Parameter Store.
func Run(ctx context.Context, opts *Options, input io.Reader, output io.Writer) error {
	return runWithDependencies(ctx, opts, input, output, dependencies{
		newClient:    ssm.NewClient,
		openTerminal: prompt.Open,
	})
}

func runWithDependencies(ctx context.Context, opts *Options, input io.Reader, output io.Writer, deps dependencies) error {
	commandRunner, err := newRunner(ctx, opts, input, output, deps)
	if err != nil {
		return err
	}

	return commandRunner.run(ctx)
}

func newRunner(ctx context.Context, opts *Options, input io.Reader, output io.Writer, deps dependencies) (*runner, error) {
	if opts == nil || opts.Options == nil {
		return nil, errors.New("delete options are required")
	}

	if input == nil {
		return nil, errors.New("delete input is required")
	}

	if output == nil {
		output = io.Discard
	}

	if err := validateInputFields(opts.FieldMappings, opts.KeyField); err != nil {
		return nil, err
	}

	parsed, err := textio.NewReader(opts.Format, input).Import(
		opts.FieldMappings.WithDefaults(),
		opts.KeyField,
	)
	if err != nil {
		return nil, errors.Wrap(err, "read delete input")
	}

	if len(parsed) == 0 {
		return nil, errors.New("delete input contains no parameters")
	}

	records, err := app.Records(parsed).ResolveNames(opts.BasePath)
	if err != nil {
		return nil, errors.Wrap(err, "resolve delete parameter names")
	}

	defaultRegions, err := resolveDefaultRegions(ctx, opts, records, deps.newClient)
	if err != nil {
		return nil, err
	}

	records, err = records.ExpandRegions(defaultRegions)
	if err != nil {
		return nil, errors.Wrap(err, "resolve delete parameter regions")
	}

	records = records.Filter(opts.FilterGroups).UniqueByIdentity()
	records.SortByIdentity()

	client := deps.newClient(ssm.ClientConfig{
		Profile: opts.Profile,
		Region:  opts.Region,
		Logger:  opts.Logger,
	})

	return &runner{
		opts:         opts,
		client:       client,
		records:      records,
		output:       output,
		openTerminal: deps.openTerminal,
	}, nil
}

func resolveDefaultRegions(ctx context.Context, opts *Options, records app.Records, newClient func(ssm.ClientConfig) ssm.Client) ([]string, error) {
	if !records.HasMissingRegion() {
		return nil, nil
	}

	if err := opts.PrepareRegions(ctx); err != nil {
		return nil, errors.Wrap(err, "prepare delete regions")
	}

	if !opts.AllRegions {
		return append([]string(nil), opts.Regions...), nil
	}

	client := newClient(ssm.ClientConfig{
		Profile: opts.Profile,
		Region:  opts.Region,
		Logger:  opts.Logger,
	})

	regions, err := client.ListRegions(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "list AWS regions")
	}

	return regions, nil
}

func validateInputFields(mappings textio.FieldMappings, keyField string) error {
	switch keyField {
	case "", textio.FieldName, textio.FieldRegion:
	default:
		return fmt.Errorf("delete key field must be %q or %q", textio.FieldName, textio.FieldRegion)
	}

	seenFileFields := make(map[string]string, len(mappings))
	for _, mapping := range mappings {
		if mapping.AWSName != textio.FieldName && mapping.AWSName != textio.FieldRegion {
			return fmt.Errorf(
				"delete field mappings support only %q and %q",
				textio.FieldName,
				textio.FieldRegion,
			)
		}

		fileField := strings.TrimSpace(mapping.FileName)
		if awsField, exists := seenFileFields[fileField]; exists && awsField != mapping.AWSName {
			return fmt.Errorf("file field %q is mapped to both %s and %s", fileField, awsField, mapping.AWSName)
		}

		seenFileFields[fileField] = mapping.AWSName
	}

	return nil
}

func (commandRunner *runner) run(ctx context.Context) error {
	if _, err := fmt.Fprintf(commandRunner.output, "Найдено %d параметров.\n", len(commandRunner.records)); err != nil {
		return errors.Wrap(err, "write delete candidate count")
	}

	if len(commandRunner.records) == 0 {
		return nil
	}

	if commandRunner.opts.DryRun {
		return commandRunner.writeDryRun()
	}

	if commandRunner.opts.NoConfirm {
		if err := commandRunner.deleteRecords(ctx, commandRunner.records); err != nil {
			return err
		}

		_, err := fmt.Fprintf(commandRunner.output, "Удалено %d параметров.\n", len(commandRunner.records))

		return errors.Wrap(err, "write delete summary")
	}

	return commandRunner.runInteractive(ctx)
}

func (commandRunner *runner) writeDryRun() error {
	for idx := range commandRunner.records {
		record := &commandRunner.records[idx]
		if _, err := fmt.Fprintf(
			commandRunner.output,
			"DRY-RUN: удалить параметр %s из %s.\n",
			record.Path,
			record.Region,
		); err != nil {
			return errors.Wrap(err, "write delete dry-run output")
		}
	}

	return nil
}

func (commandRunner *runner) runInteractive(ctx context.Context) error {
	terminal, err := commandRunner.openTerminal()
	if err != nil {
		return err
	}
	defer func() { _ = terminal.Close() }()

	deleted := 0
	skipped := 0

	for idx := range commandRunner.records {
		record := &commandRunner.records[idx]

		for {
			decision, err := readConfirmation(terminal, record)
			if err != nil {
				return err
			}

			switch decision {
			case decisionYes:
				if err := commandRunner.deleteRecords(ctx, app.Records{*record}); err != nil {
					return err
				}

				deleted++
			case decisionSkip:
				skipped++
			case decisionCancel:
				_, err := fmt.Fprintf(
					commandRunner.output,
					"Удаление отменено. Удалено: %d, пропущено: %d.\n",
					deleted,
					skipped,
				)

				return errors.Wrap(err, "write delete cancellation summary")
			case decisionDetails:
				if err := terminal.Writef("%s", recordDetails(record)); err != nil {
					return errors.Wrap(err, "write parameter details")
				}

				continue
			}

			break
		}
	}

	_, err = fmt.Fprintf(
		commandRunner.output,
		"Удалено: %d, пропущено: %d.\n",
		deleted,
		skipped,
	)

	return errors.Wrap(err, "write delete summary")
}

func readConfirmation(terminal *prompt.Terminal, record *textio.Record) (confirmationDecision, error) {
	for {
		answer, err := terminal.ReadLine(fmt.Sprintf(
			"Удалить параметр %s из %s? (y)es, (s)kip, (c)ancel, (d)etails: ",
			record.Path,
			record.Region,
		))
		if err != nil {
			return "", errors.Wrap(err, "read delete confirmation")
		}

		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "y", "yes":
			return decisionYes, nil
		case "s", "skip":
			return decisionSkip, nil
		case "c", "cancel", "":
			return decisionCancel, nil
		case "d", "details":
			return decisionDetails, nil
		}
	}
}

func (commandRunner *runner) deleteRecords(ctx context.Context, records app.Records) error {
	targets := make([]ssm.DeleteTarget, 0, len(records))
	for idx := range records {
		targets = append(targets, ssm.DeleteTarget{
			Name:   records[idx].Path,
			Region: records[idx].Region,
		})
	}

	if err := ssm.NewDeleter(commandRunner.client).Delete(ctx, targets); err != nil {
		return errors.Wrap(err, "delete parameters")
	}

	return nil
}

func recordDetails(record *textio.Record) string {
	var details strings.Builder

	appendDetail(&details, textio.FieldName, record.Path)
	appendDetail(&details, textio.FieldRegion, record.Region)
	appendDetail(&details, textio.FieldType, record.Type)
	appendDetail(&details, textio.FieldTier, record.Tier)
	appendDetail(&details, textio.FieldDataType, record.DataType)
	appendDetail(&details, textio.FieldDescription, record.Description)
	appendDetail(&details, textio.FieldPolicies, record.Policies)
	appendDetail(&details, textio.FieldDate, record.Date)

	if record.Version != 0 {
		appendDetail(&details, textio.FieldVersion, strconv.FormatInt(record.Version, 10))
	}

	if record.Len != 0 {
		appendDetail(&details, textio.FieldLen, strconv.Itoa(record.Len))
	}

	appendDetail(&details, textio.FieldSHA256, record.SHA256)
	appendDetail(&details, textio.FieldUser, record.User)

	if record.HasField(textio.FieldValue) {
		appendDetail(&details, textio.FieldValue, "[скрыто]")
	}

	return details.String()
}

func appendDetail(details *strings.Builder, field, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}

	_, _ = fmt.Fprintf(details, "  %s: %s\n", field, value)
}
