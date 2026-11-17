package config

// Development-only credentials. They are the inline defaults of
// deploy/compose/docker-compose.yml so the stack boots with no .env file.
// They are public: Load refuses the first two when APP_ENV=production, and
// the seed refuses DevSeedAdminPassword the same way.
const (
	// DevJWTSigningKey is base64 of a 32-byte Ed25519 seed.
	DevJWTSigningKey = "KOjknsSvcscUncFCKZZVyz6pIx6qzsgynPkmGSI5ap0=" // dev only
	// DevAuthCodePepper is base64 of a 32-byte HMAC pepper.
	DevAuthCodePepper = "Vx+9DiL7uyqiwY3gQJqdTNupMJqGdpB+gmkk1YUbEwY=" // dev only
	// DevSeedAdminPassword is the first admin's password in local development.
	DevSeedAdminPassword = "mypark-dev-admin-2026" // dev only
)

// M2 development-only credentials, also compose defaults. Load refuses
// DevSecretboxKey and DevCardUIDPepper when APP_ENV=production.
const (
	// DevSecretboxKey is base64 of a 32-byte secretbox key.
	DevSecretboxKey = "hjcsotOiA0zcCanr6LhEpWwP8Q5QqtFfMdhaD8DU1UQ=" // dev only
	// DevCardUIDPepper is base64 of a 32-byte HMAC pepper for card UIDs.
	DevCardUIDPepper = "5aolRvJpaNIAVU0GqBsELQfbrdRUYDraaxm64URAA9g=" // dev only
	// DevSimulatorFleetSecret is base64 of a 32-byte secret the simulator
	// derives its device secrets from.
	DevSimulatorFleetSecret = "WXDXaWhcsK1yFwswbHoQjtSo1CqGtuoMo6QHfFvF7dY=" // dev only
	// DevSimulatorControlToken is base64 of a 32-byte bearer token for the
	// simulator control API.
	DevSimulatorControlToken = "s0oW01SL2PN9O1DhLSFg8uD4e/oA+912l90mTMmfg3M=" // dev only
)

// DevRateLimitPepper is base64 of a 32-byte HMAC pepper for the rate-limit
// key hashes. A compose default; Load refuses it when APP_ENV=production.
const DevRateLimitPepper = "cpGwr3/gFKhLD9QtFMbcf9OFi4/rq004pDlCcvhdRTw=" // dev only

// DevQRHMACKey is base64 of a 32-byte HMAC key that signs reservation QR
// payloads. A compose default; Load and LoadWorker refuse it when
// APP_ENV=production.
const DevQRHMACKey = "Ip8tGiGlHYRtL89ghWgbY+I0C9ROBONVRnhMKdleggU=" // dev only
