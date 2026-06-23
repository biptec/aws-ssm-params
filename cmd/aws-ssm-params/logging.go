package main

import (
	"context"

	"github.com/cockroachdb/errors"
	"github.com/urfave/cli/v3"

	"github.com/biptec/aws-ssm-params/internal/logging"
)

func runWithLogging(ctx context.Context, cmd *cli.Command, bufferTerminal bool, action func(context.Context) error) error {
	logCfg := logging.Config{
		Level: stringFlagValueAny(cmd, flagLogLevel, logging.DefaultLevel, envLogLevel),
	}

	logger, flush, err := logging.New(logCfg, bufferTerminal)
	if err != nil {
		return errors.Wrap(err, "configure logging")
	}

	ctx = logging.WithLogger(ctx, logger)

	err = action(ctx)
	if err != nil {
		logger.Error("command failed", "command", cmd.Name, "error", err)
	}

	if flushErr := flush(); flushErr != nil && err == nil {
		return flushErr
	}

	return err
}
