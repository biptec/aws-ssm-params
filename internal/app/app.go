package app

import (
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
	PathsFile  string
	Region     string
	Regions    []string
	Profile    string
	AllRegions bool
	NoColor    bool
	Keymap     string
}

const allRegionsSeedRegion = "us-east-1"

// ConfigFromCLI converts raw urfave/cli state into a Config that the rest of the application can use.
// It enforces mutually exclusive region modes, falls back to AWS_PROFILE/AWS_REGION/AWS_DEFAULT_REGION,
// deduplicates repeated --region values, and decides whether a paths-file argument should be read for the command.
func ConfigFromCLI(ctx *cli.Context) (Config, error) {
	regionArgs := compactStrings(ctx.StringSlice("region"))
	if ctx.Bool("all-regions") && len(regionArgs) > 0 {
		return Config{}, errors.New("--region and --all-regions cannot be used together")
	}
	profile := ctx.String("profile")
	if profile == "" {
		profile = os.Getenv("AWS_PROFILE")
	}
	regions := dedupeStrings(regionArgs)
	region := ""
	if len(regions) > 0 {
		region = regions[0]
	} else if !ctx.Bool("all-regions") {
		region = os.Getenv("AWS_REGION")
		if region == "" {
			region = os.Getenv("AWS_DEFAULT_REGION")
		}
		if region != "" {
			regions = []string{region}
		}
	}
	keymap := strings.ToLower(strings.TrimSpace(ctx.String("keymap")))
	if keymap == "" {
		keymap = "emacs"
	}
	if keymap != "emacs" && keymap != "vi" {
		return Config{}, fmt.Errorf("unsupported --keymap %q; expected emacs or vi", keymap)
	}

	pathsFile := ""
	switch ctx.Command.Name {
	case "get", "set":
		pathsFile = ""
	default:
		pathsFile = ctx.Args().First()
	}
	return Config{
		PathsFile:  pathsFile,
		Region:     region,
		Regions:    regions,
		Profile:    profile,
		AllRegions: ctx.Bool("all-regions"),
		NoColor:    ctx.Bool("no-color") || os.Getenv("NO_COLOR") != "",
		Keymap:     keymap,
	}, nil
}

// NewClient creates the concrete AWS SSM client for the selected profile and primary region.
// Keeping this in one function makes command handlers independent from the AWSCLI implementation details.
func NewClient(cfg Config) ssm.Client {
	return ssm.NewAWSCLI(cfg.Profile, cfg.Region)
}

// LoadItems reads the paths file and performs command-level validation around it.
// Commands that operate on an inventory need at least one SSM path; an empty or missing file is treated as a user error.
func LoadItems(cfg Config) ([]inventory.Item, error) {
	if cfg.PathsFile == "" {
		return nil, errors.New("paths file argument is required; usage: aws-ssm-params [global options] <paths-file>")
	}
	items, err := inventory.LoadPathsFile(cfg.PathsFile)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("paths file is empty: %s", cfg.PathsFile)
	}
	return items, nil
}

// PrepareItems loads inventory entries and attaches the correct region information to each item.
// Single-region mode resolves one concrete region, while multi-region/all-regions mode marks items with "*"
// so later status loading expands them into real regional rows only where parameters exist.
func PrepareItems(cfg *Config) ([]inventory.Item, error) {
	items, err := LoadItems(*cfg)
	if err != nil {
		return nil, err
	}
	if cfg.AllRegions {
		ensureAllRegionsSeedRegion(cfg)
	} else if err := ensureRegions(cfg); err != nil {
		return nil, err
	}
	applyItemRegions(items, *cfg)
	return items, nil
}

// ensureRegions guarantees that non-all-regions commands have one usable AWS region.
// It first asks the AWS CLI profile configuration if CLI/env flags did not provide a region, then mirrors the
// resolved primary region into cfg.Regions when the user did not pass an explicit list.
func ensureRegions(cfg *Config) error {
	if cfg.AllRegions {
		return nil
	}
	if cfg.Region == "" {
		cfg.Region = ssm.ResolveConfiguredRegion(cfg.Profile)
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
// Listing AWS regions itself requires a region-aware AWS CLI invocation, so all-regions mode uses us-east-1 by default.
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
	items, err := PrepareItems(&cfg)
	if err != nil {
		return err
	}
	client := NewClient(cfg)
	if err := client.CheckAccess(); err != nil {
		return err
	}
	regionLabel := cfg.Region
	regions := append([]string(nil), cfg.Regions...)
	if cfg.AllRegions {
		regionLabel = "all regions"
		regions, err = client.ListRegions()
		if err != nil {
			return fmt.Errorf("list AWS regions: %w", err)
		}
	} else if len(regions) > 1 {
		regionLabel = strings.Join(regions, ", ")
	}
	return ui.RunInteractive(client, items, ui.Options{
		Region:  regionLabel,
		Regions: regions,
		Profile: cfg.Profile,
		NoColor: cfg.NoColor,
		Keymap:  cfg.Keymap,
	})
}

// Get prints one parameter value or writes it to a file.
// It intentionally rejects all-regions and multi-region modes because a single path could resolve to multiple values.
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
	args, flags, err := parseCommonArgFlags(ctx.Args().Slice(), ctx.String("file"), false, "")
	if err != nil {
		return err
	}
	if len(args) != 1 {
		return errors.New("get requires parameter path")
	}
	if err := ensureRegions(&cfg); err != nil {
		return err
	}
	param, err := NewClient(cfg).Get(args[0])
	if err != nil {
		return err
	}
	if flags.file != "" {
		return os.WriteFile(flags.file, []byte(param.Value), 0o600)
	}
	fmt.Print(param.Value)
	if !strings.HasSuffix(param.Value, "\n") {
		fmt.Println()
	}
	return nil
}

// Set writes one String, StringList, or SecureString value from an argument or file.
// Unless --override is used, it refuses to overwrite an existing non-empty value; when --type is omitted it preserves the existing parameter type.
func Set(ctx *cli.Context) error {
	cfg, err := ConfigFromCLI(ctx)
	if err != nil {
		return err
	}
	if cfg.AllRegions {
		return errors.New("--all-regions is not supported for set; use interactive on an existing regional row")
	}
	if len(cfg.Regions) > 1 {
		return errors.New("multiple --region values are only supported for interactive and export")
	}
	args, flags, err := parseCommonArgFlags(ctx.Args().Slice(), ctx.String("file"), ctx.Bool("override"), ctx.String("type"))
	if err != nil {
		return err
	}
	if len(args) < 1 || len(args) > 2 {
		return errors.New("set requires parameter path and value, or parameter path with --file")
	}
	path := args[0]
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

	if err := ensureRegions(&cfg); err != nil {
		return err
	}
	client := NewClient(cfg)
	existing, existingErr := client.Get(path)
	if existingErr != nil && !errors.Is(existingErr, ssm.ErrNotFound) {
		return existingErr
	}
	if !flags.override && existingErr == nil && existing.Value != "" {
		return fmt.Errorf("parameter already has a non-empty value: %s; use --override", path)
	}
	parameterType, err := resolveSetType(flags.parameterType, existing.Type)
	if err != nil {
		return err
	}
	return client.PutParameter(path, value, parameterType)
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
// Dotenv imports need paths.txt to resolve aliases such as JWT_SECRET back to full SSM paths.
// JSON imports already contain full SSM paths as keys, so paths.txt is optional for that format.
func PrepareImportItems(cfg *Config, format string) ([]inventory.Item, error) {
	switch format {
	case "dotenv":
		return PrepareItems(cfg)
	case "json":
		if cfg.PathsFile == "" {
			return nil, ensureRegions(cfg)
		}
		return PrepareItems(cfg)
	default:
		return nil, fmt.Errorf("unsupported format: %s", format)
	}
}

// Import reads dotenv or JSON data, resolves each record to an SSM path, and writes the values to Parameter Store.
// It preloads existing values so it can skip protected non-empty parameters and report all skipped paths together.
func Import(ctx *cli.Context) error {
	cfg, err := ConfigFromCLI(ctx)
	if err != nil {
		return err
	}
	if cfg.AllRegions {
		return errors.New("--all-regions is not supported for import; specify --region")
	}
	if len(cfg.Regions) > 1 {
		return errors.New("multiple --region values are only supported for interactive and export")
	}
	format := ctx.String("format")
	items, err := PrepareImportItems(&cfg, format)
	if err != nil {
		return err
	}

	reader, closeFn, err := inputReader(ctx.String("file"))
	if err != nil {
		return err
	}
	defer closeFn()

	records, err := parseImport(format, reader, items)
	if err != nil {
		return err
	}
	_, importFlags, err := parseCommonArgFlags(ctx.Args().Slice(), "", ctx.Bool("override"), ctx.String("type"))
	if err != nil {
		return err
	}

	client := NewClient(cfg)
	paths := make([]string, 0, len(records))
	for _, record := range records {
		paths = append(paths, record.Path)
	}

	values, errs := client.GetMany(paths)
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
		parameterType, err := resolveImportType(importFlags.parameterType, existingType, record.Type)
		if err != nil {
			return fmt.Errorf("%s: %w", record.Path, err)
		}
		if err := client.PutParameter(record.Path, record.Value, parameterType); err != nil {
			return err
		}
	}
	if len(skipped) > 0 {
		return fmt.Errorf("skipped existing non-empty parameters without --override:\n%s", strings.Join(skipped, "\n"))
	}
	return nil
}

// Export loads statuses for the requested inventory and writes existing parameter values as dotenv or JSON.
// Missing parameters are omitted by default, but --include-missing can keep them as empty records for templates/backups.
func Export(ctx *cli.Context) error {
	cfg, err := ConfigFromCLI(ctx)
	if err != nil {
		return err
	}
	items, err := PrepareItems(&cfg)
	if err != nil {
		return err
	}
	var statuses []ui.Status
	if ctx.String("file") == "" {
		statuses = ui.LoadStatusesForRegions(NewClient(cfg), items, true, cfg.Regions)
	} else {
		statuses = ui.LoadStatusesWithProgressForRegions(NewClient(cfg), items, true, cfg.Regions)
	}

	var records []secretfmt.Record
	for _, status := range statuses {
		if !status.Exists && !ctx.Bool("include-missing") {
			continue
		}
		value := status.Value
		if !status.Exists {
			value = ""
		}
		records = append(records, secretfmt.Record{Path: status.Item.Path, Alias: secretfmt.AliasForItem(status.Item), Value: value, Type: status.Type})
	}

	writer, closeFn, err := outputWriter(ctx.String("file"))
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
