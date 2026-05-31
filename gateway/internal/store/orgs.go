package store

import (
	"context"
	"time"

	"github.com/oneClickAgent/gateway/internal/model"
	"github.com/jackc/pgx/v5"
)

type OrgStore struct{ db *DB }

func NewOrgStore(db *DB) *OrgStore { return &OrgStore{db: db} }

func (s *OrgStore) Create(ctx context.Context, org *model.Organization) error {
	org.ID = model.NewUUID()
	now := time.Now().UTC()
	org.CreatedAt = now
	org.UpdatedAt = now

	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO organizations (id, name, description, created_by, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6)`,
		org.ID, org.Name, org.Description, org.CreatedBy, org.CreatedAt, org.UpdatedAt,
	)
	return err
}

func (s *OrgStore) GetByID(ctx context.Context, id model.UUID) (*model.Organization, error) {
	org := &model.Organization{}
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, name, description, created_by, created_at, updated_at FROM organizations WHERE id=$1`, id,
	).Scan(&org.ID, &org.Name, &org.Description, &org.CreatedBy, &org.CreatedAt, &org.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return org, err
}

func (s *OrgStore) List(ctx context.Context, cursor *model.UUID, limit int) ([]model.Organization, *model.UUID, error) {
	if limit <= 0 {
		limit = 50
	}
	cols := `id, name, description, created_by, created_at, updated_at`
	var rows pgx.Rows
	var err error
	if cursor == nil {
		rows, err = s.db.Pool.Query(ctx, `SELECT `+cols+` FROM organizations ORDER BY created_at DESC LIMIT $1`, limit+1)
	} else {
		rows, err = s.db.Pool.Query(ctx, `SELECT `+cols+` FROM organizations WHERE created_at < (SELECT created_at FROM organizations WHERE id=$1) ORDER BY created_at DESC LIMIT $2`, *cursor, limit+1)
	}
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var orgs []model.Organization
	for rows.Next() {
		var org model.Organization
		if err := rows.Scan(&org.ID, &org.Name, &org.Description, &org.CreatedBy, &org.CreatedAt, &org.UpdatedAt); err != nil {
			return nil, nil, err
		}
		orgs = append(orgs, org)
	}
	var nextCursor *model.UUID
	if len(orgs) > limit {
		nextCursor = &orgs[limit-1].ID
		orgs = orgs[:limit]
	}
	return orgs, nextCursor, nil
}

func (s *OrgStore) Update(ctx context.Context, org *model.Organization) error {
	org.UpdatedAt = time.Now().UTC()
	_, err := s.db.Pool.Exec(ctx, `UPDATE organizations SET name=$2, description=$3, updated_at=$4 WHERE id=$1`, org.ID, org.Name, org.Description, org.UpdatedAt)
	return err
}

func (s *OrgStore) Delete(ctx context.Context, id model.UUID) error {
	_, err := s.db.Pool.Exec(ctx, `DELETE FROM organizations WHERE id=$1`, id)
	return err
}

func (s *OrgStore) AddMember(ctx context.Context, orgID, userID model.UUID) error {
	_, err := s.db.Pool.Exec(ctx, `UPDATE users SET org_id=$2, updated_at=$3 WHERE id=$1`, userID, orgID, time.Now().UTC())
	return err
}

func (s *OrgStore) RemoveMember(ctx context.Context, userID model.UUID) error {
	_, err := s.db.Pool.Exec(ctx, `UPDATE users SET org_id=NULL, updated_at=$2 WHERE id=$1`, userID, time.Now().UTC())
	return err
}

func (s *OrgStore) ListMembers(ctx context.Context, orgID model.UUID) ([]model.User, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, email, username, password_hash, status, role, tier, org_id, created_at, updated_at FROM users WHERE org_id=$1 ORDER BY created_at`, orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []model.User
	for rows.Next() {
		var u model.User
		if err := rows.Scan(&u.ID, &u.Email, &u.Username, &u.PasswordHash, &u.Status, &u.Role, &u.Tier, &u.OrgID, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, nil
}
