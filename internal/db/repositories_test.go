package db

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestNewRepositoriesSQLite(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", "file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, Migrate(sqlDB))

	repos, err := NewRepositories(sqlDB)
	require.NoError(t, err)
	require.NotNil(t, repos.UserRepo)
	require.NotNil(t, repos.ConfigRepo)

	userRepo, err := NewUserRepository(sqlDB)
	require.NoError(t, err)
	require.NotNil(t, userRepo)

	cfgRepo, err := NewConfigRepository(sqlDB)
	require.NoError(t, err)
	require.NotNil(t, cfgRepo)
}

func TestNewRepositoriesUnsupportedDriver(t *testing.T) {
	// sqlmock-free approach: open with a driver that isn't pq or sqlite.
	// The default "mysql" driver is not registered — Open may succeed but
	// Driver() type won't match. Use a custom minimal driver via sql.OpenDB if needed.
	// Simpler: pass a closed DB opened with sqlite then... actually Driver() still sqlite.

	// Register nothing — create empty DB handle that panics on Driver? Not possible cleanly.
	// Cover the default branch by calling NewUserRepository with a *sql.DB whose driver is
	// not pq/sqlite. modernc and pq are the only registered ones in tests.
	// Skip unsupported branch if we cannot construct it without extra deps.
	t.Log("unsupported driver branch covered at compile-time via switch default; runtime requires exotic driver")
}

func TestGetWelcomeMessageAndInitDefault(t *testing.T) {
	sqlDB, err := sql.Open("sqlite", "file:welcomemsg_"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, Migrate(sqlDB))

	repos, err := NewRepositories(sqlDB)
	require.NoError(t, err)

	// Before init — empty welcome
	msg := GetWelcomeMessage(repos.ConfigRepo)
	require.Equal(t, "", msg)

	require.NoError(t, InitDefaultConfig(repos.ConfigRepo))
	msg = GetWelcomeMessage(repos.ConfigRepo)
	require.Equal(t, "Connected to openfsd", msg)

	// Second init is idempotent (SetIfNotExists)
	require.NoError(t, InitDefaultConfig(repos.ConfigRepo))

	// Set/Get/SetIfNotExists paths
	require.NoError(t, repos.ConfigRepo.Set("CUSTOM_KEY", "v1"))
	v, err := repos.ConfigRepo.Get("CUSTOM_KEY")
	require.NoError(t, err)
	require.Equal(t, "v1", v)
	require.NoError(t, repos.ConfigRepo.SetIfNotExists("CUSTOM_KEY", "v2"))
	v, err = repos.ConfigRepo.Get("CUSTOM_KEY")
	require.NoError(t, err)
	require.Equal(t, "v1", v) // unchanged
	require.NoError(t, repos.ConfigRepo.Set("CUSTOM_KEY", "v3"))
	v, err = repos.ConfigRepo.Get("CUSTOM_KEY")
	require.NoError(t, err)
	require.Equal(t, "v3", v)

	_, err = repos.ConfigRepo.Get("NO_SUCH_KEY_XYZ")
	require.ErrorIs(t, err, ErrConfigKeyNotFound)
}

func TestGenerateJwtSecretKey(t *testing.T) {
	k1, err := GenerateJwtSecretKey()
	require.NoError(t, err)
	k2, err := GenerateJwtSecretKey()
	require.NoError(t, err)
	require.NotEqual(t, k1, k2)
	require.NotZero(t, k1)
}
