package store

import (
	"context"
	"time"

	"github.com/iagent/gateway/internal/model"
	"github.com/jackc/pgx/v5"
)

// TokenStore handles refresh token persistence.
type TokenStore struct {
	db *DB
}

func NewTokenStore(db *DB) *TokenStore {
	return &TokenStore{db: db}
}

func (s *TokenStore) Create(ctx context.Context, token *model.RefreshToken) error {
	token.ID = model.NewUUID()
	token.CreatedAt = time.Now().UTC()

	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO refresh_tokens (id, user_id, token_hash, family, expires_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		token.ID, token.UserID, token.TokenHash, token.Family, token.ExpiresAt, token.CreatedAt,
	)
	return err
}

func (s *TokenStore) GetByHash(ctx context.Context, tokenHash string) (*model.RefreshToken, error) {
	t := &model.RefreshToken{}
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, user_id, token_hash, family, expires_at, created_at, revoked_at
		 FROM refresh_tokens WHERE token_hash=$1`, tokenHash,
	).Scan(&t.ID, &t.UserID, &t.TokenHash, &t.Family, &t.ExpiresAt, &t.CreatedAt, &t.RevokedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return t, err
}

func (s *TokenStore) Revoke(ctx context.Context, id model.UUID) error {
	now := time.Now().UTC()
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at=$2 WHERE id=$1`, id, now,
	)
	return err
}

func (s *TokenStore) RevokeFamily(ctx context.Context, family string) error {
	now := time.Now().UTC()
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at=$2 WHERE family=$1 AND revoked_at IS NULL`, family, now,
	)
	return err
}

func (s *TokenStore) DeleteExpired(ctx context.Context) (int64, error) {
	result, err := s.db.Pool.Exec(ctx,
		`DELETE FROM refresh_tokens WHERE expires_at < $1`, time.Now().UTC(),
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func (s *TokenStore) RevokeAllForUser(ctx context.Context, userID model.UUID) error {
	now := time.Now().UTC()
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at=$2 WHERE user_id=$1 AND revoked_at IS NULL`, userID, now,
	)
	return err
}
