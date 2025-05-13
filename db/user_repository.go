package db

type User struct {
	CID           int
	Password      string
	FirstName     *string
	LastName      *string
	NetworkRating int
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

	// ListUsersByCID retrieves a list of ordered User records starting at a given CID
	//ListUsersByCID(cid int, limit int, offset int) ([]*User, error)

	// VerifyPasswordHash verifies a User password hash.
	VerifyPasswordHash(plaintext string, hash string) (ok bool)
}
