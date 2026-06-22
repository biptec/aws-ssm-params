package ui

import (
	"context"
	"log/slog"
	"sort"
	"time"

	crerr "github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/logging"
	"github.com/biptec/aws-ssm-params/internal/ssm"
)

// LoadProgress reports status-loading progress to either the interactive TUI or the non-interactive live writer.
// done/total are item counters within the current region scan, region names the region being scanned, and chunk
// contains the paths currently being requested from SSM.
type LoadProgress func(done, total int, region string, chunk []inventory.Item)

// StatusBatch receives partial status rows while a long-running interactive load is still in progress.
type StatusBatch func([]Status)

// LoadStatuses loads statuses without progress output using item-level region information.
func LoadStatuses(ctx context.Context, client ssm.Client, items []inventory.Item, includeValues bool) []Status {
	return LoadStatusesForRegions(ctx, client, items, includeValues, nil)
}

// LoadStatusesForRegions loads statuses without progress output but with an explicit region scan list.
func LoadStatusesForRegions(ctx context.Context, client ssm.Client, items []inventory.Item, includeValues bool, regions []string) []Status {
	return LoadStatusesBatchForRegions(ctx, client, items, includeValues, regions, nil)
}

// LoadStatusesBatch is the shared status loader entry point for item-level region lookup.
func LoadStatusesBatch(ctx context.Context, client ssm.Client, items []inventory.Item, includeValues bool, progress LoadProgress) []Status {
	return LoadStatusesBatchForRegions(ctx, client, items, includeValues, nil, progress)
}

// LoadStatusesBatchForRegions chooses the correct status-loading strategy.
// Concrete-region items are loaded directly by their item region; wildcard items are expanded across either an explicit
// region list or every enabled AWS region discovered from the client.
func LoadStatusesBatchForRegions(ctx context.Context, client ssm.Client, items []inventory.Item, includeValues bool, regions []string, progress LoadProgress) []Status {
	return loadStatusesBatchForRegions(ctx, client, items, includeValues, regions, progress, nil)
}

// LoadStatusesBatchForRegionsStream is the interactive loader variant. It emits partial rows as soon as each
// region/chunk is loaded, then returns the complete final status set.
func LoadStatusesBatchForRegionsStream(ctx context.Context, client ssm.Client, items []inventory.Item, includeValues bool, regions []string, progress LoadProgress, batch StatusBatch) []Status {
	return loadStatusesBatchForRegions(ctx, client, items, includeValues, regions, progress, batch)
}

func loadStatusesBatchForRegions(ctx context.Context, client ssm.Client, items []inventory.Item, includeValues bool, regions []string, progress LoadProgress, batch StatusBatch) []Status {
	if len(items) == 0 {
		return loadAllStatusesForRegions(ctx, client, includeValues, regions, progress, batch)
	}
	if containsAllRegionItems(items) {
		if len(regions) > 0 {
			return loadStatusesRegions(ctx, client, items, includeValues, regions, progress, batch)
		}
		return loadStatusesAllRegions(ctx, client, items, includeValues, progress, batch)
	}
	return loadStatusesByItemRegion(ctx, client, items, includeValues, progress, batch)
}

// containsAllRegionItems reports whether at least one inventory item needs wildcard region expansion.
func containsAllRegionItems(items []inventory.Item) bool {
	for _, item := range items {
		if item.Region == "*" {
			return true
		}
	}
	return false
}

// loadStatusesByItemRegion loads items that already have concrete regions.
// Items are processed in chunks of ten and grouped by region inside each chunk so SSM batch calls stay region-correct.
func loadStatusesByItemRegion(ctx context.Context, client ssm.Client, items []inventory.Item, includeValues bool, progress LoadProgress, batch StatusBatch) []Status {
	statuses := make([]Status, 0, len(items))
	metas := map[string]ssm.Metadata{}
	values := map[string]ssm.Parameter{}
	errs := map[string]error{}

	for start := 0; start < len(items); start += 10 {
		end := start + 10
		if end > len(items) {
			end = len(items)
		}
		chunkItems := items[start:end]
		if progress != nil {
			progress(start, len(items), chunkRegion(chunkItems), chunkItems)
		}

		byRegion := map[string][]inventory.Item{}
		for _, item := range chunkItems {
			byRegion[item.Region] = append(byRegion[item.Region], item)
		}
		for region, regionItems := range byRegion {
			paths := make([]string, 0, len(regionItems))
			for _, item := range regionItems {
				paths = append(paths, item.Path)
			}
			regionalClient := client.ForRegion(region)
			described := regionalClient.DescribeMany(ctx, paths)
			for path := range described {
				metas[itemKey(region, path)] = described[path]
			}
			chunkValues, chunkErrs := regionalClient.GetMany(ctx, paths)
			for path, value := range chunkValues {
				values[itemKey(region, path)] = value
			}
			for path, err := range chunkErrs {
				errs[itemKey(region, path)] = err
			}
		}
		if batch != nil {
			chunkStatuses := make([]Status, 0, len(chunkItems))
			for _, item := range chunkItems {
				chunkStatuses = append(chunkStatuses, statusFromMaps(item, item.Region, metas, values, errs, includeValues))
			}
			emitStatusBatch(batch, chunkStatuses)
		}
	}
	if progress != nil {
		progress(len(items), len(items), "", nil)
	}

	for _, item := range items {
		status := statusFromMaps(item, item.Region, metas, values, errs, includeValues)
		statuses = append(statuses, status)
	}
	return statuses
}

func emitStatusBatch(batch StatusBatch, statuses []Status) {
	if batch == nil || len(statuses) == 0 {
		return
	}
	batch(append([]Status(nil), statuses...))
}

// loadAllStatusesForRegions discovers every SSM parameter in the selected regions.
// It is used when no paths file was provided, so the TUI can be opened as a full SSM parameter browser/manager.
func loadAllStatusesForRegions(ctx context.Context, client ssm.Client, includeValues bool, regions []string, progress LoadProgress, batch StatusBatch) []Status {
	if len(regions) == 0 {
		if region := client.DefaultRegion(); region != "" {
			regions = []string{region}
		} else {
			regions = []string{""}
		}
	}
	statuses := make([]Status, 0, len(regions)*64)
	for _, region := range regions {
		statuses = append(statuses, loadAllStatusesRegion(ctx, client.ForRegion(region), region, includeValues, progress, batch)...)
	}
	sort.Slice(statuses, func(i, j int) bool {
		if statuses[i].Item.Region != statuses[j].Item.Region {
			return statuses[i].Item.Region < statuses[j].Item.Region
		}
		return statuses[i].Item.Path < statuses[j].Item.Path
	})
	return statuses
}

func loadAllStatusesRegion(ctx context.Context, client ssm.Client, region string, includeValues bool, progress LoadProgress, batch StatusBatch) []Status {
	if progress != nil {
		progress(0, 0, region, nil)
	}
	metas, err := client.ListParameterMetadata(ctx)
	if err != nil {
		return []Status{{Item: inventory.Item{Path: "(scan error)", Region: region}, Type: ssm.DefaultParameterType.String(), Error: err.Error()}}
	}
	items := make([]inventory.Item, 0, len(metas))
	metaByKey := map[string]ssm.Metadata{}
	for i := range metas {
		meta := metas[i]
		if meta.Name == "" {
			continue
		}
		if meta.Region == "" {
			meta.Region = region
		}
		item := inventory.Item{Path: meta.Name, Region: meta.Region, Kind: "ssm", Source: "aws:ssm", SecretName: pathBase(meta.Name)}
		items = append(items, item)
		metaByKey[itemKey(item.Region, item.Path)] = meta
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })

	values := map[string]ssm.Parameter{}
	errs := map[string]error{}
	if includeValues {
		for start := 0; start < len(items); start += 10 {
			end := start + 10
			if end > len(items) {
				end = len(items)
			}
			chunkItems := items[start:end]
			if progress != nil {
				progress(start, len(items), region, chunkItems)
			}
			paths := make([]string, 0, len(chunkItems))
			for _, item := range chunkItems {
				paths = append(paths, item.Path)
			}
			chunkValues, chunkErrs := client.GetMany(ctx, paths)
			for path, value := range chunkValues {
				if value.Region == "" {
					value.Region = region
				}
				values[itemKey(region, path)] = value
			}
			for path, err := range chunkErrs {
				errs[itemKey(region, path)] = err
			}
		}
	}
	if progress != nil {
		progress(len(items), len(items), region, nil)
	}

	statuses := make([]Status, 0, len(items))
	for _, item := range items {
		status := statusFromMaps(item, item.Region, metaByKey, values, errs, includeValues)
		if status.isMissing() {
			status = statusFromMetadata(item, metaByKey[itemKey(item.Region, item.Path)])
		}
		statuses = append(statuses, status)
	}
	emitStatusBatch(batch, statuses)
	return statuses
}

// loadStatusesAllRegions discovers enabled AWS regions, then scans wildcard items across all of them.
// If region discovery fails, every item receives an error status so the UI can show the failure inline.
func loadStatusesAllRegions(ctx context.Context, client ssm.Client, items []inventory.Item, includeValues bool, progress LoadProgress, batch StatusBatch) []Status {
	regions, err := client.ListRegions(ctx)
	if err != nil {
		statuses := make([]Status, 0, len(items))
		for _, item := range items {
			status := Status{Item: item, Type: ssm.DefaultParameterType.String(), Error: err.Error()}
			statuses = append(statuses, status)
		}
		return statuses
	}
	return loadStatusesRegions(ctx, client, items, includeValues, regions, progress, batch)
}

// loadStatusesRegions scans wildcard items across a fixed region list.
// It creates one status row per region where a parameter exists or errors, then appends a wildcard missing row
// for paths that were not found in any scanned region.
func loadStatusesRegions(ctx context.Context, client ssm.Client, items []inventory.Item, includeValues bool, regions []string, progress LoadProgress, batch StatusBatch) []Status {
	logger := logging.FromContext(ctx)
	statuses := []Status{}
	found := map[string]bool{}
	var previousCompleted time.Time
	for index, region := range regions {
		if !previousCompleted.IsZero() {
			logger.LogAttrs(ctx, slog.LevelDebug, "region scan gap", slog.String("region", region), slog.Int("index", index), slog.Int64("duration_ms", elapsedStatusMillis(previousCompleted)))
		}
		started := time.Now()
		logger.LogAttrs(ctx, slog.LevelDebug, "region scan started", slog.String("region", region), slog.Int("index", index), slog.Int("item_count", len(items)), slog.Bool("include_values", includeValues))
		regionStatuses, regionFound := loadStatusesOneRegion(ctx, client.ForRegion(region), items, includeValues, region, progress, batch)
		completed := time.Now()
		logger.LogAttrs(ctx, slog.LevelDebug, "region scan completed", slog.String("region", region), slog.Int("index", index), slog.Int("status_count", len(regionStatuses)), slog.Int("found_count", len(regionFound)), slog.Int64("duration_ms", int64(completed.Sub(started)/time.Millisecond)))
		previousCompleted = completed
		statuses = append(statuses, regionStatuses...)
		for path := range regionFound {
			found[path] = true
		}
	}
	if progress != nil {
		progress(len(items), len(items), "", nil)
	}
	for _, item := range items {
		if !found[item.Path] {
			item.Region = "*"
			statuses = append(statuses, Status{Item: item, Type: ssm.DefaultParameterType.String()})
		}
	}
	return statuses
}

func loadStatusesOneRegion(ctx context.Context, client ssm.Client, items []inventory.Item, includeValues bool, region string, progress LoadProgress, batch StatusBatch) (statuses []Status, found map[string]bool) {
	logger := logging.FromContext(ctx)
	found = map[string]bool{}
	if progress != nil {
		progress(0, len(items), region, nil)
	}

	metadataStarted := time.Now()
	logger.LogAttrs(ctx, slog.LevelDebug, "region metadata scan started", slog.String("region", region))
	metas, err := client.ListParameterMetadata(ctx)
	metadataDuration := elapsedStatusMillis(metadataStarted)
	if err != nil {
		logger.LogAttrs(ctx, slog.LevelDebug, "region metadata scan failed", slog.String("region", region), slog.Int64("duration_ms", metadataDuration), slog.Any("error", err))
		return []Status{{Item: inventory.Item{Path: "(scan error)", Region: region}, Type: ssm.DefaultParameterType.String(), Error: err.Error()}}, found
	}
	logger.LogAttrs(ctx, slog.LevelDebug, "region metadata scan completed", slog.String("region", region), slog.Int64("duration_ms", metadataDuration), slog.Int("metadata_count", len(metas)))

	matchStarted := time.Now()
	metaByPath := make(map[string]ssm.Metadata, len(metas))
	for i := range metas {
		meta := metas[i]
		if meta.Name == "" {
			continue
		}
		if meta.Region == "" {
			meta.Region = region
		}
		metaByPath[meta.Name] = meta
	}

	matchedItems := make([]inventory.Item, 0, len(items))
	metaByKey := map[string]ssm.Metadata{}
	for _, item := range items {
		meta, ok := metaByPath[item.Path]
		if !ok {
			continue
		}
		item.Region = region
		matchedItems = append(matchedItems, item)
		metaByKey[itemKey(region, item.Path)] = meta
		found[item.Path] = true
	}
	logger.LogAttrs(ctx, slog.LevelDebug, "region metadata match completed", slog.String("region", region), slog.Int64("duration_ms", elapsedStatusMillis(matchStarted)), slog.Int("matched_count", len(matchedItems)), slog.Int("item_count", len(items)))

	if !includeValues {
		statuses = make([]Status, 0, len(matchedItems))
		for _, item := range matchedItems {
			statuses = append(statuses, statusFromMetadata(item, metaByKey[itemKey(region, item.Path)]))
		}
		emitStatusBatch(batch, statuses)
		if progress != nil {
			progress(len(items), len(items), region, nil)
		}
		return statuses, found
	}

	for start := 0; start < len(matchedItems); start += 10 {
		end := start + 10
		if end > len(matchedItems) {
			end = len(matchedItems)
		}
		chunkItems := matchedItems[start:end]
		if progress != nil {
			progress(start, len(matchedItems), region, chunkItems)
		}

		paths := make([]string, 0, len(chunkItems))
		for _, item := range chunkItems {
			paths = append(paths, item.Path)
		}

		valueLookupStarted := time.Now()
		logger.LogAttrs(ctx, slog.LevelDebug, "region value lookup started", slog.String("region", region), slog.Int("start", start), slog.Int("count", len(chunkItems)))
		values, errs := client.GetMany(ctx, paths)
		logger.LogAttrs(ctx, slog.LevelDebug, "region value lookup completed", slog.String("region", region), slog.Int("start", start), slog.Int("value_count", len(values)), slog.Int("error_count", len(errs)), slog.Int64("duration_ms", elapsedStatusMillis(valueLookupStarted)))
		chunkStatuses := make([]Status, 0, len(chunkItems))
		for _, item := range chunkItems {
			meta := metaByKey[itemKey(region, item.Path)]
			if param, ok := values[item.Path]; ok {
				status := statusFromValue(item, param, meta, includeValues)
				statuses = append(statuses, status)
				chunkStatuses = append(chunkStatuses, status)
				continue
			}
			if err := errs[item.Path]; err != nil && !crerr.Is(err, ssm.ErrNotFound) {
				status := Status{Item: item, Type: ssm.DefaultParameterType.String(), Error: err.Error()}
				statuses = append(statuses, status)
				chunkStatuses = append(chunkStatuses, status)
				continue
			}
			status := statusFromMetadata(item, meta)
			statuses = append(statuses, status)
			chunkStatuses = append(chunkStatuses, status)
		}
		emitStatusBatch(batch, chunkStatuses)
	}
	if progress != nil {
		progress(len(matchedItems), len(matchedItems), region, nil)
	}
	return statuses, found
}
