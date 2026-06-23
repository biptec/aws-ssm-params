package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/urfave/cli/v3"

	"github.com/biptec/aws-ssm-params/internal/fileio"
	"github.com/biptec/aws-ssm-params/internal/filter"
)

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func dedupeStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func splitCommaEnv(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return compactStrings(strings.Split(value, ","))
}

func stringSliceFlagValue(cmd *cli.Command, name string, envNames ...string) []string {
	values := compactStrings(cmd.StringSlice(name))
	if len(values) > 0 {
		return values
	}
	for _, envName := range envNames {
		if envValues := splitCommaEnv(os.Getenv(envName)); len(envValues) > 0 {
			return envValues
		}
	}
	return nil
}

func stringFlagValueAny(cmd *cli.Command, name, defaultValue string, envNames ...string) string {
	if cmd.IsSet(name) {
		return cmd.String(name)
	}
	for _, envName := range envNames {
		if value := os.Getenv(envName); strings.TrimSpace(value) != "" {
			return value
		}
	}
	return defaultValue
}

func boolFlagValueAny(cmd *cli.Command, name string, defaultValue bool, envNames ...string) bool {
	if cmd.IsSet(name) {
		return cmd.Bool(name)
	}
	for _, envName := range envNames {
		switch strings.ToLower(strings.TrimSpace(os.Getenv(envName))) {
		case "1", "t", "true", "yes", "y", "on":
			return true
		case "0", "f", "false", "no", "n", "off":
			return false
		}
	}
	return defaultValue
}

func parseFilterGroups(values []string, filtersFile string) (filter.Groups, error) {
	groups, err := filter.ParseGroups(compactStrings(values))
	if err != nil {
		return nil, errors.Wrap(err, "parse filters")
	}
	if filtersFile == "" {
		return groups, nil
	}
	file, err := fileio.Open(filtersFile)
	if err != nil {
		return nil, errors.Wrapf(err, "open filters file %s", filtersFile)
	}
	defer func() { _ = file.Close() }()
	fileGroups, err := filter.ParseFile(file)
	if err != nil {
		return nil, errors.Wrapf(err, "parse filters file %s", filtersFile)
	}
	return append(groups, fileGroups...), nil
}

func rejectCommaSeparatedFlagArgs(args []string, flagNames ...string) error {
	flags := map[string]bool{}
	for _, flagName := range flagNames {
		flags["--"+flagName] = true
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if name, value, ok := strings.Cut(arg, "="); ok && flags[name] && strings.Contains(value, ",") {
			return fmt.Errorf("%s accepts one value per flag; repeat the flag instead of using commas", name)
		}
		if flags[arg] && i+1 < len(args) && strings.Contains(args[i+1], ",") {
			return fmt.Errorf("%s accepts one value per flag; repeat the flag instead of using commas", arg)
		}
	}
	return nil
}
