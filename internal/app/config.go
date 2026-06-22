// Package app contains command implementations and configuration parsing for aws-ssm-params.
package app

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	crerr "github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/fileio"
	"github.com/biptec/aws-ssm-params/internal/filter"
	secretfmt "github.com/biptec/aws-ssm-params/internal/format"
	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/logging"
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
	regions := dedupeStrings(stringSliceFlagValue(ctx, "region", "AWS_SSM_PARAMS_REGION", "AWS_REGION"))
	if allRegions && len(regions) > 0 {
		return Config{}, errors.New("--region and --all-regions cannot be used together")
	}
	profile := stringFlagValueAny(ctx, "profile", "", "AWS_SSM_PARAMS_PROFILE", "AWS_PROFILE")
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
