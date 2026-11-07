// Package postgres is the PostgreSQL adapter of the audit feature.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/audit"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/reqctx"
)

// Recorder implements audit.Recorder over dbgen.
type Recorder struct{}

var _ audit.Recorder = Recorder{}

// NewRecorder returns a Recorder. It holds no pool: it only writes through
// the transaction in the context.
func NewRecorder() Recorder { return Recorder{} }

// Record implements audit.Recorder.
func (Recorder) Record(ctx context.Context, e audit.Entry) error {
	tx, err := database.MustTx(ctx)
	if err != nil {
		return fmt.Errorf("audit: record: %w", err)
	}
	before, err := marshalMeta(e.Before)
	if err != nil {
		return fmt.Errorf("audit: encode before: %w", err)
	}
	after, err := marshalMeta(e.After)
	if err != nil {
		return fmt.Errorf("audit: encode after: %w", err)
	}
	arg := dbgen.InsertAuditLogParams{
		ActorKind:  string(e.Actor),
		Action:     e.Action,
		EntityType: text(e.EntityType),
		EntityID:   text(e.EntityID),
		Before:     before,
		After:      after,
		RequestID:  text(reqctx.RequestID(ctx)),
	}
	if e.ActorUserID != nil {
		arg.ActorUserID = pgtype.Int8{Int64: *e.ActorUserID, Valid: true}
	}
	if ip := reqctx.ClientIP(ctx); ip.IsValid() {
		arg.Ip = &ip
	}
	if err := dbgen.New(tx).InsertAuditLog(ctx, arg); err != nil {
		return safeError("audit: insert", err)
	}
	return nil
}

func marshalMeta(m map[string]any) ([]byte, error) {
	if m == nil {
		return nil, nil
	}
	return json.Marshal(m)
}

func text(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }

// safeError drops the server message and detail of a Postgres error: a check
// or unique violation detail echoes the failing row.
func safeError(op string, err error) error {
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		return fmt.Errorf("%s: sqlstate %s constraint %q", op, pgErr.Code, pgErr.ConstraintName)
	}
	return fmt.Errorf("%s: %w", op, err)
}
