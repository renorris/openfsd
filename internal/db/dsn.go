package db

// DefaultSQLiteDSN is the on-disk default for DATABASE_SOURCE_NAME.
// WAL + busy_timeout allow colocated FSD and web to open the same file safely.
const DefaultSQLiteDSN = "openfsd.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"

// SharedMemorySQLiteDSN is a process-wide in-memory database.
// Bare ":memory:" is private per sql.Open; FSD and web each get an empty DB
// when both default to it. Use this DSN when operators want ephemeral storage
// with both services in one process.
const SharedMemorySQLiteDSN = "file:openfsd?mode=memory&cache=shared"
