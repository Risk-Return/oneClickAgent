package store

import (
	"context"
	"time"

	"github.com/oneClickAgent/gateway/internal/model"
	"github.com/jackc/pgx/v5"
)

// UserStore handles user persistence.
type UserStore struct {
	db *DB
}

func NewUserStore(db *DB) *UserStore {
	return &UserStore{db: db}
}

func (s *UserStore) Create(ctx context.Context, user *model.User) error {
	user.ID = model.NewUUID()
	now := time.Now().UTC()
	user.CreatedAt = now
	user.UpdatedAt = now

	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO users (id, email, password_hash, name, role, tier, org_id, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		user.ID, user.Email, user.PasswordHash, user.Name, user.Role, user.Tier, user.OrgID, user.CreatedAt, user.UpdatedAt,
	)
	return err
}

func (s *UserStore) GetByID(ctx context.Context, id model.UUID) (*model.User, error) {
	user := &model.User{}
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, email, password_hash, name, role, tier, org_id, created_at, updated_at
		 FROM users WHERE id = $1`, id,
	).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Name, &user.Role, &user.Tier, &user.OrgID, &user.CreatedAt, &user.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return user, err
}

func (s *UserStore) GetByEmail(ctx context.Context, email string) (*model.User, error) {
	user := &model.User{}
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, email, password_hash, name, role, tier, org_id, created_at, updated_at
		 FROM users WHERE email = $1`, email,
	).Scan(&user.ID, &user.Email, &user.PasswordHash, &user.Name, &user.Role, &user.Tier, &user.OrgID, &user.CreatedAt, &user.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return user, err
}

func (s *UserStore) Update(ctx context.Context, user *model.User) error {
	user.UpdatedAt = time.Now().UTC()
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE users SET email=$2, name=$3, role=$4, tier=$5, org_id=$6, updated_at=$7 WHERE id=$1`,
		user.ID, user.Email, user.Name, user.Role, user.Tier, user.OrgID, user.UpdatedAt,
	)
	return err
}

func (s *UserStore) UpdateTier(ctx context.Context, userID model.UUID, tier model.UserTier) error {
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE users SET tier=$2, updated_at=$3 WHERE id=$1`,
		userID, tier, time.Now().UTC(),
	)
	return err
}

func (s *UserStore) List(ctx context.Context, cursor *model.UUID, limit int) ([]model.User, *model.UUID, error) {
	var rows pgx.Rows
	var err error
	if limit <= 0 {
		limit = 50
	}

	if cursor == nil {
		rows, err = s.db.Pool.Query(ctx,
			`SELECT id, email, password_hash, name, role, tier, org_id, created_at, updated_at
			 FROM users ORDER BY created_at DESC LIMIT $1`, limit+1,
		)
	} else {
		rows, err = s.db.Pool.Query(ctx,
			`SELECT id, email, password_hash, name, role, tier, org_id, created_at, updated_at
			 FROM users WHERE created_at < (SELECT created_at FROM users WHERE id=$1)
			 ORDER BY created_at DESC LIMIT $2`, *cursor, limit+1,
		)
	}
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var users []model.User
	for rows.Next() {
		var u model.User
		if err := rows.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.Role, &u.Tier, &u.OrgID, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, nil, err
		}
		users = append(users, u)
	}

	var nextCursor *model.UUID
	if len(users) > limit {
		nextCursor = &users[limit-1].ID
		users = users[:limit]
	}

	return users, nextCursor, nil
}
