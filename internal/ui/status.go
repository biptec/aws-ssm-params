package ui

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"

	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/gosuri/uilive"
)

// Status is the UI/export view of one inventory item after querying AWS SSM.
// It combines desired inventory metadata, existence/value state, non-secret AWS metadata, and any lookup error.
type Status struct {
	Item         inventory.Item
	Exists       bool
	Empty        bool
	Type         string
	Tier         string
	Version      int64
	Length       int
	SHA256Prefix string
	Modified     string
	Description  string
	User         string
	Value        string
	Error        string
}

// LoadProgress reports status-loading progress to either the interactive TUI or the non-interactive live writer.
// done/total are item counters within the current region scan, region names the region being scanned, and chunk
// contains the paths currently being requested from SSM.
type LoadProgress func(done, total int, region string, chunk []inventory.Item)

// LoadStatusesWithProgress loads statuses and prints progress to the terminal.
// It is used by non-interactive commands that write to a file and therefore need visible progress feedback.
func LoadStatusesWithProgress(client ssm.Client, items []inventory.Item, includeValues bool) []Status {
	return LoadStatusesWithProgressForRegions(client, items, includeValues, nil)
}

// LoadStatusesWithProgressForRegions is the progress-printing variant with an explicit region list.
// It wraps the shared batch loader with a uilive writer so repeated progress updates repaint cleanly.
func LoadStatusesWithProgressForRegions(client ssm.Client, items []inventory.Item, includeValues bool, regions []string) []Status {
	writer := uilive.New()
	writer.Start()
	defer writer.Stop()

	return LoadStatusesBatchForRegions(client, items, includeValues, regions, func(done, total int, region string, chunk []inventory.Item) {
		if region != "" {
			fmt.Fprintf(writer, "Loading parameters %d/%d from %s region...\n", done, total, region)
		} else {
			fmt.Fprintf(writer, "Loading parameters %d/%d...\n", done, total)
		}
		for _, item := range chunk {
			fmt.Fprintf(writer, "%s\n", item.Path)
		}
	})
}

// LoadStatuses loads statuses without progress output using item-level region information.
func LoadStatuses(client ssm.Client, items []inventory.Item, includeValues bool) []Status {
	return LoadStatusesForRegions(client, items, includeValues, nil)
}

// LoadStatusesForRegions loads statuses without progress output but with an explicit region scan list.
func LoadStatusesForRegions(client ssm.Client, items []inventory.Item, includeValues bool, regions []string) []Status {
	return LoadStatusesBatchForRegions(client, items, includeValues, regions, nil)
}

// LoadStatusesBatch is the shared status loader entry point for item-level region lookup.
func LoadStatusesBatch(client ssm.Client, items []inventory.Item, includeValues bool, progress LoadProgress) []Status {
	return LoadStatusesBatchForRegions(client, items, includeValues, nil, progress)
}

// LoadStatusesBatchForRegions chooses the correct status-loading strategy.
// Concrete-region items are loaded directly by their item region; wildcard items are expanded across either an explicit
// region list or every enabled AWS region discovered from the client.
func LoadStatusesBatchForRegions(client ssm.Client, items []inventory.Item, includeValues bool, regions []string, progress LoadProgress) []Status {
	if containsAllRegionItems(items) {
		if len(regions) > 0 {
			return loadStatusesRegions(client, items, includeValues, regions, progress)
		}
		return loadStatusesAllRegions(client, items, includeValues, progress)
	}
	return loadStatusesByItemRegion(client, items, includeValues, progress)
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
func loadStatusesByItemRegion(client ssm.Client, items []inventory.Item, includeValues bool, progress LoadProgress) []Status {
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
			for path, meta := range regionalClient.DescribeMany(paths) {
				metas[itemKey(region, path)] = meta
			}
			chunkValues, chunkErrs := regionalClient.GetMany(paths)
			for path, value := range chunkValues {
				values[itemKey(region, path)] = value
			}
			for path, err := range chunkErrs {
				errs[itemKey(region, path)] = err
			}
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

// loadStatusesAllRegions discovers enabled AWS regions, then scans wildcard items across all of them.
// If region discovery fails, every item receives an error status so the UI can show the failure inline.
func loadStatusesAllRegions(client ssm.Client, items []inventory.Item, includeValues bool, progress LoadProgress) []Status {
	regions, err := client.ListRegions()
	if err != nil {
		statuses := make([]Status, 0, len(items))
		for _, item := range items {
			status := Status{Item: item, Type: ssm.DefaultParameterType.String(), Error: err.Error()}
			statuses = append(statuses, status)
		}
		return statuses
	}
	return loadStatusesRegions(client, items, includeValues, regions, progress)
}

// loadStatusesRegions scans wildcard items across a fixed region list.
// It creates one status row per region where a parameter exists or errors, then appends a wildcard missing row
// for paths that were not found in any scanned region.
func loadStatusesRegions(client ssm.Client, items []inventory.Item, includeValues bool, regions []string, progress LoadProgress) []Status {
	statuses := []Status{}
	found := map[string]bool{}
	for _, region := range regions {
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
			byPath := map[string]inventory.Item{}
			for _, item := range chunkItems {
				paths = append(paths, item.Path)
				byPath[item.Path] = item
			}

			regionalClient := client.ForRegion(region)
			metas := map[string]ssm.Metadata{}
			for path, meta := range regionalClient.DescribeMany(paths) {
				metas[itemKey(region, path)] = meta
			}
			values, errs := regionalClient.GetMany(paths)
			for path, param := range values {
				item := byPath[path]
				item.Region = region
				status := statusFromValue(item, param, metas[itemKey(region, path)], includeValues)
				statuses = append(statuses, status)
				found[path] = true
			}
			for path, err := range errs {
				if errors.Is(err, ssm.ErrNotFound) {
					continue
				}
				item := byPath[path]
				item.Region = region
				statuses = append(statuses, Status{Item: item, Type: ssm.DefaultParameterType.String(), Error: err.Error()})
				found[path] = true
			}
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
		status.Description = meta.Description
		status.User = meta.User
		status.Modified = meta.Modified
	}
	if param, ok := values[key]; ok {
		status = statusFromValue(item, param, metas[key], includeValues)
	} else if err, ok := errs[key]; ok && !errors.Is(err, ssm.ErrNotFound) {
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
	status := Status{Item: item, Exists: true, Type: parameterType, Tier: meta.Tier, Description: meta.Description, User: meta.User, Modified: meta.Modified, Version: param.Version, Value: param.Value, Length: len(param.Value), Empty: param.Value == "", SHA256Prefix: hashPrefix(param.Value)}
	if status.Modified == "" {
		status.Modified = param.Modified
	}
	if !includeValues {
		status.Value = ""
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

// itemKey builds a collision-safe map key for values that are scoped by both AWS region and SSM path.
func itemKey(region, path string) string {
	return region + "\x00" + path
}

// PrintStatusTable renders a compact non-interactive status table to stdout.
func PrintStatusTable(statuses []Status, noColor bool) {
	fmt.Printf("%-4s %-6s %-13s %-9s %-7s %-7s %-9s %s\n", "#", "STATUS", "TYPE", "TIER", "VERSION", "LEN", "SHA256", "PATH")
	for i, status := range statuses {
		fmt.Printf("%-4d %-6s %-13s %-9s %-7s %-7s %-9s %s\n",
			i+1,
			colorStatus(statusLabel(status), noColor),
			valueOrDash(status.Type),
			valueOrDash(status.Tier),
			intOrDash(status.Version),
			intOrDash(int64(status.Length)),
			valueOrDash(status.SHA256Prefix),
			status.Item.Path,
		)
	}
}

// statusLabel converts a Status into the short label used by the CLI table.
func statusLabel(status Status) string {
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

// statusDescription expands a Status into a human-readable explanation used by detailed UI views.
func statusDescription(status Status) string {
	switch statusLabel(status) {
	case "OK":
		return "OK"
	case "EMPTY":
		return "EMPTY (parameter exists but value is empty)"
	case "MISS":
		return "MISS (parameter does not exist)"
	case "ERR":
		return "ERR (AWS/API error)"
	default:
		return statusLabel(status)
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
