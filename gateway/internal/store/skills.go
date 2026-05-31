package store

import (
	"context"
	"time"

	"github.com/oneClickAgent/gateway/internal/model"
	"github.com/jackc/pgx/v5"
)

type SkillStore struct{ db *DB }

func NewSkillStore(db *DB) *SkillStore { return &SkillStore{db: db} }

// --- Skill Catalog ---

func (s *SkillStore) CreateSkill(ctx context.Context, sk *model.Skill) error {
	sk.ID = model.NewUUID()
	now := time.Now().UTC()
	sk.CreatedAt = now
	sk.UpdatedAt = now
	if sk.Status == "" {
		sk.Status = model.SkillActive
	}

	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO skills (id, key, name, description, visibility, latest_version, status, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		sk.ID, sk.Key, sk.Name, sk.Description, sk.Visibility, sk.LatestVersion, sk.Status, sk.CreatedAt, sk.UpdatedAt,
	)
	return err
}

func (s *SkillStore) GetSkill(ctx context.Context, id model.UUID) (*model.Skill, error) {
	sk := &model.Skill{}
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, key, name, description, visibility, latest_version, status, created_at, updated_at FROM skills WHERE id=$1`, id,
	).Scan(&sk.ID, &sk.Key, &sk.Name, &sk.Description, &sk.Visibility, &sk.LatestVersion, &sk.Status, &sk.CreatedAt, &sk.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return sk, err
}

func (s *SkillStore) ListSkills(ctx context.Context, cursor *model.UUID, limit int) ([]model.Skill, *model.UUID, error) {
	if limit <= 0 {
		limit = 50
	}
	cols := `id, key, name, description, visibility, latest_version, status, created_at, updated_at`
	var rows pgx.Rows
	var err error
	if cursor == nil {
		rows, err = s.db.Pool.Query(ctx,
			`SELECT `+cols+` FROM skills ORDER BY created_at DESC LIMIT $1`, limit+1,
		)
	} else {
		rows, err = s.db.Pool.Query(ctx,
			`SELECT `+cols+` FROM skills WHERE created_at < (SELECT created_at FROM skills WHERE id=$1) ORDER BY created_at DESC LIMIT $2`,
			*cursor, limit+1,
		)
	}
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var skills []model.Skill
	for rows.Next() {
		var sk model.Skill
		if err := rows.Scan(&sk.ID, &sk.Key, &sk.Name, &sk.Description, &sk.Visibility, &sk.LatestVersion, &sk.Status, &sk.CreatedAt, &sk.UpdatedAt); err != nil {
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

func (s *SkillStore) UpdateSkill(ctx context.Context, sk *model.Skill) error {
	sk.UpdatedAt = time.Now().UTC()
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE skills SET key=$2, name=$3, description=$4, visibility=$5, latest_version=$6, updated_at=$7 WHERE id=$1`,
		sk.ID, sk.Key, sk.Name, sk.Description, sk.Visibility, sk.LatestVersion, sk.UpdatedAt,
	)
	return err
}

func (s *SkillStore) UpdateVisibility(ctx context.Context, id model.UUID, vis model.SkillVisibility) error {
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE skills SET visibility=$2, updated_at=$3 WHERE id=$1`,
		id, vis, time.Now().UTC(),
	)
	return err
}

func (s *SkillStore) DeleteSkill(ctx context.Context, id model.UUID) error {
	_, err := s.db.Pool.Exec(ctx, `DELETE FROM skills WHERE id=$1`, id)
	return err
}

// --- Skill Versions ---

func (s *SkillStore) CreateVersion(ctx context.Context, v *model.SkillVersion) error {
	v.ID = model.NewUUID()
	v.CreatedAt = time.Now().UTC()
	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO skill_versions (id, skill_id, version, manifest, artifact_uri, sha256, size, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		v.ID, v.SkillID, v.Version, v.Manifest, v.ArtifactURI, v.SHA256, v.Size, v.CreatedAt,
	)
	return err
}

func (s *SkillStore) GetVersion(ctx context.Context, id model.UUID) (*model.SkillVersion, error) {
	v := &model.SkillVersion{}
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, skill_id, version, manifest, artifact_uri, sha256, size, created_at FROM skill_versions WHERE id=$1`, id,
	).Scan(&v.ID, &v.SkillID, &v.Version, &v.Manifest, &v.ArtifactURI, &v.SHA256, &v.Size, &v.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return v, err
}

func (s *SkillStore) GetLatestVersion(ctx context.Context, skillID model.UUID) (*model.SkillVersion, error) {
	v := &model.SkillVersion{}
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, skill_id, version, manifest, artifact_uri, sha256, size, created_at FROM skill_versions WHERE skill_id=$1 ORDER BY created_at DESC LIMIT 1`, skillID,
	).Scan(&v.ID, &v.SkillID, &v.Version, &v.Manifest, &v.ArtifactURI, &v.SHA256, &v.Size, &v.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return v, err
}

func (s *SkillStore) GetVersionByTag(ctx context.Context, skillID model.UUID, version string) (*model.SkillVersion, error) {
	v := &model.SkillVersion{}
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, skill_id, version, manifest, artifact_uri, sha256, size, created_at FROM skill_versions WHERE skill_id=$1 AND version=$2`, skillID, version,
	).Scan(&v.ID, &v.SkillID, &v.Version, &v.Manifest, &v.ArtifactURI, &v.SHA256, &v.Size, &v.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return v, err
}

func (s *SkillStore) ListVersions(ctx context.Context, skillID model.UUID) ([]model.SkillVersion, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, skill_id, version, manifest, artifact_uri, sha256, size, created_at FROM skill_versions WHERE skill_id=$1 ORDER BY created_at DESC`, skillID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var vers []model.SkillVersion
	for rows.Next() {
		var v model.SkillVersion
		if err := rows.Scan(&v.ID, &v.SkillID, &v.Version, &v.Manifest, &v.ArtifactURI, &v.SHA256, &v.Size, &v.CreatedAt); err != nil {
			return nil, err
		}
		vers = append(vers, v)
	}
	return vers, nil
}

// --- Device Skills ---

func (s *SkillStore) SetDeviceSkill(ctx context.Context, ds *model.DeviceSkill) error {
	ds.UpdatedAt = time.Now().UTC()
	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO device_skills (device_id, skill_id, version, status, installed_by, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6)
		 ON CONFLICT (device_id, skill_id) DO UPDATE
		 SET version=$3, status=$4, installed_by=$5, updated_at=$6`,
		ds.DeviceID, ds.SkillID, ds.Version, ds.Status, ds.InstalledBy, ds.UpdatedAt,
	)
	return err
}

func (s *SkillStore) UpdateDeviceSkillStatus(ctx context.Context, deviceID, skillID model.UUID, status model.SkillInstallStatus) error {
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE device_skills SET status=$3, updated_at=$4 WHERE device_id=$1 AND skill_id=$2`,
		deviceID, skillID, status, time.Now().UTC(),
	)
	return err
}

func (s *SkillStore) GetDeviceSkills(ctx context.Context, deviceID model.UUID) ([]model.DeviceSkill, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT device_id, skill_id, version, status, installed_by, error_message, updated_at FROM device_skills WHERE device_id=$1`, deviceID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var dss []model.DeviceSkill
	for rows.Next() {
		var ds model.DeviceSkill
		if err := rows.Scan(&ds.DeviceID, &ds.SkillID, &ds.Version, &ds.Status, &ds.InstalledBy, &ds.ErrorMessage, &ds.UpdatedAt); err != nil {
			return nil, err
		}
		dss = append(dss, ds)
	}
	return dss, nil
}

func (s *SkillStore) DeleteDeviceSkill(ctx context.Context, deviceID, skillID model.UUID) error {
	_, err := s.db.Pool.Exec(ctx, `DELETE FROM device_skills WHERE device_id=$1 AND skill_id=$2`, deviceID, skillID)
	return err
}

func (s *SkillStore) IsSkillInstalledOnDevice(ctx context.Context, deviceID, skillID model.UUID) (bool, error) {
	var count int
	err := s.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM device_skills WHERE device_id=$1 AND skill_id=$2 AND status='installed'`,
		deviceID, skillID,
	).Scan(&count)
	return count > 0, err
}

// --- Agent Skills ---

func (s *SkillStore) SetAgentSkill(ctx context.Context, as *model.AgentSkill) error {
	as.UpdatedAt = time.Now().UTC()
	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO agent_skills (agent_id, skill_id, status, selected_by, updated_at)
		 VALUES ($1,$2,$3,$4,$5)
		 ON CONFLICT (agent_id, skill_id) DO UPDATE
		 SET status=$3, selected_by=$4, updated_at=$5`,
		as.AgentID, as.SkillID, as.Status, as.SelectedBy, as.UpdatedAt,
	)
	return err
}

func (s *SkillStore) GetAgentSkills(ctx context.Context, agentID model.UUID) ([]model.AgentSkill, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT agent_id, skill_id, status, selected_by, updated_at FROM agent_skills WHERE agent_id=$1`, agentID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ass []model.AgentSkill
	for rows.Next() {
		var as model.AgentSkill
		if err := rows.Scan(&as.AgentID, &as.SkillID, &as.Status, &as.SelectedBy, &as.UpdatedAt); err != nil {
			return nil, err
		}
		ass = append(ass, as)
	}
	return ass, nil
}

func (s *SkillStore) DeleteAgentSkill(ctx context.Context, agentID, skillID model.UUID) error {
	_, err := s.db.Pool.Exec(ctx, `DELETE FROM agent_skills WHERE agent_id=$1 AND skill_id=$2`, agentID, skillID)
	return err
}

// --- Skill Grants ---

func (s *SkillStore) CreateGrant(ctx context.Context, g *model.SkillGrant) error {
	g.CreatedAt = time.Now().UTC()
	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO skill_grants (skill_id, principal_type, principal_id, granted_by, created_at)
		 VALUES ($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`,
		g.SkillID, g.PrincipalType, g.PrincipalID, g.GrantedBy, g.CreatedAt,
	)
	return err
}

func (s *SkillStore) DeleteGrant(ctx context.Context, skillID model.UUID, pt model.PrincipalType, pid model.UUID) error {
	_, err := s.db.Pool.Exec(ctx,
		`DELETE FROM skill_grants WHERE skill_id=$1 AND principal_type=$2 AND principal_id=$3`,
		skillID, pt, pid,
	)
	return err
}

func (s *SkillStore) ListGrants(ctx context.Context, skillID model.UUID) ([]model.SkillGrant, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT skill_id, principal_type, principal_id, granted_by, created_at FROM skill_grants WHERE skill_id=$1`, skillID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var grants []model.SkillGrant
	for rows.Next() {
		var g model.SkillGrant
		if err := rows.Scan(&g.SkillID, &g.PrincipalType, &g.PrincipalID, &g.GrantedBy, &g.CreatedAt); err != nil {
			return nil, err
		}
		grants = append(grants, g)
	}
	return grants, nil
}

func (s *SkillStore) IsSkillVisibleToUser(ctx context.Context, skillID, userID model.UUID, orgID *model.UUID) (bool, error) {
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

func (s *SkillStore) ListVisibleSkills(ctx context.Context, userID model.UUID, orgID *model.UUID) ([]model.Skill, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT DISTINCT s.id, s.key, s.name, s.description, s.visibility, s.latest_version, s.status, s.created_at, s.updated_at
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
		if err := rows.Scan(&sk.ID, &sk.Key, &sk.Name, &sk.Description, &sk.Visibility, &sk.LatestVersion, &sk.Status, &sk.CreatedAt, &sk.UpdatedAt); err != nil {
			return nil, err
		}
		skills = append(skills, sk)
	}
	return skills, nil
}
