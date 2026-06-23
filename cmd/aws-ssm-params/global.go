package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/biptec/aws-ssm-params/internal/app"
	"github.com/biptec/aws-ssm-params/internal/logging"
)

const (
	flagRegion     = "region"
	flagAllRegions = "all-regions"
	flagProfile    = "profile"
	flagNoColor    = "no-color"
	flagKeymap     = "keymap"
	flagLogLevel   = "log-level"
	flagFilter     = "filter"

	envRegion           = envVarPrefix + "REGION"
	envAllRegions       = envVarPrefix + "ALL_REGIONS"
	envProfile          = envVarPrefix + "PROFILE"
	envNoColor          = envVarPrefix + "NO_COLOR"
	envKeymap           = envVarPrefix + "KEYMAP"
	envLogLevel         = envVarPrefix + "LOG_LEVEL"
	envFilter           = envVarPrefix + "FILTER"
	envAWSRegion        = "AWS_REGION"
	envAWSDefaultRegion = "AWS_DEFAULT_REGION"
	envAWSProfile       = "AWS_PROFILE"
)

type globalOptions struct {
	*app.Options

	NoColor bool
	Keymap  string
}

func globalFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringSliceFlag{Name: flagRegion, Sources: cli.EnvVars(envRegion, envAWSRegion), Usage: "AWS region; repeat the flag for multiple regions; env accepts comma-separated values"},
		&cli.BoolFlag{Name: flagAllRegions, Sources: cli.EnvVars(envAllRegions), Usage: "search parameters across all enabled AWS regions"},
		&cli.StringFlag{Name: flagProfile, Sources: cli.EnvVars(envProfile, envAWSProfile), Usage: "AWS profile"},
		&cli.BoolFlag{Name: flagNoColor, Sources: cli.EnvVars(envNoColor), Usage: "disable colored output"},
		&cli.StringFlag{Name: flagKeymap, Value: "emacs", Sources: cli.EnvVars(envKeymap), Usage: "keyboard navigation style: emacs or vi"},
		&cli.StringFlag{Name: flagLogLevel, Value: "off", Sources: cli.EnvVars(envLogLevel), Usage: "log level: trace, debug, info, warn, error, or off"},
		&cli.StringSliceFlag{Name: flagFilter, Sources: cli.EnvVars(envFilter), Usage: "filter group; conditions inside one value are separated by semicolons; env accepts comma-separated values"},
	}
}

func globalOptionsFromCLI(ctx context.Context, cmd *cli.Command) (globalOptions, error) {
	allRegions := boolFlagValueAny(cmd, flagAllRegions, envAllRegions)

	regions := dedupeStrings(stringSliceFlagValue(cmd, flagRegion, envRegion, envAWSRegion))
	if allRegions && len(regions) > 0 {
		return globalOptions{}, fmt.Errorf(
			"--%s and --%s cannot be used together",
			flagRegion,
			flagAllRegions,
		)
	}

	profile := stringFlagValueAny(cmd, flagProfile, "", envProfile, envAWSProfile)

	region := ""
	if len(regions) > 0 {
		region = regions[0]
	} else if !allRegions {
		region = os.Getenv(envAWSRegion)
		if region == "" {
			region = os.Getenv(envAWSDefaultRegion)
		}

		if region != "" {
			regions = []string{region}
		}
	}

	keymap := strings.ToLower(strings.TrimSpace(stringFlagValueAny(cmd, flagKeymap, "emacs", envKeymap)))
	if keymap == "" {
		keymap = "emacs"
	}

	if keymap != "emacs" && keymap != "vi" {
		return globalOptions{}, fmt.Errorf("unsupported --%s %q; expected emacs or vi", flagKeymap, keymap)
	}

	filterGroups, err := parseFilterGroups(stringSliceFlagValue(cmd, flagFilter, envFilter))
	if err != nil {
		return globalOptions{}, err
	}

	return globalOptions{
		Options: &app.Options{
			Logger:       logging.FromContext(ctx),
			FilterGroups: filterGroups,
			Region:       region,
			Regions:      regions,
			Profile:      profile,
			AllRegions:   allRegions,
		},
		NoColor: boolFlagValueAny(cmd, flagNoColor, envNoColor),
		Keymap:  keymap,
	}, nil
}

func requiredStdin(commandName string) (io.Reader, error) {
	info, err := os.Stdin.Stat()
	if err == nil && info.Mode()&os.ModeCharDevice != 0 {
		return nil, fmt.Errorf("%s requires data from stdin", commandName)
	}

	return os.Stdin, nil
}
