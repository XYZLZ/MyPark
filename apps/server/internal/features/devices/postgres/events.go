package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/devices"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
)

// eventIDKey is the unique (device_id, event_id) constraint of device_events.
const eventIDKey = "device_events_device_id_event_id_key"

var _ devices.Store = (*Repo)(nil)

// FindEvent implements devices.Store.
func (r *Repo) FindEvent(ctx context.Context, deviceID int64, eventID uuid.UUID) (devices.StoredEvent, bool, error) {
	row, err := r.q(ctx).GetDeviceEvent(ctx, dbgen.GetDeviceEventParams{DeviceID: deviceID, EventID: eventID})
	if errors.Is(err, pgx.ErrNoRows) {
		return devices.StoredEvent{}, false, nil
	}
	if err != nil {
		return devices.StoredEvent{}, false, fmt.Errorf("devices: find event: %w", err)
	}
	return devices.StoredEvent{BodySHA256: row.BodySha256, Response: row.Response}, true, nil
}

// InsertEvent implements devices.Store.
func (r *Repo) InsertEvent(ctx context.Context, e devices.NewEvent) error {
	err := r.q(ctx).InsertDeviceEvent(ctx, dbgen.InsertDeviceEventParams{
		LotID: e.LotID, DeviceID: e.DeviceID, EventID: e.EventID, Type: string(e.Type),
		OccurredAt: pgtype.Timestamptz{Time: e.OccurredAt, Valid: true},
		BodySha256: e.BodySHA256, Payload: e.Payload, Response: e.Response,
	})
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" && pgErr.ConstraintName == eventIDKey {
		return devices.ErrDuplicateEvent
	}
	if err != nil {
		return fmt.Errorf("devices: insert event: %w", err)
	}
	return nil
}
