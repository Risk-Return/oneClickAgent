package store

import (
	"context"
	"time"

	"github.com/oneClickAgent/gateway/internal/model"
	"github.com/jackc/pgx/v5"
)

// SkillStore handles skill catalog, device skills, agent skills, and grants.
type SkillStore struct {
	db *DB
}

func NewSkillStore(db *DB) *SkillStore {
	return &SkillStore{db: db}
}

// --- Skill Catalog ---

func (s *SkillStore) CreateSkill(ctx context.Context, skill *model.Skill) error {
	skill.ID = model.NewUUID()
	now := time.Now().UTC()
	skill.CreatedAt = now
	skill.UpdatedAt = now

	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO skills (id, name, description, visibility, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		skill.ID, skill.Name, skill.Description, skill.Visibility, skill.CreatedAt, skill.UpdatedAt,
	)
	return err
}

func (s *SkillStore) GetSkill(ctx context.Context, id model.UUID) (*model.Skill, error) {
	sk := &model.Skill{}
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, name, description, visibility, created_at, updated_at FROM skills WHERE id=$1`, id,
	).Scan(&sk.ID, &sk.Name, &sk.Description, &sk.Visibility, &sk.CreatedAt, &sk.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return sk, err
}

func (s *SkillStore) ListSkills(ctx context.Context, cursor *model.UUID, limit int) ([]model.Skill, *model.UUID, error) {
	if limit <= 0 {
		limit = 50
	}

	var rows pgx.Rows
	var err error
	if cursor == nil {
		rows, err = s.db.Pool.Query(ctx,
			`SELECT id, name, description, visibility, created_at, updated_at
			 FROM skills ORDER BY created_at DESC LIMIT $1`, limit+1,
		)
	} else {
		rows, err = s.db.Pool.Query(ctx,
			`SELECT id, name, description, visibility, created_at, updated_at
			 FROM skills WHERE created_at < (SELECT created_at FROM skills WHERE id=$1)
			 ORDER BY created_at DESC LIMIT $2`, *cursor, limit+1,
		)
	}
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var skills []model.Skill
	for rows.Next() {
		var sk model.Skill
		if err := rows.Scan(&sk.ID, &sk.Name, &sk.Description, &sk.Visibility, &sk.CreatedAt, &sk.UpdatedAt); err != nil {
			return nil, nil, err
		}
		skills = append(skills, sk)
	}

	var nextCursor *model.UUID
	if len(skills) > limit {
		nextCursor = &skills[limit-1].ID
		skills = skills[:limit]
	}

	return skills, nextCursor, nil
}

func (s *SkillStore) UpdateSkill(ctx context.Context, skill *model.Skill) error {
	skill.UpdatedAt = time.Now().UTC()
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE skills SET name=$2, description=$3, visibility=$4, updated_at=$5 WHERE id=$1`,
		skill.ID, skill.Name, skill.Description, skill.Visibility, skill.UpdatedAt,
	)
	return err
}

func (s *SkillStore) UpdateVisibility(ctx context.Context, id model.UUID, visibility model.SkillVisibility) error {
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE skills SET visibility=$2, updated_at=$3 WHERE id=$1`,
		id, visibility, time.Now().UTC(),
	)
	return err
}

func (s *SkillStore) DeleteSkill(ctx context.Context, id model.UUID) error {
	_, err := s.db.Pool.Exec(ctx, `DELETE FROM skills WHERE id=$1`, id)
	return err
}

// --- Skill Versions ---

func (s *SkillStore) CreateVersion(ctx context.Context, ver *model.SkillVersion) error {
	ver.ID = model.NewUUID()
	ver.CreatedAt = time.Now().UTC()

	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO skill_versions (id, skill_id, version, manifest, artifact_path, sha256, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		ver.ID, ver.SkillID, ver.Version, ver.Manifest, ver.ArtifactPath, ver.SHA256, ver.CreatedAt,
	)
	return err
}

func (s *SkillStore) GetVersion(ctx context.Context, id model.UUID) (*model.SkillVersion, error) {
	v := &model.SkillVersion{}
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, skill_id, version, manifest, artifact_path, sha256, created_at
		 FROM skill_versions WHERE id=$1`, id,
	).Scan(&v.ID, &v.SkillID, &v.Version, &v.Manifest, &v.ArtifactPath, &v.SHA256, &v.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return v, err
}

func (s *SkillStore) GetLatestVersion(ctx context.Context, skillID model.UUID) (*model.SkillVersion, error) {
	v := &model.SkillVersion{}
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, skill_id, version, manifest, artifact_path, sha256, created_at
		 FROM skill_versions WHERE skill_id=$1 ORDER BY created_at DESC LIMIT 1`, skillID,
	).Scan(&v.ID, &v.SkillID, &v.Version, &v.Manifest, &v.ArtifactPath, &v.SHA256, &v.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return v, err
}

func (s *SkillStore) GetVersionByTag(ctx context.Context, skillID model.UUID, version string) (*model.SkillVersion, error) {
	v := &model.SkillVersion{}
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, skill_id, version, manifest, artifact_path, sha256, created_at
		 FROM skill_versions WHERE skill_id=$1 AND version=$2`, skillID, version,
	).Scan(&v.ID, &v.SkillID, &v.Version, &v.Manifest, &v.ArtifactPath, &v.SHA256, &v.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return v, err
}

func (s *SkillStore) ListVersions(ctx context.Context, skillID model.UUID) ([]model.SkillVersion, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, skill_id, version, manifest, artifact_path, sha256, created_at
		 FROM skill_versions WHERE skill_id=$1 ORDER BY created_at DESC`, skillID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var versions []model.SkillVersion
	for rows.Next() {
		var v model.SkillVersion
		if err := rows.Scan(&v.ID, &v.SkillID, &v.Version, &v.Manifest, &v.ArtifactPath, &v.SHA256, &v.CreatedAt); err != nil {
			return nil, err
		}
		versions = append(versions, v)
	}
	return versions, nil
}

// --- Device Skills (fleet install state) ---

func (s *SkillStore) SetDeviceSkill(ctx context.Context, ds *model.DeviceSkill) error {
	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO device_skills (device_id, skill_id, skill_version_id, status, installed_at)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (device_id, skill_id) DO UPDATE
		 SET skill_version_id=$3, status=$4, installed_at=$5`,
		ds.DeviceID, ds.SkillID, ds.SkillVersionID, ds.Status, ds.InstalledAt,
	)
	return err
}

func (s *SkillStore) UpdateDeviceSkillStatus(ctx context.Context, deviceID, skillID model.UUID, status model.SkillStatus) error {
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE device_skills SET status=$3 WHERE device_id=$1 AND skill_id=$2`,
		deviceID, skillID, status,
	)
	return err
}

func (s *SkillStore) GetDeviceSkills(ctx context.Context, deviceID model.UUID) ([]model.DeviceSkill, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT device_id, skill_id, skill_version_id, status, installed_at
		 FROM device_skills WHERE device_id=$1`, deviceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dss []model.DeviceSkill
	for rows.Next() {
		var ds model.DeviceSkill
		if err := rows.Scan(&ds.DeviceID, &ds.SkillID, &ds.SkillVersionID, &ds.Status, &ds.InstalledAt); err != nil {
			return nil, err
		}
		dss = append(dss, ds)
	}
	return dss, nil
}

func (s *SkillStore) DeleteDeviceSkill(ctx context.Context, deviceID, skillID model.UUID) error {
	_, err := s.db.Pool.Exec(ctx,
		`DELETE FROM device_skills WHERE device_id=$1 AND skill_id=$2`, deviceID, skillID,
	)
	return err
}

// --- Agent Skills (customer enable/disable) ---

func (s *SkillStore) SetAgentSkill(ctx context.Context, as *model.AgentSkill) error {
	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO agent_skills (agent_id, skill_id, skill_version_id, status, enabled_at)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (agent_id, skill_id) DO UPDATE
		 SET skill_version_id=$3, status=$4, enabled_at=$5`,
		as.AgentID, as.SkillID, as.SkillVersionID, as.Status, as.EnabledAt,
	)
	return err
}

func (s *SkillStore) GetAgentSkills(ctx context.Context, agentID model.UUID) ([]model.AgentSkill, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT agent_id, skill_id, skill_version_id, status, enabled_at
		 FROM agent_skills WHERE agent_id=$1`, agentID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ass []model.AgentSkill
	for rows.Next() {
		var as model.AgentSkill
		if err := rows.Scan(&as.AgentID, &as.SkillID, &as.SkillVersionID, &as.Status, &as.EnabledAt); err != nil {
			return nil, err
		}
		ass = append(ass, as)
	}
	return ass, nil
}

func (s *SkillStore) DeleteAgentSkill(ctx context.Context, agentID, skillID model.UUID) error {
	_, err := s.db.Pool.Exec(ctx,
		`DELETE FROM agent_skills WHERE agent_id=$1 AND skill_id=$2`, agentID, skillID,
	)
	return err
}

// IsSkillInstalledOnDevice checks if a skill is installed on a device.
func (s *SkillStore) IsSkillInstalledOnDevice(ctx context.Context, deviceID, skillID model.UUID) (bool, error) {
	var count int
	err := s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM device_skills WHERE device_id=$1 AND skill_id=$2 AND status=$3`,
		deviceID, skillID, model.SkillInstalled,
	).Scan(&count)
	return count > 0, err
}

// --- Skill Grants (visibility) ---

func (s *SkillStore) CreateGrant(ctx context.Context, grant *model.SkillGrant) error {
	grant.GrantedAt = time.Now().UTC()
	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO skill_grants (skill_id, principal_type, principal_id, granted_at)
		 VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`,
		grant.SkillID, grant.PrincipalType, grant.PrincipalID, grant.GrantedAt,
	)
	return err
}

func (s *SkillStore) DeleteGrant(ctx context.Context, skillID model.UUID, principalType model.PrincipalType, principalID model.UUID) error {
	_, err := s.db.Pool.Exec(ctx,
		`DELETE FROM skill_grants WHERE skill_id=$1 AND principal_type=$2 AND principal_id=$3`,
		skillID, principalType, principalID,
	)
	return err
}

func (s *SkillStore) ListGrants(ctx context.Context, skillID model.UUID) ([]model.SkillGrant, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT skill_id, principal_type, principal_id, granted_at
		 FROM skill_grants WHERE skill_id=$1`, skillID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var grants []model.SkillGrant
	for rows.Next() {
		var g model.SkillGrant
		if err := rows.Scan(&g.SkillID, &g.PrincipalType, &g.PrincipalID, &g.GrantedAt); err != nil {
			return nil, err
		}
		grants = append(grants, g)
	}
	return grants, nil
}

// IsSkillVisibleToUser checks visibility: public OR direct user grant OR org grant.
func (s *SkillStore) IsSkillVisibleToUser(ctx context.Context, skillID model.UUID, userID model.UUID, orgID *model.UUID) (bool, error) {
	var count int
	err := s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM skills s
		 WHERE s.id=$1 AND (
		   s.visibility='public'
		   OR EXISTS (SELECT 1 FROM skill_grants sg WHERE sg.skill_id=s.id AND sg.principal_type='user' AND sg.principal_id=$2)
		   OR ($3 IS NOT NULL AND EXISTS (SELECT 1 FROM skill_grants sg WHERE sg.skill_id=s.id AND sg.principal_type='org' AND sg.principal_id=$3))
		 )`, skillID, userID, orgID,
	).Scan(&count)
	return count > 0, err
}

// ListVisibleSkills returns skills visible to a user (public + grants).
func (s *SkillStore) ListVisibleSkills(ctx context.Context, userID model.UUID, orgID *model.UUID) ([]model.Skill, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT DISTINCT s.id, s.name, s.description, s.visibility, s.created_at, s.updated_at
		 FROM skills s
		 WHERE s.visibility='public'
		   OR EXISTS (SELECT 1 FROM skill_grants sg WHERE sg.skill_id=s.id AND sg.principal_type='user' AND sg.principal_id=$1)
		   OR ($2 IS NOT NULL AND EXISTS (SELECT 1 FROM skill_grants sg WHERE sg.skill_id=s.id AND sg.principal_type='org' AND sg.principal_id=$2))
		 ORDER BY s.created_at DESC`, userID, orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var skills []model.Skill
	for rows.Next() {
		var sk model.Skill
		if err := rows.Scan(&sk.ID, &sk.Name, &sk.Description, &sk.Visibility, &sk.CreatedAt, &sk.UpdatedAt); err != nil {
			return nil, err
		}
		skills = append(skills, sk)
	}
	return skills, nil
}
