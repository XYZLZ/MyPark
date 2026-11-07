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
