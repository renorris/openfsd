package db

import (
	"database/sql"
	"errors"
	_ "modernc.org/sqlite"
	"testing"
)

// setupConfigTestDB initializes an in-memory SQLite database, applies migrations, and returns the database connection and repository.
func setupConfigTestDB(t *testing.T) (*sql.DB, *SQLiteConfigRepository) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	err = Migrate(db)
	if err != nil {
		t.Fatalf("failed to migrate database: %v", err)
	}

	repo := &SQLiteConfigRepository{db: db}
	return db, repo
}

// TestInitDefault verifies that InitDefault correctly inserts the JWT secret key and does not overwrite it on subsequent calls.
func TestInitDefault(t *testing.T) {
	db, repo := setupConfigTestDB(t)
	defer db.Close()

	// Initially, the key should not exist
	_, err := repo.Get(ConfigJwtSecretKey)
	if !errors.Is(err, ErrConfigKeyNotFound) {
		t.Errorf("expected ErrConfigKeyNotFound, got %v", err)
	}

	// Call InitDefault
	err = repo.InitDefault()
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// Verify the key exists and is a 32-character hex string
	value, err := repo.Get(ConfigJwtSecretKey)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if len(value) != 32 {
		t.Errorf("expected 32-character hex string, got %s", value)
	}

	// Call InitDefault again
	err = repo.InitDefault()
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// Verify the key has not changed
	newValue, err := repo.Get(ConfigJwtSecretKey)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if newValue != value {
		t.Errorf("expected the same value, but it changed from %s to %s", value, newValue)
	}
}

// TestSet verifies that Set correctly inserts new key-value pairs and updates existing ones.
func TestSet(t *testing.T) {
	db, repo := setupConfigTestDB(t)
	defer db.Close()

	// Set a new key-value pair
	key := "test_key"
	value := "test_value"
	err := repo.Set(key, value)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// Retrieve and verify
	retrievedValue, err := repo.Get(key)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if retrievedValue != value {
		t.Errorf("expected %s, got %s", value, retrievedValue)
	}

	// Update the key with a new value
	newValue := "new_test_value"
	err = repo.Set(key, newValue)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// Retrieve and verify again
	retrievedValue, err = repo.Get(key)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if retrievedValue != newValue {
		t.Errorf("expected %s, got %s", newValue, retrievedValue)
	}
}

// TestGet verifies that Get retrieves values for existing keys and returns an error for non-existing keys.
func TestGet(t *testing.T) {
	db, repo := setupConfigTestDB(t)
	defer db.Close()

	// Try to get a non-existing key
	_, err := repo.Get("non_existing_key")
	if !errors.Is(err, ErrConfigKeyNotFound) {
		t.Errorf("expected ErrConfigKeyNotFound, got %v", err)
	}

	// Set a key-value pair
	key := "another_key"
	value := "another_value"
	err = repo.Set(key, value)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// Retrieve and verify
	retrievedValue, err := repo.Get(key)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if retrievedValue != value {
		t.Errorf("expected %s, got %s", value, retrievedValue)
	}
}

// TestMultipleSets verifies that setting multiple keys works correctly and updating one does not affect others.
func TestMultipleSets(t *testing.T) {
	db, repo := setupConfigTestDB(t)
	defer db.Close()

	// Set multiple keys
	err := repo.Set("key1", "value1")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	err = repo.Set("key2", "value2")
	if err != nil {
		t.Errorf("expected no{kcal error, got %v", err)
	}

	// Retrieve and verify
	val1, err := repo.Get("key1")
	if err != nil || val1 != "value1" {
		t.Errorf("expected value1, got %s, err %v", val1, err)
	}
	val2, err := repo.Get("key2")
	if err != nil || val2 != "value2" {
		t.Errorf("expected value2, got %s, err %v", val2, err)
	}

	// Update one key
	err = repo.Set("key1", "new_value1")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// Check both keys
	val1, err = repo.Get("key1")
	if err != nil || val1 != "new_value1" {
		t.Errorf("expected new_value1, got %s, err %v", val1, err)
	}
	val2, err = repo.Get("key2")
	if err != nil || val2 != "value2" {
		t.Errorf("expected value2, got %s, err %v", val2, err)
	}
}
