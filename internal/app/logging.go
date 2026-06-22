package app

import (
	crerr "github.com/cockroachdb/errors"

	"github.com/biptec/aws-ssm-params/internal/logging"
)

// RunWithLogging configures command logging, executes action, and flushes buffered terminal logs.
func RunWithLogging(ctx *CLIContext, bufferTerminal bool, action func(*CLIContext) error) error {
	logCfg := loggingConfigFromCLI(ctx)
	logger, flush, err := logging.New(logCfg, bufferTerminal)
	if err != nil {
		return crerr.Wrap(err, "configure logging")
	}
	ctx.Context = logging.WithLogger(ctx.Context, logger)
	err = action(ctx)
	if err != nil {
		commandName := ""
		if ctx.Command != nil {
			commandName = ctx.Command.Name
		}
		logger.Error("command failed", "command", commandName, "error", err)
	}
	if flushErr := flush(); flushErr != nil && err == nil {
		return flushErr
	}
	return err
}

func loggingConfigFromCLI(ctx *CLIContext) logging.Config {
	return logging.Config{Level: stringFlagValueAny(ctx, "log-level", logging.DefaultLevel, "AWS_SSM_PARAMS_LOG_LEVEL")}
}
