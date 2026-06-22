package ui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	crerr "github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/filter"
	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/logging"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/gosuri/uilive"
)

// Status is the UI/export view of one inventory item after querying AWS SSM.
// It combines desired inventory metadata, existence/value state, non-secret AWS metadata, and any lookup error.
type Status struct {
	Item         inventory.Item
	Pending      bool
	Exists       bool
	Empty        bool
	Type         string
	Tier         string
	DataType     string
	Policies     string
	Version      int64
	Length       int
	SHA256Prefix string
	Modified     string
	Description  string
	User         string
	Value        string
	Error        string
}

// FilterRecord converts a status into the normalized shape used by CLI filters.
func (status Status) FilterRecord() filter.Record {
	return filter.Record{
		Name:        status.Item.Path,
		Region:      status.Item.Region,
		Type:        status.Type,
		Tier:        status.Tier,
		DataType:    status.DataType,
		Description: status.Description,
		Policies:    status.Policies,
		Value:       status.Value,
	}
}

// FilterStatusesByGroups returns statuses that match at least one configured filter group.
// Empty group configuration means no filtering.
func FilterStatusesByGroups(statuses []Status, groups []filter.Group) []Status {
	if len(groups) == 0 {
		return statuses
	}
	out := make([]Status, 0, len(statuses))
	for i := range statuses {
		if filter.MatchAny(groups, statuses[i].FilterRecord()) {
			out = append(out, statuses[i])
		}
	}
	return out
}

// Label converts a Status into the short label used by compact status tables.
func (status Status) Label() string {
	if status.Pending {
		return "LOAD"
	}
	if status.Error != "" {
		return "ERR"
	}
	if !status.Exists {
		return "MISS"
	}
	if status.Empty {
		return "EMPTY"
	}
	return "OK"
}

// DisplayLabel converts a Status into the longer labels used in the interactive table.
func (status Status) DisplayLabel() string {
	switch status.Label() {
	case "LOAD":
		return "LOADING"
	case "MISS":
		return "MISSING"
	case "ERR":
		return "ERROR"
	default:
		return status.Label()
	}
}

// RegionLabel returns the region label shown in UI tables and detail blocks.
func (status Status) RegionLabel(fallback string) string {
	if status.Item.Region == "*" {
		return "-"
	}
	if status.Item.Region != "" {
		return status.Item.Region
	}
	return valueOrDash(fallback)
}

// HasSensitiveValue reports whether the status value should be treated as secret by default.
func (status Status) HasSensitiveValue() bool {
	parameterType, err := ssm.ParseParameterType(status.Type)
	if err != nil {
		return true
	}
	return parameterType == ssm.ParameterTypeSecureString
}

func (status Status) isMissing() bool {
	return !status.Pending && !status.Exists && status.Error == ""
}

// LoadProgress reports status-loading progress to either the interactive TUI or the non-interactive live writer.
// done/total are item counters within the current region scan, region names the region being scanned, and chunk
// contains the paths currently being requested from SSM.
type LoadProgress func(done, total int, region string, chunk []inventory.Item)

// StatusBatch receives partial status rows while a long-running interactive load is still in progress.
type StatusBatch func([]Status)

type exactNameFilterGroup struct {
	Name  string
	Group filter.Group
}

type mergedPrefilterGroup struct {
	Filter ssm.ParameterFilter
	Groups []filter.Group
}

const awsPrefilterValueBatchSize = 50

// LoadStatusesWithProgress loads statuses and prints progress to the terminal.
// It is used by non-interactive commands that write to a file and therefore need visible progress feedback.
func LoadStatusesWithProgress(ctx context.Context, client ssm.Client, items []inventory.Item, includeValues bool) []Status {
	return LoadStatusesWithProgressForRegions(ctx, client, items, includeValues, nil)
}

// LoadStatusesWithProgressForRegions is the progress-printing variant with an explicit region list.
// It wraps the shared batch loader with a uilive writer so repeated progress updates repaint cleanly.
func LoadStatusesWithProgressForRegions(ctx context.Context, client ssm.Client, items []inventory.Item, includeValues bool, regions []string) []Status {
	writer := uilive.New()
	writer.Start()
	defer writer.Stop()

	return LoadStatusesBatchForRegions(ctx, client, items, includeValues, regions, func(done, total int, region string, chunk []inventory.Item) {
		if region != "" {
			_, _ = fmt.Fprintf(writer, "Loading parameters %d/%d from %s region...\n", done, total, region)
		} else {
			_, _ = fmt.Fprintf(writer, "Loading parameters %d/%d...\n", done, total)
		}
		for _, item := range chunk {
			_, _ = fmt.Fprintf(writer, "%s\n", item.Path)
		}
	})
}

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

// LoadFilteredStatusesWithProgressForRegions discovers parameters with filter groups and prints progress.
func LoadFilteredStatusesWithProgressForRegions(ctx context.Context, client ssm.Client, groups []filter.Group, includeValues bool, regions []string) []Status {
	writer := uilive.New()
	writer.Start()
	defer writer.Stop()

	return LoadFilteredStatusesBatchForRegions(ctx, client, groups, includeValues, regions, func(done, total int, region string, chunk []inventory.Item) {
		if region != "" {
			_, _ = fmt.Fprintf(writer, "Loading parameters %d/%d from %s region...\n", done, total, region)
		} else {
			_, _ = fmt.Fprintf(writer, "Loading parameters %d/%d...\n", done, total)
		}
		for _, item := range chunk {
			_, _ = fmt.Fprintf(writer, "%s\n", item.Path)
		}
	})
}

// LoadFilteredStatusesBatchForRegions discovers parameter metadata with DescribeParameters prefilters,
// enriches matching rows with GetParameters values, then applies exact local filter matching.
func LoadFilteredStatusesBatchForRegions(ctx context.Context, client ssm.Client, groups []filter.Group, includeValues bool, regions []string, progress LoadProgress) []Status {
	if len(groups) == 0 {
		return LoadStatusesBatchForRegions(ctx, client, nil, includeValues, regions, progress)
	}
	if len(regions) == 0 {
		if region := client.DefaultRegion(); region != "" {
			regions = []string{region}
		} else {
			regions = []string{""}
		}
	}

	exactGroups, remainingGroups := splitExactNameFilterGroups(groups)
	mergedGroups, scanGroups := splitMergeablePrefilterGroups(remainingGroups)
	statuses := make([]Status, 0, len(regions)*64)
	seen := map[string]bool{}
	appendStatuses := func(groupStatuses []Status) {
		for i := range groupStatuses {
			key := itemKey(groupStatuses[i].Item.Region, groupStatuses[i].Item.Path)
			if seen[key] {
				continue
			}
			seen[key] = true
			statuses = append(statuses, groupStatuses[i])
		}
	}

	for _, region := range regions {
		regionalClient := client.ForRegion(region)
		if len(exactGroups) > 0 {
			appendStatuses(loadExactNameFilteredStatusesRegion(ctx, regionalClient, region, exactGroups, includeValues, progress))
		}
		for _, mergedGroup := range mergedGroups {
			appendStatuses(loadMergedPrefilterStatusesRegion(ctx, regionalClient, region, mergedGroup, includeValues, progress))
		}
		for _, group := range scanGroups {
			appendStatuses(loadFilteredStatusesRegion(ctx, regionalClient, region, group, includeValues, progress))
		}
	}
	sort.Slice(statuses, func(i, j int) bool {
		if statuses[i].Item.Region != statuses[j].Item.Region {
			return statuses[i].Item.Region < statuses[j].Item.Region
		}
		return statuses[i].Item.Path < statuses[j].Item.Path
	})
	return statuses
}

func splitExactNameFilterGroups(groups []filter.Group) ([]exactNameFilterGroup, []filter.Group) {
	exactGroups := make([]exactNameFilterGroup, 0, len(groups))
	scanGroups := make([]filter.Group, 0, len(groups))
	for _, group := range groups {
		name, ok := group.ExactName()
		if !ok {
			scanGroups = append(scanGroups, group)
			continue
		}
		exactGroups = append(exactGroups, exactNameFilterGroup{Name: name, Group: group})
	}
	return exactGroups, scanGroups
}

func splitMergeablePrefilterGroups(groups []filter.Group) ([]mergedPrefilterGroup, []filter.Group) {
	mergedByKey := map[string]int{}
	mergedGroups := []mergedPrefilterGroup{}
	scanGroups := make([]filter.Group, 0, len(groups))
	for _, group := range groups {
		awsFilter, ok := mergeablePrefilter(group)
		if !ok {
			scanGroups = append(scanGroups, group)
			continue
		}
		key := awsFilter.Key + "\x00" + awsFilter.Option
		idx, ok := mergedByKey[key]
		if !ok {
			mergedByKey[key] = len(mergedGroups)
			mergedGroups = append(mergedGroups, mergedPrefilterGroup{Filter: awsFilter, Groups: []filter.Group{group}})
			continue
		}
		mergedGroups[idx].Filter.Values = appendUniqueStrings(mergedGroups[idx].Filter.Values, awsFilter.Values...)
		mergedGroups[idx].Groups = append(mergedGroups[idx].Groups, group)
	}
	return mergedGroups, scanGroups
}

func mergeablePrefilter(group filter.Group) (ssm.ParameterFilter, bool) {
	if len(group.Conditions) != 1 {
		return ssm.ParameterFilter{}, false
	}
	awsFilters := group.AWSFilters()
	if len(awsFilters) != 1 {
		return ssm.ParameterFilter{}, false
	}
	awsFilter := awsFilters[0]
	if len(awsFilter.Values) == 0 {
		return ssm.ParameterFilter{}, false
	}
	return ssm.ParameterFilter{Key: awsFilter.Key, Option: awsFilter.Option, Values: append([]string(nil), awsFilter.Values...)}, true
}

func appendUniqueStrings(values []string, additions ...string) []string {
	seen := make(map[string]bool, len(values)+len(additions))
	for _, value := range values {
		seen[value] = true
	}
	for _, value := range additions {
		if seen[value] {
			continue
		}
		seen[value] = true
		values = append(values, value)
	}
	return values
}

func loadExactNameFilteredStatusesRegion(ctx context.Context, client ssm.Client, region string, groups []exactNameFilterGroup, includeValues bool, progress LoadProgress) []Status {
	if len(groups) == 0 {
		return nil
	}
	groupsByName := make(map[string][]filter.Group, len(groups))
	items := make([]inventory.Item, 0, len(groups))
	seen := map[string]bool{}
	fetchValues := includeValues
	for _, group := range groups {
		groupsByName[group.Name] = append(groupsByName[group.Name], group.Group)
		if group.Group.HasField(filter.FieldValue) {
			fetchValues = true
		}
		if seen[group.Name] {
			continue
		}
		seen[group.Name] = true
		items = append(items, inventory.Item{Path: group.Name, Region: region, Kind: "ssm", Source: "filter", SecretName: pathBase(group.Name)})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })

	loaded := loadStatusesByItemRegion(ctx, client, items, fetchValues, progress, nil)
	statuses := make([]Status, 0, len(loaded))
	for idx := range loaded {
		status := loaded[idx]
		if !status.Exists {
			continue
		}
		matchStatus := status
		if !includeValues {
			status.Value = ""
			status.Length = 0
			status.SHA256Prefix = ""
		}
		for _, group := range groupsByName[status.Item.Path] {
			if group.Match(matchStatus.FilterRecord()) {
				statuses = append(statuses, status)
				break
			}
		}
	}
	return statuses
}

func loadMergedPrefilterStatusesRegion(ctx context.Context, client ssm.Client, region string, mergedGroup mergedPrefilterGroup, includeValues bool, progress LoadProgress) []Status {
	if len(mergedGroup.Filter.Values) <= awsPrefilterValueBatchSize {
		return loadPrefilteredStatusesRegion(ctx, client, region, mergedGroup.Groups, []ssm.ParameterFilter{mergedGroup.Filter}, includeValues, progress)
	}
	statuses := []Status{}
	for start := 0; start < len(mergedGroup.Filter.Values); start += awsPrefilterValueBatchSize {
		end := start + awsPrefilterValueBatchSize
		if end > len(mergedGroup.Filter.Values) {
			end = len(mergedGroup.Filter.Values)
		}
		filterChunk := mergedGroup.Filter
		filterChunk.Values = append([]string(nil), mergedGroup.Filter.Values[start:end]...)
		statuses = append(statuses, loadPrefilteredStatusesRegion(ctx, client, region, mergedGroup.Groups, []ssm.ParameterFilter{filterChunk}, includeValues, progress)...)
	}
	return statuses
}

func loadFilteredStatusesRegion(ctx context.Context, client ssm.Client, region string, group filter.Group, includeValues bool, progress LoadProgress) []Status {
	return loadPrefilteredStatusesRegion(ctx, client, region, []filter.Group{group}, ssmFiltersFromGroup(group), includeValues, progress)
}

func loadPrefilteredStatusesRegion(ctx context.Context, client ssm.Client, region string, groups []filter.Group, awsFilters []ssm.ParameterFilter, includeValues bool, progress LoadProgress) []Status {
	if progress != nil {
		progress(0, 0, region, nil)
	}
	metas, err := client.ListParameterMetadataWithFilters(ctx, awsFilters)
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
	fetchValues := includeValues || filter.GroupsHaveField(groups, filter.FieldValue)
	if fetchValues {
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
		status := statusFromMaps(item, item.Region, metaByKey, values, errs, fetchValues)
		if !includeValues {
			status.Value = ""
			status.Length = 0
			status.SHA256Prefix = ""
		}
		matchStatus := statusFromMaps(item, item.Region, metaByKey, values, errs, fetchValues)
		if filter.MatchAny(groups, matchStatus.FilterRecord()) {
			statuses = append(statuses, status)
		}
	}
	return statuses
}

func ssmFiltersFromGroup(group filter.Group) []ssm.ParameterFilter {
	awsFilters := group.AWSFilters()
	filters := make([]ssm.ParameterFilter, 0, len(awsFilters))
	for _, awsFilter := range awsFilters {
		filters = append(filters, ssm.ParameterFilter{Key: awsFilter.Key, Option: awsFilter.Option, Values: append([]string(nil), awsFilter.Values...)})
	}
	return filters
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

func statusFromMetadata(item inventory.Item, meta ssm.Metadata) Status {
	if (item.Region == "" || item.Region == "*") && meta.Region != "" {
		item.Region = meta.Region
	}
	parameterType := meta.Type
	if parameterType == "" {
		parameterType = ssm.DefaultParameterType.String()
	}
	return Status{Item: item, Exists: true, Type: parameterType, Tier: meta.Tier, DataType: meta.DataType, Policies: meta.Policies, Description: meta.Description, User: meta.User, Modified: meta.Modified}
}

// statusFromMaps combines batched metadata, values, and errors into one Status for a concrete item/region pair.
// Missing values remain non-existing statuses with the default target type; non-not-found errors are surfaced in Status.Error.
func statusFromMaps(item inventory.Item, region string, metas map[string]ssm.Metadata, values map[string]ssm.Parameter, errs map[string]error, includeValues bool) Status {
	if item.Region == "" && region != "" {
		item.Region = region
	}
	status := Status{Item: item, Type: ssm.DefaultParameterType.String()}
	key := itemKey(region, item.Path)
	if meta, ok := metas[key]; ok {
		if meta.Type != "" {
			status.Type = meta.Type
		}
		status.Tier = meta.Tier
		status.DataType = meta.DataType
		status.Policies = meta.Policies
		status.Description = meta.Description
		status.User = meta.User
		status.Modified = meta.Modified
	}
	if param, ok := values[key]; ok {
		status = statusFromValue(item, param, metas[key], includeValues)
	} else if err, ok := errs[key]; ok && !crerr.Is(err, ssm.ErrNotFound) {
		status.Error = err.Error()
	}
	if !includeValues {
		status.Value = ""
	}
	return status
}

// statusFromValue builds an existing-parameter Status and computes derived fields such as length, empty flag, and hash prefix.
// Region and modified date can come from either value or metadata because different AWS commands expose different fields.
func statusFromValue(item inventory.Item, param ssm.Parameter, meta ssm.Metadata, includeValues bool) Status {
	if (item.Region == "" || item.Region == "*") && param.Region != "" {
		item.Region = param.Region
	}
	if (item.Region == "" || item.Region == "*") && meta.Region != "" {
		item.Region = meta.Region
	}
	parameterType := param.Type
	if parameterType == "" {
		parameterType = meta.Type
	}
	if parameterType == "" {
		parameterType = ssm.DefaultParameterType.String()
	}
	valueKnown := includeValues && !param.ValueHidden
	if parameterType != ssm.ParameterTypeSecureString.String() {
		valueKnown = true
	}
	value := param.Value
	length := len(value)
	sha256Prefix := hashPrefix(value)
	empty := valueKnown && value == ""
	if !valueKnown {
		value = ""
		length = 0
		sha256Prefix = ""
	}
	status := Status{Item: item, Exists: true, Type: parameterType, Tier: meta.Tier, DataType: meta.DataType, Policies: meta.Policies, Description: meta.Description, User: meta.User, Modified: meta.Modified, Version: param.Version, Value: value, Length: length, Empty: empty, SHA256Prefix: sha256Prefix}
	if status.Modified == "" {
		status.Modified = param.Modified
	}
	return status
}

// chunkRegion returns the common region for a chunk, or empty when the chunk contains mixed regions.
func chunkRegion(items []inventory.Item) string {
	if len(items) == 0 {
		return ""
	}
	region := items[0].Region
	for _, item := range items[1:] {
		if item.Region != region {
			return ""
		}
	}
	return region
}

func elapsedStatusMillis(started time.Time) int64 {
	if started.IsZero() {
		return 0
	}
	return int64(time.Since(started) / time.Millisecond)
}

// itemKey builds a collision-safe map key for values that are scoped by both AWS region and SSM name.
func itemKey(region, path string) string {
	return region + "\x00" + path
}

// PrintStatusTable renders a compact non-interactive status table to stdout.
func PrintStatusTable(statuses []Status, noColor bool) {
	_, _ = fmt.Fprintf(os.Stdout, "%-4s %-6s %-13s %-9s %-7s %-7s %-9s %s\n", "#", "STATUS", "TYPE", "TIER", "VERSION", "LEN", "SHA256", "NAME")
	for i := range statuses {
		status := &statuses[i]
		_, _ = fmt.Fprintf(
			os.Stdout,
			"%-4d %-6s %-13s %-9s %-7s %-7s %-9s %s\n",
			i+1,
			colorStatus(status.Label(), noColor),
			valueOrDash(status.Type),
			valueOrDash(status.Tier),
			intOrDash(status.Version),
			intOrDash(int64(status.Length)),
			valueOrDash(status.SHA256Prefix),
			status.Item.Path,
		)
	}
}

// colorStatus applies ANSI color to short status labels unless color output is disabled.
func colorStatus(status string, noColor bool) string {
	if noColor {
		return status
	}
	switch status {
	case "OK":
		return "\033[32m" + status + "\033[0m"
	case "MISS", "EMPTY":
		return "\033[33m" + status + "\033[0m"
	case "ERR":
		return "\033[31m" + status + "\033[0m"
	default:
		return status
	}
}

func pathBase(path string) string {
	path = strings.TrimRight(path, "/")
	idx := strings.LastIndex(path, "/")
	if idx >= 0 && idx < len(path)-1 {
		return path[idx+1:]
	}
	return path
}

// valueOrDash returns a dash placeholder for empty table fields.
func valueOrDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

// intOrDash returns a dash placeholder for zero numeric fields that are not meaningful when absent.
func intOrDash(value int64) string {
	if value == 0 {
		return "-"
	}
	return strconv.FormatInt(value, 10)
}

// hashPrefix returns the first eight hex characters of a SHA-256 hash for safe value comparison without exposing secrets.
func hashPrefix(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:8]
}
