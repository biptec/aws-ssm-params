package ssm

import (
	"context"
	"log/slog"

	"github.com/biptec/aws-ssm-params/internal/logging"
)

func (c *AWSClient) logger(ctx context.Context) *slog.Logger {
	if c.Logger != nil {
		return c.Logger
	}
	return logging.FromContext(ctx)
}

func (c *AWSClient) logDebug(ctx context.Context, msg string, attrs ...slog.Attr) {
	c.logger(ctx).LogAttrs(ctx, slog.LevelDebug, msg, attrs...)
}

func (c *AWSClient) logInfo(ctx context.Context, msg string, attrs ...slog.Attr) {
	c.logger(ctx).LogAttrs(ctx, slog.LevelInfo, msg, attrs...)
}

func (c *AWSClient) logAPIError(ctx context.Context, operation string, err error, attrs ...slog.Attr) error {
	normalized := normalizeAWSError(err)
	level := slog.LevelError
	message := "aws api request failed"
	if IsThrottlingError(err) {
		level = slog.LevelWarn
		message = "aws api throttling"
	}
	allAttrs := make([]slog.Attr, 0, 3+len(attrs))
	allAttrs = append(allAttrs, slog.String("operation", operation), slog.String("region", c.Region), slog.Any("error", err))
	allAttrs = append(allAttrs, attrs...)
	c.logger(ctx).LogAttrs(ctx, level, message, allAttrs...)
	return normalized
}
