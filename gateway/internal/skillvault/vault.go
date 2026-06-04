// Package skillvault manages the cloud skill catalog: CRUD on skills,
// version publishing with manifest+artifact, deprecation/deletion.
package skillvault

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/oneClickAgent/gateway/internal/model"
	"github.com/oneClickAgent/gateway/internal/store"
)

// Vault manages the skill catalog and artifact storage.
type Vault struct {
	skills  *store.SkillStore
	baseDir string // artifact storage directory
}

// NewVault creates a new skill vault.
func NewVault(skills *store.SkillStore, artifactDir string) *Vault {
	return &Vault{
		skills:  skills,
		baseDir: artifactDir,
	}
}

// CreateSkill creates a new skill in the catalog.
func (v *Vault) CreateSkill(ctx context.Context, key, name, description string, visibility model.SkillVisibility) (*model.Skill, error) {
	skill := &model.Skill{
		Key:         key,
		Name:        name,
		Description: description,
		Visibility:  visibility,
	}

	if err := v.skills.CreateSkill(ctx, skill); err != nil {
		return nil, fmt.Errorf("create skill: %w", err)
	}

	return skill, nil
}

// GetSkill retrieves a skill from the catalog.
func (v *Vault) GetSkill(ctx context.Context, id model.UUID) (*model.Skill, error) {
	return v.skills.GetSkill(ctx, id)
}

// ListSkills lists all skills in the catalog.
func (v *Vault) ListSkills(ctx context.Context, cursor *model.UUID, limit int) ([]model.Skill, *model.UUID, error) {
	return v.skills.ListSkills(ctx, cursor, limit)
}

// UpdateSkill updates skill metadata.
func (v *Vault) UpdateSkill(ctx context.Context, skill *model.Skill) error {
	return v.skills.UpdateSkill(ctx, skill)
}

// UpdateVisibility changes skill visibility.
func (v *Vault) UpdateVisibility(ctx context.Context, id model.UUID, visibility model.SkillVisibility) error {
	return v.skills.UpdateVisibility(ctx, id, visibility)
}

// DeleteSkill removes a skill from the catalog.
func (v *Vault) DeleteSkill(ctx context.Context, id model.UUID) error {
	return v.skills.DeleteSkill(ctx, id)
}

// PublishVersion publishes a new version of a skill with artifact data.
func (v *Vault) PublishVersion(ctx context.Context, skillID model.UUID, version, manifest string, artifactReader io.Reader) (*model.SkillVersion, error) {
	// Create version directory
	versionDir := filepath.Join(v.baseDir, skillID.String(), version)
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		return nil, fmt.Errorf("create version dir: %w", err)
	}

	// Store artifact
	artifactPath := filepath.Join(versionDir, "artifact.tar.gz")
	f, err := os.Create(artifactPath)
	if err != nil {
		return nil, fmt.Errorf("create artifact file: %w", err)
	}
	defer f.Close()

	hasher := sha256.New()
	tee := io.TeeReader(artifactReader, hasher)

	if _, err := io.Copy(f, tee); err != nil {
		os.Remove(artifactPath)
		return nil, fmt.Errorf("write artifact: %w", err)
	}

	sha256Sum := hex.EncodeToString(hasher.Sum(nil))

	ver := &model.SkillVersion{
		SkillID:     skillID,
		Version:     version,
		Manifest:    json.RawMessage(manifest),
		ArtifactURI: artifactPath,
		SHA256:      sha256Sum,
	}

	if err := v.skills.CreateVersion(ctx, ver); err != nil {
		os.Remove(artifactPath)
		return nil, fmt.Errorf("create version: %w", err)
	}

	return ver, nil
}

// GetVersion retrieves a skill version.
func (v *Vault) GetVersion(ctx context.Context, id model.UUID) (*model.SkillVersion, error) {
	return v.skills.GetVersion(ctx, id)
}

// GetLatestVersion retrieves the latest version of a skill.
func (v *Vault) GetLatestVersion(ctx context.Context, skillID model.UUID) (*model.SkillVersion, error) {
	return v.skills.GetLatestVersion(ctx, skillID)
}

// GetVersionByTag retrieves a specific version of a skill by version string.
func (v *Vault) GetVersionByTag(ctx context.Context, skillID model.UUID, version string) (*model.SkillVersion, error) {
	return v.skills.GetVersionByTag(ctx, skillID, version)
}

// ListVersions lists all versions of a skill.
func (v *Vault) ListVersions(ctx context.Context, skillID model.UUID) ([]model.SkillVersion, error) {
	return v.skills.ListVersions(ctx, skillID)
}

// OpenArtifact opens the artifact file for a version for reading/dispatch.
func (v *Vault) OpenArtifact(version *model.SkillVersion) (io.ReadCloser, error) {
	return os.Open(version.ArtifactURI)
}

// --- Visibility Grants ---

// GrantVisibility grants skill visibility to a user or organization.
func (v *Vault) GrantVisibility(ctx context.Context, skillID model.UUID, principalType model.PrincipalType, principalID model.UUID) error {
	grant := &model.SkillGrant{
		SkillID:       skillID,
		PrincipalType: principalType,
		PrincipalID:   principalID,
	}
	return v.skills.CreateGrant(ctx, grant)
}

// RevokeVisibility revokes skill visibility from a user or organization.
func (v *Vault) RevokeVisibility(ctx context.Context, skillID model.UUID, principalType model.PrincipalType, principalID model.UUID) error {
	return v.skills.DeleteGrant(ctx, skillID, principalType, principalID)
}

// ListGrants lists all visibility grants for a skill.
func (v *Vault) ListGrants(ctx context.Context, skillID model.UUID) ([]model.SkillGrant, error) {
	return v.skills.ListGrants(ctx, skillID)
}

// IsVisibleToUser checks if a skill is visible to a user.
func (v *Vault) IsVisibleToUser(ctx context.Context, skillID, userID model.UUID, orgID *model.UUID) (bool, error) {
	return v.skills.IsSkillVisibleToUser(ctx, skillID, userID, orgID)
}

// ListVisibleSkills returns skills visible to a user.
func (v *Vault) ListVisibleSkills(ctx context.Context, userID model.UUID, orgID *model.UUID) ([]model.Skill, error) {
	return v.skills.ListVisibleSkills(ctx, userID, orgID)
}

// GetSkillWithLatestVersion combines skill metadata with its latest version.
func (v *Vault) GetSkillWithLatestVersion(ctx context.Context, skillID model.UUID) (*model.SkillWithLatestVersion, error) {
	skill, err := v.skills.GetSkill(ctx, skillID)
	if err != nil {
		return nil, err
	}
	if skill == nil {
		return nil, nil
	}

	ver, _ := v.skills.GetLatestVersion(ctx, skillID)

	return &model.SkillWithLatestVersion{
		Skill:         *skill,
		LatestVersion: ver,
	}, nil
}
