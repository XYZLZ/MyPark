// Package postgres is the PostgreSQL adapter of the alerts feature.
package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/alerts"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
)

// Recorder implements alerts.Recorder over dbgen.
type Recorder struct{}

var _ alerts.Recorder = Recorder{}

// NewRecorder returns a Recorder. It holds no state: it always writes on the
// transaction of the context.
func NewRecorder() Recorder { return Recorder{} }

// Open implements alerts.Recorder.
func (Recorder) Open(ctx context.Context, lotID int64, kind alerts.Kind, details map[string]string, now time.Time) error {
	tx, err := database.MustTx(ctx)
	if err != nil {
		return fmt.Errorf("alerts: open %s: %w", kind, err)
	}
	raw, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("alerts: details of %s: %w", kind, err)
	}
	err = dbgen.New(tx).InsertReconciliationAlert(ctx, dbgen.InsertReconciliationAlertParams{
		LotID: lotID, Kind: string(kind), Details: raw, OpenedAt: pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("alerts: open %s: %w", kind, err)
	}
	return nil
}
