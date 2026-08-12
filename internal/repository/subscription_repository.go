package repository

import (
	"context"
	"errors"
	"fmt"
	db "linkMe/internal/db/generated"
	"linkMe/internal/models"
	"linkMe/internal/msgs"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// subscriptionRepository implements SubscriptionRepository on top of
// sqlc-generated queries. It holds the base *db.Queries used outside a
// transaction.
type subscriptionRepository struct {
	queries *db.Queries
}

// NewSubscriptionRepository creates a subscriptionRepository bound to
// dbtx, which is either the connection pool or an active transaction.
func NewSubscriptionRepository(dbtx db.DBTX) *subscriptionRepository {
	return &subscriptionRepository{queries: db.New(dbtx)}
}

// querier returns db.Queries bound to the active transaction when ctx
// carries one (see injectTx), otherwise it falls back to the repository's
// base queries so calls participate in an ongoing transaction.
func (r *subscriptionRepository) querier(ctx context.Context) *db.Queries {
	if tx, ok := extractTx(ctx); ok {
		return db.New(tx)
	}
	return r.queries
}

// CreateUserSubscription inserts a new user subscription via the
// CreateUserSubscription query and maps the returned row to a domain
// models.Subscription. Any database error is wrapped with context and
// returned.
func (r *subscriptionRepository) CreateUserSubscription(ctx context.Context, sub models.Subscription) (models.Subscription, error) {
	q := r.querier(ctx)

	row, err := q.CreateUserSubscription(ctx, db.CreateUserSubscriptionParams{
		ID:     sub.ID,
		UserID: sub.UserID,
		PlanID: sub.PlanID,
		Status: sub.Status,
	})
	if err != nil {
		return models.Subscription{}, fmt.Errorf("error creating user subscription: %w", err)
	}

	return dbSubscriptionToDomain(row), nil
}

// GetActiveSubscriptionByUserID returns the most recent active subscription
// for the given user. pgx.ErrNoRows is translated to
// msgs.ErrSubscriptionNotFound; other errors are wrapped with context.
func (r *subscriptionRepository) GetActiveSubscriptionByUserID(ctx context.Context, userID uuid.UUID) (models.Subscription, error) {
	q := r.querier(ctx)
	row, err := q.GetActiveSubscriptionByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.Subscription{}, msgs.ErrSubscriptionNotFound
		}
		return models.Subscription{}, fmt.Errorf("getting active subscription: %w", err)
	}
	return dbSubscriptionToDomain(row), nil
}

// dbSubscriptionToDomain maps a sqlc db.UserSubscription row to a domain
// models.Subscription, converting the nullable EndsAt pgtype field into a
// nil-able pointer.
func dbSubscriptionToDomain(row db.UserSubscription) models.Subscription {
	sub := models.Subscription{
		ID:        row.ID,
		UserID:    row.UserID,
		PlanID:    row.PlanID,
		Status:    row.Status,
		StartedAt: row.StartedAt.Time,
		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}
	if row.EndsAt.Valid {
		sub.EndsAt = &row.EndsAt.Time
	}
	return sub
}
