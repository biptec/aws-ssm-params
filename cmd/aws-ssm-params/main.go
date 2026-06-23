// Package main wires CLI flags and commands to the application layer.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"
)

const (
	appName      = "aws-ssm-params"
	envVarPrefix = "AWS_SSM_PARAMS_"
)

func newCLIApp(rawArgs []string) *cli.Command {
	return &cli.Command{
		Name:                  appName,
		Usage:                 "Manage AWS SSM parameters",
		UsageText:             appName + " [global options] <command> [command options]",
		EnableShellCompletion: true,
		Before: func(ctx context.Context, _ *cli.Command) (context.Context, error) {
			return ctx, rejectCommaSeparatedFlagArgs(
				rawArgs,
				flagRegion,
				flagFilter,
			)
		},
		Flags: globalFlags(),
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() > 0 {
				return fmt.Errorf("unknown command: %s", cmd.Args().First())
			}
			return cli.ShowRootCommandHelp(cmd)
		},
		Commands: []*cli.Command{
			tuiCLICommand(),
			importCLICommand(),
			exportCLICommand(),
		},
	}
}

func main() {
	ctx := context.Background()
	cliApp := newCLIApp(os.Args[1:])
	if err := cliApp.Run(ctx, os.Args); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
}
