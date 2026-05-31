// Package auth provides JWT issue/verify (golang-jwt), key rollover (kid),
// and claims validation (sub, role, exp, jti).
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/iagent/gateway/internal/model"
)

// Claims represents the JWT claims for IAgent.
type Claims struct {
	jwt.RegisteredClaims
	Role  model.UserRole `json:"role"`
	Tier  model.UserTier `json:"tier"`
	OrgID *model.UUID    `json:"org_id,omitempty"`
}

// JWTManager handles token issuance and verification.
type JWTManager struct {
	secret    []byte
	accessTTL time.Duration
}

// NewJWTManager creates a new JWT manager.
func NewJWTManager(secret string, accessTTL time.Duration) *JWTManager {
	return &JWTManager{
		secret:    []byte(secret),
		accessTTL: accessTTL,
	}
}

// IssueAccessToken creates an access token for a user.
func (m *JWTManager) IssueAccessToken(user *model.User) (string, error) {
	now := time.Now().UTC()
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.accessTTL)),
			ID:        model.NewUUID().String(),
		},
		Role:  user.Role,
		Tier:  user.Tier,
		OrgID: user.OrgID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(m.secret)
}

// VerifyToken parses and validates an access token.
func (m *JWTManager) VerifyToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return m.secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	return claims, nil
}

// IssueRefreshToken generates a cryptographically random refresh token.
func IssueRefreshToken() (token string, tokenHash string, err error) {
	b := make([]byte, 64)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("failed to generate refresh token: %w", err)
	}
	token = hex.EncodeToString(b)

	// Hash for database storage
	h := sha256.Sum256([]byte(token))
	tokenHash = hex.EncodeToString(h[:])

	return token, tokenHash, nil
}

// HashToken creates a SHA-256 hash of a token for database storage.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// ExtractUserID extracts the user ID from verified claims.
func ExtractUserID(claims *Claims) (model.UUID, error) {
	return model.ParseUUID(claims.Subject)
}
