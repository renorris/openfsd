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

func TestListUsersAndCount(t *testing.T) {
	db, repo := setupTestDB(t)
	defer db.Close()

	// Seed a few users with known names/ratings
	seeds := []struct {
		first, last string
		rating      int
	}{
		{"Alice", "Smith", 1},
		{"Bob", "Jones", 2},
		{"Carol", "Smith", 11},
		{"Dave", "Brown", 1},
	}
	for _, s := range seeds {
		u := &User{
			Password:      "password1",
			FirstName:     ptr(s.first),
			LastName:      ptr(s.last),
			NetworkRating: s.rating,
		}
		if err := repo.CreateUser(u); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
	}

	t.Run("list all ordered by cid", func(t *testing.T) {
		users, err := repo.ListUsers(UserListFilter{Sort: "cid", Limit: 100})
		if err != nil {
			t.Fatalf("ListUsers: %v", err)
		}
		if len(users) != 4 {
			t.Fatalf("got %d users, want 4", len(users))
		}
		for i := 1; i < len(users); i++ {
			if users[i].CID <= users[i-1].CID {
				t.Fatalf("expected ascending cid, got %d then %d", users[i-1].CID, users[i].CID)
			}
		}
	})

	t.Run("count all", func(t *testing.T) {
		n, err := repo.CountUsers(UserListFilter{})
		if err != nil {
			t.Fatalf("CountUsers: %v", err)
		}
		if n != 4 {
			t.Fatalf("count=%d want 4", n)
		}
	})

	t.Run("count with query", func(t *testing.T) {
		n, err := repo.CountUsers(UserListFilter{Query: "smith"})
		if err != nil {
			t.Fatalf("CountUsers: %v", err)
		}
		if n != 2 {
			t.Fatalf("count smith=%d want 2", n)
		}
	})

	t.Run("count ignores limit and offset", func(t *testing.T) {
		n, err := repo.CountUsers(UserListFilter{Limit: 1, Offset: 100})
		if err != nil {
			t.Fatalf("CountUsers: %v", err)
		}
		if n != 4 {
			t.Fatalf("count with Limit/Offset=%d want 4 (full total)", n)
		}
		n, err = repo.CountUsers(UserListFilter{Query: "smith", Limit: 1, Offset: 50})
		if err != nil {
			t.Fatalf("CountUsers: %v", err)
		}
		if n != 2 {
			t.Fatalf("count smith with Limit/Offset=%d want 2", n)
		}
	})

	t.Run("filter by rating", func(t *testing.T) {
		r := 1
		users, err := repo.ListUsers(UserListFilter{Rating: &r, Sort: "cid"})
		if err != nil {
			t.Fatalf("ListUsers: %v", err)
		}
		if len(users) != 2 {
			t.Fatalf("got %d, want 2 rating=1 users", len(users))
		}
		n, err := repo.CountUsers(UserListFilter{Rating: &r})
		if err != nil {
			t.Fatalf("CountUsers: %v", err)
		}
		if n != 2 {
			t.Fatalf("count=%d want 2", n)
		}
	})

	t.Run("search by last name", func(t *testing.T) {
		users, err := repo.ListUsers(UserListFilter{Query: "smith", Sort: "name"})
		if err != nil {
			t.Fatalf("ListUsers: %v", err)
		}
		if len(users) != 2 {
			t.Fatalf("got %d smiths, want 2", len(users))
		}
	})

	t.Run("search by full name", func(t *testing.T) {
		users, err := repo.ListUsers(UserListFilter{Query: "alice smith"})
		if err != nil {
			t.Fatalf("ListUsers: %v", err)
		}
		if len(users) != 1 {
			t.Fatalf("got %d, want 1", len(users))
		}
		if *users[0].FirstName != "Alice" {
			t.Fatalf("first=%q", *users[0].FirstName)
		}
	})

	t.Run("search by cid substring", func(t *testing.T) {
		// First created user typically has cid=1
		users, err := repo.ListUsers(UserListFilter{Query: "1", Sort: "cid"})
		if err != nil {
			t.Fatalf("ListUsers: %v", err)
		}
		if len(users) < 1 {
			t.Fatal("expected at least one match for cid containing 1")
		}
	})

	t.Run("sort by rating desc", func(t *testing.T) {
		users, err := repo.ListUsers(UserListFilter{Sort: "rating", Desc: true, Limit: 10})
		if err != nil {
			t.Fatalf("ListUsers: %v", err)
		}
		if len(users) < 2 {
			t.Fatal("need users")
		}
		if users[0].NetworkRating < users[1].NetworkRating {
			t.Fatalf("expected desc rating, got %d then %d", users[0].NetworkRating, users[1].NetworkRating)
		}
	})

	t.Run("pagination", func(t *testing.T) {
		page1, err := repo.ListUsers(UserListFilter{Sort: "cid", Limit: 2, Offset: 0})
		if err != nil {
			t.Fatalf("page1: %v", err)
		}
		page2, err := repo.ListUsers(UserListFilter{Sort: "cid", Limit: 2, Offset: 2})
		if err != nil {
			t.Fatalf("page2: %v", err)
		}
		if len(page1) != 2 || len(page2) != 2 {
			t.Fatalf("page sizes %d %d", len(page1), len(page2))
		}
		if page1[0].CID == page2[0].CID {
			t.Fatal("pages should not overlap")
		}
	})

	t.Run("like metacharacters escaped", func(t *testing.T) {
		// Literal metacharacters must not act as wildcards against existing names
		for _, q := range []string{"%", "_", `\`} {
			users, err := repo.ListUsers(UserListFilter{Query: q})
			if err != nil {
				t.Fatalf("ListUsers Query=%q: %v", q, err)
			}
			if len(users) != 0 {
				t.Fatalf("literal %q should match none among seed names, got %d", q, len(users))
			}
		}
	})

	t.Run("password empty on list results", func(t *testing.T) {
		users, err := repo.ListUsers(UserListFilter{Sort: "cid", Limit: 100})
		if err != nil {
			t.Fatalf("ListUsers: %v", err)
		}
		if len(users) == 0 {
			t.Fatal("expected users")
		}
		for _, u := range users {
			if u.Password != "" {
				t.Fatalf("list result Password must be empty, got %q for cid=%d", u.Password, u.CID)
			}
		}
	})

	t.Run("limit default when <=0", func(t *testing.T) {
		// Zero-value Limit → default 50; with only 4 users we still get all 4
		users, err := repo.ListUsers(UserListFilter{Limit: 0})
		if err != nil {
			t.Fatalf("ListUsers: %v", err)
		}
		if len(users) != 4 {
			t.Fatalf("got %d, want 4 (default limit 50 still returns all)", len(users))
		}
		users, err = repo.ListUsers(UserListFilter{Limit: -5})
		if err != nil {
			t.Fatalf("ListUsers: %v", err)
		}
		if len(users) != 4 {
			t.Fatalf("got %d, want 4 for negative limit", len(users))
		}
	})

	t.Run("limit hard cap 200", func(t *testing.T) {
		// Cap is applied; with only 4 users result size is still 4
		users, err := repo.ListUsers(UserListFilter{Limit: 1000})
		if err != nil {
			t.Fatalf("ListUsers: %v", err)
		}
		if len(users) != 4 {
			t.Fatalf("got %d, want 4", len(users))
		}
	})

	t.Run("offset negative clamps to 0", func(t *testing.T) {
		users, err := repo.ListUsers(UserListFilter{Sort: "cid", Limit: 2, Offset: -10})
		if err != nil {
			t.Fatalf("ListUsers: %v", err)
		}
		if len(users) != 2 {
			t.Fatalf("got %d, want 2", len(users))
		}
		// Should match offset 0 page
		page0, err := repo.ListUsers(UserListFilter{Sort: "cid", Limit: 2, Offset: 0})
		if err != nil {
			t.Fatalf("ListUsers: %v", err)
		}
		if users[0].CID != page0[0].CID {
			t.Fatalf("negative offset should clamp to 0, got cid %d want %d", users[0].CID, page0[0].CID)
		}
	})

	t.Run("invalid sort falls back to cid", func(t *testing.T) {
		users, err := repo.ListUsers(UserListFilter{Sort: "nope", Limit: 100})
		if err != nil {
			t.Fatalf("ListUsers: %v", err)
		}
		if len(users) != 4 {
			t.Fatalf("got %d, want 4", len(users))
		}
		for i := 1; i < len(users); i++ {
			if users[i].CID <= users[i-1].CID {
				t.Fatalf("expected ascending cid fallback, got %d then %d", users[i-1].CID, users[i].CID)
			}
		}
	})

	t.Run("rating filter zero and negative", func(t *testing.T) {
		// Seed suspended (0) and inactive (-1)
		if err := repo.CreateUser(&User{Password: "password1", FirstName: ptr("Eve"), LastName: ptr("Suspended"), NetworkRating: 0}); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		if err := repo.CreateUser(&User{Password: "password1", FirstName: ptr("Frank"), LastName: ptr("Inactive"), NetworkRating: -1}); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		r0 := 0
		users, err := repo.ListUsers(UserListFilter{Rating: &r0})
		if err != nil {
			t.Fatalf("ListUsers: %v", err)
		}
		if len(users) != 1 || users[0].NetworkRating != 0 {
			t.Fatalf("rating=0: got %d users", len(users))
		}
		rm1 := -1
		users, err = repo.ListUsers(UserListFilter{Rating: &rm1})
		if err != nil {
			t.Fatalf("ListUsers: %v", err)
		}
		if len(users) != 1 || users[0].NetworkRating != -1 {
			t.Fatalf("rating=-1: got %d users", len(users))
		}
		// nil rating still means all
		n, err := repo.CountUsers(UserListFilter{})
		if err != nil {
			t.Fatalf("CountUsers: %v", err)
		}
		if n != 6 {
			t.Fatalf("count all after seed=%d want 6", n)
		}
	})
}

func TestEscapeLike(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`\`, `\\`},
		{`%`, `\%`},
		{`_`, `\_`},
		{`a%b_c\d`, `a\%b\_c\\d`},
		{`plain`, `plain`},
		{``, ``},
	}
	for _, tc := range cases {
		got := escapeLike(tc.in)
		if got != tc.want {
			t.Errorf("escapeLike(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

// TestListUsersLikeLiteralMetacharacters seeds names that literally contain
// %, _, \ and asserts Query finds those rows without treating the chars as wildcards.
func TestListUsersLikeLiteralMetacharacters(t *testing.T) {
	db, repo := setupTestDB(t)
	defer db.Close()

	// Decoy users so unescaped wildcards would match widely
	for _, s := range []struct{ first, last string }{
		{"Alice", "Smith"},
		{"Bob", "Jones"},
	} {
		if err := repo.CreateUser(&User{
			Password: "password1", FirstName: ptr(s.first), LastName: ptr(s.last), NetworkRating: 1,
		}); err != nil {
			t.Fatalf("CreateUser decoy: %v", err)
		}
	}

	if err := repo.CreateUser(&User{
		Password: "password1", FirstName: ptr("Pat"), LastName: ptr("100%"), NetworkRating: 1,
	}); err != nil {
		t.Fatalf("CreateUser %%: %v", err)
	}
	if err := repo.CreateUser(&User{
		Password: "password1", FirstName: ptr("Und"), LastName: ptr("a_b"), NetworkRating: 1,
	}); err != nil {
		t.Fatalf("CreateUser _: %v", err)
	}
	if err := repo.CreateUser(&User{
		Password: "password1", FirstName: ptr("Back"), LastName: ptr(`x\y`), NetworkRating: 1,
	}); err != nil {
		t.Fatalf("CreateUser \\: %v", err)
	}

	users, err := repo.ListUsers(UserListFilter{Query: "100%"})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 1 || *users[0].LastName != "100%" {
		t.Fatalf("literal 100%%: got %d users", len(users))
	}

	users, err = repo.ListUsers(UserListFilter{Query: "a_b"})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 1 || *users[0].LastName != "a_b" {
		t.Fatalf("literal a_b: got %d users", len(users))
	}

	// Bare "_" must not widen to every single-char-gap match of other names
	users, err = repo.ListUsers(UserListFilter{Query: "_"})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 1 || *users[0].LastName != "a_b" {
		t.Fatalf("literal _: want only a_b row, got %d", len(users))
	}

	users, err = repo.ListUsers(UserListFilter{Query: `x\y`})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 1 || *users[0].LastName != `x\y` {
		t.Fatalf(`literal x\y: got %d users`, len(users))
	}

	// Bare "%" still matches only names that contain a percent sign
	users, err = repo.ListUsers(UserListFilter{Query: "%"})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 1 || *users[0].LastName != "100%" {
		t.Fatalf("literal %%: want only 100%% row, got %d", len(users))
	}
}

func TestListUsersEmptyDB(t *testing.T) {
	db, repo := setupTestDB(t)
	defer db.Close()

	users, err := repo.ListUsers(UserListFilter{})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 0 {
		t.Fatalf("got %d, want 0", len(users))
	}
	n, err := repo.CountUsers(UserListFilter{})
	if err != nil {
		t.Fatalf("CountUsers: %v", err)
	}
	if n != 0 {
		t.Fatalf("count=%d want 0", n)
	}
}

// TestListUsersLimitDefaultWithManyRows verifies Limit<=0 applies default 50
// and Limit>200 is capped at 200 against a larger seed set.
// Users are bulk-inserted via SQL (fixed bcrypt hash) to avoid bcrypt cost.
func TestListUsersLimitDefaultWithManyRows(t *testing.T) {
	db, repo := setupTestDB(t)
	defer db.Close()

	// Precomputed bcrypt hash for "x" (not verified here; list omits password).
	const hash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"
	const total = 220
	for i := 0; i < total; i++ {
		if _, err := db.Exec(
			`INSERT INTO users (password, first_name, last_name, network_rating) VALUES (?, ?, ?, ?)`,
			hash, "User", "X", 1,
		); err != nil {
			t.Fatalf("bulk insert: %v", err)
		}
	}

	// Default limit 50
	users, err := repo.ListUsers(UserListFilter{Limit: 0})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 50 {
		t.Fatalf("default limit: got %d, want 50", len(users))
	}

	// Negative limit also defaults to 50
	users, err = repo.ListUsers(UserListFilter{Limit: -1})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 50 {
		t.Fatalf("negative limit default: got %d, want 50", len(users))
	}

	// Hard cap 200
	users, err = repo.ListUsers(UserListFilter{Limit: 1000})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 200 {
		t.Fatalf("hard cap: got %d, want 200", len(users))
	}

	n, err := repo.CountUsers(UserListFilter{})
	if err != nil {
		t.Fatalf("CountUsers: %v", err)
	}
	if n != total {
		t.Fatalf("total=%d want %d", n, total)
	}
}

func TestDeleteUser(t *testing.T) {
	db, repo := setupTestDB(t)
	defer db.Close()

	user := &User{
		Password:      "password123",
		FirstName:     ptr("Delete"),
		LastName:      ptr("Me"),
		NetworkRating: 1,
	}
	if err := repo.CreateUser(user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	cid := user.CID

	if err := repo.DeleteUser(cid); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	_, err := repo.GetUserByCID(cid)
	if err == nil {
		t.Fatal("expected ErrNoRows after delete")
	}
	if err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}

	// Missing CID
	err = repo.DeleteUser(999999)
	if err == nil {
		t.Fatal("expected ErrNoRows for missing CID")
	}
	if err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}
}
