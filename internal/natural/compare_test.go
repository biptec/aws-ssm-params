package natural

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCompare(t *testing.T) {
	assert.Negative(t, Compare("item2", "item10"))
	assert.Positive(t, Compare("item10", "item2"))
	assert.Zero(t, Compare(" Item2 ", "item2"))
	assert.Negative(t, Compare("item2", "item02"))
	assert.Negative(t, Compare("item99999999999999999999", "item100000000000000000000"))
}
