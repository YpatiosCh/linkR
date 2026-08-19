-- name: CreateAuditEvent :one
INSERT INTO audit_events (id, user_id, event_type, ip_address, user_agent, metadata)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;
