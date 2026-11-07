package testdb

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/stretchr/testify/require"

	"kumparan-assessment/internal/postgres"
	"kumparan-assessment/migrations"
)

var (
	loadOnce sync.Once
	loadErr  error
)

func URL(t testing.TB) string {
	t.Helper()

	loadOnce.Do(func() {
		wd, err := os.Getwd()
		if err != nil {
			loadErr = err
			return
		}
		loadErr = loadEnvFrom(wd)
	})
	require.NoError(t, loadErr)

	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping integration test")
	}
	return url
}

func Pool(t testing.TB) *pgxpool.Pool {
	t.Helper()
	url := URL(t)

	m, err := migrations.New(url)
	require.NoError(t, err)
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		require.NoError(t, err)
	}
	srcErr, dbErr := m.Close()
	require.NoError(t, srcErr)
	require.NoError(t, dbErr)

	pool, err := postgres.NewPool(context.Background(), url, 4)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

func loadEnvFrom(dir string) error {
	root, ok := findRoot(dir)
	if !ok {
		return nil
	}
	if err := godotenv.Load(filepath.Join(root, ".env")); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func findRoot(dir string) (string, bool) {
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}
