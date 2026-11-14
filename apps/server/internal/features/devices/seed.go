package devices

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/XYZLZ/MyPark/apps/server/internal/devicefleet"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/secretbox"
)

// SeedResult counts what SeedFleet changed.
type SeedResult struct {
	// Inserted devices were missing; Resealed ones existed with a stale secret.
	Inserted, Resealed int
}

// SeedFleet provisions the simulator fleet in lot lotID (M2C-25). A missing
// device is inserted. An existing one is re-sealed and updated when its stored
// secret is not the one derived from fleet, or when it is sealed under a key
// id other than the sealer's current one (key rotation, or a retired id: the
// secret is derived, so it can always be regenerated). A secret sealed under
// the current key id that does not open means the SECRETBOX_KEY bytes are
// wrong (rotated without changing SECRETBOX_KEY_ID) or the row was tampered
// with: SeedFleet fails and writes nothing for that device rather than hide it.
// A run that changes nothing returns {0, 0}. No secret is ever logged or put
// in an error. Run it on a q bound to a transaction to make it atomic.
func SeedFleet(ctx context.Context, q *dbgen.Queries, s *secretbox.Sealer, lotID int64, fleet secret.Bytes) (SeedResult, error) {
	if len(fleet) == 0 {
		return SeedResult{}, errors.New("devices: seed fleet: empty fleet secret")
	}
	var res SeedResult
	for _, d := range devicefleet.Devices() {
		want := devicefleet.DeviceSecret(fleet, d.PublicID).Reveal()
		stored, err := q.GetFleetDevice(ctx, d.PublicID)
		missing := errors.Is(err, pgx.ErrNoRows)
		if err != nil && !missing {
			return SeedResult{}, fmt.Errorf("devices: seed fleet: get %s: %w", d.Name, err)
		}
		if !missing {
			stale, err := isStale(s, stored.SecretCiphertext, stored.SecretKeyID, want)
			if err != nil {
				return SeedResult{}, fmt.Errorf("devices: seed fleet: device %s (%s) is sealed under the current key id %q but does not open: check that SECRETBOX_KEY matches SECRETBOX_KEY_ID",
					d.Name, d.PublicID, stored.SecretKeyID)
			}
			if !stale {
				continue
			}
		}
		ct, keyID, err := s.Seal(want)
		if err != nil {
			return SeedResult{}, fmt.Errorf("devices: seed fleet: seal %s: %w", d.Name, err)
		}
		if missing {
			var n int64
			n, err = q.InsertFleetDevice(ctx, dbgen.InsertFleetDeviceParams{
				PublicID: d.PublicID, LotID: lotID, Kind: string(d.Kind), Name: d.Name,
				SecretCiphertext: ct, SecretKeyID: keyID,
			})
			res.Inserted += int(n) // 0 when a concurrent seed inserted it first
		} else {
			err = q.UpdateDeviceSecret(ctx, dbgen.UpdateDeviceSecretParams{
				ID: stored.ID, SecretCiphertext: ct, SecretKeyID: keyID,
			})
			res.Resealed++
		}
		if err != nil {
			return SeedResult{}, fmt.Errorf("devices: seed fleet: write %s: %w", d.Name, err)
		}
	}
	return res, nil
}

// isStale reports whether the stored secret must be rewritten. It returns an
// error, never a value, when the secret is sealed under the current key id
// but cannot be opened.
func isStale(s *secretbox.Sealer, ciphertext []byte, keyID string, want []byte) (bool, error) {
	if keyID != s.CurrentKeyID() {
		return true, nil
	}
	plain, err := s.Open(ciphertext, keyID)
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare(plain, want) != 1, nil
}
