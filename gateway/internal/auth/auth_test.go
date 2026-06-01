package auth

import (
	"testing"
	"time"

	"github.com/oneClickAgent/gateway/internal/model"
)

func TestPasswordHashVerify(t *testing.T) {
	h := NewPasswordHasher()
	hash, err := h.Hash("my-strong-password-123")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if hash == "" {
		t.Error("hash should not be empty")
	}

	match, err := h.Verify("my-strong-password-123", hash)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !match {
		t.Error("password should match")
	}

	match, _ = h.Verify("wrong-password", hash)
	if match {
		t.Error("wrong password should not match")
	}
}

func TestValidatePassword(t *testing.T) {
	if ValidatePassword("short") == nil {
		t.Error("short password should fail")
	}
	if ValidatePassword("This-Is-Long-Enough1!") != nil {
		t.Error("long password should pass")
	}
}

func TestJWTIssueVerify(t *testing.T) {
	jwtMgr := NewJWTManager("my-32-char-secret-key-for-testing!!", 15*time.Minute)

	user := &model.User{
		ID:    model.NewUUID(),
		Email: "test@example.com",
		Role:  model.RoleUser,
		Tier:  model.TierFree,
	}

	token, err := jwtMgr.IssueAccessToken(user)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	if token == "" {
		t.Error("token should not be empty")
	}

	claims, err := jwtMgr.VerifyToken(token)
	if err != nil {
		t.Fatalf("verify token: %v", err)
	}
	if claims.Role != model.RoleUser {
		t.Errorf("expected role user, got %s", claims.Role)
	}
	if claims.Tier != model.TierFree {
		t.Errorf("expected tier free, got %s", claims.Tier)
	}

	id, err := ExtractUserID(claims)
	if err != nil {
		t.Fatalf("extract user ID: %v", err)
	}
	if id != user.ID {
		t.Error("user ID should match")
	}
}

func TestJWTInvalidToken(t *testing.T) {
	jwtMgr := NewJWTManager("my-32-char-secret-key-for-testing!!", 15*time.Minute)
	_, err := jwtMgr.VerifyToken("invalid-token")
	if err == nil {
		t.Error("should fail on invalid token")
	}
}

func TestIssueRefreshToken(t *testing.T) {
	token, hash, err := IssueRefreshToken()
	if err != nil {
		t.Fatalf("issue refresh token: %v", err)
	}
	if token == "" || hash == "" {
		t.Error("token and hash should not be empty")
	}
	if len(token) != 128 {
		t.Errorf("token should be 128 hex chars (64 bytes), got %d", len(token))
	}
}

func TestHashToken(t *testing.T) {
	hash := HashToken("test-token")
	if hash == "" || hash == "test-token" {
		t.Error("hash should be a non-empty digest")
	}
	// Same input should produce same hash
	hash2 := HashToken("test-token")
	if hash != hash2 {
		t.Error("same input should produce same hash")
	}
}

func TestRBACIsAdmin(t *testing.T) {
	adminClaims := &Claims{Role: model.RoleAdmin}
	if !IsAdmin(adminClaims) {
		t.Error("admin claims should be admin")
	}
	userClaims := &Claims{Role: model.RoleUser}
	if IsAdmin(userClaims) {
		t.Error("user claims should not be admin")
	}
}

func TestRBACRequireAdmin(t *testing.T) {
	adminClaims := &Claims{Role: model.RoleAdmin}
	if err := RequireAdmin(adminClaims); err != nil {
		t.Errorf("admin should pass: %v", err)
	}
	userClaims := &Claims{Role: model.RoleUser}
	if err := RequireAdmin(userClaims); err == nil {
		t.Error("user should fail admin requirement")
	}
}
