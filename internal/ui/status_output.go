package ui

import (
	"context"
	"fmt"
	"io"

	"github.com/biptec/aws-ssm-params/internal/filter"
	"github.com/biptec/aws-ssm-params/internal/inventory"
	"github.com/biptec/aws-ssm-params/internal/ssm"
	"github.com/gosuri/uilive"
)

// LoadStatusesWithProgress loads statuses and prints progress to the terminal.
// It is used by non-interactive commands that write to a file and therefore need visible progress feedback.
func LoadStatusesWithProgress(ctx context.Context, client ssm.Client, items inventory.Items, includeValues bool) Statuses {
	return LoadStatusesWithProgressForRegions(ctx, client, items, includeValues, nil)
}

// LoadStatusesWithProgressForRegions is the progress-printing variant with an explicit region list.
// It wraps the shared batch loader with a uilive writer so repeated progress updates repaint cleanly.
func LoadStatusesWithProgressForRegions(ctx context.Context, client ssm.Client, items inventory.Items, includeValues bool, regions []string) Statuses {
	writer := uilive.New()

	writer.Start()
	defer writer.Stop()

	return LoadStatusesBatchForRegions(ctx, client, items, includeValues, regions, writeLoadProgress(writer))
}

// LoadFilteredStatusesWithProgressForRegions discovers parameters with filter groups and prints progress.
func LoadFilteredStatusesWithProgressForRegions(ctx context.Context, client ssm.Client, groups filter.Groups, includeValues bool, regions []string) Statuses {
	writer := uilive.New()

	writer.Start()
	defer writer.Stop()

	return LoadFilteredStatusesBatchForRegions(ctx, client, groups, includeValues, regions, writeLoadProgress(writer))
}

func writeLoadProgress(writer io.Writer) LoadProgress {
	return func(done, total int, region string, chunk inventory.Items) {
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
