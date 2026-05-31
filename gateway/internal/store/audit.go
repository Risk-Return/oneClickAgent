package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/oneClickAgent/gateway/internal/model"
	"github.com/jackc/pgx/v5"
)

// AuditStore handles append-only audit logging.
type AuditStore struct {
	db *DB
}

func NewAuditStore(db *DB) *AuditStore {
	return &AuditStore{db: db}
}

// Log records an audit entry.
func (s *AuditStore) Log(ctx context.Context, actorID model.UUID, action, resourceType string, resourceID *model.UUID, detail interface{}) error {
	entry := &model.AuditLog{
		ID:           model.NewUUID(),
		ActorID:      actorID,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		CreatedAt:    time.Now().UTC(),
	}

	if detail != nil {
		b, err := json.Marshal(detail)
		if err != nil {
			return err
		}
		entry.Detail = string(b)
	}

	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO audit_log (id, actor_id, action, resource_type, resource_id, detail, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		entry.ID, entry.ActorID, entry.Action, entry.ResourceType, entry.ResourceID, entry.Detail, entry.CreatedAt,
	)
	return err
}

// List returns audit log entries with pagination.
func (s *AuditStore) List(ctx context.Context, cursor *model.UUID, limit int) ([]model.AuditLog, *model.UUID, error) {
	if limit <= 0 {
		limit = 50
	}

	var rows pgx.Rows
	var err error
	if cursor == nil {
		rows, err = s.db.Pool.Query(ctx,
			`SELECT id, actor_id, action, resource_type, resource_id, detail, created_at
			 FROM audit_log ORDER BY created_at DESC LIMIT $1`, limit+1,
		)
	} else {
		rows, err = s.db.Pool.Query(ctx,
			`SELECT id, actor_id, action, resource_type, resource_id, detail, created_at
			 FROM audit_log WHERE created_at < (SELECT created_at FROM audit_log WHERE id=$1)
			 ORDER BY created_at DESC LIMIT $2`, *cursor, limit+1,
		)
	}
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var entries []model.AuditLog
	for rows.Next() {
		var e model.AuditLog
		if err := rows.Scan(&e.ID, &e.ActorID, &e.Action, &e.ResourceType, &e.ResourceID, &e.Detail, &e.CreatedAt); err != nil {
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

// ListByActor returns audit entries for a specific actor.
func (s *AuditStore) ListByActor(ctx context.Context, actorID model.UUID, cursor *model.UUID, limit int) ([]model.AuditLog, *model.UUID, error) {
	if limit <= 0 {
		limit = 50
	}

	var rows pgx.Rows
	var err error
	if cursor == nil {
		rows, err = s.db.Pool.Query(ctx,
			`SELECT id, actor_id, action, resource_type, resource_id, detail, created_at
			 FROM audit_log WHERE actor_id=$1 ORDER BY created_at DESC LIMIT $2`, actorID, limit+1,
		)
	} else {
		rows, err = s.db.Pool.Query(ctx,
			`SELECT id, actor_id, action, resource_type, resource_id, detail, created_at
			 FROM audit_log WHERE actor_id=$1 AND created_at < (SELECT created_at FROM audit_log WHERE id=$2)
			 ORDER BY created_at DESC LIMIT $3`, actorID, *cursor, limit+1,
		)
	}
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var entries []model.AuditLog
	for rows.Next() {
		var e model.AuditLog
		if err := rows.Scan(&e.ID, &e.ActorID, &e.Action, &e.ResourceType, &e.ResourceID, &e.Detail, &e.CreatedAt); err != nil {
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
