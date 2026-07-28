package afv

import "golang.org/x/crypto/bcrypt"

// dummyBcryptHash equalizes auth timing when CID is unknown (same recipe as FSD).
var dummyBcryptHash = mustDummyBcrypt()

func mustDummyBcrypt() string {
	h, err := bcrypt.GenerateFromPassword([]byte("openfsd-timing-pad"), bcrypt.DefaultCost)
	if err != nil {
		// Fallback never used for real auth; CompareHashAndPassword will fail.
		return "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZRGdjGj/n3.rOqP1qP1qP1qP1qP1q"
	}
	return string(h)
}
