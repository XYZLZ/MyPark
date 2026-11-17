package notifications_test

import (
	"bytes"
	"image"
	_ "image/png" // registers the decoder
	"strings"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/notifications"
)

func TestQRImage(t *testing.T) {
	token := strings.Repeat("A1b2", 21) + "Zz" // 86 characters, the QR token length
	if len(token) != 86 {
		t.Fatalf("test token has %d characters", len(token))
	}
	got, err := notifications.QRImage(token)
	if err != nil {
		t.Fatal(err)
	}
	img, format, err := image.Decode(bytes.NewReader(got))
	if err != nil || format != "png" {
		t.Fatalf("decode = %q, %v; want a png", format, err)
	}
	if b := img.Bounds(); b.Dx() != b.Dy() || b.Dx() < 100 {
		t.Errorf("image is %dx%d, want a square of a scannable size", b.Dx(), b.Dy())
	}
	again, _ := notifications.QRImage(token)
	if !bytes.Equal(got, again) {
		t.Error("the same token must give the same bytes")
	}
	other, _ := notifications.QRImage(token[:85] + "Y")
	if bytes.Equal(got, other) {
		t.Error("another token must give another image")
	}
	if _, err := notifications.QRImage(""); err == nil {
		t.Error("an empty token must be rejected")
	}
}
