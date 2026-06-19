// Package app contains command implementations and configuration parsing for aws-ssm-params.
package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"strings"

	crerr "github.com/cockroachdb/errors"
	"github.com/gosuri/uilive"
	"github.com/urfave/cli/v2"

	"github.com/biptec/aws-ssm-params/internal/fileio"
	"github.com/biptec/aws-ssm-params/internal/filter"
	secretfmt "github.com/biptec/aws-ssm-params/internal/format"
	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/logging"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/biptec/aws-ssm-params/internal/ui"
)

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
	Names                     []string
	NamesFile                 string
	Region                    string
	Regions                   []string
	Profile                   string
	AllRegions                bool
	NoColor                   bool
	WithDecryption            bool
	Keymap                    string
	ShowColumns               []string
	SortColumn                string
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
func RunWithLogging(ctx *cli.Context, bufferTerminal bool, action func(*cli.Context) error) error {
	logCfg := loggingConfigFromCLI(ctx)
	if ctx.Command != nil && ctx.Command.Name == "export" && strings.EqualFold(strings.TrimSpace(logCfg.Target), "stdout") && loggingLevelEnabled(logCfg.Level) {
		return errors.New("--log-target=stdout cannot be used with export when logging is enabled; use stderr or file")
	}
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

func loggingLevelEnabled(level string) bool {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "", "off":
		return false
	default:
		return true
	}
}

func loggingConfigFromCLI(ctx *cli.Context) logging.Config {
	return logging.Config{
		Level:  stringFlagValueAny(ctx, "log-level", logging.DefaultLevel, "AWS_SSM_PARAMS_LOG_LEVEL"),
		Target: stringFlagValueAny(ctx, "log-target", logging.DefaultTarget, "AWS_SSM_PARAMS_LOG_TARGET"),
		File:   stringFlagValueAny(ctx, "log-file", logging.DefaultFile, "AWS_SSM_PARAMS_LOG_FILE"),
	}
}

func parseFieldMappings(values []string) ([]secretfmt.FieldMapping, []string, error) {
	parts := compactStrings(values)
	if len(parts) == 0 {
		return nil, nil, nil
	}
	seen := map[string]bool{}
	mappings := make([]secretfmt.FieldMapping, 0, len(parts))
	fields := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.Contains(part, ",") {
			return nil, nil, fmt.Errorf("--field accepts one value per flag; repeat --field instead of using commas")
		}
		awsField, fileField, ok := strings.Cut(part, ":")
		if !ok {
			fileField = awsField
		}
		canonical, ok := supportedFields[strings.ToLower(strings.TrimSpace(awsField))]
		if !ok {
			return nil, nil, fmt.Errorf("unsupported --field value %q", awsField)
		}
		fileField = strings.TrimSpace(fileField)
		if fileField == "" {
			return nil, nil, fmt.Errorf("field mapping %q has empty file field", part)
		}
		if seen[canonical] {
			continue
		}
		seen[canonical] = true
		fields = append(fields, canonical)
		mappings = append(mappings, secretfmt.FieldMapping{AWSName: canonical, FileName: fileField})
	}
	return mappings, fields, nil
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
func ConfigFromCLI(ctx *cli.Context) (Config, error) {
	allRegions := boolFlagValueAny(ctx, "all-regions", false, "AWS_SSM_PARAMS_ALL_REGIONS")
	regions := dedupeStrings(stringSliceFlagValue(ctx, "region", "AWS_SSM_PARAMS_REGIONS", "AWS_SSM_PARAMS_REGION"))
	if allRegions && len(regions) > 0 {
		return Config{}, errors.New("--region and --all-regions cannot be used together")
	}
	profile := stringFlagValueAny(ctx, "profile", "", "AWS_SSM_PARAMS_PROFILE")
	if profile == "" {
		profile = os.Getenv("AWS_PROFILE")
	}
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

	fieldMappings, fields, err := parseFieldMappings(stringSliceFlagValue(ctx, "field", "AWS_SSM_PARAMS_FIELDS"))
	if err != nil {
		return Config{}, err
	}
	names, err := parseNames(stringSliceFlagValue(ctx, "name", "AWS_SSM_PARAMS_NAME"))
	if err != nil {
		return Config{}, err
	}
	namesFile := strings.TrimSpace(stringFlagValueAny(ctx, "names-file", "", "AWS_SSM_PARAMS_NAMES_FILE"))
	showColumns, err := ui.ParseColumnOption(strings.Join(stringSliceFlagValue(ctx, "show-column", "AWS_SSM_PARAMS_SHOW_COLUMNS"), ","))
	if err != nil {
		return Config{}, crerr.Wrap(err, "parse show columns")
	}
	filtersFile := strings.TrimSpace(stringFlagValueAny(ctx, "filters-file", "", "AWS_SSM_PARAMS_FILTERS_FILE"))
	filterGroups, err := parseFilterGroups(stringSliceFlagValue(ctx, "filter", "AWS_SSM_PARAMS_FILTERS", "AWS_SSM_PARAMS_FILTER"), filtersFile)
	if err != nil {
		return Config{}, err
	}

	return Config{
		Logger:                    logging.FromContext(ctx.Context),
		FiltersFile:               filtersFile,
		FilterGroups:              filterGroups,
		FieldMappings:             fieldMappings,
		Fields:                    fields,
		Names:                     names,
		NamesFile:                 namesFile,
		Region:                    region,
		Regions:                   regions,
		Profile:                   profile,
		AllRegions:                allRegions,
		NoColor:                   boolFlagValueAny(ctx, "no-color", false, "AWS_SSM_PARAMS_NO_COLOR") || os.Getenv("NO_COLOR") != "",
		WithDecryption:            boolFlagValueAny(ctx, "with-decryption", false, "AWS_SSM_PARAMS_WITH_DECRYPTION"),
		Keymap:                    keymap,
		ShowColumns:               showColumns,
		SortColumn:                strings.TrimSpace(stringFlagValueAny(ctx, "sort-column", "", "AWS_SSM_PARAMS_SORT_COLUMN")),
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

// LoadItems builds explicit inventory items from --name and --names-file.
// When no names are configured, callers can still discover parameters directly from AWS SSM.
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

	if strings.TrimSpace(cfg.NamesFile) != "" {
		fileItems, err := inventory.LoadPathsFile(cfg.NamesFile)
		if err != nil {
			return nil, crerr.Wrap(err, "load names file")
		}
		for _, item := range fileItems {
			add(item)
		}
	}
	for _, name := range cfg.Names {
		if !strings.HasPrefix(name, "/") {
			return nil, fmt.Errorf("invalid SSM name: %s", name)
		}
		add(inventory.Item{Path: name, Kind: "name", Source: "--name", SecretName: path.Base(name)})
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

func parseNames(values []string) ([]string, error) {
	names := compactStrings(values)
	for _, name := range names {
		if !strings.HasPrefix(name, "/") {
			return nil, fmt.Errorf("invalid --name value %q", name)
		}
	}
	return dedupeStrings(names), nil
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
		return fmt.Errorf("%s requires field %q; remove --field restrictions or include %s", command, field, field)
	}
	return nil
}

func rejectDisallowedFlag(ctx *cli.Context, cfg Config, flagName, field string) error {
	if ctx.IsSet(flagName) && !fieldAllowed(cfg.Fields, field) {
		return fmt.Errorf("--%s requires field %q; remove --field restrictions or include %s", flagName, field, field)
	}
	return nil
}

func parseSingleField(value, flagName, defaultField string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = defaultField
	}
	if strings.Contains(value, ",") {
		return "", fmt.Errorf("--%s accepts one field only", flagName)
	}
	canonical, ok := supportedFields[strings.ToLower(value)]
	if !ok {
		return "", fmt.Errorf("unsupported --%s value %q", flagName, value)
	}
	return canonical, nil
}

func recordHasField(record secretfmt.Record, field string) bool {
	for _, candidate := range record.Fields {
		if candidate == field {
			return true
		}
	}
	return len(record.Fields) == 0
}

func importDefaultValue(ctx *cli.Context) (string, error) {
	if valueFile := strings.TrimSpace(ctx.String("default-value-file")); valueFile != "" {
		data, err := fileio.ReadFile(valueFile)
		if err != nil {
			return "", crerr.Wrapf(err, "read default value file %s", valueFile)
		}
		return string(data), nil
	}
	return ctx.String("default-value"), nil
}

func importDefaultOptions(ctx *cli.Context, cfg Config) (ssm.PutParameterOptions, error) {
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
	opts := ssm.PutParameterOptions{Overwrite: ctx.Bool("default-override")}
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

func importOptionsForRecord(record secretfmt.Record, defaults ssm.PutParameterOptions, cfg Config) (ssm.PutParameterOptions, error) {
	opts := defaults
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
	if fieldAllowed(cfg.Fields, "description") && recordHasField(record, "description") {
		opts.Description = record.Description
	}
	if fieldAllowed(cfg.Fields, "policies") && recordHasField(record, "policies") {
		opts.Policies = record.Policies
	}
	return opts, nil
}

func setOptions(ctx *cli.Context, cfg Config, overwrite bool) (ssm.PutParameterOptions, error) {
	if err := rejectDisallowedFlag(ctx, cfg, "tier", "tier"); err != nil {
		return ssm.PutParameterOptions{}, err
	}
	if err := rejectDisallowedFlag(ctx, cfg, "data-type", "data-type"); err != nil {
		return ssm.PutParameterOptions{}, err
	}
	if err := rejectDisallowedFlag(ctx, cfg, "description", "description"); err != nil {
		return ssm.PutParameterOptions{}, err
	}
	if err := rejectDisallowedFlag(ctx, cfg, "policies", "policies"); err != nil {
		return ssm.PutParameterOptions{}, err
	}
	if err := rejectDisallowedFlag(ctx, cfg, "policies-file", "policies"); err != nil {
		return ssm.PutParameterOptions{}, err
	}
	tier, err := ssm.ParseParameterTier(ctx.String("tier"))
	if err != nil {
		return ssm.PutParameterOptions{}, crerr.Wrap(err, "parse tier")
	}
	dataType, err := ssm.ParseParameterDataType(ctx.String("data-type"))
	if err != nil {
		return ssm.PutParameterOptions{}, crerr.Wrap(err, "parse data type")
	}
	policies := ctx.String("policies")
	if file := strings.TrimSpace(ctx.String("policies-file")); file != "" {
		data, err := fileio.ReadFile(file)
		if err != nil {
			return ssm.PutParameterOptions{}, crerr.Wrapf(err, "read policies file %s", file)
		}
		policies = string(data)
	}
	return ssm.PutParameterOptions{Overwrite: overwrite, Tier: tier, DataType: dataType, Description: ctx.String("description"), Policies: policies}, nil
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

func stringFlagValueAny(ctx *cli.Context, name, fallback string, envNames ...string) string {
	if ctx.IsSet(name) {
		return ctx.String(name)
	}
	for _, envName := range envNames {
		if value := strings.TrimSpace(os.Getenv(envName)); value != "" {
			return value
		}
	}
	return fallback
}

func boolFlagValueAny(ctx *cli.Context, name string, fallback bool, envNames ...string) bool {
	if ctx.IsSet(name) {
		return ctx.Bool(name)
	}
	for _, envName := range envNames {
		if value := strings.TrimSpace(os.Getenv(envName)); value != "" {
			return parseBoolEnv(value)
		}
	}
	return fallback
}

func stringSliceFlagValue(ctx *cli.Context, name string, envNames ...string) []string {
	values := splitCommaSeparatedValues(ctx.StringSlice(name))
	if ctx.IsSet(name) || len(values) > 0 {
		return dedupeStrings(values)
	}
	for _, envName := range envNames {
		if value := strings.TrimSpace(os.Getenv(envName)); value != "" {
			return dedupeStrings(splitCommaSeparatedValues([]string{value}))
		}
	}
	return nil
}

func splitCommaSeparatedValues(values []string) []string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, strings.Split(value, ",")...)
	}
	return compactStrings(parts)
}

func parseBoolEnv(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "t", "true", "y", "yes", "on":
		return true
	default:
		return false
	}
}

// compactStrings trims whitespace and removes empty flag values while preserving the original order.
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

// dedupeStrings removes duplicate strings while preserving first occurrence order.
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

// Interactive prepares inventory, validates AWS access, resolves the region label shown to users, and starts the TUI.
// In all-regions mode it lists enabled AWS regions once up front so the UI can show progress region-by-region.
func Interactive(ctx *cli.Context) error {
	cfg, err := ConfigFromCLI(ctx)
	if err != nil {
		return err
	}
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
		NamesFile:                 cfg.NamesFile,
		FilterGroups:              cfg.FilterGroups,
		NoColor:                   cfg.NoColor,
		Keymap:                    cfg.Keymap,
		ShowColumns:               cfg.ShowColumns,
		Sort:                      cfg.SortColumn,
		Fields:                    cfg.Fields,
		IncludeValues:             cfg.WithDecryption || includeValuesForFields(cfg.Fields) || includeValuesForFilterGroups(cfg.FilterGroups),
		ShowSecureValues:          cfg.WithDecryption,
		NoConfirmOverwriteFile:    cfg.NoConfirmOverwriteFile,
		NoConfirmWriteSecureValue: cfg.NoConfirmWriteSecureValue,
		NoConfirmDeleteOne:        cfg.NoConfirmDeleteOne,
		NoConfirmDeleteAll:        cfg.NoConfirmDeleteAll,
	}), "run interactive")
}

// Get prints one selected parameter field.
// It intentionally rejects all-regions and multi-region modes because a single name could resolve to multiple values.
func Get(ctx *cli.Context) error {
	cfg, err := ConfigFromCLI(ctx)
	if err != nil {
		return err
	}
	if cfg.AllRegions {
		return errors.New("--all-regions is not supported for get; use interactive or export")
	}
	if len(cfg.Regions) > 1 {
		return errors.New("multiple --region values are only supported for interactive and export")
	}
	args := ctx.Args().Slice()
	if len(args) != 2 {
		return errors.New("get requires parameter name and field")
	}
	field, err := parseSingleField(args[1], "field", "value")
	if err != nil {
		return err
	}
	if err := ensureRegions(ctx.Context, &cfg); err != nil {
		return err
	}
	value, err := getParameterField(ctx.Context, NewClient(cfg), args[0], field, cfg.Region)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprint(os.Stdout, value)
	if !strings.HasSuffix(value, "\n") {
		_, _ = fmt.Fprintln(os.Stdout)
	}
	return nil
}

func getParameterField(ctx context.Context, client ssm.Client, name, field, region string) (string, error) {
	switch field {
	case "value", "version", "len", "sha256":
		param, err := client.Get(ctx, name)
		if err != nil {
			return "", crerr.Wrapf(err, "get parameter %s", name)
		}
		switch field {
		case "value":
			return param.Value, nil
		case "version":
			return fmt.Sprint(param.Version), nil
		case "len":
			return fmt.Sprint(len(param.Value)), nil
		case "sha256":
			return hashPrefix(param.Value), nil
		}
	}

	metas := client.DescribeMany(ctx, []string{name})
	if meta, ok := metas[name]; ok {
		switch field {
		case "name":
			return meta.Name, nil
		case "region":
			if meta.Region != "" {
				return meta.Region, nil
			}
			return region, nil
		case "type":
			return meta.Type, nil
		case "tier":
			return meta.Tier, nil
		case "data-type":
			return meta.DataType, nil
		case "policies":
			return meta.Policies, nil
		case "description":
			return meta.Description, nil
		case "date":
			return meta.Modified, nil
		case "user":
			return meta.User, nil
		}
	}

	param, err := client.Get(ctx, name)
	if err != nil {
		return "", crerr.Wrapf(err, "get parameter %s", name)
	}
	switch field {
	case "name":
		if param.Name != "" {
			return param.Name, nil
		}
		return name, nil
	case "region":
		if param.Region != "" {
			return param.Region, nil
		}
		return region, nil
	case "type":
		return param.Type, nil
	case "date":
		return param.Modified, nil
	}
	return "", fmt.Errorf("field %q is not available for %s", field, name)
}

func hashPrefix(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:8]
}

// Put writes one String, StringList, or SecureString value from positional arguments.
// Unless --override is used, it refuses to overwrite an existing non-empty value; when --type is omitted it preserves the existing parameter type.
func Put(ctx *cli.Context) error {
	cfg, err := ConfigFromCLI(ctx)
	if err != nil {
		return err
	}
	if err := requireFieldForCommand(cfg, "value", "put"); err != nil {
		return err
	}
	if cfg.AllRegions {
		return errors.New("--all-regions is not supported for put; use interactive on an existing regional row")
	}
	if len(cfg.Regions) > 1 {
		return errors.New("multiple --region values are only supported for interactive and export")
	}
	args := ctx.Args().Slice()
	if len(args) != 2 {
		return errors.New("put requires parameter name and value")
	}
	parameterName := args[0]
	value := args[1]
	overwrite := ctx.Bool("override")

	if commandRegion := strings.TrimSpace(ctx.String("region")); commandRegion != "" {
		cfg.Region = commandRegion
		cfg.Regions = []string{commandRegion}
	}
	if err := ensureRegions(ctx.Context, &cfg); err != nil {
		return err
	}
	client := NewClient(cfg)
	existing, existingErr := client.Get(ctx.Context, parameterName)
	if existingErr != nil && !crerr.Is(existingErr, ssm.ErrNotFound) {
		return crerr.Wrapf(existingErr, "get existing parameter %s", parameterName)
	}
	if !overwrite && existingErr == nil && existing.Value != "" {
		return fmt.Errorf("parameter already has a non-empty value: %s; use --override", parameterName)
	}
	if ctx.IsSet("type") && !fieldAllowed(cfg.Fields, "type") {
		return errors.New("--type requires field \"type\"; remove --field restrictions or include type")
	}
	parameterType, err := resolveSetType(ctx.String("type"), existing.Type)
	if err != nil {
		return err
	}
	opts, err := setOptions(ctx, cfg, overwrite)
	if err != nil {
		return err
	}
	return crerr.Wrapf(client.PutParameterWithOptions(ctx.Context, parameterName, value, parameterType, opts), "put parameter %s", parameterName)
}

func resolveSetType(requestedType, existingType string) (ssm.ParameterType, error) {
	for _, candidate := range []string{requestedType, existingType} {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		return wrapParameterType(ssm.ParseParameterType(candidate))
	}
	return ssm.DefaultParameterType, nil
}

func resolveImportType(defaultType, existingType, recordType string) (ssm.ParameterType, error) {
	for _, candidate := range []string{recordType, existingType, defaultType} {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		return wrapParameterType(ssm.ParseParameterType(candidate))
	}
	return ssm.DefaultParameterType, nil
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

// Import reads dotenv or JSON data, resolves each record to an SSM name, and writes the values to Parameter Store.
// It preloads existing values so it can skip protected non-empty parameters and report all skipped paths together.
func Import(ctx *cli.Context) error {
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
		return errors.New("multiple --region values are only supported for interactive and export")
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

	records, err := parseImport(format, reader, items, cfg.FieldMappings, strings.TrimSpace(ctx.String("json-key-field")))
	if err != nil {
		return err
	}
	records = filterRecordsByGroups(records, cfg.FilterGroups)
	defaultValue, err := importDefaultValue(ctx)
	if err != nil {
		return err
	}
	for i := range records {
		if records[i].Value == "" && defaultValue != "" {
			records[i].Value = defaultValue
		}
	}
	if err := requireFieldForCommand(cfg, "value", "import"); err != nil {
		return err
	}
	defaultOpts, err := importDefaultOptions(ctx, cfg)
	if err != nil {
		return err
	}

	client := NewClient(cfg)
	paths := make([]string, 0, len(records))
	for i := range records {
		paths = append(paths, records[i].Path)
	}

	values, errs := client.GetMany(ctx.Context, paths)
	var skipped []string
	writer := uilive.New()
	writer.Start()
	defer writer.Stop()
	for i := range records {
		record := &records[i]
		_, _ = fmt.Fprintf(writer, "Importing %d/%d...\n%s\n", i, len(records), record.Path)
		if !ctx.Bool("default-override") {
			if existing, ok := values[record.Path]; ok && existing.Value != "" {
				skipped = append(skipped, record.Path)
				continue
			}
			if err, ok := errs[record.Path]; ok && !crerr.Is(err, ssm.ErrNotFound) {
				return crerr.Wrap(err, record.Path)
			}
		}
		existingType := ""
		if existing, ok := values[record.Path]; ok {
			existingType = existing.Type
		}
		recordType := record.Type
		if !fieldAllowed(cfg.Fields, "type") || !recordHasField(*record, "type") {
			recordType = ""
		}
		defaultType := ctx.String("default-type")
		if !fieldAllowed(cfg.Fields, "type") {
			defaultType = ""
		}
		parameterType, err := resolveImportType(defaultType, existingType, recordType)
		if err != nil {
			return crerr.Wrap(err, record.Path)
		}
		opts, err := importOptionsForRecord(*record, defaultOpts, cfg)
		if err != nil {
			return crerr.Wrap(err, record.Path)
		}
		opts.Overwrite = ctx.Bool("default-override")
		writeClient := client
		if fieldAllowed(cfg.Fields, "region") && recordHasField(*record, "region") && strings.TrimSpace(record.Region) != "" {
			writeClient = client.ForRegion(strings.TrimSpace(record.Region))
		}
		if err := writeClient.PutParameterWithOptions(ctx.Context, record.Path, record.Value, parameterType, opts); err != nil {
			return crerr.Wrap(err, record.Path)
		}
	}
	if len(skipped) > 0 {
		return fmt.Errorf("skipped existing non-empty parameters without --override:\n%s", strings.Join(skipped, "\n"))
	}
	return nil
}

var allExportFields = []string{"name", "region", "type", "tier", "data-type", "policies", "description", "value", "date", "version", "len", "sha256", "user"}

func exportFields(cfg Config) []string {
	if len(cfg.Fields) == 0 {
		return append([]string(nil), allExportFields...)
	}
	return append([]string(nil), cfg.Fields...)
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

// Export loads statuses for the requested inventory and writes existing parameter values as dotenv or JSON.
// Missing parameters are omitted by default, but --include-missing can keep them as empty records for templates/backups.
func Export(ctx *cli.Context) error {
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

	includeValues := cfg.WithDecryption || includeValuesForFields(cfg.Fields) || includeValuesForFilterGroups(cfg.FilterGroups)
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

	fields := exportFields(cfg)
	var records []secretfmt.Record
	for i := range statuses {
		if !statuses[i].Exists && !ctx.Bool("include-missing") {
			continue
		}
		records = append(records, exportRecordFromStatus(statuses[i], fields))
	}

	writer := os.Stdout

	switch ctx.String("format") {
	case "dotenv":
		return crerr.Wrap(secretfmt.ExportDotenv(writer, records), "export dotenv")
	case "json":
		return crerr.Wrap(secretfmt.ExportJSONMapped(writer, records, cfg.FieldMappings, strings.TrimSpace(ctx.String("json-key-field"))), "export JSON")
	default:
		return fmt.Errorf("unsupported format: %s", ctx.String("format"))
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
