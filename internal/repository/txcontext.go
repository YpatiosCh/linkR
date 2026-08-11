package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// txKey is the unexported context key used to carry an active pgx.Tx
// through the context so repository queries can run inside a transaction.
type txKey struct{}

// injectTx returns a new context derived from ctx that carries tx under
// txKey, making the transaction visible to the repository querier helpers.
func injectTx(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}

// extractTx retrieves the pgx.Tx previously stored by injectTx, if any.
// It returns the transaction and true when present, or nil and false when
// the context was not created inside a transaction.
func extractTx(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(txKey{}).(pgx.Tx)
	return tx, ok
}
