package ui

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/biptec/aws-ssm-params/internal/filter"
	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/gosuri/uilive"
)

// LoadStatusesWithProgress loads statuses and prints progress to the terminal.
// It is used by non-interactive commands that write to a file and therefore need visible progress feedback.
func LoadStatusesWithProgress(ctx context.Context, client ssm.Client, items []inventory.Item, includeValues bool) []Status {
	return LoadStatusesWithProgressForRegions(ctx, client, items, includeValues, nil)
}

// LoadStatusesWithProgressForRegions is the progress-printing variant with an explicit region list.
// It wraps the shared batch loader with a uilive writer so repeated progress updates repaint cleanly.
func LoadStatusesWithProgressForRegions(ctx context.Context, client ssm.Client, items []inventory.Item, includeValues bool, regions []string) []Status {
	writer := uilive.New()
	writer.Start()
	defer writer.Stop()

	return LoadStatusesBatchForRegions(ctx, client, items, includeValues, regions, writeLoadProgress(writer))
}

// LoadFilteredStatusesWithProgressForRegions discovers parameters with filter groups and prints progress.
func LoadFilteredStatusesWithProgressForRegions(ctx context.Context, client ssm.Client, groups []filter.Group, includeValues bool, regions []string) []Status {
	writer := uilive.New()
	writer.Start()
	defer writer.Stop()

	return LoadFilteredStatusesBatchForRegions(ctx, client, groups, includeValues, regions, writeLoadProgress(writer))
}

func writeLoadProgress(writer io.Writer) LoadProgress {
	return func(done, total int, region string, chunk []inventory.Item) {
		if region != "" {
			_, _ = fmt.Fprintf(writer, "Loading parameters %d/%d from %s region...\n", done, total, region)
		} else {
			_, _ = fmt.Fprintf(writer, "Loading parameters %d/%d...\n", done, total)
		}
		for _, item := range chunk {
			_, _ = fmt.Fprintf(writer, "%s\n", item.Path)
		}
	}
}

// PrintStatusTable renders a compact non-interactive status table to stdout.
func PrintStatusTable(statuses []Status, noColor bool) {
	_, _ = fmt.Fprintf(os.Stdout, "%-4s %-6s %-13s %-9s %-7s %-7s %-9s %s\n", "#", "STATUS", "TYPE", "TIER", "VERSION", "LEN", "SHA256", "NAME")
	for i := range statuses {
		status := &statuses[i]
		_, _ = fmt.Fprintf(
			os.Stdout,
			"%-4d %-6s %-13s %-9s %-7s %-7s %-9s %s\n",
			i+1,
			colorStatus(status.Label(), noColor),
			valueOrDash(status.Type),
			valueOrDash(status.Tier),
			intOrDash(status.Version),
			intOrDash(int64(status.Length)),
			valueOrDash(status.SHA256Prefix),
			status.Item.Path,
		)
	}
}

// colorStatus applies ANSI color to short status labels unless color output is disabled.
func colorStatus(status string, noColor bool) string {
	if noColor {
		return status
	}
	switch status {
	case "OK":
		return "\033[32m" + status + "\033[0m"
	case "MISS", "EMPTY":
		return "\033[33m" + status + "\033[0m"
	case "ERR":
		return "\033[31m" + status + "\033[0m"
	default:
		return status
	}
}
