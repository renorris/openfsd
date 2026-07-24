package db

type User struct {
	CID           int
	Password      string
	FirstName     *string
	LastName      *string
	NetworkRating int
	// PilotRating is the VATSIM pilot rating wire ID (0, 1, 3, 7, 15, 31, 63).
	// See protocol.PilotRating / protocol.PilotRatingScale.
	PilotRating int
}

// UserListFilter controls ListUsers / CountUsers.
//
// Limit policy (authoritative):
//   - Limit <= 0 → default 50
//   - Limit > 200 → capped at 200
//   - Offset < 0 → 0
//
// Sort: "cid" | "name" | "rating"; anything else → "cid".
// Rating: nil = all ratings; non-nil exact match (0 and -1 are valid).
// Zero-value filter therefore returns the first page of all users by cid asc
// (Limit default 50) — not an unbounded dump.
type UserListFilter struct {
	Query  string
	Rating *int
	Sort   string // "cid" | "name" | "rating"; default cid
	Desc   bool
	Limit  int // <=0 → default 50; hard cap 200
	Offset int // <0 → 0
}

type UserRepository interface {
	// CreateUser creates a new User record.
	// The CID value is automatically populated in the provided User struct.
	//
	// The provided password must be in plaintext.
	CreateUser(*User) (err error)

	// GetUserByCID retrieves a User record by CID.
	//
	// Returns sql.ErrNoRows when no rows are found.
	GetUserByCID(cid int) (*User, error)

	// UpdateUser updates a User record by CID.
	//
	// All fields must be provided except:
	//
	// 1. Password is only updated if a non-empty string is provided.
	UpdateUser(*User) error

	// ListUsers returns matching users. Password must be left empty (list SELECT
	// omits password column). Callers that need the hash use GetUserByCID.
	ListUsers(filter UserListFilter) ([]*User, error)

	// CountUsers counts rows matching Query + Rating only (Sort/Limit/Offset ignored).
	CountUsers(filter UserListFilter) (int, error)

	// VerifyPasswordHash verifies a User password hash.
	VerifyPasswordHash(plaintext string, hash string) (ok bool)
}
