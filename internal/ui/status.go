package ui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/filter"
	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/natural"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/biptec/aws-ssm-params/internal/textio"
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

type statusSortRule struct {
	field      string
	descending bool
}

// StatusSort is the ordered set of field rules used by Statuses.Sort.
type StatusSort []statusSortRule

type statusOrder struct {
	value      func(*Status) string
	descending bool
}

// ParseStatusSort parses field:direction values into status sort rules.
func ParseStatusSort(values []string) StatusSort {
	rules := make(StatusSort, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}

		parts := strings.Split(value, ":")

		field, ok := normalizeStatusSortField(parts[0])
		if !ok {
			continue
		}

		descending := false

		if len(parts) > 1 {
			switch strings.ToLower(strings.TrimSpace(parts[1])) {
			case "desc", "descending":
				descending = true
			}
		}

		rules = rules.with(field, descending)
	}

	return rules
}

// RequiresValues reports whether sorting depends on parameter values or their
// derived length/hash fields.
func (rules StatusSort) RequiresValues() bool {
	for _, rule := range rules {
		switch rule.field {
		case textio.FieldValue, textio.FieldLen, textio.FieldSHA256:
			return true
		}
	}

	return false
}

// Sort orders statuses by the supplied rules and uses region/name as stable
// identity tie-breakers.
func (statuses Statuses) Sort(rules StatusSort) {
	orders := make([]statusOrder, 0, len(rules))
	for _, rule := range rules {
		currentRule := rule
		orders = append(orders, statusOrder{
			value:      currentRule.value,
			descending: currentRule.descending,
		})
	}

	statuses.sort(orders)
}

func (statuses Statuses) sort(orders []statusOrder) {
	if len(orders) == 0 {
		return
	}

	sort.SliceStable(statuses, func(i, j int) bool {
		left := &statuses[i]
		right := &statuses[j]

		for _, order := range orders {
			comparison := natural.Compare(order.value(left), order.value(right))
			if comparison == 0 {
				continue
			}

			if order.descending {
				return comparison > 0
			}

			return comparison < 0
		}

		if comparison := natural.Compare(left.Item.Region, right.Item.Region); comparison != 0 {
			return comparison < 0
		}

		return natural.Compare(left.Item.Path, right.Item.Path) < 0
	})
}

func (rules StatusSort) with(field string, descending bool) StatusSort {
	out := make(StatusSort, 0, len(rules)+1)
	for _, rule := range rules {
		if rule.field != field {
			out = append(out, rule)
		}
	}

	return append(out, statusSortRule{field: field, descending: descending})
}

func (rule statusSortRule) value(status *Status) string {
	switch rule.field {
	case textio.FieldName:
		return status.Item.Path
	case textio.FieldRegion:
		return status.Item.Region
	case textio.FieldType:
		return status.Type
	case textio.FieldTier:
		return status.Tier
	case textio.FieldDataType:
		return status.DataType
	case textio.FieldPolicies:
		return status.Policies
	case textio.FieldDescription:
		return status.Description
	case textio.FieldValue:
		return status.Value
	case textio.FieldDate:
		return status.Modified
	case textio.FieldVersion:
		return fmt.Sprint(status.Version)
	case textio.FieldLen:
		return fmt.Sprint(status.Length)
	case textio.FieldSHA256:
		return status.SHA256Prefix
	case textio.FieldUser:
		return status.User
	default:
		return ""
	}
}

func normalizeStatusSortField(field string) (string, bool) {
	field = strings.ToLower(strings.TrimSpace(field))

	switch field {
	case textio.FieldName, "path":
		return textio.FieldName, true
	case textio.FieldRegion,
		textio.FieldType,
		textio.FieldTier,
		textio.FieldPolicies,
		textio.FieldDescription,
		textio.FieldValue,
		textio.FieldDate,
		textio.FieldVersion,
		textio.FieldLen,
		textio.FieldSHA256,
		textio.FieldUser:
		return field, true
	case textio.FieldDataType, "datatype", "data_type":
		return textio.FieldDataType, true
	default:
		return "", false
	}
}

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

// FilterRecord converts a status into the normalized shape used by CLI filters.
func (status *Status) FilterRecord() *filter.Record {
	return &filter.Record{
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
func (status *Status) Label() string {
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
func (status *Status) DisplayLabel() string {
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
func (status *Status) RegionLabel(fallback string) string {
	if status.Item.Region == "*" {
		return "-"
	}

	if status.Item.Region != "" {
		return status.Item.Region
	}

	return valueOrDash(fallback)
}

// HasSensitiveValue reports whether the status value should be treated as secret by default.
func (status *Status) HasSensitiveValue() bool {
	parameterType, err := ssm.ParseParameterType(status.Type)
	if err != nil {
		return true
	}

	return parameterType == ssm.ParameterTypeSecureString
}

func (status *Status) isMissing() bool {
	return !status.Pending && !status.Exists && status.Error == ""
}

func statusFromMetadata(item *inventory.Item, meta *ssm.Metadata) Status {
	resolvedItem := *item
	if (resolvedItem.Region == "" || resolvedItem.Region == "*") && meta.Region != "" {
		resolvedItem.Region = meta.Region
	}

	parameterType := meta.Type
	if parameterType == "" {
		parameterType = ssm.DefaultParameterType.String()
	}

	return Status{Item: resolvedItem, Exists: true, Type: parameterType, Tier: meta.Tier, DataType: meta.DataType, Policies: meta.Policies, Description: meta.Description, User: meta.User, Modified: meta.Modified}
}

// statusFromMaps combines batched metadata, values, and errors into one Status for a concrete item/region pair.
// Missing values remain non-existing statuses with the default target type; non-not-found errors are surfaced in Status.Error.
func statusFromMaps(item *inventory.Item, region string, metas map[string]ssm.Metadata, values map[string]ssm.Parameter, errs map[string]error, includeValues bool) Status {
	resolvedItem := *item
	if resolvedItem.Region == "" && region != "" {
		resolvedItem.Region = region
	}

	status := Status{Item: resolvedItem, Type: ssm.DefaultParameterType.String()}

	key := itemKey(region, resolvedItem.Path)
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
		meta := metas[key]
		status = statusFromValue(&resolvedItem, &param, &meta, includeValues)
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
func statusFromValue(item *inventory.Item, param *ssm.Parameter, meta *ssm.Metadata, includeValues bool) Status {
	resolvedItem := *item
	if (resolvedItem.Region == "" || resolvedItem.Region == "*") && param.Region != "" {
		resolvedItem.Region = param.Region
	}

	if (resolvedItem.Region == "" || resolvedItem.Region == "*") && meta.Region != "" {
		resolvedItem.Region = meta.Region
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

	status := Status{Item: resolvedItem, Exists: true, Type: parameterType, Tier: meta.Tier, DataType: meta.DataType, Policies: meta.Policies, Description: meta.Description, User: meta.User, Modified: meta.Modified, Version: param.Version, Value: value, Length: length, Empty: empty, SHA256Prefix: sha256Prefix}
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
