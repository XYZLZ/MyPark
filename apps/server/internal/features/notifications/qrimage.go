package notifications

import (
	"errors"

	"rsc.io/qr"
)

// qrScale is the pixel size of one QR module: a version-8 symbol (49 modules
// plus the quiet zone) becomes a PNG a phone camera reads from a screen.
const qrScale = 6

// QRImage renders token as a PNG QR code at error-correction level M. The
// token is a credential: it is never logged, and the errors never carry it.
func QRImage(token string) ([]byte, error) {
	if token == "" {
		return nil, errors.New("notifications: empty qr token")
	}
	code, err := qr.Encode(token, qr.M)
	if err != nil {
		return nil, errors.New("notifications: encode qr") // the cause could echo the token
	}
	code.Scale = qrScale
	return code.PNG(), nil
}
