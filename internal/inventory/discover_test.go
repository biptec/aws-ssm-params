package inventory

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDedupeMergesDistinctOverlappingMetadataValues(t *testing.T) {
	items := dedupe([]Item{
		{Path: "/app/token", App: "api-v2", Kind: "app-secret"},
		{Path: "/app/token", App: "api", Kind: "app-secret"},
		{Path: "/app/token", App: "api", Kind: "tls.key"},
	})

	assert.Equal(t, []Item{{
		Path: "/app/token",
		App:  "api-v2,api",
		Kind: "app-secret,tls.key",
	}}, items)
}

func TestMergeNormalizesCommaSeparatedValues(t *testing.T) {
	assert.Equal(t, "one,two,three", merge("one, two", "two,three"))
	assert.Equal(t, "one", merge("one", ""))
	assert.Equal(t, "two", merge("", "two"))
}
