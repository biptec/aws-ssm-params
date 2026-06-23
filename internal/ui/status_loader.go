package ui

import (
	"context"
	"log/slog"
	"sort"
	"time"

	"github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/logging"
	"github.com/biptec/aws-ssm-params/internal/ssm"
)

// LoadProgress reports status-loading progress to either the interactive TUI or the non-interactive live writer.
// done/total are item counters within the current region scan, region names the region being scanned, and chunk
// contains the paths currently being requested from SSM.
type LoadProgress func(int, int, string, inventory.Items)

// StatusBatch receives partial status rows while a long-running interactive load is still in progress.
type StatusBatch func(Statuses)

type statusLoader struct {
	context       func() context.Context
	client        ssm.Client
	includeValues bool
	progress      LoadProgress
	batch         StatusBatch
}

func newStatusLoader(ctx context.Context, client ssm.Client, includeValues bool, progress LoadProgress, batch StatusBatch) statusLoader {
	return statusLoader{
		context:       func() context.Context { return ctx },
		client:        client,
		includeValues: includeValues,
		progress:      progress,
		batch:         batch,
	}
}

func (loader statusLoader) withClient(client ssm.Client) statusLoader {
	loader.client = client
	return loader
}

// LoadStatuses loads statuses without progress output using item-level region information.
func LoadStatuses(ctx context.Context, client ssm.Client, items inventory.Items, includeValues bool) Statuses {
	return LoadStatusesForRegions(ctx, client, items, includeValues, nil)
}

// LoadStatusesForRegions loads statuses without progress output but with an explicit region scan list.
func LoadStatusesForRegions(ctx context.Context, client ssm.Client, items inventory.Items, includeValues bool, regions []string) Statuses {
	return LoadStatusesBatchForRegions(ctx, client, items, includeValues, regions, nil)
}

// LoadStatusesBatch is the shared status loader entry point for item-level region lookup.
func LoadStatusesBatch(ctx context.Context, client ssm.Client, items inventory.Items, includeValues bool, progress LoadProgress) Statuses {
	return LoadStatusesBatchForRegions(ctx, client, items, includeValues, nil, progress)
}

// LoadStatusesBatchForRegions chooses the correct status-loading strategy.
// Concrete-region items are loaded directly by their item region; wildcard items are expanded across either an explicit
// region list or every enabled AWS region discovered from the client.
func LoadStatusesBatchForRegions(ctx context.Context, client ssm.Client, items inventory.Items, includeValues bool, regions []string, progress LoadProgress) Statuses {
	return newStatusLoader(ctx, client, includeValues, progress, nil).load(items, regions)
}

// LoadStatusesBatchForRegionsStream is the interactive loader variant. It emits partial rows as soon as each
// region/chunk is loaded, then returns the complete final status set.
func LoadStatusesBatchForRegionsStream(ctx context.Context, client ssm.Client, items inventory.Items, includeValues bool, regions []string, progress LoadProgress, batch StatusBatch) Statuses {
	return newStatusLoader(ctx, client, includeValues, progress, batch).load(items, regions)
}

func (loader statusLoader) load(items inventory.Items, regions []string) Statuses {
	if len(items) == 0 {
		return loader.loadAllForRegions(regions)
	}

	if items.HasWildcardRegion() {
		if len(regions) > 0 {
			return loader.loadRegions(items, regions)
		}

		return loader.loadAllRegions(items)
	}

	return loader.loadByItemRegion(items)
}

// loadStatusesByItemRegion loads items that already have concrete regions.
// Items are processed in chunks of ten and grouped by region inside each chunk so SSM batch calls stay region-correct.
func (loader statusLoader) loadByItemRegion(items inventory.Items) Statuses {
	statuses := make(Statuses, 0, len(items))
	metas := map[string]ssm.Metadata{}
	values := map[string]ssm.Parameter{}
	errs := map[string]error{}

	for start := 0; start < len(items); start += 10 {
		end := start + 10
		if end > len(items) {
			end = len(items)
		}

		chunkItems := items[start:end]
		if loader.progress != nil {
			loader.progress(start, len(items), chunkItems.CommonRegion(), chunkItems)
		}

		byRegion := map[string]inventory.Items{}
		for _, item := range chunkItems {
			byRegion[item.Region] = append(byRegion[item.Region], item)
		}

		for region, regionItems := range byRegion {
			paths := make([]string, 0, len(regionItems))
			for _, item := range regionItems {
				paths = append(paths, item.Path)
			}

			regionalClient := loader.client.ForRegion(region)

			described := regionalClient.DescribeMany(loader.context(), paths)
			for path := range described {
				metas[itemKey(region, path)] = described[path]
			}

			chunkValues, chunkErrs := regionalClient.GetMany(loader.context(), paths)
			for path, value := range chunkValues {
				values[itemKey(region, path)] = value
			}

			for path, err := range chunkErrs {
				errs[itemKey(region, path)] = err
			}
		}

		if loader.batch != nil {
			chunkStatuses := make(Statuses, 0, len(chunkItems))
			for _, item := range chunkItems {
				chunkStatuses = append(chunkStatuses, statusFromMaps(&item, item.Region, metas, values, errs, loader.includeValues))
			}

			loader.emit(chunkStatuses)
		}
	}

	if loader.progress != nil {
		loader.progress(len(items), len(items), "", nil)
	}

	for _, item := range items {
		status := statusFromMaps(&item, item.Region, metas, values, errs, loader.includeValues)
		statuses = append(statuses, status)
	}

	return statuses
}

func (loader statusLoader) emit(statuses Statuses) {
	if loader.batch == nil || len(statuses) == 0 {
		return
	}

	loader.batch(append(Statuses(nil), statuses...))
}

// loadAllStatusesForRegions discovers every SSM parameter in the selected regions.
// It is used when no paths file was provided, so the TUI can be opened as a full SSM parameter browser/manager.
func (loader statusLoader) loadAllForRegions(regions []string) Statuses {
	if len(regions) == 0 {
		if region := loader.client.DefaultRegion(); region != "" {
			regions = []string{region}
		} else {
			regions = []string{""}
		}
	}

	statuses := make(Statuses, 0, len(regions)*64)
	for _, region := range regions {
		statuses = append(statuses, loader.withClient(loader.client.ForRegion(region)).loadAllRegion(region)...)
	}

	sort.Slice(statuses, func(i, j int) bool {
		if statuses[i].Item.Region != statuses[j].Item.Region {
			return statuses[i].Item.Region < statuses[j].Item.Region
		}

		return statuses[i].Item.Path < statuses[j].Item.Path
	})

	return statuses
}

func (loader statusLoader) loadAllRegion(region string) Statuses {
	if loader.progress != nil {
		loader.progress(0, 0, region, nil)
	}

	metas, err := loader.client.ListParameterMetadata(loader.context())
	if err != nil {
		return Statuses{{Item: inventory.Item{Path: "(scan error)", Region: region}, Type: ssm.DefaultParameterType.String(), Error: err.Error()}}
	}

	items := make(inventory.Items, 0, len(metas))
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

	if loader.includeValues {
		for start := 0; start < len(items); start += 10 {
			end := start + 10
			if end > len(items) {
				end = len(items)
			}

			chunkItems := items[start:end]
			if loader.progress != nil {
				loader.progress(start, len(items), region, chunkItems)
			}

			paths := make([]string, 0, len(chunkItems))
			for _, item := range chunkItems {
				paths = append(paths, item.Path)
			}

			chunkValues, chunkErrs := loader.client.GetMany(loader.context(), paths)
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

	if loader.progress != nil {
		loader.progress(len(items), len(items), region, nil)
	}

	statuses := make(Statuses, 0, len(items))
	for _, item := range items {
		status := statusFromMaps(&item, item.Region, metaByKey, values, errs, loader.includeValues)
		if status.isMissing() {
			meta := metaByKey[itemKey(item.Region, item.Path)]
			status = statusFromMetadata(&item, &meta)
		}

		statuses = append(statuses, status)
	}

	loader.emit(statuses)

	return statuses
}

// loadStatusesAllRegions discovers enabled AWS regions, then scans wildcard items across all of them.
// If region discovery fails, every item receives an error status so the UI can show the failure inline.
func (loader statusLoader) loadAllRegions(items inventory.Items) Statuses {
	regions, err := loader.client.ListRegions(loader.context())
	if err != nil {
		statuses := make(Statuses, 0, len(items))
		for _, item := range items {
			status := Status{Item: item, Type: ssm.DefaultParameterType.String(), Error: err.Error()}
			statuses = append(statuses, status)
		}

		return statuses
	}

	return loader.loadRegions(items, regions)
}

// loadStatusesRegions scans wildcard items across a fixed region list.
// It creates one status row per region where a parameter exists or errors, then appends a wildcard missing row
// for paths that were not found in any scanned region.
func (loader statusLoader) loadRegions(items inventory.Items, regions []string) Statuses {
	logger := logging.FromContext(loader.context())
	statuses := Statuses{}
	found := map[string]bool{}

	var previousCompleted time.Time
	for index, region := range regions {
		if !previousCompleted.IsZero() {
			logger.LogAttrs(loader.context(), slog.LevelDebug, "region scan gap", slog.String("region", region), slog.Int("index", index), slog.Int64("duration_ms", elapsedStatusMillis(previousCompleted)))
		}

		started := time.Now()

		logger.LogAttrs(loader.context(), slog.LevelDebug, "region scan started", slog.String("region", region), slog.Int("index", index), slog.Int("item_count", len(items)), slog.Bool("include_values", loader.includeValues))
		regionStatuses, regionFound := loader.withClient(loader.client.ForRegion(region)).loadOneRegion(items, region)
		completed := time.Now()
		logger.LogAttrs(loader.context(), slog.LevelDebug, "region scan completed", slog.String("region", region), slog.Int("index", index), slog.Int("status_count", len(regionStatuses)), slog.Int("found_count", len(regionFound)), slog.Int64("duration_ms", int64(completed.Sub(started)/time.Millisecond)))
		previousCompleted = completed

		statuses = append(statuses, regionStatuses...)

		for path := range regionFound {
			found[path] = true
		}
	}

	if loader.progress != nil {
		loader.progress(len(items), len(items), "", nil)
	}

	for _, item := range items {
		if !found[item.Path] {
			item.Region = "*"
			statuses = append(statuses, Status{Item: item, Type: ssm.DefaultParameterType.String()})
		}
	}

	return statuses
}

func (loader statusLoader) loadOneRegion(items inventory.Items, region string) (statuses Statuses, found map[string]bool) {
	logger := logging.FromContext(loader.context())
	found = map[string]bool{}

	if loader.progress != nil {
		loader.progress(0, len(items), region, nil)
	}

	metadataStarted := time.Now()

	logger.LogAttrs(loader.context(), slog.LevelDebug, "region metadata scan started", slog.String("region", region))
	metas, err := loader.client.ListParameterMetadata(loader.context())

	metadataDuration := elapsedStatusMillis(metadataStarted)
	if err != nil {
		logger.LogAttrs(loader.context(), slog.LevelDebug, "region metadata scan failed", slog.String("region", region), slog.Int64("duration_ms", metadataDuration), slog.Any("error", err))
		return Statuses{{Item: inventory.Item{Path: "(scan error)", Region: region}, Type: ssm.DefaultParameterType.String(), Error: err.Error()}}, found
	}

	logger.LogAttrs(loader.context(), slog.LevelDebug, "region metadata scan completed", slog.String("region", region), slog.Int64("duration_ms", metadataDuration), slog.Int("metadata_count", len(metas)))

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

	matchedItems := make(inventory.Items, 0, len(items))
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

	logger.LogAttrs(loader.context(), slog.LevelDebug, "region metadata match completed", slog.String("region", region), slog.Int64("duration_ms", elapsedStatusMillis(matchStarted)), slog.Int("matched_count", len(matchedItems)), slog.Int("item_count", len(items)))

	if !loader.includeValues {
		statuses = make(Statuses, 0, len(matchedItems))
		for _, item := range matchedItems {
			meta := metaByKey[itemKey(region, item.Path)]
			statuses = append(statuses, statusFromMetadata(&item, &meta))
		}

		loader.emit(statuses)

		if loader.progress != nil {
			loader.progress(len(items), len(items), region, nil)
		}

		return statuses, found
	}

	for start := 0; start < len(matchedItems); start += 10 {
		end := start + 10
		if end > len(matchedItems) {
			end = len(matchedItems)
		}

		chunkItems := matchedItems[start:end]
		if loader.progress != nil {
			loader.progress(start, len(matchedItems), region, chunkItems)
		}

		paths := make([]string, 0, len(chunkItems))
		for _, item := range chunkItems {
			paths = append(paths, item.Path)
		}

		valueLookupStarted := time.Now()

		logger.LogAttrs(loader.context(), slog.LevelDebug, "region value lookup started", slog.String("region", region), slog.Int("start", start), slog.Int("count", len(chunkItems)))
		values, errs := loader.client.GetMany(loader.context(), paths)
		logger.LogAttrs(loader.context(), slog.LevelDebug, "region value lookup completed", slog.String("region", region), slog.Int("start", start), slog.Int("value_count", len(values)), slog.Int("error_count", len(errs)), slog.Int64("duration_ms", elapsedStatusMillis(valueLookupStarted)))

		chunkStatuses := make(Statuses, 0, len(chunkItems))
		for _, item := range chunkItems {
			meta := metaByKey[itemKey(region, item.Path)]
			if param, ok := values[item.Path]; ok {
				status := statusFromValue(&item, &param, &meta, loader.includeValues)
				statuses = append(statuses, status)
				chunkStatuses = append(chunkStatuses, status)

				continue
			}

			if err := errs[item.Path]; err != nil && !errors.Is(err, ssm.ErrNotFound) {
				status := Status{Item: item, Type: ssm.DefaultParameterType.String(), Error: err.Error()}
				statuses = append(statuses, status)
				chunkStatuses = append(chunkStatuses, status)

				continue
			}

			status := statusFromMetadata(&item, &meta)
			statuses = append(statuses, status)
			chunkStatuses = append(chunkStatuses, status)
		}

		loader.emit(chunkStatuses)
	}

	if loader.progress != nil {
		loader.progress(len(matchedItems), len(matchedItems), region, nil)
	}

	return statuses, found
}
