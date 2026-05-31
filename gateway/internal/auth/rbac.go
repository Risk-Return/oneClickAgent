// RBAC helpers: role checks, tenant scoping assertions,
// and visibility resolution for skills (public / user grant / org grant).
package auth

import (
	"errors"

	"github.com/iagent/gateway/internal/model"
)

var (
	ErrNotAdmin      = errors.New("admin role required")
	ErrNotOwner      = errors.New("resource not owned by user")
	ErrForbidden     = errors.New("forbidden")
)

// IsAdmin checks if the claims have the admin role.
func IsAdmin(claims *Claims) bool {
	return claims.Role == model.RoleAdmin
}

// IsUser checks if the claims have the user (customer) role.
func IsUser(claims *Claims) bool {
	return claims.Role == model.RoleUser
}

// RequireAdmin returns an error if the claims are not admin.
func RequireAdmin(claims *Claims) error {
	if !IsAdmin(claims) {
		return ErrNotAdmin
	}
	return nil
}

// IsOwner checks if the authenticated user owns the resource.
func IsOwner(claims *Claims, resourceOwnerID model.UUID) bool {
	userID, err := ExtractUserID(claims)
	if err != nil {
		return false
	}
	return userID == resourceOwnerID
}

// RequireOwner returns an error if the user does not own the resource.
func RequireOwner(claims *Claims, resourceOwnerID model.UUID) error {
	if !IsOwner(claims, resourceOwnerID) {
		return ErrNotOwner
	}
	return nil
}

// CanAccessAgent checks if a user can access a pooled agent.
// Admins can access all agents; users can only access agents allocated to their active jobs.
func CanAccessAgent(claims *Claims, agent *model.Agent) error {
	if IsAdmin(claims) {
		return nil
	}
	if agent.UserID != nil {
		userID, err := ExtractUserID(claims)
		if err != nil {
			return err
		}
		if *agent.UserID == userID {
			return nil
		}
	}
	return ErrNotOwner
}

// CanManageDevice checks if a user can manage devices (admin only).
func CanManageDevice(claims *Claims) error {
	return RequireAdmin(claims)
}

// CanManageSkills checks if a user can manage skill vault and fleet (admin only).
func CanManageSkills(claims *Claims) error {
	return RequireAdmin(claims)
}

// CanManageOrgs checks if a user can manage organizations (admin only).
func CanManageOrgs(claims *Claims) error {
	return RequireAdmin(claims)
}

// CanSetUserTier checks if a user can change tiers (admin only).
func CanSetUserTier(claims *Claims) error {
	return RequireAdmin(claims)
}

// CanSubmitJob checks if a user can submit a job.
func CanSubmitJob(claims *Claims) error {
	if claims.Role != model.RoleAdmin && claims.Role != model.RoleUser {
		return ErrForbidden
	}
	return nil
}
