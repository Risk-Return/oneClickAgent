// Argon2id password hashing and verification (alexedwards/argon2id).
package auth

import (
	"github.com/alexedwards/argon2id"
)

// PasswordHasher wraps Argon2id password hashing.
type PasswordHasher struct {
	params *argon2id.Params
}

// NewPasswordHasher creates a new password hasher with sensible defaults.
func NewPasswordHasher() *PasswordHasher {
	return &PasswordHasher{
		params: argon2id.DefaultParams,
	}
}

// Hash hashes a plain-text password using Argon2id.
func (h *PasswordHasher) Hash(password string) (string, error) {
	return argon2id.CreateHash(password, h.params)
}

// Verify checks a plain-text password against an Argon2id hash.
func (h *PasswordHasher) Verify(password, hash string) (bool, error) {
	match, err := argon2id.ComparePasswordAndHash(password, hash)
	if err != nil {
		return false, err
	}
	return match, nil
}

// ValidatePassword performs basic password strength validation.
func ValidatePassword(password string) error {
	if len(password) < 12 {
		return errPasswordTooShort
	}
	return nil
}

var errPasswordTooShort = &passwordError{"password must be at least 12 characters"}

type passwordError struct {
	msg string
}

func (e *passwordError) Error() string {
	return e.msg
}
