package devices

// SetVerifyHook replaces the MAC check of a, so tests can count the dummy-key work.
func SetVerifyHook(a *Authenticator, f func(secret, canonical []byte, sig string) bool) { a.verify = f }

// DummyKey returns a copy of the fixed key unknown and revoked devices are checked against.
func DummyKey() []byte { return append([]byte(nil), dummyKey[:]...) }
