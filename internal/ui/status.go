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

	State        parameterState
	PendingState parameterState
	Cloud        parameterSnapshot
	PushError    string
	DraftType    ssm.ParameterType
	DraftOptions ssm.PutParameterOptions
}

// Statuses is an ordered collection of parameter statuses.
type Statuses []Status

type parameterState string

const (
	parameterStateClean    parameterState = ""
	parameterStateModified parameterState = "MOD"
	parameterStateNew      parameterState = "NEW"
	parameterStateDeleted  parameterState = "DEL"
	parameterStateError    parameterState = "ERR"
)

type parameterSnapshot struct {
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
	case "state":
		return status.StateLabel()
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
	case "state":
		return "state", true
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

// StateLabel returns the short table label for the current local state.
func (status *Status) StateLabel() string {
	return string(status.State)
}

// HasLocalChanges reports whether the local snapshot differs from the cloud snapshot.
func (status *Status) HasLocalChanges() bool {
	return status.State != parameterStateClean
}

// IsNewLocal reports whether this status represents a not-yet-pushed parameter.
func (status *Status) IsNewLocal() bool {
	return status.pendingOperation() == parameterStateNew
}

// pendingOperation returns the operation that should be pushed for this status.
func (status *Status) pendingOperation() parameterState {
	if status.State == parameterStateError && status.PendingState != parameterStateClean {
		return status.PendingState
	}

	return status.State
}

func (status *Status) snapshot() parameterSnapshot {
	return parameterSnapshot{
		Item:         status.Item,
		Pending:      status.Pending,
		Exists:       status.Exists,
		Empty:        status.Empty,
		Type:         status.Type,
		Tier:         status.Tier,
		DataType:     status.DataType,
		Policies:     status.Policies,
		Version:      status.Version,
		Length:       status.Length,
		SHA256Prefix: status.SHA256Prefix,
		Modified:     status.Modified,
		Description:  status.Description,
		User:         status.User,
		Value:        status.Value,
		Error:        status.Error,
	}
}

func (snapshot *parameterSnapshot) status() Status {
	return Status{
		Item:         snapshot.Item,
		Pending:      snapshot.Pending,
		Exists:       snapshot.Exists,
		Empty:        snapshot.Empty,
		Type:         snapshot.Type,
		Tier:         snapshot.Tier,
		DataType:     snapshot.DataType,
		Policies:     snapshot.Policies,
		Version:      snapshot.Version,
		Length:       snapshot.Length,
		SHA256Prefix: snapshot.SHA256Prefix,
		Modified:     snapshot.Modified,
		Description:  snapshot.Description,
		User:         snapshot.User,
		Value:        snapshot.Value,
		Error:        snapshot.Error,
	}
}

func (snapshot *parameterSnapshot) isZero() bool {
	return snapshot.Item.Path == "" &&
		snapshot.Item.Region == "" &&
		!snapshot.Pending &&
		!snapshot.Exists &&
		!snapshot.Empty &&
		snapshot.Type == "" &&
		snapshot.Tier == "" &&
		snapshot.DataType == "" &&
		snapshot.Policies == "" &&
		snapshot.Version == 0 &&
		snapshot.Length == 0 &&
		snapshot.SHA256Prefix == "" &&
		snapshot.Modified == "" &&
		snapshot.Description == "" &&
		snapshot.User == "" &&
		snapshot.Value == "" &&
		snapshot.Error == ""
}

func (snapshot *parameterSnapshot) sameLocalValue(status *Status) bool {
	if status == nil {
		return false
	}

	return snapshot.Item.Path == status.Item.Path &&
		snapshot.Item.Region == status.Item.Region &&
		snapshot.Exists == status.Exists &&
		snapshot.Type == status.Type &&
		snapshot.Tier == status.Tier &&
		snapshot.DataType == status.DataType &&
		snapshot.Policies == status.Policies &&
		snapshot.Description == status.Description &&
		snapshot.Value == status.Value
}

func (status *Status) ensureCloudSnapshot() {
	if status.Cloud.isZero() {
		status.Cloud = status.snapshot()
	}
}

func (status *Status) clearLocalState() {
	status.State = parameterStateClean
	status.PendingState = parameterStateClean
	status.Cloud = parameterSnapshot{}
	status.PushError = ""
	status.DraftType = ""
	status.DraftOptions = ssm.PutParameterOptions{}
}

func (status *Status) refreshDerivedFields() {
	status.Length = len(status.Value)

	status.Empty = status.Exists && status.Value == ""
	if status.Value == "" {
		status.SHA256Prefix = ""
		return
	}

	status.SHA256Prefix = hashPrefix(status.Value)
}

func (status *Status) applyLocalModification(cloud *parameterSnapshot, parameterType ssm.ParameterType, opts ssm.PutParameterOptions) {
	status.Cloud = *cloud
	status.PendingState = parameterStateModified
	status.State = parameterStateModified
	status.PushError = ""
	status.DraftType = parameterType
	status.DraftOptions = opts
	status.refreshDerivedFields()

	if cloud.sameLocalValue(status) {
		status.clearLocalState()
	}
}

func (status *Status) applyLocalCreate(parameterType ssm.ParameterType, opts ssm.PutParameterOptions) {
	status.State = parameterStateNew
	status.PendingState = parameterStateNew
	status.Cloud = parameterSnapshot{}
	status.PushError = ""
	status.DraftType = parameterType
	status.DraftOptions = opts
	status.refreshDerivedFields()
}

func (status *Status) applyLocalDelete() {
	status.ensureCloudSnapshot()

	cloud := status.Cloud
	if !cloud.isZero() {
		*status = cloud.status()
		status.Cloud = cloud
	}

	status.State = parameterStateDeleted
	status.PendingState = parameterStateDeleted
	status.PushError = ""
}

func (status *Status) applyPushError(operation parameterState, err error) {
	if operation == parameterStateClean {
		operation = status.pendingOperation()
	}

	status.State = parameterStateError

	status.PendingState = operation
	if err != nil {
		status.PushError = err.Error()
	}
}

func (status *Status) pushType() ssm.ParameterType {
	if status.DraftType.IsValid() {
		return status.DraftType
	}

	if parameterType, err := ssm.ParseParameterType(status.Type); err == nil {
		return parameterType
	}

	return ssm.DefaultParameterType
}

func (status *Status) pushOptions() ssm.PutParameterOptions {
	opts := status.DraftOptions
	if !opts.Tier.IsValid() {
		if tier, err := ssm.ParseParameterTier(status.Tier); err == nil {
			opts.Tier = tier
		}
	}

	if !opts.Tier.IsValid() {
		opts.Tier = ssm.DefaultParameterTier
	}

	if !opts.DataType.IsValid() {
		if dataType, err := ssm.ParseParameterDataType(status.DataType); err == nil {
			opts.DataType = dataType
		}
	}

	if !opts.DataType.IsValid() {
		opts.DataType = ssm.DefaultParameterDataType
	}

	if opts.Description == "" {
		opts.Description = status.Description
	}

	if opts.Policies == "" && status.Policies != "" {
		opts.Policies = status.Policies
	}

	return opts
}

func (status *Status) cloudStatus() Status {
	if status.Cloud.isZero() {
		return *status
	}

	return (&status.Cloud).status()
}

func (status *Status) localStatus() Status {
	if status.pendingOperation() != parameterStateDeleted {
		return *status
	}

	item := status.Item
	if !status.Cloud.isZero() {
		item = status.Cloud.Item
	}

	return Status{Item: item, Type: ssm.DefaultParameterType.String()}
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
