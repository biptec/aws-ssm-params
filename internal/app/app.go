package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	secretfmt "github.com/biptec/aws-ssm-params/internal/format"
	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/biptec/aws-ssm-params/internal/ui"
	"github.com/gosuri/uilive"
	"github.com/urfave/cli/v2"
)

// Config is the normalized runtime configuration shared by all commands.
// It is built from CLI flags plus AWS-related environment variables before command-specific code runs.
// Region stores the primary/default AWS region, while Regions stores every explicitly requested region;
// AllRegions tells the rest of the app to discover enabled AWS regions and mark inventory items as regional wildcards.
type Config struct {
	NamesFile                 string
	Names                     []string
	Fields                    []string
	Region                    string
	Regions                   []string
	Profile                   string
	AllRegions                bool
	NoColor                   bool
	WithoutDecryption         bool
	Keymap                    string
	ShowColumns               []string
	Sort                      string
	AllowNamesFileUpdate      bool
	ShowSecureValues          bool
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

func parseFieldList(values []string) ([]string, error) {
	parts := compactStrings(values)
	if len(parts) == 0 {
		return nil, nil
	}
	seen := map[string]bool{}
	fields := make([]string, 0, len(parts)+1)
	for _, part := range parts {
		canonical, ok := supportedFields[strings.ToLower(strings.TrimSpace(part))]
		if !ok {
			return nil, fmt.Errorf("unsupported --fields value %q", part)
		}
		if !seen[canonical] {
			seen[canonical] = true
			fields = append(fields, canonical)
		}
	}
	if !seen["name"] {
		fields = append([]string{"name"}, fields...)
	}
	return fields, nil
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

// RejectRepeatedFlagArgs reports an error when a command-line flag that is intended to be provided once
// appears more than once. Values may still contain comma-separated lists, for example --regions a,b.
func RejectRepeatedFlagArgs(args []string, flagName string) error {
	needle := "--" + flagName
	count := 0
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if arg == needle || strings.HasPrefix(arg, needle+"=") {
			count++
			if count > 1 {
				return fmt.Errorf("--%s can only be specified once; use comma-separated values", flagName)
			}
		}
	}
	return nil
}

// ConfigFromCLI converts raw urfave/cli state into a Config that the rest of the application can use.
// It enforces mutually exclusive region modes, falls back to AWS_PROFILE/AWS_REGION/AWS_DEFAULT_REGION,
// parses comma-separated --regions values, and decides whether a names-file argument should be read for the command.
func ConfigFromCLI(ctx *cli.Context) (Config, error) {
	allRegions := boolFlagValue(ctx, "all-regions", "AWS_SSM_PARAMS_ALL_REGIONS", false)
	regionArgs := compactStrings([]string{stringFlagValue(ctx, "regions", "AWS_SSM_PARAMS_REGIONS", "")})
	if allRegions && len(regionArgs) > 0 {
		return Config{}, errors.New("--regions and --all-regions cannot be used together")
	}
	profile := stringFlagValue(ctx, "profile", "AWS_SSM_PARAMS_PROFILE", "")
	if profile == "" {
		profile = os.Getenv("AWS_PROFILE")
	}
	regions := dedupeStrings(regionArgs)
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
	keymap := strings.ToLower(strings.TrimSpace(stringFlagValue(ctx, "keymap", "AWS_SSM_PARAMS_KEYMAP", "emacs")))
	if keymap == "" {
		keymap = "emacs"
	}
	if keymap != "emacs" && keymap != "vi" {
		return Config{}, fmt.Errorf("unsupported --keymap %q; expected emacs or vi", keymap)
	}

	namesFile := strings.TrimSpace(stringFlagValue(ctx, "names-file", "AWS_SSM_PARAMS_NAMES_FILE", ""))
	names := dedupeStrings(compactStrings(ctx.StringSlice("names")))
	if len(names) == 0 {
		names = dedupeStrings(compactStrings([]string{os.Getenv("AWS_SSM_PARAMS_NAMES")}))
	}
	fieldArgs := ctx.StringSlice("fields")
	if len(compactStrings(fieldArgs)) == 0 {
		fieldArgs = []string{os.Getenv("AWS_SSM_PARAMS_FIELDS")}
	}
	fields, err := parseFieldList(fieldArgs)
	if err != nil {
		return Config{}, err
	}
	showColumns, err := ui.ParseColumnOption(stringFlagValue(ctx, "show-columns", "AWS_SSM_PARAMS_SHOW_COLUMNS", ""))
	if err != nil {
		return Config{}, err
	}
	allowNamesFileUpdate := boolFlagValue(ctx, "allow-names-file-update", "AWS_SSM_PARAMS_ALLOW_NAMES_FILE_UPDATE", false)
	if allowNamesFileUpdate && namesFile == "" {
		return Config{}, errors.New("--allow-names-file-update requires --names-file")
	}
	return Config{
		NamesFile:                 namesFile,
		Names:                     names,
		Fields:                    fields,
		Region:                    region,
		Regions:                   regions,
		Profile:                   profile,
		AllRegions:                allRegions,
		NoColor:                   boolFlagValue(ctx, "no-color", "AWS_SSM_PARAMS_NO_COLOR", false) || os.Getenv("NO_COLOR") != "",
		WithoutDecryption:         boolFlagValue(ctx, "without-decryption", "AWS_SSM_PARAMS_WITHOUT_DECRYPTION", false),
		Keymap:                    keymap,
		ShowColumns:               showColumns,
		Sort:                      stringFlagValue(ctx, "sort", "AWS_SSM_PARAMS_SORT", ""),
		AllowNamesFileUpdate:      allowNamesFileUpdate,
		ShowSecureValues:          boolFlagValue(ctx, "show-secure-values", "AWS_SSM_PARAMS_SHOW_SECURE_VALUES", false),
		NoConfirmOverwriteFile:    boolFlagValue(ctx, "no-confirm-overwrite-file", "AWS_SSM_PARAMS_NO_CONFIRM_OVERWRITE_FILE", false),
		NoConfirmWriteSecureValue: boolFlagValue(ctx, "no-confirm-write-securestring", "AWS_SSM_PARAMS_NO_CONFIRM_WRITE_SECURESTRING", false),
		NoConfirmDeleteOne:        boolFlagValue(ctx, "no-confirm-delete-one", "AWS_SSM_PARAMS_NO_CONFIRM_DELETE_ONE", false),
		NoConfirmDeleteAll:        boolFlagValue(ctx, "no-confirm-delete-all", "AWS_SSM_PARAMS_NO_CONFIRM_DELETE_ALL", false),
	}, nil
}

// NewClient creates the concrete AWS SSM client for the selected profile and primary region.
// Keeping this in one function makes command handlers independent from the AWS SDK implementation details.
func NewClient(cfg Config) ssm.Client {
	client := ssm.NewAWSClient(cfg.Profile, cfg.Region)
	client.WithDecryption = !cfg.WithoutDecryption
	return client
}

// LoadItems reads an optional paths file and validates it when present.
// An omitted file means the caller wants to discover parameters directly from AWS SSM.
func LoadItems(cfg Config) ([]inventory.Item, error) {
	var items []inventory.Item
	if cfg.NamesFile != "" {
		fileItems, err := inventory.LoadPathsFile(cfg.NamesFile)
		if err != nil {
			return nil, err
		}
		items = append(items, fileItems...)
	}
	for _, name := range cfg.Names {
		items = append(items, inventory.Item{Path: name})
	}
	items = dedupeItemsByPath(items)
	if cfg.NamesFile != "" && len(items) == 0 {
		return nil, fmt.Errorf("names file is empty: %s", cfg.NamesFile)
	}
	if len(items) == 0 {
		return nil, nil
	}
	return items, nil
}

// PrepareItems loads optional inventory entries and attaches the correct region information to each item.
// Without a paths file, the returned inventory is nil and downstream loaders discover parameters from AWS SSM.
func PrepareItems(ctx context.Context, cfg *Config) ([]inventory.Item, error) {
	if cfg.AllRegions {
		ensureAllRegionsSeedRegion(cfg)
	} else if err := ensureRegions(ctx, cfg); err != nil {
		return nil, err
	}
	items, err := LoadItems(*cfg)
	if err != nil {
		return nil, err
	}
	if len(items) > 0 {
		applyItemRegions(items, *cfg)
	}
	return items, nil
}

func dedupeItemsByPath(items []inventory.Item) []inventory.Item {
	seen := map[string]bool{}
	out := make([]inventory.Item, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Path) == "" || seen[item.Path] {
			continue
		}
		seen[item.Path] = true
		out = append(out, item)
	}
	return out
}

func filterRecordsByNames(records []secretfmt.Record, names []string) []secretfmt.Record {
	if len(names) == 0 {
		return records
	}
	allowed := map[string]bool{}
	for _, name := range names {
		allowed[name] = true
	}
	out := make([]secretfmt.Record, 0, len(records))
	for _, record := range records {
		if allowed[record.Path] {
			out = append(out, record)
		}
	}
	return out
}

func namesFromItems(items []inventory.Item) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item.Path != "" {
			out = append(out, item.Path)
		}
	}
	return dedupeStrings(out)
}

func combinedFilterNames(cfg Config, items []inventory.Item) []string {
	return dedupeStrings(append(append([]string{}, cfg.Names...), namesFromItems(items)...))
}

func nameInScope(name string, cfg Config) bool {
	items, err := LoadItems(cfg)
	if err != nil || len(items) == 0 {
		return true
	}
	for _, item := range items {
		if item.Path == name {
			return true
		}
	}
	return false
}

func requireNameInScope(name string, cfg Config, command string) error {
	if nameInScope(name, cfg) {
		return nil
	}
	return fmt.Errorf("%s name %q is outside --names/--names-file scope", command, name)
}

func requireFieldForCommand(cfg Config, field, command string) error {
	if !fieldAllowed(cfg.Fields, field) {
		return fmt.Errorf("%s requires field %q; remove --fields or include %s", command, field, field)
	}
	return nil
}

func rejectDisallowedFlag(ctx *cli.Context, cfg Config, flagName, field string) error {
	if ctx.IsSet(flagName) && !fieldAllowed(cfg.Fields, field) {
		return fmt.Errorf("--%s requires field %q; remove --fields or include %s", flagName, field, field)
	}
	return nil
}

func parseSingleField(value, flagName string, defaultField string) (string, error) {
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

func importDefaultOptions(ctx *cli.Context, cfg Config) (ssm.PutParameterOptions, error) {
	tier, err := ssm.ParseParameterTier(ctx.String("default-tier"))
	if err != nil {
		return ssm.PutParameterOptions{}, err
	}
	dataType, err := ssm.ParseParameterDataType(ctx.String("default-data-type"))
	if err != nil {
		return ssm.PutParameterOptions{}, err
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
	return opts, nil
}

func importOptionsForRecord(record secretfmt.Record, defaults ssm.PutParameterOptions, cfg Config) (ssm.PutParameterOptions, error) {
	opts := defaults
	if fieldAllowed(cfg.Fields, "tier") && recordHasField(record, "tier") && strings.TrimSpace(record.Tier) != "" {
		tier, err := ssm.ParseParameterTier(record.Tier)
		if err != nil {
			return ssm.PutParameterOptions{}, err
		}
		opts.Tier = tier
	}
	if fieldAllowed(cfg.Fields, "data-type") && recordHasField(record, "data-type") && strings.TrimSpace(record.DataType) != "" {
		dataType, err := ssm.ParseParameterDataType(record.DataType)
		if err != nil {
			return ssm.PutParameterOptions{}, err
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
		return ssm.PutParameterOptions{}, err
	}
	dataType, err := ssm.ParseParameterDataType(ctx.String("data-type"))
	if err != nil {
		return ssm.PutParameterOptions{}, err
	}
	policies := ctx.String("policies")
	if file := strings.TrimSpace(ctx.String("policies-file")); file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return ssm.PutParameterOptions{}, err
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
		return errors.New("AWS region is required; pass --regions, set AWS_REGION/AWS_DEFAULT_REGION, or configure a default region in the AWS profile")
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

// applyItemRegions mutates loaded inventory items so the UI/export code can interpret their region scope uniformly.
// A concrete region means direct lookup; "*" means the item should be searched across the requested region set.
func applyItemRegions(items []inventory.Item, cfg Config) {
	for i := range items {
		if cfg.AllRegions || len(cfg.Regions) > 1 {
			items[i].Region = "*"
		} else {
			items[i].Region = cfg.Region
		}
	}
}

func stringFlagValue(ctx *cli.Context, name, envName, fallback string) string {
	if ctx.IsSet(name) {
		return ctx.String(name)
	}
	if envName != "" {
		if value := strings.TrimSpace(os.Getenv(envName)); value != "" {
			return value
		}
	}
	return fallback
}

func boolFlagValue(ctx *cli.Context, name, envName string, fallback bool) bool {
	if ctx.IsSet(name) {
		return ctx.Bool(name)
	}
	if envName != "" {
		if value := strings.TrimSpace(os.Getenv(envName)); value != "" {
			return parseBoolEnv(value)
		}
	}
	return fallback
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
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
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
		return err
	}
	regionLabel := cfg.Region
	regions := append([]string(nil), cfg.Regions...)
	if cfg.AllRegions {
		regionLabel = "all regions"
		regions, err = client.ListRegions(ctx.Context)
		if err != nil {
			return fmt.Errorf("list AWS regions: %w", err)
		}
	} else if len(regions) > 1 {
		regionLabel = strings.Join(regions, ", ")
	}
	return ui.RunInteractive(ctx.Context, client, items, ui.Options{
		Region:                    regionLabel,
		Regions:                   regions,
		Profile:                   cfg.Profile,
		NamesFile:                 cfg.NamesFile,
		NoColor:                   cfg.NoColor,
		Keymap:                    cfg.Keymap,
		ShowColumns:               cfg.ShowColumns,
		Sort:                      cfg.Sort,
		Fields:                    cfg.Fields,
		IncludeValues:             includeValuesForFields(cfg.Fields),
		ShowSecureValues:          cfg.ShowSecureValues,
		AllowNamesFileUpdate:      cfg.AllowNamesFileUpdate,
		NoConfirmOverwriteFile:    cfg.NoConfirmOverwriteFile,
		NoConfirmWriteSecureValue: cfg.NoConfirmWriteSecureValue,
		NoConfirmDeleteOne:        cfg.NoConfirmDeleteOne,
		NoConfirmDeleteAll:        cfg.NoConfirmDeleteAll,
	})
}

// Get prints one selected parameter field or writes it to a file.
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
		return errors.New("multiple --regions values are only supported for interactive and export")
	}
	args, flags, err := parseGetArgFlags(ctx.Args().Slice(), ctx.String("file"), ctx.String("field"))
	if err != nil {
		return err
	}
	if len(args) != 1 {
		return errors.New("get requires parameter name")
	}
	if err := requireFieldForCommand(cfg, flags.field, "get"); err != nil {
		return err
	}
	if err := requireNameInScope(args[0], cfg, "get"); err != nil {
		return err
	}
	if err := ensureRegions(ctx.Context, &cfg); err != nil {
		return err
	}
	value, err := getParameterField(ctx.Context, NewClient(cfg), args[0], flags.field, cfg.Region)
	if err != nil {
		return err
	}
	if flags.file != "" {
		return os.WriteFile(flags.file, []byte(value), 0o600)
	}
	fmt.Print(value)
	if !strings.HasSuffix(value, "\n") {
		fmt.Println()
	}
	return nil
}

type getArgFlags struct {
	file  string
	field string
}

func parseGetArgFlags(raw []string, initialFile string, initialField string) ([]string, getArgFlags, error) {
	field, err := parseSingleField(initialField, "field", "value")
	if err != nil {
		return nil, getArgFlags{}, err
	}
	args := []string{}
	flags := getArgFlags{file: initialFile, field: field}
	for i := 0; i < len(raw); i++ {
		arg := raw[i]
		switch {
		case arg == "--file":
			if i+1 >= len(raw) {
				return nil, getArgFlags{}, errors.New("--file requires a value")
			}
			flags.file = raw[i+1]
			i++
		case strings.HasPrefix(arg, "--file="):
			flags.file = strings.TrimPrefix(arg, "--file=")
		case arg == "--field":
			if i+1 >= len(raw) {
				return nil, getArgFlags{}, errors.New("--field requires a value")
			}
			field, err := parseSingleField(raw[i+1], "field", "value")
			if err != nil {
				return nil, getArgFlags{}, err
			}
			flags.field = field
			i++
		case strings.HasPrefix(arg, "--field="):
			field, err := parseSingleField(strings.TrimPrefix(arg, "--field="), "field", "value")
			if err != nil {
				return nil, getArgFlags{}, err
			}
			flags.field = field
		default:
			args = append(args, arg)
		}
	}
	return args, flags, nil
}

func getParameterField(ctx context.Context, client ssm.Client, name, field, region string) (string, error) {
	switch field {
	case "value", "version", "len", "sha256":
		param, err := client.Get(ctx, name)
		if err != nil {
			return "", err
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
		return "", err
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

// Set writes one String, StringList, or SecureString value from an argument or file.
// Unless --override is used, it refuses to overwrite an existing non-empty value; when --type is omitted it preserves the existing parameter type.
func Set(ctx *cli.Context) error {
	cfg, err := ConfigFromCLI(ctx)
	if err != nil {
		return err
	}
	if err := requireFieldForCommand(cfg, "value", "set"); err != nil {
		return err
	}
	if cfg.AllRegions {
		return errors.New("--all-regions is not supported for set; use interactive on an existing regional row")
	}
	if len(cfg.Regions) > 1 {
		return errors.New("multiple --regions values are only supported for interactive and export")
	}
	args, flags, err := parseCommonArgFlags(ctx.Args().Slice(), ctx.String("file"), ctx.Bool("override"), ctx.String("type"))
	if err != nil {
		return err
	}
	if len(args) < 1 || len(args) > 2 {
		return errors.New("set requires parameter path and value, or parameter path with --file")
	}
	path := args[0]
	if err := requireNameInScope(path, cfg, "set"); err != nil {
		return err
	}
	if flags.file != "" && len(args) == 2 {
		return errors.New("set accepts either value argument or --file, not both")
	}
	var value string
	if flags.file != "" {
		data, err := os.ReadFile(flags.file)
		if err != nil {
			return err
		}
		value = string(data)
	} else {
		if len(args) != 2 {
			return errors.New("set requires value when --file is not provided")
		}
		value = args[1]
	}

	if commandRegion := strings.TrimSpace(ctx.String("region")); commandRegion != "" {
		cfg.Region = commandRegion
		cfg.Regions = []string{commandRegion}
	}
	if err := ensureRegions(ctx.Context, &cfg); err != nil {
		return err
	}
	client := NewClient(cfg)
	existing, existingErr := client.Get(ctx.Context, path)
	if existingErr != nil && !errors.Is(existingErr, ssm.ErrNotFound) {
		return existingErr
	}
	if !flags.override && existingErr == nil && existing.Value != "" {
		return fmt.Errorf("parameter already has a non-empty value: %s; use --override", path)
	}
	if ctx.IsSet("type") && !fieldAllowed(cfg.Fields, "type") {
		return errors.New("--type requires field \"type\"; remove --fields or include type")
	}
	parameterType, err := resolveSetType(flags.parameterType, existing.Type)
	if err != nil {
		return err
	}
	opts, err := setOptions(ctx, cfg, flags.override)
	if err != nil {
		return err
	}
	return client.PutParameterWithOptions(ctx.Context, path, value, parameterType, opts)
}

type commonArgFlags struct {
	file          string
	override      bool
	parameterType string
}

// parseCommonArgFlags supports both normal urfave/cli flags and command-tail flags.
// This lets users place --file/--override/--type after positional arguments while still returning clean positional args.
func parseCommonArgFlags(raw []string, initialFile string, initialOverride bool, initialType string) ([]string, commonArgFlags, error) {
	args := []string{}
	flags := commonArgFlags{file: initialFile, override: initialOverride, parameterType: initialType}
	for i := 0; i < len(raw); i++ {
		arg := raw[i]
		switch {
		case arg == "--override":
			flags.override = true
		case arg == "--file":
			if i+1 >= len(raw) {
				return nil, commonArgFlags{}, errors.New("--file requires a value")
			}
			flags.file = raw[i+1]
			i++
		case strings.HasPrefix(arg, "--file="):
			flags.file = strings.TrimPrefix(arg, "--file=")
		case arg == "--type" || arg == "-t":
			if i+1 >= len(raw) {
				return nil, commonArgFlags{}, errors.New("--type requires a value")
			}
			flags.parameterType = raw[i+1]
			i++
		case strings.HasPrefix(arg, "--type="):
			flags.parameterType = strings.TrimPrefix(arg, "--type=")
		case strings.HasPrefix(arg, "-t="):
			flags.parameterType = strings.TrimPrefix(arg, "-t=")
		default:
			args = append(args, arg)
		}
	}
	return args, flags, nil
}

func resolveSetType(requestedType, existingType string) (ssm.ParameterType, error) {
	for _, candidate := range []string{requestedType, existingType} {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		return ssm.ParseParameterType(candidate)
	}
	return ssm.DefaultParameterType, nil
}

func resolveImportType(defaultType, existingType, recordType string) (ssm.ParameterType, error) {
	for _, candidate := range []string{recordType, existingType, defaultType} {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		return ssm.ParseParameterType(candidate)
	}
	return ssm.DefaultParameterType, nil
}

// PrepareImportItems loads path inventory only when the selected import format needs it.
// Dotenv imports need paths.txt to resolve aliases such as JWT_SECRET back to full SSM names.
// JSON imports already contain full SSM names as keys, so paths.txt is optional for that format.
func PrepareImportItems(ctx context.Context, cfg *Config, format string) ([]inventory.Item, error) {
	switch format {
	case "dotenv":
		if cfg.NamesFile == "" {
			return nil, errors.New("--names-file is required for dotenv import")
		}
		return PrepareItems(ctx, cfg)
	case "json":
		if cfg.NamesFile == "" {
			return nil, ensureRegions(ctx, cfg)
		}
		return PrepareItems(ctx, cfg)
	default:
		return nil, fmt.Errorf("unsupported format: %s", format)
	}
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
		return errors.New("--all-regions is not supported for import; specify --regions")
	}
	if len(cfg.Regions) > 1 {
		return errors.New("multiple --regions values are only supported for interactive and export")
	}
	format := ctx.String("format")
	items, err := PrepareImportItems(ctx.Context, &cfg, format)
	if err != nil {
		return err
	}

	reader, closeFn, err := inputReader(ctx.String("from-file"))
	if err != nil {
		return err
	}
	defer closeFn()

	records, err := parseImport(format, reader, items)
	if err != nil {
		return err
	}
	records = filterRecordsByNames(records, combinedFilterNames(cfg, items))
	if err := requireFieldForCommand(cfg, "value", "import"); err != nil {
		return err
	}
	_, importFlags, err := parseCommonArgFlags(ctx.Args().Slice(), "", ctx.Bool("default-override"), ctx.String("default-type"))
	if err != nil {
		return err
	}

	defaultOpts, err := importDefaultOptions(ctx, cfg)
	if err != nil {
		return err
	}

	client := NewClient(cfg)
	paths := make([]string, 0, len(records))
	for _, record := range records {
		paths = append(paths, record.Path)
	}

	values, errs := client.GetMany(ctx.Context, paths)
	var skipped []string
	writer := uilive.New()
	writer.Start()
	defer writer.Stop()
	for i, record := range records {
		fmt.Fprintf(writer, "Importing %d/%d...\n%s\n", i, len(records), record.Path)
		if !importFlags.override {
			if existing, ok := values[record.Path]; ok && existing.Value != "" {
				skipped = append(skipped, record.Path)
				continue
			}
			if err, ok := errs[record.Path]; ok && !errors.Is(err, ssm.ErrNotFound) {
				return err
			}
		}
		existingType := ""
		if existing, ok := values[record.Path]; ok {
			existingType = existing.Type
		}
		recordType := record.Type
		if !fieldAllowed(cfg.Fields, "type") || !recordHasField(record, "type") {
			recordType = ""
		}
		defaultType := importFlags.parameterType
		if !fieldAllowed(cfg.Fields, "type") {
			defaultType = ""
		}
		parameterType, err := resolveImportType(defaultType, existingType, recordType)
		if err != nil {
			return fmt.Errorf("%s: %w", record.Path, err)
		}
		opts, err := importOptionsForRecord(record, defaultOpts, cfg)
		if err != nil {
			return fmt.Errorf("%s: %w", record.Path, err)
		}
		opts.Overwrite = importFlags.override
		writeClient := client
		if fieldAllowed(cfg.Fields, "region") && recordHasField(record, "region") && strings.TrimSpace(record.Region) != "" {
			writeClient = client.ForRegion(strings.TrimSpace(record.Region))
		}
		if err := writeClient.PutParameterWithOptions(ctx.Context, record.Path, record.Value, parameterType, opts); err != nil {
			return err
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
			return fmt.Errorf("list AWS regions: %w", err)
		}
	}

	var statuses []ui.Status
	if ctx.String("to-file") == "" {
		statuses = ui.LoadStatusesForRegions(ctx.Context, client, items, includeValuesForFields(cfg.Fields), regions)
	} else {
		statuses = ui.LoadStatusesWithProgressForRegions(ctx.Context, client, items, includeValuesForFields(cfg.Fields), regions)
	}

	fields := exportFields(cfg)
	var records []secretfmt.Record
	for _, status := range statuses {
		if !status.Exists && !ctx.Bool("include-missing") {
			continue
		}
		records = append(records, exportRecordFromStatus(status, fields))
	}

	writer, closeFn, err := outputWriter(ctx.String("to-file"))
	if err != nil {
		return err
	}
	defer closeFn()

	switch ctx.String("format") {
	case "dotenv":
		return secretfmt.ExportDotenv(writer, records)
	case "json":
		return secretfmt.ExportJSON(writer, records)
	default:
		return fmt.Errorf("unsupported format: %s", ctx.String("format"))
	}
}

// parseImport dispatches import parsing by format while keeping the Import command handler format-agnostic.
func parseImport(format string, reader io.Reader, items []inventory.Item) ([]secretfmt.Record, error) {
	switch format {
	case "dotenv":
		return secretfmt.ImportDotenv(reader, items)
	case "json":
		return secretfmt.ImportJSON(reader)
	default:
		return nil, fmt.Errorf("unsupported format: %s", format)
	}
}

// inputReader returns stdin or an opened file plus a cleanup function.
// The no-op cleanup for stdin lets callers defer closeFn unconditionally.
func inputReader(file string) (io.Reader, func(), error) {
	if file == "" {
		return os.Stdin, func() {}, nil
	}
	f, err := os.Open(file)
	if err != nil {
		return nil, func() {}, err
	}
	return f, func() { _ = f.Close() }, nil
}

// outputWriter returns stdout or a freshly created private file plus a cleanup function.
// Exported secret material is written with 0600 permissions so only the current user can read it by default.
func outputWriter(file string) (io.Writer, func(), error) {
	if file == "" {
		return os.Stdout, func() {}, nil
	}
	f, err := os.OpenFile(file, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, func() {}, err
	}
	return f, func() { _ = f.Close() }, nil
}

// findRepoRoot walks upward from the current directory looking for the original infra repository layout.
// It is kept as a small helper for legacy discovery workflows that expect clusters/ and terraform/ directories.
func findRepoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for dir := wd; ; dir = filepath.Dir(dir) {
		if exists(filepath.Join(dir, "clusters")) && exists(filepath.Join(dir, "terraform")) {
			return dir, nil
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	return "", errors.New("could not find repo root")
}

// exists reports whether a path exists and is accessible to stat.
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
