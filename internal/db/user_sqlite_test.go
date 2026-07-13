package db

import (
	"database/sql"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
	"strings"
	"testing"
)

// testSqliteFlightplanSetupDb initializes an in-memory SQLite database, applies migrations, and returns the database connection and repository.
func setupTestDB(t *testing.T) (*sql.DB, *SQLiteUserRepository) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	err = Migrate(db)
	if err != nil {
		t.Fatalf("failed to migrate database: %v", err)
	}

	repo := &SQLiteUserRepository{db: db}
	return db, repo
}

// ptr creates a pointer to a string for use in User struct fields.
func ptr(s string) *string {
	return &s
}

// TestCreateUser verifies the CreateUser method, ensuring users are correctly inserted and passwords are hashed.
func TestCreateUser(t *testing.T) {
	db, repo := setupTestDB(t)
	defer db.Close()

	// Test with all fields provided
	user := &User{
		CID:           100, // Should be ignored
		Password:      "password123",
		FirstName:     ptr("Alice"),
		LastName:      ptr("Smith"),
		NetworkRating: 100,
	}
	err := repo.CreateUser(user)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if user.CID <= 0 {
		t.Errorf("expected cid > 0, got %d", user.CID)
	}
	if user.CID == 100 {
		t.Errorf("expected new CID, not the provided one")
	}

	// Retrieve and verify the user
	retrievedUser, err := repo.GetUserByCID(user.CID)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if retrievedUser == nil {
		t.Errorf("expected user, got nil")
	}
	if retrievedUser.CID != user.CID {
		t.Errorf("expected CID %d, got %d", user.CID, retrievedUser.CID)
	}
	if *retrievedUser.FirstName != "Alice" {
		t.Errorf("expected FirstName Alice, got %s", *retrievedUser.FirstName)
	}
	if *retrievedUser.LastName != "Smith" {
		t.Errorf("expected LastName Smith, got %s", *retrievedUser.LastName)
	}
	if retrievedUser.NetworkRating != 100 {
		t.Errorf("expected NetworkRating 100, got %d", retrievedUser.NetworkRating)
	}
	if retrievedUser.Password == "password123" {
		t.Errorf("password should be hashed, but got plaintext")
	}
	if len(retrievedUser.Password) != 60 {
		t.Errorf("expected password hash length 60, got %d", len(retrievedUser.Password))
	}

	// Verify password
	ok := repo.VerifyPasswordHash("password123", retrievedUser.Password)
	if !ok {
		t.Errorf("expected password to match, but it didn't")
	}
	ok = repo.VerifyPasswordHash("wrongpassword", retrievedUser.Password)
	if ok {
		t.Errorf("expected password not to match, but it did")
	}

	// Test with nil FirstName and LastName
	user = &User{
		Password:      "anotherpass",
		FirstName:     nil,
		LastName:      nil,
		NetworkRating: 200,
	}
	err = repo.CreateUser(user)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if user.CID <= 0 {
		t.Errorf("expected cid > 0, got %d", user.CID)
	}

	retrievedUser, err = repo.GetUserByCID(user.CID)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if retrievedUser.FirstName != nil {
		t.Errorf("expected FirstName to be nil, got %v", retrievedUser.FirstName)
	}
	if retrievedUser.LastName != nil {
		t.Errorf("expected LastName to be nil, got %v", retrievedUser.LastName)
	}
	if retrievedUser.NetworkRating != 200 {
		t.Errorf("expected NetworkRating 200, got %d", retrievedUser.NetworkRating)
	}

	// Test CID auto-increment
	user1 := &User{Password: "pass1", NetworkRating: 1}
	err = repo.CreateUser(user1)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	user2 := &User{Password: "pass2", NetworkRating: 2}
	err = repo.CreateUser(user2)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if user2.CID <= user1.CID {
		t.Errorf("expected cid2 > cid1, got cid1=%d, cid2=%d", user1.CID, user2.CID)
	}
}

// TestGetUserByCID verifies the GetUserByCID method, ensuring users can be retrieved and non-existent users return an error.
func TestGetUserByCID(t *testing.T) {
	db, repo := setupTestDB(t)
	defer db.Close()

	// Insert a test user
	user := &User{
		Password:      "testpass",
		FirstName:     ptr("Bob"),
		LastName:      ptr("Brown"),
		NetworkRating: 150,
	}
	err := repo.CreateUser(user)
	if err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}

	// Retrieve the user
	retrievedUser, err := repo.GetUserByCID(user.CID)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if retrievedUser == nil {
		t.Errorf("expected user, got nil")
	}
	if retrievedUser.CID != user.CID {
		t.Errorf("expected CID %d, got %d", user.CID, retrievedUser.CID)
	}
	if *retrievedUser.FirstName != "Bob" {
		t.Errorf("expected FirstName Bob, got %s", *retrievedUser.FirstName)
	}
	if *retrievedUser.LastName != "Brown" {
		t.Errorf("expected LastName Brown, got %s", *retrievedUser.LastName)
	}
	if retrievedUser.NetworkRating != 150 {
		t.Errorf("expected NetworkRating 150, got %d", retrievedUser.NetworkRating)
	}

	// Test with non-existing CID
	_, err = repo.GetUserByCID(9999)
	if err == nil {
		t.Errorf("expected error, got nil")
	} else if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}

	// Test with CID = 0
	_, err = repo.GetUserByCID(0)
	if err == nil {
		t.Errorf("expected error, got nil")
	} else if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

// TestVerifyPasswordHash verifies the VerifyPasswordHash method, ensuring correct password verification.
func TestVerifyPasswordHash(t *testing.T) {
	repo := &SQLiteUserRepository{} // db not needed

	// Generate a hash
	password := "securepassword"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed to generate hash: %v", err)
	}

	// Test with correct password
	ok := repo.VerifyPasswordHash(password, string(hash))
	if !ok {
		t.Errorf("expected true, got false")
	}

	// Test with incorrect password
	ok = repo.VerifyPasswordHash("wrongpassword", string(hash))
	if ok {
		t.Errorf("expected false, got true")
	}

	// Test with empty password
	ok = repo.VerifyPasswordHash("", string(hash))
	if ok {
		t.Errorf("expected false for empty password, got true")
	}

	// Test with empty hash
	ok = repo.VerifyPasswordHash(password, "")
	if ok {
		t.Errorf("expected false for empty hash, got true")
	}

	// Test with special characters in password
	password = "pass@#$%"
	hash, err = bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("failed to generate hash: %v", err)
	}
	ok = repo.VerifyPasswordHash(password, string(hash))
	if !ok {
		t.Errorf("expected true for password with special characters, got false")
	}
}

func TestUpdateUser(t *testing.T) {
	db, repo := setupTestDB(t)
	defer db.Close()

	t.Run("Update without password", func(t *testing.T) {
		user := &User{
			Password:      "initialpass",
			FirstName:     ptr("John"),
			LastName:      ptr("Doe"),
			NetworkRating: 100,
		}
		err := repo.CreateUser(user)
		if err != nil {
			t.Fatalf("failed to create test user: %v", err)
		}

		updateUser := &User{
			CID:           user.CID,
			Password:      "",
			FirstName:     ptr("Jane"),
			LastName:      ptr("Doe"),
			NetworkRating: 200,
		}
		err = repo.UpdateUser(updateUser)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		retrievedUser, err := repo.GetUserByCID(user.CID)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if *retrievedUser.FirstName != "Jane" {
			t.Errorf("expected FirstName Jane, got %s", *retrievedUser.FirstName)
		}
		if *retrievedUser.LastName != "Doe" {
			t.Errorf("expected LastName Doe, got %s", *retrievedUser.LastName)
		}
		if retrievedUser.NetworkRating != 200 {
			t.Errorf("expected NetworkRating 200, got %d", retrievedUser.NetworkRating)
		}
		ok := repo.VerifyPasswordHash("initialpass", retrievedUser.Password)
		if !ok {
			t.Errorf("expected password to remain the same, but it didn't match")
		}
	})

	t.Run("Update with new password", func(t *testing.T) {
		user := &User{
			Password:      "initialpass",
			FirstName:     ptr("John"),
			LastName:      ptr("Doe"),
			NetworkRating: 100,
		}
		err := repo.CreateUser(user)
		if err != nil {
			t.Fatalf("failed to create test user: %v", err)
		}

		updateUser := &User{
			CID:           user.CID,
			Password:      "newpass",
			FirstName:     ptr("Jane"),
			LastName:      ptr("Smith"),
			NetworkRating: 300,
		}
		err = repo.UpdateUser(updateUser)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		retrievedUser, err := repo.GetUserByCID(user.CID)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if *retrievedUser.FirstName != "Jane" {
			t.Errorf("expected FirstName Jane, got %s", *retrievedUser.FirstName)
		}
		if *retrievedUser.LastName != "Smith" {
			t.Errorf("expected LastName Smith, got %s", *retrievedUser.LastName)
		}
		if retrievedUser.NetworkRating != 300 {
			t.Errorf("expected NetworkRating 300, got %d", retrievedUser.NetworkRating)
		}
		ok := repo.VerifyPasswordHash("newpass", retrievedUser.Password)
		if !ok {
			t.Errorf("expected password to be updated to 'newpass', but it didn't match")
		}
		ok = repo.VerifyPasswordHash("initialpass", retrievedUser.Password)
		if ok {
			t.Errorf("expected old password not to match, but it did")
		}
	})

	t.Run("Update with invalid password", func(t *testing.T) {
		user := &User{
			Password:      "initialpass",
			FirstName:     ptr("John"),
			LastName:      ptr("Doe"),
			NetworkRating: 100,
		}
		err := repo.CreateUser(user)
		if err != nil {
			t.Fatalf("failed to create test user: %v", err)
		}

		updateUser := &User{
			CID:           user.CID,
			Password:      "pass:word",
			FirstName:     ptr("Invalid"),
			LastName:      ptr("Password"),
			NetworkRating: 400,
		}
		err = repo.UpdateUser(updateUser)
		if err == nil {
			t.Errorf("expected error due to colon in password, got nil")
		} else if !strings.Contains(err.Error(), "colon") {
			t.Errorf("expected error message about colon, got %v", err)
		}

		// Check that user was not updated
		retrievedUser, err := repo.GetUserByCID(user.CID)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if *retrievedUser.FirstName != "John" {
			t.Errorf("expected FirstName to remain John, got %s", *retrievedUser.FirstName)
		}
		if *retrievedUser.LastName != "Doe" {
			t.Errorf("expected LastName to remain Doe, got %s", *retrievedUser.LastName)
		}
		if retrievedUser.NetworkRating != 100 {
			t.Errorf("expected NetworkRating to remain 100, got %d", retrievedUser.NetworkRating)
		}
		ok := repo.VerifyPasswordHash("initialpass", retrievedUser.Password)
		if !ok {
			t.Errorf("expected password to remain the same, but it didn't match")
		}
	})

	t.Run("Update non-existent user", func(t *testing.T) {
		nonExistentUser := &User{
			CID:           9999,
			Password:      "somepass",
			FirstName:     ptr("Ghost"),
			LastName:      ptr("User"),
			NetworkRating: 0,
		}
		err := repo.UpdateUser(nonExistentUser)
		if err == nil {
			t.Errorf("expected error, got nil")
		} else if err != sql.ErrNoRows {
			t.Errorf("expected sql.ErrNoRows, got %v", err)
		}
	})

	t.Run("Update with nil FirstName", func(t *testing.T) {
		user := &User{
			Password:      "initialpass",
			FirstName:     ptr("John"),
			LastName:      ptr("Doe"),
			NetworkRating: 100,
		}
		err := repo.CreateUser(user)
		if err != nil {
			t.Fatalf("failed to create test user: %v", err)
		}

		updateUser := &User{
			CID:           user.CID,
			Password:      "",
			FirstName:     nil,
			LastName:      ptr("Doe"),
			NetworkRating: 200,
		}
		err = repo.UpdateUser(updateUser)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		retrievedUser, err := repo.GetUserByCID(user.CID)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if retrievedUser.FirstName != nil {
			t.Errorf("expected FirstName to be nil, got %v", retrievedUser.FirstName)
		}
		if *retrievedUser.LastName != "Doe" {
			t.Errorf("expected LastName Doe, got %s", *retrievedUser.LastName)
		}
		if retrievedUser.NetworkRating != 200 {
			t.Errorf("expected NetworkRating 200, got %d", retrievedUser.NetworkRating)
		}
	})

	t.Run("Update with empty FirstName", func(t *testing.T) {
		user := &User{
			Password:      "initialpass",
			FirstName:     ptr("John"),
			LastName:      ptr("Doe"),
			NetworkRating: 100,
		}
		err := repo.CreateUser(user)
		if err != nil {
			t.Fatalf("failed to create test user: %v", err)
		}

		updateUser := &User{
			CID:           user.CID,
			Password:      "",
			FirstName:     ptr(""),
			LastName:      ptr("Doe"),
			NetworkRating: 200,
		}
		err = repo.UpdateUser(updateUser)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}

		retrievedUser, err := repo.GetUserByCID(user.CID)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if retrievedUser.FirstName == nil {
			t.Errorf("expected FirstName to be empty string, got nil")
		} else if *retrievedUser.FirstName != "" {
			t.Errorf("expected FirstName to be empty string, got %s", *retrievedUser.FirstName)
		}
		if *retrievedUser.LastName != "Doe" {
			t.Errorf("expected LastName Doe, got %s", *retrievedUser.LastName)
		}
		if retrievedUser.NetworkRating != 200 {
			t.Errorf("expected NetworkRating 200, got %d", retrievedUser.NetworkRating)
		}
	})
}
