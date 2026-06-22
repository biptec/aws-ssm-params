package ui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	crerr "github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/filter"
	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/ssm"
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

// Statuses is an ordered collection of parameter statuses.
type Statuses []Status

// Filter returns statuses matching at least one group. Empty groups preserve the original collection.
func (statuses Statuses) Filter(groups filter.Groups) Statuses {
	if len(groups) == 0 {
		return statuses
	}
	out := make(Statuses, 0, len(statuses))
	for i := range statuses {
		if groups.Match(statuses[i].FilterRecord()) {
			out = append(out, statuses[i])
		}
	}
	return out
}

// PrintTable renders a compact non-interactive status table to stdout.
func (statuses Statuses) PrintTable(noColor bool) {
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
