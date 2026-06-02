package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/oneClickAgent/gateway/internal/model"
	"github.com/jackc/pgx/v5"
)

// CredentialStore handles browser credential persistence.
type CredentialStore struct{ db *DB }

func NewCredentialStore(db *DB) *CredentialStore { return &CredentialStore{db: db} }

func (s *CredentialStore) Create(ctx context.Context, cred *model.BrowserCredential) error {
	cred.ID = model.NewUUID()
	now := time.Now().UTC()
	cred.CreatedAt = now
	cred.UpdatedAt = now

	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO browser_credentials (id, user_id, label, origin, storage_state_enc, nonce, auth_tag, key_id, sha256, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		cred.ID, cred.UserID, cred.Label, cred.Origin, cred.StorageStateEnc, cred.Nonce, cred.AuthTag,
		cred.KeyID, cred.SHA256, cred.CreatedAt, cred.UpdatedAt,
	)
	return err
}

func (s *CredentialStore) GetByID(ctx context.Context, id model.UUID) (*model.BrowserCredential, error) {
	c := &model.BrowserCredential{}
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, user_id, label, origin, storage_state_enc, nonce, auth_tag, key_id, sha256, last_used_at, created_at, updated_at
		 FROM browser_credentials WHERE id=$1`, id,
	).Scan(&c.ID, &c.UserID, &c.Label, &c.Origin, &c.StorageStateEnc, &c.Nonce, &c.AuthTag,
		&c.KeyID, &c.SHA256, &c.LastUsedAt, &c.CreatedAt, &c.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return c, err
}

func (s *CredentialStore) ListByUser(ctx context.Context, userID model.UUID) ([]model.BrowserCredential, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, user_id, label, origin, storage_state_enc, nonce, auth_tag, key_id, sha256, last_used_at, created_at, updated_at
		 FROM browser_credentials WHERE user_id=$1 ORDER BY created_at DESC`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var creds []model.BrowserCredential
	for rows.Next() {
		var c model.BrowserCredential
		if err := rows.Scan(&c.ID, &c.UserID, &c.Label, &c.Origin, &c.StorageStateEnc, &c.Nonce, &c.AuthTag,
			&c.KeyID, &c.SHA256, &c.LastUsedAt, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		creds = append(creds, c)
	}
	return creds, nil
}

func (s *CredentialStore) Update(ctx context.Context, cred *model.BrowserCredential) error {
	cred.UpdatedAt = time.Now().UTC()
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE browser_credentials SET label=$2, updated_at=$3 WHERE id=$1`,
		cred.ID, cred.Label, cred.UpdatedAt,
	)
	return err
}

func (s *CredentialStore) Touch(ctx context.Context, id model.UUID) error {
	now := time.Now().UTC()
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE browser_credentials SET last_used_at=$2 WHERE id=$1`, id, now,
	)
	return err
}

func (s *CredentialStore) Delete(ctx context.Context, id model.UUID) error {
	_, err := s.db.Pool.Exec(ctx, `DELETE FROM browser_credentials WHERE id=$1`, id)
	return err
}

func (s *CredentialStore) LinkToJob(ctx context.Context, jobID, credID model.UUID) error {
	tag, err := s.db.Pool.Exec(ctx,
		`INSERT INTO job_credentials (job_id, credential_id)
		 SELECT j.id, bc.id
		 FROM jobs j
		 JOIN browser_credentials bc ON bc.id = $2 AND bc.user_id = j.user_id
		 WHERE j.id = $1
		 ON CONFLICT DO NOTHING`,
		jobID, credID,
	)
	if err != nil {
		return fmt.Errorf("link credential: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errors.New("credential does not belong to the job owner")
	}
	return nil
}

func (s *CredentialStore) ListByJob(ctx context.Context, jobID model.UUID) ([]model.BrowserCredential, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT bc.id, bc.user_id, bc.label, bc.origin, bc.storage_state_enc, bc.nonce, bc.auth_tag, bc.key_id, bc.sha256, bc.last_used_at, bc.created_at, bc.updated_at
		 FROM browser_credentials bc JOIN job_credentials jc ON bc.id = jc.credential_id
		 WHERE jc.job_id=$1`, jobID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var creds []model.BrowserCredential
	for rows.Next() {
		var c model.BrowserCredential
		if err := rows.Scan(&c.ID, &c.UserID, &c.Label, &c.Origin, &c.StorageStateEnc, &c.Nonce, &c.AuthTag,
			&c.KeyID, &c.SHA256, &c.LastUsedAt, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		creds = append(creds, c)
	}
	return creds, nil
}
