package app

import (
	"context"

	"github.com/urfave/cli/v3"
)

// CLIContext is the small adapter used by application code so the business layer is not coupled
// to urfave/cli internals.
type CLIContext struct {
	Context context.Context
	Command *cli.Command
}

// NewCLIContext adapts a urfave/cli/v3 command invocation for application code.
func NewCLIContext(ctx context.Context, cmd *cli.Command) *CLIContext {
	return &CLIContext{Context: ctx, Command: cmd}
}

// String returns a string flag value by name.
func (ctx *CLIContext) String(name string) string { return ctx.Command.String(name) }

// Bool returns a boolean flag value by name.
func (ctx *CLIContext) Bool(name string) bool { return ctx.Command.Bool(name) }

// StringSlice returns a repeated string flag value by name.
func (ctx *CLIContext) StringSlice(name string) []string { return ctx.Command.StringSlice(name) }

// IsSet reports whether a flag was set explicitly by the user.
func (ctx *CLIContext) IsSet(name string) bool { return ctx.Command.IsSet(name) }

// Args returns positional command arguments.
func (ctx *CLIContext) Args() cli.Args { return ctx.Command.Args() }
