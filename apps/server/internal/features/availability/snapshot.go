package availability

import (
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
)

// Status is a lot's public status. Its precedence is closed, then full, then
// open.
type Status string

// The lot statuses of the Availability payload.
const (
	StatusOpen   Status = "open"
	StatusFull   Status = "full"
	StatusClosed Status = "closed"
)

// SnapshotInput is what Compute reads: the lot row, its occupancy counter,
// the out-of-service count and the two reservation numbers.
type SnapshotInput struct {
	LotPublicID                                                 uuid.UUID
	Capacity, OutOfService, Occupied, Holds, UpcomingArrivals2h int
	LotClosed                                                   bool
	Version                                                     int64
	UpdatedAt                                                   time.Time
}

// Snapshot is the Availability payload (design §11.1).
type Snapshot struct {
	LotID                  uuid.UUID `json:"lot_id"`
	Available              int       `json:"available"`
	Occupied               int       `json:"occupied"`
	EffectiveCapacity      int       `json:"effective_capacity"`
	Capacity               int       `json:"capacity"`
	EffectiveHolds         int       `json:"effective_holds"`
	UpcomingReservations2h int       `json:"upcoming_reservations_2h"`
	Status                 Status    `json:"status"`
	Version                int64     `json:"version"`
	UpdatedAt              time.Time `json:"updated_at"`
}

// Compute derives the snapshot (design §11.2). The status comes from
// CanAdmitDriveUp, the rule the entry gate uses, so the REST view and the
// gate decision cannot disagree. Pending entries are not counted here: they
// hold a space at the gate only (spec §5.2 rule 5).
func Compute(in SnapshotInput) Snapshot {
	effective := in.Capacity - in.OutOfService
	status := StatusOpen
	switch _, code := CanAdmitDriveUp(AdmitInput{
		LotClosed: in.LotClosed, Capacity: in.Capacity, OutOfService: in.OutOfService,
		Occupied: in.Occupied, Holds: in.Holds,
	}); code {
	case errcodes.GateLotClosed:
		status = StatusClosed
	case errcodes.GateLotFull:
		status = StatusFull
	}
	return Snapshot{
		LotID:                  in.LotPublicID,
		Available:              max(0, effective-in.Occupied-in.Holds),
		Occupied:               in.Occupied,
		EffectiveCapacity:      effective,
		Capacity:               in.Capacity,
		EffectiveHolds:         in.Holds,
		UpcomingReservations2h: in.UpcomingArrivals2h,
		Status:                 status,
		Version:                in.Version,
		UpdatedAt:              in.UpdatedAt.UTC(),
	}
}
