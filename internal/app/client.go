package app

import "github.com/biptec/aws-ssm-params/internal/ssm"

// NewClient creates the concrete AWS SSM client for the selected profile and primary region.
// Keeping this in one function makes command handlers independent from the AWS SDK implementation details.
func NewClient(cfg Config) ssm.Client {
	client := ssm.NewAWSClient(cfg.Profile, cfg.Region)
	client.WithDecryption = cfg.WithDecryption
	client.Logger = cfg.Logger
	return client
}
