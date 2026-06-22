package ssm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	crerr "github.com/cockroachdb/errors"
)

type signedEC2RegionAPI struct{}

type describeRegionsResponse struct {
	Regions []describeRegionsItem `xml:"regionInfo>item"`
}

type describeRegionsItem struct {
	Name        string `xml:"regionName"`
	OptInStatus string `xml:"optInStatus"`
}

func (signedEC2RegionAPI) DescribeRegions(ctx context.Context, client *AWSClient) ([]awsRegion, error) {
	cfg, err := client.sdkConfig(ctx)
	if err != nil {
		return nil, err
	}
	region := strings.TrimSpace(client.Region)
	if region == "" {
		region = strings.TrimSpace(cfg.Region)
	}
	if region == "" {
		region = "us-east-1"
	}
	credentials, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		return nil, crerr.Wrap(err, "retrieve AWS credentials")
	}
	body := "Action=DescribeRegions&Version=2016-11-15&AllRegions=true"
	endpoint := fmt.Sprintf("https://ec2.%s.amazonaws.com/", region)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		return nil, crerr.Wrap(err, "create EC2 DescribeRegions request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	sum := sha256.Sum256([]byte(body))
	payloadHash := hex.EncodeToString(sum[:])
	if err := v4.NewSigner().SignHTTP(ctx, credentials, req, payloadHash, "ec2", region, time.Now()); err != nil {
		return nil, crerr.Wrap(err, "sign EC2 DescribeRegions request")
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, crerr.Wrap(err, "call EC2 DescribeRegions")
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, crerr.Wrap(err, "read EC2 DescribeRegions response")
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("EC2 DescribeRegions failed with HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	var out describeRegionsResponse
	if err := xml.Unmarshal(responseBody, &out); err != nil {
		return nil, crerr.Wrap(err, "parse EC2 DescribeRegions response")
	}
	regions := make([]awsRegion, 0, len(out.Regions))
	for _, region := range out.Regions {
		regions = append(regions, awsRegion(region))
	}
	return regions, nil
}
