package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/oneClickAgent/gateway/internal/model"
)

type AuditStore struct{ db *DB }

func NewAuditStore(db *DB) *AuditStore { return &AuditStore{db: db} }

func (s *AuditStore) Log(ctx context.Context, actorID model.UUID, action, targetType string, targetID *model.UUID, detail interface{}) error {
	entry := &model.AuditLog{
		ID:         model.NewUUID(),
		UserID:     &actorID,
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		CreatedAt:  time.Now().UTC(),
	}
	if detail != nil {
		b, _ := json.Marshal(detail)
		meta := string(b)
		entry.Meta = &meta
	}

	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO audit_log (id, user_id, action, target_type, target_id, meta, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		entry.ID, entry.UserID, entry.Action, entry.TargetType, entry.TargetID, entry.Meta, entry.CreatedAt,
	)
	return err
}

func (s *AuditStore) List(ctx context.Context, cursor *model.UUID, limit int) ([]model.AuditLog, *model.UUID, error) {
	if limit <= 0 {
		limit = 50
	}
	cols := `id, user_id, actor, action, target_type, target_id, meta, created_at`
	query := `SELECT ` + cols + ` FROM audit_log ORDER BY created_at DESC LIMIT $1`
	args := []interface{}{limit + 1}
	if cursor != nil {
		query = `SELECT ` + cols + ` FROM audit_log WHERE created_at < (SELECT created_at FROM audit_log WHERE id=$1) ORDER BY created_at DESC LIMIT $2`
		args = []interface{}{*cursor, limit + 1}
	}
	rows, err := s.db.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var entries []model.AuditLog
	for rows.Next() {
		var e model.AuditLog
		if err := rows.Scan(&e.ID, &e.UserID, &e.Actor, &e.Action, &e.TargetType, &e.TargetID, &e.Meta, &e.CreatedAt); err != nil {
			return nil, nil, err
		}
		entries = append(entries, e)
	}
	var nextCursor *model.UUID
	if len(entries) > limit {
		nextCursor = &entries[limit-1].ID
		entries = entries[:limit]
	}
	return entries, nextCursor, nil
}
