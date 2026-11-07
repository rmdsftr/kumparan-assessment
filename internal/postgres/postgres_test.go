package postgres_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"kumparan-assessment/internal/postgres"
	"kumparan-assessment/internal/testdb"
)

func TestNewPoolForcesCustomPlans(t *testing.T) {
	pool, err := postgres.NewPool(context.Background(), testdb.URL(t), 2)
	require.NoError(t, err)
	defer pool.Close()

	var mode string
	require.NoError(t, pool.QueryRow(context.Background(), "SHOW plan_cache_mode").Scan(&mode))
	assert.Equal(t, "force_custom_plan", mode)
}
