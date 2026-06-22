package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	crerr "github.com/cockroachdb/errors"

	outputfmt "github.com/biptec/aws-ssm-params/internal/format"
	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/ssm"
)

type strictMetadataDescriber interface {
	DescribeManyStrict(context.Context, []string) (map[string]ssm.Metadata, map[string]error)
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

func applyRootPathToRecords(records []outputfmt.Record, rootPath string) ([]outputfmt.Record, error) {
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
	resolved := make([]outputfmt.Record, 0, len(records))
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
			record.Alias = outputfmt.AliasForPath(record.Path, inventory.Item{})
		}
		resolved = append(resolved, record)
	}
	return resolved, nil
}

func recordRegion(record outputfmt.Record, cfg Config) string {
	if fieldAllowed(cfg.Fields, "region") && recordHasField(record, "region") && strings.TrimSpace(record.Region) != "" {
		return strings.TrimSpace(record.Region)
	}
	return cfg.Region
}

func metadataForImportRecords(ctx context.Context, client ssm.Client, records []outputfmt.Record, cfg Config) (metadataByKey map[string]ssm.Metadata, errorsByKey map[string]error) {
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

func resolveImportType(defaultType string, existing ssm.Metadata, exists bool, record outputfmt.Record, cfg Config) (ssm.ParameterType, error) {
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
