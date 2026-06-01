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

// ValidatePassword performs password strength validation.
// Requires: 12+ chars, at least 1 uppercase, 1 lowercase, 1 digit, 1 special character.
func ValidatePassword(password string) error {
	if len(password) < 12 {
		return errPasswordTooShort
	}

	var hasUpper, hasLower, hasDigit, hasSpecial bool
	for _, c := range password {
		switch {
		case c >= 'A' && c <= 'Z':
			hasUpper = true
		case c >= 'a' && c <= 'z':
			hasLower = true
		case c >= '0' && c <= '9':
			hasDigit = true
		default:
			hasSpecial = true
		}
	}

	if !hasUpper {
		return &passwordError{"password must contain at least one uppercase letter"}
	}
	if !hasLower {
		return &passwordError{"password must contain at least one lowercase letter"}
	}
	if !hasDigit {
		return &passwordError{"password must contain at least one digit"}
	}
	if !hasSpecial {
		return &passwordError{"password must contain at least one special character"}
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
