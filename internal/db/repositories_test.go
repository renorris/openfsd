package db

import (
	"database/sql"
	"database/sql/driver"
	"strings"
	"sync"
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

// stubDriver is a minimal database/sql driver used only to hit the
// "unsupported database" branch of NewUserRepository / NewConfigRepository.
type stubDriver struct{}

func (stubDriver) Open(name string) (driver.Conn, error) { return stubConn{}, nil }

type stubConn struct{}

func (stubConn) Prepare(query string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (stubConn) Close() error                              { return nil }
func (stubConn) Begin() (driver.Tx, error)                 { return nil, driver.ErrSkip }

var registerStubDriver sync.Once

func TestNewRepositoriesUnsupportedDriver(t *testing.T) {
	const name = "openfsd_stub_unsupported"
	registerStubDriver.Do(func() {
		sql.Register(name, stubDriver{})
	})

	sqlDB, err := sql.Open(name, "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	_, err = NewUserRepository(sqlDB)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported database")

	_, err = NewConfigRepository(sqlDB)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported database")

	_, err = NewRepositories(sqlDB)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported database")
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
