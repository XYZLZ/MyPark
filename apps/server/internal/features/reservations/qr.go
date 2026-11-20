package reservations

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
)

// qrTokenLen is the length of a QR token: base64url (no padding) of
// public_id (16) || nonce (16) || HMAC-SHA256 (32) = 64 bytes (design 11.2).
const qrTokenLen = 86

var qrEncoding = base64.RawURLEncoding.Strict()

// EncodeQR signs a reservation QR token: the public id and the nonce, then
// their HMAC-SHA256 under key. Rotating the nonce invalidates an old token.
func EncodeQR(key []byte, publicID uuid.UUID, nonce [16]byte) string {
	raw := make([]byte, 0, 64)
	raw = append(raw, publicID[:]...)
	raw = append(raw, nonce[:]...)
	raw = append(raw, qrMAC(key, raw)...)
	return qrEncoding.EncodeToString(raw)
}

// DecodeQR verifies token and returns its public id and nonce. It rejects any
// length other than 86, any character outside base64url (the standard decoder
// would skip a CR or LF), padding, a non-canonical last character and a bad
// MAC (compared in constant time).
func DecodeQR(key []byte, token string) (uuid.UUID, [16]byte, bool) {
	var (
		id    uuid.UUID
		nonce [16]byte
	)
	if len(token) != qrTokenLen {
		return id, nonce, false
	}
	for i := range len(token) {
		if !isBase64URL(token[i]) {
			return id, nonce, false
		}
	}
	raw, err := qrEncoding.DecodeString(token)
	if err != nil || len(raw) != 64 {
		return id, nonce, false
	}
	if !hmac.Equal(raw[32:], qrMAC(key, raw[:32])) {
		return id, nonce, false
	}
	copy(id[:], raw[:16])
	copy(nonce[:], raw[16:32])
	return id, nonce, true
}

func qrMAC(key, msg []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(msg)
	return m.Sum(nil)
}

func isBase64URL(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_'
}

// QRView is the QR token of a reservation and the times that bound entry.
type QRView struct {
	Token                           string
	EntryValidFrom, EntryValidUntil time.Time
}

// QR returns the entry token of a confirmed or checked-in reservation of the
// driver (design 7.9). Any other state is 409 reservation.invalid_state. The
// token is secret and never logged.
func (s *Service) QR(ctx context.Context, userID int64, id uuid.UUID) (QRView, error) {
	var out QRView
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.repo.SetTxLimits(ctx); err != nil {
			return err
		}
		d, err := s.repo.QRData(ctx, userID, id)
		if errors.Is(err, ErrNotFound) {
			return apperr.NotFound(errcodes.ReservationNotFound)
		}
		if err != nil {
			return err
		}
		if d.Status != StatusConfirmed && d.Status != StatusCheckedIn {
			return apperr.Conflict(errcodes.ReservationInvalidState).
				WithDetails(map[string]any{"status": string(d.Status)})
		}
		if len(d.Nonce) != 16 {
			return fmt.Errorf("reservations: %s %s has a qr nonce of %d bytes", d.Status, d.PublicID, len(d.Nonce))
		}
		out = QRView{
			Token:          EncodeQR(s.qrKey, d.PublicID, [16]byte(d.Nonce)),
			EntryValidFrom: d.Window.Start, EntryValidUntil: d.Window.End,
		}
		return nil
	})
	return out, err
}
