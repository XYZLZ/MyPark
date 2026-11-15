package testsupport

import (
	"bytes"

	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/ratelimit"
)

// TestKeyPepper is the rate-limit key pepper of TestKeyHasher.
var TestKeyPepper = bytes.Repeat([]byte{0x6b}, 32)

// TestKeyHasher returns a rate-limit KeyHasher over TestKeyPepper. It needs
// no *testing.T: the fixed pepper is always long enough.
func TestKeyHasher() ratelimit.KeyHasher {
	h, err := ratelimit.NewKeyHasher(TestKeyPepper)
	if err != nil {
		panic(err)
	}
	return h
}
