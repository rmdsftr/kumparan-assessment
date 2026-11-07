package idgen_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"kumparan-assessment/internal/idgen"
)

func TestUUIDv7(t *testing.T) {
	a, err := idgen.UUIDv7()
	require.NoError(t, err)
	b, err := idgen.UUIDv7()
	require.NoError(t, err)

	assert.Regexp(t, `^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`, a)
	assert.NotEqual(t, a, b)
}
