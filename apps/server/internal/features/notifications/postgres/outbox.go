// Package postgres is the PostgreSQL adapter of the notifications feature.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/notifications"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
)

// Notifier implements notifications.Notifier over dbgen.
type Notifier struct{}

var _ notifications.Notifier = Notifier{}

// NewNotifier returns a Notifier. It holds no pool: it only writes through
// the transaction in the context.
func NewNotifier() Notifier { return Notifier{} }

// Enqueue implements notifications.Notifier.
func (Notifier) Enqueue(ctx context.Context, e notifications.Email) error {
	tx, err := database.MustTx(ctx)
	if err != nil {
		return fmt.Errorf("notifications: enqueue: %w", err)
	}
	data := e.Data
	if data == nil {
		data = map[string]string{}
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("notifications: encode payload: %w", err)
	}
	arg := dbgen.EnqueueOutboxEmailParams{
		Template:  string(e.Template),
		Locale:    e.Locale,
		ToAddress: e.To,
		Payload:   payload,
		DedupeKey: e.DedupeKey,
	}
	if e.UserID != nil {
		arg.UserID = pgtype.Int8{Int64: *e.UserID, Valid: true}
	}
	if err := dbgen.New(tx).EnqueueOutboxEmail(ctx, arg); err != nil {
		return safeError("notifications: enqueue", err)
	}
	return nil
}

// ScrubPendingForUser implements notifications.Notifier.
func (Notifier) ScrubPendingForUser(ctx context.Context, userID int64) error {
	tx, err := database.MustTx(ctx)
	if err != nil {
		return fmt.Errorf("notifications: scrub: %w", err)
	}
	if err := dbgen.New(tx).ScrubPendingOutboxCodesForUser(ctx, pgtype.Int8{Int64: userID, Valid: true}); err != nil {
		return safeError("notifications: scrub", err)
	}
	return nil
}

// safeError drops the server message and detail of a Postgres error: a check
// violation detail echoes the failing row, which holds the address and code.
func safeError(op string, err error) error {
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		return fmt.Errorf("%s: sqlstate %s constraint %q", op, pgErr.Code, pgErr.ConstraintName)
	}
	return fmt.Errorf("%s: %w", op, err)
}
