package store

import (
	"context"
	"time"

	"github.com/oneClickAgent/gateway/internal/model"
	"github.com/jackc/pgx/v5"
)

// OrgStore handles organization persistence.
type OrgStore struct {
	db *DB
}

func NewOrgStore(db *DB) *OrgStore {
	return &OrgStore{db: db}
}

func (s *OrgStore) Create(ctx context.Context, org *model.Organization) error {
	org.ID = model.NewUUID()
	org.CreatedAt = time.Now().UTC()

	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO organizations (id, name, created_at) VALUES ($1, $2, $3)`,
		org.ID, org.Name, org.CreatedAt,
	)
	return err
}

func (s *OrgStore) GetByID(ctx context.Context, id model.UUID) (*model.Organization, error) {
	org := &model.Organization{}
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, name, created_at FROM organizations WHERE id=$1`, id,
	).Scan(&org.ID, &org.Name, &org.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return org, err
}

func (s *OrgStore) List(ctx context.Context, cursor *model.UUID, limit int) ([]model.Organization, *model.UUID, error) {
	if limit <= 0 {
		limit = 50
	}

	var rows pgx.Rows
	var err error
	if cursor == nil {
		rows, err = s.db.Pool.Query(ctx,
			`SELECT id, name, created_at FROM organizations ORDER BY created_at DESC LIMIT $1`, limit+1,
		)
	} else {
		rows, err = s.db.Pool.Query(ctx,
			`SELECT id, name, created_at FROM organizations WHERE created_at < (SELECT created_at FROM organizations WHERE id=$1)
			 ORDER BY created_at DESC LIMIT $2`, *cursor, limit+1,
		)
	}
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var orgs []model.Organization
	for rows.Next() {
		var org model.Organization
		if err := rows.Scan(&org.ID, &org.Name, &org.CreatedAt); err != nil {
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
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE organizations SET name=$2 WHERE id=$1`, org.ID, org.Name,
	)
	return err
}

func (s *OrgStore) Delete(ctx context.Context, id model.UUID) error {
	_, err := s.db.Pool.Exec(ctx, `DELETE FROM organizations WHERE id=$1`, id)
	return err
}

// AddMember adds a user to an organization.
func (s *OrgStore) AddMember(ctx context.Context, orgID, userID model.UUID) error {
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE users SET org_id=$2 WHERE id=$1`, userID, orgID,
	)
	return err
}

// RemoveMember removes a user from an organization.
func (s *OrgStore) RemoveMember(ctx context.Context, userID model.UUID) error {
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE users SET org_id=NULL WHERE id=$1`, userID,
	)
	return err
}

// ListMembers returns all users in an organization.
func (s *OrgStore) ListMembers(ctx context.Context, orgID model.UUID) ([]model.User, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, email, password_hash, name, role, tier, org_id, created_at, updated_at
		 FROM users WHERE org_id=$1 ORDER BY created_at`, orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []model.User
	for rows.Next() {
		var u model.User
		if err := rows.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.Role, &u.Tier, &u.OrgID, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, nil
}
