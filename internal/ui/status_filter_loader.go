package ui

import (
	"context"
	"sort"

	"github.com/biptec/aws-ssm-params/internal/filter"
	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/ssm"
)

type exactNameFilterGroup struct {
	Name  string
	Group filter.Group
}

type mergedPrefilterGroup struct {
	Filter ssm.ParameterFilter
	Groups filter.Groups
}

const awsPrefilterValueBatchSize = 50

type filteredStatusLoader struct {
	statusLoader
	groups filter.Groups
}

func newFilteredStatusLoader(ctx context.Context, client ssm.Client, groups filter.Groups, includeValues bool, progress LoadProgress) filteredStatusLoader {
	return filteredStatusLoader{
		statusLoader: newStatusLoader(ctx, client, includeValues, progress, nil),
		groups:       groups,
	}
}

func (loader filteredStatusLoader) withClient(client ssm.Client) filteredStatusLoader {
	loader.statusLoader = loader.statusLoader.withClient(client)
	return loader
}

// LoadFilteredStatusesBatchForRegions discovers parameter metadata with DescribeParameters prefilters,
// enriches matching rows with GetParameters values, then applies exact local filter matching.
func LoadFilteredStatusesBatchForRegions(ctx context.Context, client ssm.Client, groups filter.Groups, includeValues bool, regions []string, progress LoadProgress) Statuses {
	return newFilteredStatusLoader(ctx, client, groups, includeValues, progress).load(regions)
}

func (loader filteredStatusLoader) load(regions []string) Statuses {
	if len(loader.groups) == 0 {
		return loader.statusLoader.load(nil, regions)
	}
	if len(regions) == 0 {
		if region := loader.client.DefaultRegion(); region != "" {
			regions = []string{region}
		} else {
			regions = []string{""}
		}
	}

	exactGroups, remainingGroups := splitExactNameFilterGroups(loader.groups)
	mergedGroups, scanGroups := splitMergeablePrefilterGroups(remainingGroups)
	statuses := make(Statuses, 0, len(regions)*64)
	seen := map[string]bool{}
	appendStatuses := func(groupStatuses Statuses) {
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
		regionalLoader := loader.withClient(loader.client.ForRegion(region))
		if len(exactGroups) > 0 {
			appendStatuses(regionalLoader.loadExactNames(region, exactGroups))
		}
		for _, mergedGroup := range mergedGroups {
			appendStatuses(regionalLoader.loadMergedPrefilter(region, mergedGroup))
		}
		for _, group := range scanGroups {
			appendStatuses(regionalLoader.loadGroup(region, group))
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

func splitExactNameFilterGroups(groups filter.Groups) ([]exactNameFilterGroup, filter.Groups) {
	exactGroups := make([]exactNameFilterGroup, 0, len(groups))
	scanGroups := make(filter.Groups, 0, len(groups))
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

func splitMergeablePrefilterGroups(groups filter.Groups) ([]mergedPrefilterGroup, filter.Groups) {
	mergedByKey := map[string]int{}
	mergedGroups := []mergedPrefilterGroup{}
	scanGroups := make(filter.Groups, 0, len(groups))
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
			mergedGroups = append(mergedGroups, mergedPrefilterGroup{Filter: awsFilter, Groups: filter.Groups{group}})
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

func (loader filteredStatusLoader) loadExactNames(region string, groups []exactNameFilterGroup) Statuses {
	if len(groups) == 0 {
		return nil
	}
	groupsByName := make(map[string]filter.Groups, len(groups))
	items := make(inventory.Items, 0, len(groups))
	seen := map[string]bool{}
	fetchValues := loader.includeValues
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

	valueLoader := loader.statusLoader
	valueLoader.includeValues = fetchValues
	loaded := valueLoader.loadByItemRegion(items)
	statuses := make(Statuses, 0, len(loaded))
	for idx := range loaded {
		status := loaded[idx]
		if !status.Exists {
			continue
		}
		matchStatus := status
		if !loader.includeValues {
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

func (loader filteredStatusLoader) loadMergedPrefilter(region string, mergedGroup mergedPrefilterGroup) Statuses {
	if len(mergedGroup.Filter.Values) <= awsPrefilterValueBatchSize {
		return loader.loadPrefiltered(region, mergedGroup.Groups, []ssm.ParameterFilter{mergedGroup.Filter})
	}
	statuses := Statuses{}
	for start := 0; start < len(mergedGroup.Filter.Values); start += awsPrefilterValueBatchSize {
		end := start + awsPrefilterValueBatchSize
		if end > len(mergedGroup.Filter.Values) {
			end = len(mergedGroup.Filter.Values)
		}
		filterChunk := mergedGroup.Filter
		filterChunk.Values = append([]string(nil), mergedGroup.Filter.Values[start:end]...)
		statuses = append(statuses, loader.loadPrefiltered(region, mergedGroup.Groups, []ssm.ParameterFilter{filterChunk})...)
	}
	return statuses
}

func (loader filteredStatusLoader) loadGroup(region string, group filter.Group) Statuses {
	return loader.loadPrefiltered(region, filter.Groups{group}, ssmFiltersFromGroup(group))
}

func (loader filteredStatusLoader) loadPrefiltered(region string, groups filter.Groups, awsFilters []ssm.ParameterFilter) Statuses {
	if loader.progress != nil {
		loader.progress(0, 0, region, nil)
	}
	metas, err := loader.client.ListParameterMetadataWithFilters(loader.ctx, awsFilters)
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
	fetchValues := loader.includeValues || groups.HasField(filter.FieldValue)
	if fetchValues {
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
			chunkValues, chunkErrs := loader.client.GetMany(loader.ctx, paths)
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
		status := statusFromMaps(item, item.Region, metaByKey, values, errs, fetchValues)
		if !loader.includeValues {
			status.Value = ""
			status.Length = 0
			status.SHA256Prefix = ""
		}
		matchStatus := statusFromMaps(item, item.Region, metaByKey, values, errs, fetchValues)
		if groups.Match(matchStatus.FilterRecord()) {
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
