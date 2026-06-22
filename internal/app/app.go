// Package app contains command implementations and configuration parsing for aws-ssm-params.
package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"unicode"

	crerr "github.com/cockroachdb/errors"
	"github.com/urfave/cli/v3"

	"github.com/biptec/aws-ssm-params/internal/fileio"
	"github.com/biptec/aws-ssm-params/internal/filter"
	secretfmt "github.com/biptec/aws-ssm-params/internal/format"
	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/logging"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/biptec/aws-ssm-params/internal/ui"
)

// CLIContext is the small adapter used by application code so the business layer is not coupled
// to urfave/cli internals.
type CLIContext struct {
	Context context.Context
	Command *cli.Command
}

// NewCLIContext adapts a urfave/cli/v3 command invocation for application code.
func NewCLIContext(ctx context.Context, cmd *cli.Command) *CLIContext {
	return &CLIContext{Context: ctx, Command: cmd}
}

// String returns a string flag value by name.
func (ctx *CLIContext) String(name string) string { return ctx.Command.String(name) }

// Bool returns a boolean flag value by name.
func (ctx *CLIContext) Bool(name string) bool { return ctx.Command.Bool(name) }

// StringSlice returns a repeated string flag value by name.
func (ctx *CLIContext) StringSlice(name string) []string { return ctx.Command.StringSlice(name) }

// IsSet reports whether a flag was set explicitly by the user.
func (ctx *CLIContext) IsSet(name string) bool { return ctx.Command.IsSet(name) }

// Args returns positional command arguments.
func (ctx *CLIContext) Args() cli.Args { return ctx.Command.Args() }

// Config is the normalized runtime configuration shared by all commands.
// It is built from CLI flags plus AWS-related environment variables before command-specific code runs.
// Region stores the primary/default AWS region, while Regions stores every explicitly requested region;
// AllRegions tells the rest of the app to discover enabled AWS regions and mark inventory items as regional wildcards.
type Config struct {
	Logger                    *slog.Logger
	FiltersFile               string
	FilterGroups              []filter.Group
	FieldMappings             []secretfmt.FieldMapping
	Fields                    []string
	InventoryItems            []inventory.Item
	Region                    string
	Regions                   []string
	Profile                   string
	AllRegions                bool
	NoColor                   bool
	WithDecryption            bool
	Keymap                    string
	ShowColumns               []string
	SortColumns               []string
	NoConfirmOverwriteFile    bool
	NoConfirmWriteSecureValue bool
	NoConfirmDeleteOne        bool
	NoConfirmDeleteAll        bool
}

const allRegionsSeedRegion = "us-east-1"

var supportedFields = map[string]string{
	"name":               "name",
	"region":             "region",
	"type":               "type",
	"tier":               "tier",
	"data-type":          "data-type",
	"datatype":           "data-type",
	"data_type":          "data-type",
	"policies":           "policies",
	"description":        "description",
	"value":              "value",
	"date":               "date",
	"modified":           "date",
	"last-modified-date": "date",
	"version":            "version",
	"len":                "len",
	"length":             "len",
	"sha256":             "sha256",
	"hash":               "sha256",
	"user":               "user",
}

// RunWithLogging configures command logging, executes action, and flushes buffered terminal logs.
func RunWithLogging(ctx *CLIContext, bufferTerminal bool, action func(*CLIContext) error) error {
	logCfg := loggingConfigFromCLI(ctx)
	logger, flush, err := logging.New(logCfg, bufferTerminal)
	if err != nil {
		return crerr.Wrap(err, "configure logging")
	}
	ctx.Context = logging.WithLogger(ctx.Context, logger)
	err = action(ctx)
	if err != nil {
		commandName := ""
		if ctx.Command != nil {
			commandName = ctx.Command.Name
		}
		logger.Error("command failed", "command", commandName, "error", err)
	}
	if flushErr := flush(); flushErr != nil && err == nil {
		return flushErr
	}
	return err
}

func loggingConfigFromCLI(ctx *CLIContext) logging.Config {
	return logging.Config{Level: stringFlagValueAny(ctx, "log-level", logging.DefaultLevel, "AWS_SSM_PARAMS_LOG_LEVEL")}
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func dedupeStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func splitCommaEnv(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return compactStrings(strings.Split(value, ","))
}

func stringSliceFlagValue(ctx *CLIContext, name string, envNames ...string) []string {
	values := compactStrings(ctx.StringSlice(name))
	if len(values) > 0 {
		return values
	}
	for _, envName := range envNames {
		if envValues := splitCommaEnv(os.Getenv(envName)); len(envValues) > 0 {
			return envValues
		}
	}
	return nil
}

func stringFlagValueAny(ctx *CLIContext, name, defaultValue string, envNames ...string) string {
	if ctx.IsSet(name) {
		return ctx.String(name)
	}
	for _, envName := range envNames {
		if value := os.Getenv(envName); strings.TrimSpace(value) != "" {
			return value
		}
	}
	return defaultValue
}

func boolFlagValueAny(ctx *CLIContext, name string, defaultValue bool, envNames ...string) bool {
	if ctx.IsSet(name) {
		return ctx.Bool(name)
	}
	for _, envName := range envNames {
		value := strings.ToLower(strings.TrimSpace(os.Getenv(envName)))
		switch value {
		case "1", "t", "true", "yes", "y", "on":
			return true
		case "0", "f", "false", "no", "n", "off":
			return false
		}
	}
	return defaultValue
}

func parseOutputFields(values []string) ([]string, error) {
	parts := compactStrings(values)
	if len(parts) == 0 {
		return nil, nil
	}
	seen := map[string]bool{}
	fields := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.Contains(part, ",") {
			return nil, fmt.Errorf("--output-field accepts one value per flag; repeat --output-field instead of using commas")
		}
		canonical, ok := supportedFields[strings.ToLower(strings.TrimSpace(part))]
		if !ok {
			return nil, fmt.Errorf("unsupported --output-field value %q", part)
		}
		if seen[canonical] {
			continue
		}
		seen[canonical] = true
		fields = append(fields, canonical)
	}
	return fields, nil
}

func parseMapFields(values []string) ([]secretfmt.FieldMapping, error) {
	parts := compactStrings(values)
	if len(parts) == 0 {
		return nil, nil
	}
	seen := map[string]bool{}
	mappings := make([]secretfmt.FieldMapping, 0, len(parts))
	for _, part := range parts {
		if strings.Contains(part, ",") {
			return nil, fmt.Errorf("--map-field accepts one value per flag; repeat --map-field instead of using commas")
		}
		awsField, fileField, ok := strings.Cut(part, ":")
		if !ok {
			return nil, fmt.Errorf("--map-field requires aws_field:file_field")
		}
		canonical, ok := supportedFields[strings.ToLower(strings.TrimSpace(awsField))]
		if !ok {
			return nil, fmt.Errorf("unsupported --map-field AWS field %q", awsField)
		}
		fileField = strings.TrimSpace(fileField)
		if fileField == "" {
			return nil, fmt.Errorf("field mapping %q has empty file field", part)
		}
		if seen[canonical] {
			continue
		}
		seen[canonical] = true
		mappings = append(mappings, secretfmt.FieldMapping{AWSName: canonical, FileName: fileField})
	}
	return mappings, nil
}

func parseFilterGroups(values []string, filtersFile string) ([]filter.Group, error) {
	groups, err := filter.ParseGroups(compactStrings(values))
	if err != nil {
		return nil, crerr.Wrap(err, "parse filters")
	}
	if filtersFile == "" {
		return groups, nil
	}
	file, err := fileio.Open(filtersFile)
	if err != nil {
		return nil, crerr.Wrapf(err, "open filters file %s", filtersFile)
	}
	defer func() { _ = file.Close() }()
	fileGroups, err := filter.ParseFile(file)
	if err != nil {
		return nil, crerr.Wrapf(err, "parse filters file %s", filtersFile)
	}
	return append(groups, fileGroups...), nil
}

func fieldAllowed(fields []string, field string) bool {
	if len(fields) == 0 || field == "name" {
		return true
	}
	for _, candidate := range fields {
		if candidate == field {
			return true
		}
	}
	return false
}

func includeValuesForFields(fields []string) bool {
	if len(fields) == 0 {
		return true
	}
	for _, field := range fields {
		switch field {
		case "value", "len", "sha256", "version":
			return true
		}
	}
	return false
}

func includeValuesForFilterGroups(groups []filter.Group) bool {
	for _, group := range groups {
		if group.HasField(filter.FieldValue) {
			return true
		}
	}
	return false
}

// RejectCommaSeparatedFlagArgs reports an error when repeated CLI flags are given comma-separated values.
// Environment variables may still use comma-separated lists; this function inspects raw command-line arguments only.
func RejectCommaSeparatedFlagArgs(args []string, flagNames ...string) error {
	flags := map[string]bool{}
	for _, flagName := range flagNames {
		flags["--"+flagName] = true
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if name, value, ok := strings.Cut(arg, "="); ok && flags[name] && strings.Contains(value, ",") {
			return fmt.Errorf("%s accepts one value per flag; repeat the flag instead of using commas", name)
		}
		if flags[arg] && i+1 < len(args) && strings.Contains(args[i+1], ",") {
			return fmt.Errorf("%s accepts one value per flag; repeat the flag instead of using commas", arg)
		}
	}
	return nil
}

// ConfigFromCLI converts raw urfave/cli state into a Config that the rest of the application can use.
// CLI list values are provided by repeating flags; only environment variables may contain comma-separated lists.
func ConfigFromCLI(ctx *CLIContext) (Config, error) {
	allRegions := boolFlagValueAny(ctx, "all-regions", false, "AWS_SSM_PARAMS_ALL_REGIONS")
	regions := dedupeStrings(stringSliceFlagValue(ctx, "region", "AWS_SSM_PARAMS_REGION"))
	if allRegions && len(regions) > 0 {
		return Config{}, errors.New("--region and --all-regions cannot be used together")
	}
	profile := stringFlagValueAny(ctx, "profile", "", "AWS_SSM_PARAMS_PROFILE")
	region := ""
	if len(regions) > 0 {
		region = regions[0]
	} else if !allRegions {
		region = os.Getenv("AWS_REGION")
		if region == "" {
			region = os.Getenv("AWS_DEFAULT_REGION")
		}
		if region != "" {
			regions = []string{region}
		}
	}

	keymap := strings.ToLower(strings.TrimSpace(stringFlagValueAny(ctx, "keymap", "emacs", "AWS_SSM_PARAMS_KEYMAP")))
	if keymap == "" {
		keymap = "emacs"
	}
	if keymap != "emacs" && keymap != "vi" {
		return Config{}, fmt.Errorf("unsupported --keymap %q; expected emacs or vi", keymap)
	}

	fieldMappings, err := parseMapFields(stringSliceFlagValue(ctx, "map-field", "AWS_SSM_PARAMS_MAP_FIELD"))
	if err != nil {
		return Config{}, err
	}
	fields, err := parseOutputFields(stringSliceFlagValue(ctx, "output-field", "AWS_SSM_PARAMS_OUTPUT_FIELD"))
	if err != nil {
		return Config{}, err
	}
	showColumns, err := ui.ParseColumnOption(strings.Join(stringSliceFlagValue(ctx, "show-column", "AWS_SSM_PARAMS_SHOW_COLUMN"), ","))
	if err != nil {
		return Config{}, crerr.Wrap(err, "parse show columns")
	}
	filtersFile := strings.TrimSpace(stringFlagValueAny(ctx, "filters-file", "", "AWS_SSM_PARAMS_FILTER_FILE"))
	filterGroups, err := parseFilterGroups(stringSliceFlagValue(ctx, "filter", "AWS_SSM_PARAMS_FILTER"), filtersFile)
	if err != nil {
		return Config{}, err
	}

	return Config{
		Logger:                    logging.FromContext(ctx.Context),
		FiltersFile:               filtersFile,
		FilterGroups:              filterGroups,
		FieldMappings:             fieldMappings,
		Fields:                    fields,
		Region:                    region,
		Regions:                   regions,
		Profile:                   profile,
		AllRegions:                allRegions,
		NoColor:                   boolFlagValueAny(ctx, "no-color", false, "AWS_SSM_PARAMS_NO_COLOR"),
		WithDecryption:            boolFlagValueAny(ctx, "with-decryption", false, "AWS_SSM_PARAMS_WITH_DECRYPTION"),
		Keymap:                    keymap,
		ShowColumns:               showColumns,
		SortColumns:               compactStrings(stringSliceFlagValue(ctx, "sort-by", "AWS_SSM_PARAMS_SORT_BY")),
		NoConfirmOverwriteFile:    boolFlagValueAny(ctx, "no-confirm-overwrite-file", false, "AWS_SSM_PARAMS_NO_CONFIRM_OVERWRITE_FILE"),
		NoConfirmWriteSecureValue: boolFlagValueAny(ctx, "no-confirm-write-securestring", false, "AWS_SSM_PARAMS_NO_CONFIRM_WRITE_SECURESTRING"),
		NoConfirmDeleteOne:        boolFlagValueAny(ctx, "no-confirm-delete-one", false, "AWS_SSM_PARAMS_NO_CONFIRM_DELETE_ONE"),
		NoConfirmDeleteAll:        boolFlagValueAny(ctx, "no-confirm-delete-all", false, "AWS_SSM_PARAMS_NO_CONFIRM_DELETE_ALL"),
	}, nil
}

// NewClient creates the concrete AWS SSM client for the selected profile and primary region.
// Keeping this in one function makes command handlers independent from the AWS SDK implementation details.
func NewClient(cfg Config) ssm.Client {
	client := ssm.NewAWSClient(cfg.Profile, cfg.Region)
	client.WithDecryption = cfg.WithDecryption
	client.Logger = cfg.Logger
	return client
}

// LoadItems builds explicit inventory items from configured sources.
// When no inventory is configured, callers can still discover parameters directly from AWS SSM.
func LoadItems(cfg Config) ([]inventory.Item, error) {
	seen := map[string]bool{}
	items := []inventory.Item{}
	add := func(item inventory.Item) {
		item.Path = strings.TrimSpace(item.Path)
		if item.Path == "" || seen[item.Path] {
			return
		}
		seen[item.Path] = true
		items = append(items, item)
	}

	for _, item := range cfg.InventoryItems {
		add(item)
	}
	return items, nil
}

// PrepareItems resolves regions and explicit names for commands that load SSM parameters.
func PrepareItems(ctx context.Context, cfg *Config) ([]inventory.Item, error) {
	items, err := LoadItems(*cfg)
	if err != nil {
		return nil, err
	}
	if cfg.AllRegions {
		ensureAllRegionsSeedRegion(cfg)
		return applyInventoryRegion(items, "*"), nil
	}
	if err := ensureRegions(ctx, cfg); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	if len(cfg.Regions) > 1 {
		return applyInventoryRegion(items, "*"), nil
	}
	return applyInventoryRegion(items, cfg.Region), nil
}

// ensureRegions guarantees that non-all-regions commands have one usable AWS region.
// It first asks the AWS SDK profile configuration if CLI/env flags did not provide a region, then mirrors the
// resolved primary region into cfg.Regions when the user did not pass an explicit list.
func ensureRegions(ctx context.Context, cfg *Config) error {
	if cfg.AllRegions {
		return nil
	}
	if cfg.Region == "" {
		cfg.Region = ssm.ResolveConfiguredRegion(ctx, cfg.Profile)
	}
	if cfg.Region == "" {
		return errors.New("AWS region is required; pass --region, set AWS_REGION/AWS_DEFAULT_REGION, or configure a default region in the AWS profile")
	}
	if len(cfg.Regions) == 0 {
		cfg.Regions = []string{cfg.Region}
	}
	return nil
}

// ensureAllRegionsSeedRegion sets a safe seed region for AWS API calls that are needed before per-region scanning.
// Listing AWS regions itself requires a region-aware AWS SDK call, so all-regions mode uses us-east-1 by default.
func ensureAllRegionsSeedRegion(cfg *Config) {
	if cfg.Region == "" {
		cfg.Region = allRegionsSeedRegion
	}
}

func applyInventoryRegion(items []inventory.Item, region string) []inventory.Item {
	if len(items) == 0 {
		return nil
	}
	out := make([]inventory.Item, len(items))
	copy(out, items)
	for i := range out {
		if out[i].Region == "" {
			out[i].Region = region
		}
	}
	return out
}

func filterRecordsByGroups(records []secretfmt.Record, groups []filter.Group) []secretfmt.Record {
	if len(groups) == 0 {
		return records
	}
	out := make([]secretfmt.Record, 0, len(records))
	for i := range records {
		if filter.MatchAny(groups, filter.Record{
			Name:        records[i].Path,
			Region:      records[i].Region,
			Type:        records[i].Type,
			Tier:        records[i].Tier,
			DataType:    records[i].DataType,
			Description: records[i].Description,
			Policies:    records[i].Policies,
			Value:       records[i].Value,
		}) {
			out = append(out, records[i])
		}
	}
	return out
}

func requireFieldForCommand(cfg Config, field, command string) error {
	if !fieldAllowed(cfg.Fields, field) {
		return fmt.Errorf("%s requires field %q; remove --output-field restrictions or include %s", command, field, field)
	}
	return nil
}

func recordHasField(record secretfmt.Record, field string) bool {
	for _, candidate := range record.Fields {
		if candidate == field {
			return true
		}
	}
	return len(record.Fields) == 0
}

func importDefaultOptions(ctx *CLIContext, cfg Config) (ssm.PutParameterOptions, error) {
	tier, err := ssm.ParseParameterTier(ctx.String("default-tier"))
	if err != nil {
		return ssm.PutParameterOptions{}, crerr.Wrap(err, "parse default tier")
	}
	dataType, err := ssm.ParseParameterDataType(ctx.String("default-data-type"))
	if err != nil {
		return ssm.PutParameterOptions{}, crerr.Wrap(err, "parse default data type")
	}
	policies := ctx.String("default-policies")
	if policiesFile := strings.TrimSpace(ctx.String("default-policies-file")); policiesFile != "" {
		data, err := fileio.ReadFile(policiesFile)
		if err != nil {
			return ssm.PutParameterOptions{}, crerr.Wrapf(err, "read default policies file %s", policiesFile)
		}
		policies = string(data)
	}
	opts := ssm.PutParameterOptions{}
	if fieldAllowed(cfg.Fields, "tier") {
		opts.Tier = tier
	}
	if fieldAllowed(cfg.Fields, "data-type") {
		opts.DataType = dataType
	}
	if fieldAllowed(cfg.Fields, "description") {
		opts.Description = ctx.String("default-description")
	}
	if fieldAllowed(cfg.Fields, "policies") {
		opts.Policies = policies
	}
	return opts, nil
}

func importOptionsForRecord(record secretfmt.Record, cloud ssm.Metadata, exists bool, defaults ssm.PutParameterOptions, cfg Config) (ssm.PutParameterOptions, error) {
	opts := defaults
	if exists {
		if fieldAllowed(cfg.Fields, "tier") && strings.TrimSpace(cloud.Tier) != "" {
			tier, err := ssm.ParseParameterTier(cloud.Tier)
			if err != nil {
				return ssm.PutParameterOptions{}, crerr.Wrap(err, "parse cloud tier")
			}
			opts.Tier = tier
		}
		if fieldAllowed(cfg.Fields, "data-type") && strings.TrimSpace(cloud.DataType) != "" {
			dataType, err := ssm.ParseParameterDataType(cloud.DataType)
			if err != nil {
				return ssm.PutParameterOptions{}, crerr.Wrap(err, "parse cloud data type")
			}
			opts.DataType = dataType
		}
		if fieldAllowed(cfg.Fields, "description") {
			opts.Description = cloud.Description
		}
		if fieldAllowed(cfg.Fields, "policies") {
			opts.Policies = cloud.Policies
		}
	}
	if fieldAllowed(cfg.Fields, "tier") && recordHasField(record, "tier") && strings.TrimSpace(record.Tier) != "" {
		tier, err := ssm.ParseParameterTier(record.Tier)
		if err != nil {
			return ssm.PutParameterOptions{}, crerr.Wrap(err, "parse record tier")
		}
		opts.Tier = tier
	}
	if fieldAllowed(cfg.Fields, "data-type") && recordHasField(record, "data-type") && strings.TrimSpace(record.DataType) != "" {
		dataType, err := ssm.ParseParameterDataType(record.DataType)
		if err != nil {
			return ssm.PutParameterOptions{}, crerr.Wrap(err, "parse record data type")
		}
		opts.DataType = dataType
	}
	if fieldAllowed(cfg.Fields, "description") && recordHasField(record, "description") && strings.TrimSpace(record.Description) != "" {
		opts.Description = record.Description
	}
	if fieldAllowed(cfg.Fields, "policies") && recordHasField(record, "policies") {
		if strings.TrimSpace(record.Policies) == "" {
			opts.Policies = "[{}]"
			opts.PoliciesSet = true
		} else {
			opts.Policies = record.Policies
		}
	}
	return opts, nil
}

// Interactive opens the terminal UI.
func Interactive(ctx *CLIContext) error {
	cfg, err := ConfigFromCLI(ctx)
	if err != nil {
		return err
	}
	stdinItems, useTTYInput, err := loadInteractiveInventoryFromStdin()
	if err != nil {
		return err
	}
	cfg.InventoryItems = append(cfg.InventoryItems, stdinItems...)
	items, err := PrepareItems(ctx.Context, &cfg)
	if err != nil {
		return err
	}
	client := NewClient(cfg)
	if err := client.CheckAccess(ctx.Context); err != nil {
		return crerr.Wrap(err, "check AWS access")
	}
	regionLabel := cfg.Region
	regions := append([]string(nil), cfg.Regions...)
	if cfg.AllRegions {
		regionLabel = "all regions"
		regions, err = client.ListRegions(ctx.Context)
		if err != nil {
			return crerr.Wrap(err, "list AWS regions")
		}
	} else if len(regions) > 1 {
		regionLabel = strings.Join(regions, ", ")
	}
	return crerr.Wrap(ui.RunInteractive(ctx.Context, client, items, ui.Options{
		Region:                    regionLabel,
		Regions:                   regions,
		Profile:                   cfg.Profile,
		FilterGroups:              cfg.FilterGroups,
		NoColor:                   cfg.NoColor,
		Keymap:                    cfg.Keymap,
		ShowColumns:               cfg.ShowColumns,
		Sort:                      cfg.SortColumns,
		Fields:                    cfg.Fields,
		IncludeValues:             cfg.WithDecryption || includeValuesForFields(cfg.Fields) || includeValuesForFilterGroups(cfg.FilterGroups),
		ShowSecureValues:          cfg.WithDecryption,
		NoConfirmOverwriteFile:    cfg.NoConfirmOverwriteFile,
		NoConfirmWriteSecureValue: cfg.NoConfirmWriteSecureValue,
		NoConfirmDeleteOne:        cfg.NoConfirmDeleteOne,
		NoConfirmDeleteAll:        cfg.NoConfirmDeleteAll,
		UseInputTTY:               useTTYInput,
	}), "run interactive")
}

func loadInteractiveInventoryFromStdin() ([]inventory.Item, bool, error) {
	info, err := os.Stdin.Stat()
	if err != nil {
		return nil, false, crerr.Wrap(err, "stat stdin")
	}
	if info.Mode()&os.ModeCharDevice != 0 {
		return nil, false, nil
	}

	items, err := inventory.LoadPaths(os.Stdin, "stdin")
	if err != nil {
		return nil, true, crerr.Wrap(err, "load TUI inventory from stdin")
	}
	return items, true, nil
}

type writePolicyAction string

const (
	writePolicyDefault writePolicyAction = ""
	writePolicySkip    writePolicyAction = "skip"
	writePolicyError   writePolicyAction = "error"
	writePolicyAsk     writePolicyAction = "ask"
)

type writePolicy struct {
	OnCreate writePolicyAction
	OnUpdate writePolicyAction
}

type writeOperation string

const (
	writeOperationCreate writeOperation = "create"
	writeOperationUpdate writeOperation = "update"
)

type importSummary struct {
	Created int
	Updated int
	Skipped int
	Failed  int
}

type strictMetadataDescriber interface {
	DescribeManyStrict(context.Context, []string) (map[string]ssm.Metadata, map[string]error)
}

func parseWritePolicy(ctx *CLIContext) (writePolicy, error) {
	onCreate, err := parseWritePolicyAction(ctx.String("on-create"), "on-create")
	if err != nil {
		return writePolicy{}, err
	}
	onUpdate, err := parseWritePolicyAction(ctx.String("on-update"), "on-update")
	if err != nil {
		return writePolicy{}, err
	}
	return writePolicy{OnCreate: onCreate, OnUpdate: onUpdate}, nil
}

func parseWritePolicyAction(value, flagName string) (writePolicyAction, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return writePolicyDefault, nil
	case "skip":
		return writePolicySkip, nil
	case "error":
		return writePolicyError, nil
	case "ask":
		return writePolicyAsk, nil
	default:
		return "", fmt.Errorf("unsupported --%s value %q; use skip, error, or ask", flagName, value)
	}
}

func recordKey(region, path string) string {
	return strings.TrimSpace(region) + "\x00" + strings.TrimSpace(path)
}

func metadataForPaths(ctx context.Context, client ssm.Client, paths []string) (metadataByPath map[string]ssm.Metadata, errorsByPath map[string]error) {
	if describer, ok := client.(strictMetadataDescriber); ok {
		return describer.DescribeManyStrict(ctx, paths)
	}
	metas := client.DescribeMany(ctx, paths)
	errs := map[string]error{}
	for _, path := range paths {
		if _, ok := metas[path]; !ok {
			errs[path] = ssm.ErrNotFound
		}
	}
	return metas, errs
}

func askWriteConfirmation(action writeOperation, region, name string) (bool, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false, errors.New("ask requires an interactive terminal")
	}
	defer func() { _ = tty.Close() }()

	questionAction := "Create"
	if action == writeOperationUpdate {
		questionAction = "Update"
	}
	if region != "" {
		_, _ = fmt.Fprintf(tty, "%s parameter %s in %s? [y/N] ", questionAction, name, region)
	} else {
		_, _ = fmt.Fprintf(tty, "%s parameter %s? [y/N] ", questionAction, name)
	}
	answer, err := bufio.NewReader(tty).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, crerr.Wrap(err, "read write confirmation")
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}

func resolveWritePolicy(action writePolicyAction, operation writeOperation, region, name string) (bool, error) {
	switch action {
	case writePolicyDefault:
		return true, nil
	case writePolicySkip:
		return false, nil
	case writePolicyError:
		return false, fmt.Errorf("parameter %s would %s; --on-%s=error stops the command", name, operation, operationPolicyName(operation))
	case writePolicyAsk:
		return askWriteConfirmation(operation, region, name)
	default:
		return false, fmt.Errorf("unsupported write policy action %q", action)
	}
}

func operationPolicyName(operation writeOperation) string {
	if operation == writeOperationCreate {
		return "create"
	}
	return "update"
}

func logSkipped(logger *slog.Logger, command string, operation writeOperation, policy writePolicyAction, region, name string) {
	if logger == nil {
		return
	}
	logger.Info(command+" record skipped", "action", string(operation), "policy", "on-"+operationPolicyName(operation)+"="+string(policy), "region", region, "name", name)
}

func logRecordError(logger *slog.Logger, command string, operation writeOperation, region, name string, err error) {
	if logger == nil {
		return
	}
	logger.Error(command+" record failed", "action", string(operation), "region", region, "name", name, "error", err)
}

func logContinueOnError(logger *slog.Logger, command, region, name string) {
	if logger == nil {
		return
	}
	logger.Info("continuing after "+command+" error", "region", region, "name", name)
}

func wrapParameterType(parameterType ssm.ParameterType, err error) (ssm.ParameterType, error) {
	if err != nil {
		return "", crerr.Wrap(err, "parse parameter type")
	}
	return parameterType, nil
}

// PrepareImportItems resolves regions before import. Dotenv imports may still use # ssm comments or exact aliases.
func PrepareImportItems(ctx context.Context, cfg *Config, _ string) ([]inventory.Item, error) {
	if cfg.AllRegions {
		return nil, errors.New("--all-regions is not supported for import; specify --region")
	}
	if err := ensureRegions(ctx, cfg); err != nil {
		return nil, err
	}
	items, err := LoadItems(*cfg)
	if err != nil {
		return nil, err
	}
	return applyInventoryRegion(items, cfg.Region), nil
}

func applyRootPathToRecords(records []secretfmt.Record, rootPath string) ([]secretfmt.Record, error) {
	rootPath = strings.TrimSpace(rootPath)
	if rootPath != "" {
		if !strings.HasPrefix(rootPath, "/") {
			return nil, errors.New("--root-path must start with /")
		}
		rootPath = strings.TrimRight(rootPath, "/")
		if rootPath == "" {
			rootPath = "/"
		}
	}
	resolved := make([]secretfmt.Record, 0, len(records))
	for idx := range records {
		record := records[idx]
		path := strings.TrimSpace(record.Path)
		if path == "" {
			return nil, errors.New("import record is missing name; use --root-path with relative names or provide absolute SSM names")
		}
		if strings.HasPrefix(path, "/") {
			record.Path = path
			resolved = append(resolved, record)
			continue
		}
		if rootPath == "" {
			return nil, fmt.Errorf("import record name %q is not an absolute SSM path; use --root-path or # ssm comments", path)
		}
		if rootPath == "/" {
			record.Path = "/" + strings.TrimLeft(path, "/")
		} else {
			record.Path = rootPath + "/" + strings.TrimLeft(path, "/")
		}
		if record.Alias == "" {
			record.Alias = secretfmt.AliasForPath(record.Path, inventory.Item{})
		}
		resolved = append(resolved, record)
	}
	return resolved, nil
}

func recordRegion(record secretfmt.Record, cfg Config) string {
	if fieldAllowed(cfg.Fields, "region") && recordHasField(record, "region") && strings.TrimSpace(record.Region) != "" {
		return strings.TrimSpace(record.Region)
	}
	return cfg.Region
}

func metadataForImportRecords(ctx context.Context, client ssm.Client, records []secretfmt.Record, cfg Config) (metadataByKey map[string]ssm.Metadata, errorsByKey map[string]error) {
	pathsByRegion := map[string][]string{}
	seen := map[string]bool{}
	for i := range records {
		record := &records[i]
		region := recordRegion(*record, cfg)
		key := recordKey(region, record.Path)
		if seen[key] {
			continue
		}
		seen[key] = true
		pathsByRegion[region] = append(pathsByRegion[region], record.Path)
	}
	metadata := map[string]ssm.Metadata{}
	errs := map[string]error{}
	for region, paths := range pathsByRegion {
		metas, regionErrs := metadataForPaths(ctx, client.ForRegion(region), paths)
		for path := range metas {
			meta := metas[path]
			if meta.Region == "" {
				meta.Region = region
			}
			metadata[recordKey(region, path)] = meta
		}
		for path, err := range regionErrs {
			errs[recordKey(region, path)] = err
		}
	}
	return metadata, errs
}

func resolveImportType(defaultType string, existing ssm.Metadata, exists bool, record secretfmt.Record, cfg Config) (ssm.ParameterType, error) {
	recordType := ""
	if fieldAllowed(cfg.Fields, "type") && recordHasField(record, "type") && strings.TrimSpace(record.Type) != "" {
		recordType = record.Type
	}
	existingType := ""
	if exists {
		existingType = existing.Type
	}
	if !fieldAllowed(cfg.Fields, "type") {
		defaultType = ""
	}
	for _, candidate := range []string{recordType, existingType, defaultType} {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		return wrapParameterType(ssm.ParseParameterType(candidate))
	}
	return ssm.DefaultParameterType, nil
}

// Import reads dotenv, JSON, or YAML data, resolves each record to an SSM name, and writes values to Parameter Store.
func Import(ctx *CLIContext) error {
	cfg, err := ConfigFromCLI(ctx)
	if err != nil {
		return err
	}
	if defaultRegion := strings.TrimSpace(ctx.String("default-region")); defaultRegion != "" && cfg.Region == "" {
		cfg.Region = defaultRegion
		cfg.Regions = []string{defaultRegion}
	}
	if cfg.AllRegions {
		return errors.New("--all-regions is not supported for import; specify --region")
	}
	if len(cfg.Regions) > 1 {
		return errors.New("multiple --region values are only supported for tui and export")
	}
	format := ctx.String("format")
	items, err := PrepareImportItems(ctx.Context, &cfg, format)
	if err != nil {
		return err
	}

	reader, err := stdinReader()
	if err != nil {
		return err
	}

	records, err := parseImport(format, reader, items, effectiveFieldMappings(cfg.FieldMappings), strings.TrimSpace(ctx.String("key-field")))
	if err != nil {
		return err
	}
	records, err = applyRootPathToRecords(records, ctx.String("root-path"))
	if err != nil {
		return err
	}
	records = filterRecordsByGroups(records, cfg.FilterGroups)
	if err := requireFieldForCommand(cfg, "value", "import"); err != nil {
		return err
	}
	defaultOpts, err := importDefaultOptions(ctx, cfg)
	if err != nil {
		return err
	}
	policy, err := parseWritePolicy(ctx)
	if err != nil {
		return err
	}
	continueOnError := ctx.Bool("continue-on-error")
	summaryEnabled := ctx.Bool("summary")

	client := NewClient(cfg)
	metadata, metadataErrs := metadataForImportRecords(ctx.Context, client, records, cfg)
	summary := importSummary{}
	var recordErrors []string
	handleRecordError := func(operation writeOperation, region, name string, err error) error {
		wrapped := crerr.Wrap(err, name)
		logRecordError(cfg.Logger, "import", operation, region, name, wrapped)
		summary.Failed++
		recordErrors = append(recordErrors, wrapped.Error())
		if continueOnError {
			logContinueOnError(cfg.Logger, "import", region, name)
			return nil
		}
		return wrapped
	}

	for i := range records {
		record := &records[i]
		region := recordRegion(*record, cfg)
		key := recordKey(region, record.Path)
		existingMeta, exists := metadata[key]
		if err, ok := metadataErrs[key]; ok {
			if !crerr.Is(err, ssm.ErrNotFound) {
				if err := handleRecordError(writeOperationUpdate, region, record.Path, err); err != nil {
					return err
				}
				continue
			}
			exists = false
		}
		operation := writeOperationCreate
		policyAction := policy.OnCreate
		if exists {
			operation = writeOperationUpdate
			policyAction = policy.OnUpdate
		}
		if strings.TrimSpace(record.Value) == "" {
			if err := handleRecordError(operation, region, record.Path, errors.New("import record value is required")); err != nil {
				return err
			}
			continue
		}
		shouldWrite, err := resolveWritePolicy(policyAction, operation, region, record.Path)
		if err != nil {
			if err := handleRecordError(operation, region, record.Path, err); err != nil {
				return err
			}
			continue
		}
		if !shouldWrite {
			logSkipped(cfg.Logger, "import", operation, policyAction, region, record.Path)
			summary.Skipped++
			continue
		}
		parameterType, err := resolveImportType(ctx.String("default-type"), existingMeta, exists, *record, cfg)
		if err != nil {
			if err := handleRecordError(operation, region, record.Path, err); err != nil {
				return err
			}
			continue
		}
		opts, err := importOptionsForRecord(*record, existingMeta, exists, defaultOpts, cfg)
		if err != nil {
			if err := handleRecordError(operation, region, record.Path, err); err != nil {
				return err
			}
			continue
		}
		opts.Overwrite = exists
		if err := client.ForRegion(region).PutParameterWithOptions(ctx.Context, record.Path, record.Value, parameterType, opts); err != nil {
			if err := handleRecordError(operation, region, record.Path, err); err != nil {
				return err
			}
			continue
		}
		if exists {
			summary.Updated++
		} else {
			summary.Created++
		}
	}
	if summaryEnabled {
		_, _ = fmt.Fprintf(os.Stderr, "Import summary:\n  created: %d\n  updated: %d\n  skipped: %d\n  failed: %d\n", summary.Created, summary.Updated, summary.Skipped, summary.Failed)
	}
	if len(recordErrors) > 0 {
		return fmt.Errorf("import completed with %d error(s):\n%s", len(recordErrors), strings.Join(recordErrors, "\n"))
	}
	return nil
}

type exportSortRule struct {
	field      string
	descending bool
}

func includeValuesForSortColumns(values []string) bool {
	for _, rule := range parseExportSortRules(values) {
		switch rule.field {
		case "value", "len", "sha256":
			return true
		}
	}
	return false
}

func parseExportSortRules(values []string) []exportSortRule {
	rules := make([]exportSortRule, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		parts := strings.Split(value, ":")
		field, ok := normalizeExportSortField(parts[0])
		if !ok {
			continue
		}
		descending := false
		if len(parts) > 1 {
			switch strings.ToLower(strings.TrimSpace(parts[1])) {
			case "desc", "descending":
				descending = true
			}
		}
		rules = upsertExportSortRule(rules, field, descending)
	}
	return rules
}

func normalizeExportSortField(field string) (string, bool) {
	field = strings.ToLower(strings.TrimSpace(field))
	switch field {
	case "name", "path":
		return "name", true
	case "region", "type", "tier", "policies", "description", "value", "date", "version", "len", "sha256", "user":
		return field, true
	case "data-type", "datatype", "data_type":
		return "data-type", true
	default:
		return "", false
	}
}

func upsertExportSortRule(rules []exportSortRule, field string, descending bool) []exportSortRule {
	out := make([]exportSortRule, 0, len(rules)+1)
	for _, rule := range rules {
		if rule.field != field {
			out = append(out, rule)
		}
	}
	return append(out, exportSortRule{field: field, descending: descending})
}

func sortStatusesForExport(statuses []ui.Status, values []string) {
	rules := parseExportSortRules(values)
	if len(rules) == 0 {
		return
	}
	sort.SliceStable(statuses, func(i, j int) bool {
		left := statuses[i]
		right := statuses[j]
		for _, rule := range rules {
			cmp := naturalExportCompare(exportStatusSortValue(left, rule.field), exportStatusSortValue(right, rule.field))
			if cmp == 0 {
				continue
			}
			if rule.descending {
				return cmp > 0
			}
			return cmp < 0
		}
		if cmp := naturalExportCompare(left.Item.Region, right.Item.Region); cmp != 0 {
			return cmp < 0
		}
		return naturalExportCompare(left.Item.Path, right.Item.Path) < 0
	})
}

func exportStatusSortValue(status ui.Status, field string) string {
	switch field {
	case "name":
		return status.Item.Path
	case "region":
		return status.Item.Region
	case "type":
		return status.Type
	case "tier":
		return status.Tier
	case "data-type":
		return status.DataType
	case "policies":
		return status.Policies
	case "description":
		return status.Description
	case "value":
		return status.Value
	case "date":
		return status.Modified
	case "version":
		return fmt.Sprint(status.Version)
	case "len":
		return fmt.Sprint(status.Length)
	case "sha256":
		return status.SHA256Prefix
	case "user":
		return status.User
	default:
		return ""
	}
}

func naturalExportCompare(left, right string) int {
	leftRunes := []rune(strings.ToLower(strings.TrimSpace(left)))
	rightRunes := []rune(strings.ToLower(strings.TrimSpace(right)))
	i, j := 0, 0
	for i < len(leftRunes) && j < len(rightRunes) {
		if unicode.IsDigit(leftRunes[i]) && unicode.IsDigit(rightRunes[j]) {
			li, rj := i, j
			for i < len(leftRunes) && unicode.IsDigit(leftRunes[i]) {
				i++
			}
			for j < len(rightRunes) && unicode.IsDigit(rightRunes[j]) {
				j++
			}
			if cmp := compareExportDigitRuns(leftRunes[li:i], rightRunes[rj:j]); cmp != 0 {
				return cmp
			}
			continue
		}
		if leftRunes[i] < rightRunes[j] {
			return -1
		}
		if leftRunes[i] > rightRunes[j] {
			return 1
		}
		i++
		j++
	}
	if len(leftRunes)-i < len(rightRunes)-j {
		return -1
	}
	if len(leftRunes)-i > len(rightRunes)-j {
		return 1
	}
	return 0
}

func compareExportDigitRuns(left, right []rune) int {
	leftTrimmed := trimExportLeadingZeroes(left)
	rightTrimmed := trimExportLeadingZeroes(right)
	if len(leftTrimmed) < len(rightTrimmed) {
		return -1
	}
	if len(leftTrimmed) > len(rightTrimmed) {
		return 1
	}
	for i := range leftTrimmed {
		if leftTrimmed[i] < rightTrimmed[i] {
			return -1
		}
		if leftTrimmed[i] > rightTrimmed[i] {
			return 1
		}
	}
	if len(left) < len(right) {
		return -1
	}
	if len(left) > len(right) {
		return 1
	}
	return 0
}

func trimExportLeadingZeroes(value []rune) []rune {
	idx := 0
	for idx < len(value)-1 && value[idx] == '0' {
		idx++
	}
	return value[idx:]
}

var allExportFields = []string{"name", "region", "type", "tier", "data-type", "policies", "description", "value", "date", "version", "len", "sha256", "user"}

func exportFields(cfg Config) []string {
	if len(cfg.Fields) == 0 {
		return append([]string(nil), allExportFields...)
	}
	return append([]string(nil), cfg.Fields...)
}

func scalarExportField(ctx *CLIContext, cfg Config) (string, error) {
	if !ctx.Bool("scalar") {
		return "", nil
	}
	rawFields := compactStrings(ctx.StringSlice("output-field"))
	if len(rawFields) != 1 || len(cfg.Fields) != 1 {
		return "", errors.New("--scalar requires exactly one --output-field")
	}
	return cfg.Fields[0], nil
}

func validateKeyFieldOutputFields(keyField string, outputFields []string) error {
	keyField = strings.TrimSpace(keyField)
	if keyField == "" {
		return nil
	}
	for _, field := range outputFields {
		if field == keyField {
			return fmt.Errorf("--key-field and --output-field cannot use the same field: %s", keyField)
		}
	}
	return nil
}

func effectiveFieldMappings(overrides []secretfmt.FieldMapping) []secretfmt.FieldMapping {
	base := secretfmt.DefaultFieldMappings()
	if len(overrides) == 0 {
		return base
	}
	byField := map[string]string{}
	for _, mapping := range base {
		byField[mapping.AWSName] = mapping.FileName
	}
	for _, mapping := range overrides {
		byField[mapping.AWSName] = mapping.FileName
	}
	for i := range base {
		base[i].FileName = byField[base[i].AWSName]
	}
	return base
}

func exportFieldMappings(fields []string, overrides []secretfmt.FieldMapping) []secretfmt.FieldMapping {
	effective := effectiveFieldMappings(overrides)
	out := make([]secretfmt.FieldMapping, 0, len(fields))
	for _, field := range fields {
		for _, mapping := range effective {
			if mapping.AWSName == field {
				out = append(out, mapping)
				break
			}
		}
	}
	return out
}

func exportRecordFields(fields []string, scalarField, keyField string) []string {
	out := append([]string(nil), fields...)
	for _, field := range []string{scalarField, keyField} {
		field = strings.TrimSpace(field)
		if field == "" || hasExportField(out, field) {
			continue
		}
		out = append(out, field)
	}
	return out
}

func hasExportField(fields []string, field string) bool {
	for _, candidate := range fields {
		if candidate == field {
			return true
		}
	}
	return false
}

func exportRecordFromStatus(status ui.Status, fields []string) secretfmt.Record {
	record := secretfmt.Record{Path: status.Item.Path, Alias: secretfmt.AliasForItem(status.Item), Fields: fields}
	if hasExportField(fields, "region") {
		record.Region = status.Item.Region
	}
	if hasExportField(fields, "type") {
		record.Type = status.Type
	}
	if hasExportField(fields, "tier") {
		record.Tier = status.Tier
	}
	if hasExportField(fields, "data-type") {
		record.DataType = status.DataType
	}
	if hasExportField(fields, "policies") {
		record.Policies = status.Policies
	}
	if hasExportField(fields, "description") {
		record.Description = status.Description
	}
	if hasExportField(fields, "value") && status.Exists {
		record.Value = status.Value
	}
	if hasExportField(fields, "date") {
		record.Date = status.Modified
	}
	if hasExportField(fields, "version") {
		record.Version = status.Version
	}
	if hasExportField(fields, "len") {
		record.Len = status.Length
	}
	if hasExportField(fields, "sha256") {
		record.SHA256 = status.SHA256Prefix
	}
	if hasExportField(fields, "user") {
		record.User = status.User
	}
	return record
}

// Export loads statuses for the requested inventory and writes existing parameter values to stdout.
func Export(ctx *CLIContext) error {
	cfg, err := ConfigFromCLI(ctx)
	if err != nil {
		return err
	}
	items, err := PrepareItems(ctx.Context, &cfg)
	if err != nil {
		return err
	}
	client := NewClient(cfg)
	regions := append([]string(nil), cfg.Regions...)
	if cfg.AllRegions {
		var err error
		regions, err = client.ListRegions(ctx.Context)
		if err != nil {
			return crerr.Wrap(err, "list AWS regions")
		}
	}

	scalarField, err := scalarExportField(ctx, cfg)
	if err != nil {
		return err
	}
	keyField := strings.TrimSpace(ctx.String("key-field"))
	if err := validateKeyFieldOutputFields(keyField, cfg.Fields); err != nil {
		return err
	}
	fields := exportFields(cfg)
	recordFields := exportRecordFields(fields, scalarField, keyField)
	includeValues := cfg.WithDecryption || includeValuesForFields(recordFields) || includeValuesForFilterGroups(cfg.FilterGroups) || includeValuesForSortColumns(cfg.SortColumns)
	showProgress := false
	var statuses []ui.Status
	switch {
	case len(cfg.FilterGroups) > 0 && len(items) == 0 && showProgress:
		statuses = ui.LoadFilteredStatusesWithProgressForRegions(ctx.Context, client, cfg.FilterGroups, includeValues, regions)
	case len(cfg.FilterGroups) > 0 && len(items) == 0:
		statuses = ui.LoadFilteredStatusesBatchForRegions(ctx.Context, client, cfg.FilterGroups, includeValues, regions, nil)
	case showProgress:
		statuses = ui.LoadStatusesWithProgressForRegions(ctx.Context, client, items, includeValues, regions)
	default:
		statuses = ui.LoadStatusesForRegions(ctx.Context, client, items, includeValues, regions)
	}
	if len(items) > 0 {
		statuses = ui.FilterStatusesByGroups(statuses, cfg.FilterGroups)
	}

	sortStatusesForExport(statuses, cfg.SortColumns)

	var records []secretfmt.Record
	for i := range statuses {
		if !statuses[i].Exists {
			continue
		}
		records = append(records, exportRecordFromStatus(statuses[i], recordFields))
	}

	writer := os.Stdout
	format := ctx.String("format")
	if scalarField != "" {
		switch format {
		case "dotenv":
			return crerr.Wrap(secretfmt.ExportScalarLines(writer, records, scalarField), "export scalar")
		case "json":
			return crerr.Wrap(secretfmt.ExportJSONScalar(writer, records, scalarField, keyField), "export scalar JSON")
		case "yaml", "yml":
			return crerr.Wrap(secretfmt.ExportYAMLScalar(writer, records, scalarField, keyField), "export scalar YAML")
		default:
			return fmt.Errorf("unsupported format: %s", format)
		}
	}

	switch format {
	case "dotenv":
		return crerr.Wrap(secretfmt.ExportDotenv(writer, records), "export dotenv")
	case "json":
		return crerr.Wrap(secretfmt.ExportJSONMapped(writer, records, exportFieldMappings(recordFields, cfg.FieldMappings), keyField), "export JSON")
	case "yaml", "yml":
		return crerr.Wrap(secretfmt.ExportYAMLMapped(writer, records, exportFieldMappings(recordFields, cfg.FieldMappings), keyField), "export YAML")
	default:
		return fmt.Errorf("unsupported format: %s", format)
	}
}

// parseImport dispatches import parsing by format while keeping the Import command handler format-agnostic.
func parseImport(format string, reader io.Reader, items []inventory.Item, mappings []secretfmt.FieldMapping, jsonKeyField string) ([]secretfmt.Record, error) {
	switch format {
	case "dotenv":
		records, err := secretfmt.ImportDotenv(reader, items)
		return records, crerr.Wrap(err, "import dotenv")
	case "json":
		records, err := secretfmt.ImportJSONMapped(reader, mappings, jsonKeyField)
		return records, crerr.Wrap(err, "import JSON")
	case "yaml", "yml":
		records, err := secretfmt.ImportYAMLMapped(reader, mappings, jsonKeyField)
		return records, crerr.Wrap(err, "import YAML")
	default:
		return nil, fmt.Errorf("unsupported format: %s", format)
	}
}

func stdinReader() (io.Reader, error) {
	info, err := os.Stdin.Stat()
	if err == nil && info.Mode()&os.ModeCharDevice != 0 {
		return nil, errors.New("import requires data from stdin")
	}
	return os.Stdin, nil
}
