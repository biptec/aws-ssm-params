package ssm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Client is the small SSM capability surface used by commands and the TUI.
// The interface keeps AWS access mockable in tests and lets status-loading code operate without knowing about aws CLI details.
type Client interface {
	CheckAccess() error
	ListRegions() ([]string, error)
	ForRegion(region string) Client
	DefaultRegion() string
	Get(path string) (Parameter, error)
	GetMany(paths []string) (map[string]Parameter, map[string]error)
	DescribeMany(paths []string) map[string]Metadata
	ListParameterMetadata() ([]Metadata, error)
	PutParameter(path, value string, parameterType ParameterType) error
	PutParameterWithOptions(path, value string, parameterType ParameterType, opts PutParameterOptions) error
	DeleteMany(paths []string) error
}

// AWSCLI implements Client by shelling out to the local aws command.
// This avoids a direct AWS SDK dependency and reuses the user's existing AWS CLI profiles, SSO sessions, and config.
type AWSCLI struct {
	Profile        string
	Region         string
	WithDecryption bool
}

// Parameter is the normalized subset of AWS SSM get-parameters output needed by the app.
type Parameter struct {
	Name     string
	Region   string
	Type     string
	Value    string
	Version  int64
	Modified string
}

// Metadata is the normalized subset of AWS SSM describe-parameters output shown in the UI/export status.
type Metadata struct {
	Name        string
	Region      string
	Type        string
	Tier        string
	DataType    string
	Policies    string
	Description string
	User        string
	Modified    string
}

// PutParameterOptions contains optional AWS SSM put-parameter fields.
type PutParameterOptions struct {
	Description string
	Tier        ParameterTier
	DataType    ParameterDataType
	Policies    string
	Overwrite   bool
}

// ParameterDataType is an AWS SSM data type accepted by put-parameter.
type ParameterDataType string

const (
	// ParameterDataTypeText stores an ordinary text value without additional service-side validation.
	ParameterDataTypeText ParameterDataType = "text"
	// ParameterDataTypeEC2Image validates that the value is an EC2 AMI id.
	ParameterDataTypeEC2Image ParameterDataType = "aws:ec2:image"
	// ParameterDataTypeSSMIntegration is used by AWS SSM service integrations.
	ParameterDataTypeSSMIntegration ParameterDataType = "aws:ssm:integration"
)

// DefaultParameterDataType is used when AWS does not report a data type.
const DefaultParameterDataType = ParameterDataTypeText

// ParseParameterDataType normalizes user-facing data type names into AWS SSM data types.
func ParseParameterDataType(value string) (ParameterDataType, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "text":
		return ParameterDataTypeText, nil
	case "aws:ec2:image", "ec2:image", "ami", "image":
		return ParameterDataTypeEC2Image, nil
	case "aws:ssm:integration", "ssm:integration", "integration":
		return ParameterDataTypeSSMIntegration, nil
	default:
		return "", fmt.Errorf("unsupported parameter data type %q; use text, aws:ec2:image, or aws:ssm:integration", value)
	}
}

// String returns the AWS API spelling of the parameter data type.
func (t ParameterDataType) String() string { return string(t) }

// IsValid reports whether the data type is supported by AWS SSM Parameter Store.
func (t ParameterDataType) IsValid() bool {
	switch t {
	case ParameterDataTypeText, ParameterDataTypeEC2Image, ParameterDataTypeSSMIntegration:
		return true
	default:
		return false
	}
}

// ParameterTier is an AWS SSM parameter tier accepted by put-parameter.
type ParameterTier string

const (
	// ParameterTierStandard stores parameters in the AWS Standard tier.
	ParameterTierStandard ParameterTier = "Standard"
	// ParameterTierAdvanced stores parameters in the AWS Advanced tier.
	ParameterTierAdvanced ParameterTier = "Advanced"
	// ParameterTierIntelligentTiering lets AWS choose Standard or Advanced as needed.
	ParameterTierIntelligentTiering ParameterTier = "Intelligent-Tiering"
)

// DefaultParameterTier is used for new parameters and non-interactive writes.
const DefaultParameterTier = ParameterTierIntelligentTiering

// ParseParameterTier normalizes user-facing tier names into AWS SSM tier names.
func ParseParameterTier(value string) (ParameterTier, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "intelligent-tiering", "intelligent_tiering", "intelligenttiering":
		return ParameterTierIntelligentTiering, nil
	case "standard":
		return ParameterTierStandard, nil
	case "advanced":
		return ParameterTierAdvanced, nil
	default:
		return "", fmt.Errorf("unsupported parameter tier %q; use standard, advanced, or intelligent-tiering", value)
	}
}

// String returns the AWS API spelling of the parameter tier.
func (t ParameterTier) String() string { return string(t) }

// IsValid reports whether the tier is one of the AWS SSM supported parameter tiers.
func (t ParameterTier) IsValid() bool {
	switch t {
	case ParameterTierStandard, ParameterTierAdvanced, ParameterTierIntelligentTiering:
		return true
	default:
		return false
	}
}

// ParameterType is an AWS SSM parameter type accepted by put-parameter.
type ParameterType string

const (
	// ParameterTypeString stores plaintext scalar values.
	ParameterTypeString ParameterType = "String"
	// ParameterTypeStringList stores comma-separated plaintext lists.
	ParameterTypeStringList ParameterType = "StringList"
	// ParameterTypeSecureString stores encrypted values using AWS KMS.
	ParameterTypeSecureString ParameterType = "SecureString"
)

// DefaultParameterType is used when creating a new parameter and no type was supplied by the user or import file.
const DefaultParameterType = ParameterTypeSecureString

// ParseParameterType normalizes user-facing type names into AWS SSM type names.
// It accepts AWS spelling and CLI-friendly aliases such as secure-string and string-list.
func ParseParameterType(value string) (ParameterType, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "securestring", "secure-string", "secure_string":
		return ParameterTypeSecureString, nil
	case "string":
		return ParameterTypeString, nil
	case "stringlist", "string-list", "string_list":
		return ParameterTypeStringList, nil
	default:
		return "", fmt.Errorf("unsupported parameter type %q; use string, string-list, or secure-string", value)
	}
}

// String returns the AWS API spelling of the parameter type.
func (t ParameterType) String() string { return string(t) }

// IsValid reports whether the type is one of the AWS SSM supported parameter types.
func (t ParameterType) IsValid() bool {
	switch t {
	case ParameterTypeString, ParameterTypeStringList, ParameterTypeSecureString:
		return true
	default:
		return false
	}
}

var ErrNotFound = errors.New("parameter not found")

// NewAWSCLI constructs an AWS CLI backed client for one profile/region pair.
func NewAWSCLI(profile, region string) *AWSCLI {
	return &AWSCLI{Profile: profile, Region: region, WithDecryption: true}
}

// ResolveConfiguredRegion asks the AWS CLI which default region is configured for a profile.
// Errors are swallowed because callers use this only as a fallback before reporting a clearer region error.
func ResolveConfiguredRegion(profile string) string {
	args := []string{"configure", "get", "region"}
	if profile != "" {
		args = append(args, "--profile", profile)
	}
	out, err := runAWS(args...)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// CheckAccess validates credentials/profile by calling sts get-caller-identity.
// It fails early before the UI starts so users do not wait through partial scans with broken credentials.
func (c *AWSCLI) CheckAccess() error {
	args := c.args("sts", "get-caller-identity")
	args = append(args, "--output", "json")
	if _, err := runAWS(args...); err != nil {
		return fmt.Errorf("cannot access AWS with current credentials/profile: %w", err)
	}
	return nil
}

// ListRegions returns AWS regions that are available for scanning.
// Regions with OptInStatus=not-opted-in are excluded because SSM calls there would fail for the account.
func (c *AWSCLI) ListRegions() ([]string, error) {
	args := c.args("ec2", "describe-regions")
	args = append(args, "--all-regions", "--output", "json")
	out, err := runAWS(args...)
	if err != nil {
		return nil, err
	}
	var response struct {
		Regions []struct {
			RegionName  string `json:"RegionName"`
			OptInStatus string `json:"OptInStatus"`
		} `json:"Regions"`
	}
	if err := json.Unmarshal(out, &response); err != nil {
		return nil, err
	}
	var regions []string
	for _, region := range response.Regions {
		if region.RegionName == "" || region.OptInStatus == "not-opted-in" {
			continue
		}
		regions = append(regions, region.RegionName)
	}
	return regions, nil
}

// ForRegion returns a client targeting another region while preserving the selected AWS profile.
// When the requested region is empty or already current, it reuses the receiver to avoid unnecessary allocations.
func (c *AWSCLI) ForRegion(region string) Client {
	if region == "" || region == c.Region {
		return c
	}
	return &AWSCLI{Profile: c.Profile, Region: region, WithDecryption: c.WithDecryption}
}

// DefaultRegion returns the region associated with this client.
func (c *AWSCLI) DefaultRegion() string {
	return c.Region
}

// Get loads exactly one parameter by delegating to GetMany and normalizing missing values to ErrNotFound.
func (c *AWSCLI) Get(path string) (Parameter, error) {
	values, errs := c.GetMany([]string{path})
	if value, ok := values[path]; ok {
		return value, nil
	}
	if err, ok := errs[path]; ok {
		return Parameter{}, err
	}
	return Parameter{}, ErrNotFound
}

// GetMany loads parameters in AWS SSM batches of up to ten names.
// It initializes every requested path as ErrNotFound, clears successful entries, and preserves per-path errors so
// callers can distinguish missing parameters from AWS/API failures.
func (c *AWSCLI) GetMany(paths []string) (map[string]Parameter, map[string]error) {
	values := map[string]Parameter{}
	errs := map[string]error{}
	for _, path := range paths {
		if path != "" {
			errs[path] = ErrNotFound
		}
	}

	for _, chunk := range chunkStrings(paths, 10) {
		if len(chunk) == 0 {
			continue
		}
		args := c.args("ssm", "get-parameters")
		args = append(args, "--names")
		args = append(args, chunk...)
		if c.WithDecryption {
			args = append(args, "--with-decryption")
		}
		args = append(args, "--output", "json")
		out, err := runAWS(args...)
		if err != nil {
			for _, path := range chunk {
				errs[path] = err
			}
			continue
		}

		var response struct {
			Parameters []struct {
				Name             string `json:"Name"`
				Type             string `json:"Type"`
				Value            string `json:"Value"`
				Version          int64  `json:"Version"`
				LastModifiedDate any    `json:"LastModifiedDate"`
			} `json:"Parameters"`
			InvalidParameters []string `json:"InvalidParameters"`
		}
		if err := json.Unmarshal(out, &response); err != nil {
			for _, path := range chunk {
				errs[path] = err
			}
			continue
		}

		for _, param := range response.Parameters {
			values[param.Name] = Parameter{
				Name:     param.Name,
				Region:   c.Region,
				Type:     param.Type,
				Value:    param.Value,
				Version:  param.Version,
				Modified: formatModifiedDate(param.LastModifiedDate),
			}
			delete(errs, param.Name)
		}
		for _, path := range response.InvalidParameters {
			errs[path] = ErrNotFound
		}
	}

	return values, errs
}

// ListParameterMetadata returns metadata for every parameter visible in the client's region.
// Values are intentionally not included; callers can batch GetMany for the returned names when needed.
func (c *AWSCLI) ListParameterMetadata() ([]Metadata, error) {
	var result []Metadata
	nextToken := ""
	for {
		args := c.args("ssm", "describe-parameters")
		args = append(args, "--max-results", "50", "--output", "json")
		if nextToken != "" {
			args = append(args, "--next-token", nextToken)
		}
		out, err := runAWS(args...)
		if err != nil {
			return nil, err
		}
		var response struct {
			Parameters []struct {
				Name             string `json:"Name"`
				Type             string `json:"Type"`
				Tier             string `json:"Tier"`
				DataType         string `json:"DataType"`
				Policies         any    `json:"Policies"`
				Description      string `json:"Description"`
				LastModifiedUser string `json:"LastModifiedUser"`
				LastModifiedDate any    `json:"LastModifiedDate"`
			} `json:"Parameters"`
			NextToken string `json:"NextToken"`
		}
		if err := json.Unmarshal(out, &response); err != nil {
			return nil, err
		}
		for _, param := range response.Parameters {
			if param.Name == "" {
				continue
			}
			result = append(result, Metadata{
				Name:        param.Name,
				Region:      c.Region,
				Type:        param.Type,
				Tier:        param.Tier,
				DataType:    param.DataType,
				Policies:    formatPolicies(param.Policies),
				Description: param.Description,
				User:        param.LastModifiedUser,
				Modified:    formatModifiedDate(param.LastModifiedDate),
			})
		}
		if response.NextToken == "" {
			break
		}
		nextToken = response.NextToken
	}
	return result, nil
}

// DescribeMany loads non-secret metadata for parameter paths in batches.
// Describe failures are ignored per batch because metadata is supplementary; GetMany still provides the authoritative value status.
func (c *AWSCLI) DescribeMany(paths []string) map[string]Metadata {
	result := map[string]Metadata{}
	for _, chunk := range chunkStrings(paths, 10) {
		if len(chunk) == 0 {
			continue
		}
		args := c.args("ssm", "describe-parameters")
		args = append(args, "--parameter-filters", fmt.Sprintf("Key=Name,Option=Equals,Values=%s", strings.Join(chunk, ",")), "--output", "json")
		out, err := runAWS(args...)
		if err != nil {
			continue
		}

		var response struct {
			Parameters []struct {
				Name             string `json:"Name"`
				Type             string `json:"Type"`
				Tier             string `json:"Tier"`
				DataType         string `json:"DataType"`
				Policies         any    `json:"Policies"`
				Description      string `json:"Description"`
				LastModifiedUser string `json:"LastModifiedUser"`
				LastModifiedDate any    `json:"LastModifiedDate"`
			} `json:"Parameters"`
		}
		if err := json.Unmarshal(out, &response); err != nil {
			continue
		}
		for _, param := range response.Parameters {
			result[param.Name] = Metadata{
				Name:        param.Name,
				Region:      c.Region,
				Type:        param.Type,
				Tier:        param.Tier,
				DataType:    param.DataType,
				Policies:    formatPolicies(param.Policies),
				Description: param.Description,
				User:        param.LastModifiedUser,
				Modified:    formatModifiedDate(param.LastModifiedDate),
			}
		}
	}
	return result
}

// PutParameter creates or overwrites one SSM parameter using the requested AWS parameter type.
// SecureString values are encrypted by AWS SSM/KMS, while String and StringList are stored as plaintext parameters.
func (c *AWSCLI) PutParameter(path, value string, parameterType ParameterType) error {
	return c.PutParameterWithOptions(path, value, parameterType, PutParameterOptions{Overwrite: true})
}

func (c *AWSCLI) PutParameterWithOptions(path, value string, parameterType ParameterType, opts PutParameterOptions) error {
	if !parameterType.IsValid() {
		return fmt.Errorf("unsupported parameter type %q; use string, string-list, or secure-string", parameterType)
	}
	tier := opts.Tier
	if !tier.IsValid() {
		tier = DefaultParameterTier
	}
	dataType := opts.DataType
	if !dataType.IsValid() {
		dataType = DefaultParameterDataType
	}
	args := c.args("ssm", "put-parameter")
	args = append(args,
		"--name", path,
		"--type", parameterType.String(),
		"--tier", tier.String(),
		"--data-type", dataType.String(),
		"--value", value,
	)
	if opts.Overwrite {
		args = append(args, "--overwrite")
	}
	if strings.TrimSpace(opts.Description) != "" {
		args = append(args, "--description", opts.Description)
	}
	if strings.TrimSpace(opts.Policies) != "" {
		args = append(args, "--policies", opts.Policies)
	}
	args = append(args, "--output", "json")
	_, err := runAWS(args...)
	return err
}

// DeleteMany deletes paths in AWS SSM batches and stops at the first AWS CLI error.
func (c *AWSCLI) DeleteMany(paths []string) error {
	for _, chunk := range chunkStrings(paths, 10) {
		if len(chunk) == 0 {
			continue
		}
		args := c.args("ssm", "delete-parameters")
		args = append(args, "--names")
		args = append(args, chunk...)
		args = append(args, "--output", "json")
		if _, err := runAWS(args...); err != nil {
			return err
		}
	}
	return nil
}

// args builds the common aws CLI argument prefix for a service operation, profile, and region.
func (c *AWSCLI) args(service, operation string) []string {
	args := []string{service, operation}
	if c.Profile != "" {
		args = append(args, "--profile", c.Profile)
	}
	if c.Region != "" {
		args = append(args, "--region", c.Region)
	}
	return args
}

// runAWS executes the aws CLI and returns stdout or a cleaned stderr error.
// AWS ParameterNotFound errors are normalized to ErrNotFound so higher layers can handle missing secrets consistently.
func runAWS(args ...string) ([]byte, error) {
	cmd := exec.Command("aws", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		if strings.Contains(message, "ParameterNotFound") {
			return nil, ErrNotFound
		}
		return nil, errors.New(message)
	}
	return out, nil
}

// formatModifiedDate normalizes AWS JSON date shapes into a readable RFC1123 string.
// AWS CLI output can contain numeric Unix timestamps or RFC3339 strings depending on the command/version.
func formatModifiedDate(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case float64:
		if v <= 0 {
			return ""
		}
		return time.Unix(int64(v), 0).Format(time.RFC1123)
	case string:
		if v == "" {
			return ""
		}
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return time.Unix(int64(f), 0).Format(time.RFC1123)
		}
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t.Format(time.RFC1123)
		}
		return v
	default:
		return fmt.Sprint(v)
	}
}

func formatPolicies(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		trimmed := strings.TrimSpace(string(encoded))
		if trimmed == "null" || trimmed == "[]" || trimmed == "{}" {
			return ""
		}
		return trimmed
	}
}

// chunkStrings splits a slice into non-empty chunks.
// AWS SSM get/delete APIs accept limited batch sizes, so callers use this to stay within those limits.
func chunkStrings(values []string, size int) [][]string {
	if size <= 0 {
		size = 10
	}
	var chunks [][]string
	for start := 0; start < len(values); start += size {
		end := start + size
		if end > len(values) {
			end = len(values)
		}
		chunks = append(chunks, values[start:end])
	}
	return chunks
}
