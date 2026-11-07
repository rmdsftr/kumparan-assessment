package testdb

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const probeKey = "TESTDB_PROBE_FROM_FILE"

func unsetForTest(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	require.NoError(t, os.Unsetenv(key))
}

func writeProject(t *testing.T, envFile string) (root, nested string) {
	t.Helper()
	root = t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module probe\n"), 0o644))
	if envFile != "" {
		require.NoError(t, os.WriteFile(filepath.Join(root, ".env"), []byte(envFile), 0o644))
	}
	nested = filepath.Join(root, "internal", "pkg")
	require.NoError(t, os.MkdirAll(nested, 0o755))
	return root, nested
}

func TestLoadEnvFromFindsRootEnvFromNestedDirectory(t *testing.T) {
	unsetForTest(t, probeKey)
	_, nested := writeProject(t, probeKey+"=from-file\r\nOTHER=1\r\n")

	require.NoError(t, loadEnvFrom(nested))
	assert.Equal(t, "from-file", os.Getenv(probeKey), "CRLF line endings must not leak into the value")
}

func TestLoadEnvFromKeepsAlreadySetVariable(t *testing.T) {
	t.Setenv(probeKey, "from-shell")
	_, nested := writeProject(t, probeKey+"=from-file\n")

	require.NoError(t, loadEnvFrom(nested))
	assert.Equal(t, "from-shell", os.Getenv(probeKey))
}

func TestLoadEnvFromWithoutEnvFileIsNotAnError(t *testing.T) {
	unsetForTest(t, probeKey)
	_, nested := writeProject(t, "")

	require.NoError(t, loadEnvFrom(nested))
	assert.Empty(t, os.Getenv(probeKey))
}

func TestFindRoot(t *testing.T) {
	root, nested := writeProject(t, "")

	got, ok := findRoot(nested)
	require.True(t, ok)
	assert.Equal(t, root, got)

	got, ok = findRoot(root)
	require.True(t, ok)
	assert.Equal(t, root, got)
}
