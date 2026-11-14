package gate

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/XYZLZ/MyPark/apps/server/internal/devicefleet"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
)

// cardRevokedAt is when the demo revoked card lost its access (America/Santo_Domingo).
var cardRevokedAt = time.Date(2026, 11, 1, 0, 0, 0, 0, time.FixedZone("AST", -4*3600))

// HashCardUID returns HMAC-SHA256(pepper, normalized uid), the only form in
// which a card UID is stored or compared (M2C-26). A UID that does not
// normalize is hashed as given: it cannot match a stored hash, because the
// gate rejects it before hashing.
func HashCardUID(pepper []byte, uid string) []byte {
	if n, ok := NormalizeCardUID(uid); ok {
		uid = n
	}
	mac := hmac.New(sha256.New, pepper)
	mac.Write([]byte(uid))
	return mac.Sum(nil)
}

// SeedCards inserts the 31 simulator employee cards in lot lotID and returns
// how many were missing. EMP-031 is inserted revoked, to demo the denial. A
// card that already exists is left alone, so a second run returns 0. Only the
// hash of each UID is written; errors name the card code, never the UID or
// the hash. Run it on a q bound to a transaction to make it atomic.
func SeedCards(ctx context.Context, q *dbgen.Queries, lotID int64, fleet secret.Bytes, pepper []byte) (inserted int, err error) {
	if len(fleet) == 0 || len(pepper) == 0 {
		return 0, errors.New("gate: seed cards: empty fleet secret or card uid pepper")
	}
	for _, c := range devicefleet.Cards() {
		p := dbgen.InsertEmployeeCardParams{
			LotID:        lotID,
			CardUidHash:  HashCardUID(pepper, devicefleet.CardUID(fleet, c.Code)),
			HolderName:   c.Holder,
			EmployeeCode: c.Code,
			Status:       "active",
		}
		if c.Revoked {
			p.Status = "revoked"
			p.RevokedAt = pgtype.Timestamptz{Time: cardRevokedAt, Valid: true}
		}
		n, err := q.InsertEmployeeCard(ctx, p)
		if err != nil {
			return 0, seedError(c.Code, err)
		}
		inserted += int(n)
	}
	return inserted, nil
}

// seedError reports a failed insert without the driver's text: a Postgres
// error can quote the failing row. Only the card code and the SQLSTATE (or the
// error type) survive; a context error stays matchable.
func seedError(code string, err error) error {
	pgErr, isPg := errors.AsType[*pgconn.PgError](err)
	switch {
	case errors.Is(err, context.Canceled):
		return fmt.Errorf("gate: seed cards: insert %s: %w", code, context.Canceled)
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("gate: seed cards: insert %s: %w", code, context.DeadlineExceeded)
	case isPg:
		return fmt.Errorf("gate: seed cards: insert %s: SQLSTATE %s", code, pgErr.Code)
	default:
		return fmt.Errorf("gate: seed cards: insert %s: %T", code, err)
	}
}
