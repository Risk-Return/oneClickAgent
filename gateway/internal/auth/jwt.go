// Package auth provides JWT issue/verify (golang-jwt), key rollover (kid),
// and claims validation (sub, role, exp, jti).
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/oneClickAgent/gateway/internal/model"
)

// Claims represents the JWT claims for oneClickAgent.
type Claims struct {
	jwt.RegisteredClaims
	Role  model.UserRole `json:"role"`
	Tier  model.UserTier `json:"tier"`
	OrgID *model.UUID    `json:"org_id,omitempty"`
}

// JWTManager handles token issuance and verification.
type JWTManager struct {
	mu        sync.RWMutex
	keys      map[string][]byte // kid → secret
	activeKID string
	accessTTL time.Duration
}

// NewJWTManager creates a new JWT manager.
func NewJWTManager(secret string, accessTTL time.Duration) *JWTManager {
	kid := "key-" + model.NewUUID().String()[:8]
	m := &JWTManager{
		keys:      map[string][]byte{kid: []byte(secret)},
		activeKID: kid,
		accessTTL: accessTTL,
	}
	return m
}

// AddKey registers a new signing key and optionally makes it active.
func (m *JWTManager) AddKey(kid string, secret []byte, makeActive bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.keys[kid] = secret
	if makeActive {
		m.activeKID = kid
	}
}

// RotateKey generates a new active key from a secret string, keeping the old key for verification.
func (m *JWTManager) RotateKey(newSecret string) string {
	kid := "key-" + model.NewUUID().String()[:8]
	m.AddKey(kid, []byte(newSecret), true)
	return kid
}

// IssueAccessToken creates an access token for a user.
func (m *JWTManager) IssueAccessToken(user *model.User) (string, error) {
	m.mu.RLock()
	kid := m.activeKID
	secret := m.keys[kid]
	m.mu.RUnlock()

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
	token.Header["kid"] = kid
	return token.SignedString(secret)
}

// VerifyToken parses and validates an access token.
func (m *JWTManager) VerifyToken(tokenString string) (*Claims, error) {
	parser := jwt.NewParser()
	parsed, _, err := parser.ParseUnverified(tokenString, &Claims{})
	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	kid, _ := parsed.Header["kid"].(string)

	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}

		m.mu.RLock()
		defer m.mu.RUnlock()

		// Try the kid from the header first
		if kid != "" {
			if secret, ok := m.keys[kid]; ok {
				return secret, nil
			}
		}

		// Fallback: try the active key
		if secret, ok := m.keys[m.activeKID]; ok {
			return secret, nil
		}

		// Try all known keys
		for _, secret := range m.keys {
			return secret, nil
		}

		return nil, fmt.Errorf("no valid signing key found")
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
