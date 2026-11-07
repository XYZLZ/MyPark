// Package config loads and validates the server configuration from
// environment variables. It fails fast: every invalid or missing variable is
// reported in a single aggregated error that names each offending variable.
package config

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"net/netip"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/go-playground/validator/v10"
)

// Environment variable names.
const (
	EnvAppEnv             = "APP_ENV"
	EnvHTTPAddr           = "HTTP_ADDR"
	EnvDatabaseURL        = "DATABASE_URL"
	EnvLogLevel           = "LOG_LEVEL"
	EnvCORSAllowedOrigins = "CORS_ALLOWED_ORIGINS"
	EnvDBMaxConns         = "DB_MAX_CONNS"
	EnvJWTSigningKey      = "JWT_SIGNING_KEY"
	EnvJWTKeyID           = "JWT_KEY_ID"
	EnvJWTVerifyKeys      = "JWT_VERIFY_KEYS"
	EnvAuthCodePepper     = "AUTH_CODE_PEPPER"
	EnvTrustedProxyCIDRs  = "TRUSTED_PROXY_CIDRS"
	EnvMaxVehiclesPerUser = "MAX_VEHICLES_PER_USER"
)

// Default values applied when an optional variable is unset or empty.
// Required variables (DATABASE_URL) have no default.
const (
	DefaultEnv        = "development"
	DefaultHTTPAddr   = ":8080"
	DefaultLogLevel   = slog.LevelInfo
	DefaultDBMaxConns = int32(10)

	// DefaultMaxVehiclesPerUser is the per-user vehicle cap when unset.
	DefaultMaxVehiclesPerUser = 5
)

const (
	jwtSeedLen   = ed25519.SeedSize // 32
	minPepperLen = 32
	redacted     = "[redacted]"
)

// Config is the validated, read-only server configuration. Build it with
// Load; the zero value is not a valid configuration.
type Config struct {
	env                string
	httpAddr           string
	databaseURL        string
	logLevel           slog.Level
	corsAllowedOrigins []string
	dbMaxConns         int32

	jwtSigningSeed     []byte
	jwtKeyID           string
	jwtVerifyKeys      map[string]ed25519.PublicKey
	authCodePepper     []byte
	trustedProxyCIDRs  []netip.Prefix
	maxVehiclesPerUser int
}

// Env returns the deployment environment: development, test or production.
func (c Config) Env() string { return c.env }

// HTTPAddr returns the listen address, e.g. ":8080".
func (c Config) HTTPAddr() string { return c.httpAddr }

// DatabaseURL returns the Postgres connection URL. It contains credentials:
// never log it directly (log the Config, which redacts it).
func (c Config) DatabaseURL() string { return c.databaseURL }

// LogLevel returns the minimum log level.
func (c Config) LogLevel() slog.Level { return c.logLevel }

// CORSAllowedOrigins returns a copy of the allowed CORS origins.
func (c Config) CORSAllowedOrigins() []string { return slices.Clone(c.corsAllowedOrigins) }

// DBMaxConns returns the maximum size of the database pool.
func (c Config) DBMaxConns() int32 { return c.dbMaxConns }

// JWTSigningSeed returns a copy of the 32-byte Ed25519 seed that signs access
// tokens. Never log it.
func (c Config) JWTSigningSeed() []byte { return slices.Clone(c.jwtSigningSeed) }

// JWTKeyID returns the kid written to the tokens the signing key issues.
func (c Config) JWTKeyID() string { return c.jwtKeyID }

// JWTVerifyKeys returns the public keys accepted when verifying tokens by
// kid: the signing key's own public key plus any rotated-out keys from
// JWT_VERIFY_KEYS.
func (c Config) JWTVerifyKeys() map[string]ed25519.PublicKey { return maps.Clone(c.jwtVerifyKeys) }

// AuthCodePepper returns a copy of the HMAC pepper for one-time codes. Never
// log it.
func (c Config) AuthCodePepper() []byte { return slices.Clone(c.authCodePepper) }

// TrustedProxyCIDRs returns a copy of the networks whose forwarding headers
// are trusted.
func (c Config) TrustedProxyCIDRs() []netip.Prefix { return slices.Clone(c.trustedProxyCIDRs) }

// MaxVehiclesPerUser returns the per-user vehicle cap.
func (c Config) MaxVehiclesPerUser() int { return c.maxVehiclesPerUser }

// String renders the redacted LogValue group, so %v, %+v and %s never print
// credentials.
func (c Config) String() string { return c.LogValue().String() }

// GoString renders the same redacted group for %#v.
func (c Config) GoString() string { return c.String() }

// LogValue implements slog.LogValuer so a Config can be logged safely: the
// database URL password is replaced with "xxxxx". A value that is not a URL
// with a scheme is replaced entirely, since its credential cannot be located.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("env", c.env),
		slog.String("http_addr", c.httpAddr),
		slog.String("database_url", redactURL(c.databaseURL)),
		slog.String("log_level", c.logLevel.String()),
		slog.Any("cors_allowed_origins", c.corsAllowedOrigins),
		slog.Int("db_max_conns", int(c.dbMaxConns)),
		slog.String("jwt_signing_key", redacted),
		slog.String("jwt_key_id", c.jwtKeyID),
		slog.Int("jwt_verify_keys", len(c.jwtVerifyKeys)),
		slog.String("auth_code_pepper", redacted),
		slog.Any("trusted_proxy_cidrs", c.trustedProxyCIDRs),
		slog.Int("max_vehicles_per_user", c.maxVehiclesPerUser),
	)
}

func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return "[redacted]"
	}
	u.RawQuery = redactQuery(u.RawQuery)
	return u.Redacted()
}

// secretQueryKeys are connection parameters that carry credentials in the
// query string (libpq and pgx accept both), matched case-insensitively.
var secretQueryKeys = []string{"password", "sslpassword"}

// redactQuery replaces the values of secret query parameters with "xxxxx".
// It edits the raw query in place so other parameters keep their order and
// encoding. A key that cannot be unescaped is redacted as a precaution.
func redactQuery(raw string) string {
	if raw == "" {
		return raw
	}
	parts := strings.Split(raw, "&")
	for i, part := range parts {
		rawKey, _, _ := strings.Cut(part, "=")
		key, err := url.QueryUnescape(rawKey)
		if err != nil || slices.ContainsFunc(secretQueryKeys, func(s string) bool { return strings.EqualFold(s, key) }) {
			parts[i] = rawKey + "=xxxxx"
		}
	}
	return strings.Join(parts, "&")
}

// rawEnv holds the environment values after defaults and normalization, just
// before validation. The env tag is used as the field name in errors.
type rawEnv struct {
	Env         string `env:"APP_ENV" validate:"oneof=development test production"`
	HTTPAddr    string `env:"HTTP_ADDR" validate:"listen_addr"`
	DatabaseURL string `env:"DATABASE_URL" validate:"required,url"`
	LogLevel    string `env:"LOG_LEVEL" validate:"oneof=debug info warn error"`
	DBMaxConns  int64  `env:"DB_MAX_CONNS" validate:"min=1,max=1000"`

	JWTSigningKey      string `env:"JWT_SIGNING_KEY" validate:"required"`
	JWTKeyID           string `env:"JWT_KEY_ID" validate:"required,max=64"`
	AuthCodePepper     string `env:"AUTH_CODE_PEPPER" validate:"required"`
	MaxVehiclesPerUser int64  `env:"MAX_VEHICLES_PER_USER" validate:"min=1,max=20"`
}

// secretVars are never echoed back in error messages.
var secretVars = map[string]bool{
	EnvDatabaseURL:    true,
	EnvJWTSigningKey:  true,
	EnvAuthCodePepper: true,
}

var envValidator = sync.OnceValue(func() *validator.Validate {
	v := validator.New(validator.WithRequiredStructEnabled())
	v.RegisterTagNameFunc(func(f reflect.StructField) string { return f.Tag.Get("env") })
	// hostname_port rejects port 0 (used for ephemeral ports in tests) and
	// IPv6 hosts, so the listen address gets its own rule.
	if err := v.RegisterValidation("listen_addr", isListenAddr); err != nil {
		panic(err)
	}
	return v
})

// isListenAddr accepts "host:port" or ":port" with a numeric port in 0-65535.
func isListenAddr(fl validator.FieldLevel) bool {
	host, port, err := net.SplitHostPort(fl.Field().String())
	if err != nil || strings.ContainsAny(host, " \t") {
		return false
	}
	n, err := strconv.ParseUint(port, 10, 16)
	return err == nil && n <= 65535
}

// Load builds a Config using getenv (typically os.Getenv). All validation
// errors are joined with errors.Join so every problem is reported at once,
// and each error names the offending variable.
func Load(getenv func(string) string) (Config, error) {
	raw := rawEnv{
		Env:         valueOr(getenv(EnvAppEnv), DefaultEnv),
		HTTPAddr:    valueOr(getenv(EnvHTTPAddr), DefaultHTTPAddr),
		DatabaseURL: strings.TrimSpace(getenv(EnvDatabaseURL)),
		LogLevel:    strings.ToLower(valueOr(getenv(EnvLogLevel), DefaultLogLevel.String())),
		DBMaxConns:  int64(DefaultDBMaxConns),

		JWTSigningKey:      strings.TrimSpace(getenv(EnvJWTSigningKey)),
		JWTKeyID:           strings.TrimSpace(getenv(EnvJWTKeyID)),
		AuthCodePepper:     strings.TrimSpace(getenv(EnvAuthCodePepper)),
		MaxVehiclesPerUser: DefaultMaxVehiclesPerUser,
	}
	var errs []error

	if s := strings.TrimSpace(getenv(EnvMaxVehiclesPerUser)); s != "" {
		n, err := strconv.ParseInt(s, 10, 32)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s must be an integer, got %q", EnvMaxVehiclesPerUser, s))
		} else {
			raw.MaxVehiclesPerUser = n
		}
	}

	if s := strings.TrimSpace(getenv(EnvDBMaxConns)); s != "" {
		n, err := strconv.ParseInt(s, 10, 32)
		if err != nil {
			// Keep the default so the validator does not report it twice.
			errs = append(errs, fmt.Errorf("%s must be an integer, got %q", EnvDBMaxConns, s))
		} else {
			raw.DBMaxConns = n
		}
	}

	if err := envValidator().Struct(raw); err != nil {
		verrs, ok := errors.AsType[validator.ValidationErrors](err)
		if !ok {
			return Config{}, fmt.Errorf("invalid configuration: %w", err)
		}
		for _, fe := range verrs {
			errs = append(errs, envError(fe.Field(), secretVars[fe.Field()], fe))
		}
	}

	production := raw.Env == "production"
	var seed, pepper []byte
	if raw.JWTSigningKey != "" {
		var err error
		if seed, err = decodeKey(EnvJWTSigningKey, raw.JWTSigningKey, jwtSeedLen, true); err != nil {
			errs = append(errs, err)
		} else if production && raw.JWTSigningKey == DevJWTSigningKey {
			errs = append(errs, fmt.Errorf("%s must not be the development default in production", EnvJWTSigningKey))
		}
	}
	if raw.AuthCodePepper != "" {
		var err error
		if pepper, err = decodeKey(EnvAuthCodePepper, raw.AuthCodePepper, minPepperLen, false); err != nil {
			errs = append(errs, err)
		} else if production && raw.AuthCodePepper == DevAuthCodePepper {
			errs = append(errs, fmt.Errorf("%s must not be the development default in production", EnvAuthCodePepper))
		}
	}
	verifyKeys := map[string]ed25519.PublicKey{}
	if seed != nil {
		verifyKeys[raw.JWTKeyID] = ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	}
	extra, err := parseVerifyKeys(getenv(EnvJWTVerifyKeys))
	if err != nil {
		errs = append(errs, err)
	}
	for kid, key := range extra {
		if _, dup := verifyKeys[kid]; dup {
			errs = append(errs, fmt.Errorf("%s repeats the signing key id", EnvJWTVerifyKeys))
			continue
		}
		verifyKeys[kid] = key
	}
	cidrs, err := parseCIDRs(getenv(EnvTrustedProxyCIDRs))
	if err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return Config{}, fmt.Errorf("invalid configuration: %w", errors.Join(errs...))
	}

	level, err := parseLogLevel(raw.LogLevel)
	if err != nil {
		return Config{}, fmt.Errorf("invalid configuration: %w", err)
	}
	return Config{
		env:                raw.Env,
		httpAddr:           raw.HTTPAddr,
		databaseURL:        raw.DatabaseURL,
		logLevel:           level,
		corsAllowedOrigins: splitList(getenv(EnvCORSAllowedOrigins)),
		dbMaxConns:         int32(raw.DBMaxConns),

		jwtSigningSeed:     seed,
		jwtKeyID:           raw.JWTKeyID,
		jwtVerifyKeys:      verifyKeys,
		authCodePepper:     pepper,
		trustedProxyCIDRs:  cidrs,
		maxVehiclesPerUser: int(raw.MaxVehiclesPerUser),
	}, nil
}

// decodeKey decodes a standard-base64 secret named name. With exact it must
// be exactly n bytes, otherwise at least n. The error never includes the value.
func decodeKey(name, value string, n int, exact bool) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(value)
	switch {
	case err != nil:
		return nil, fmt.Errorf("%s must be base64", name)
	case exact && len(b) != n:
		return nil, fmt.Errorf("%s must decode to exactly %d bytes", name, n)
	case !exact && len(b) < n:
		return nil, fmt.Errorf("%s must decode to at least %d bytes", name, n)
	}
	return b, nil
}

// parseVerifyKeys parses "kid:base64(public key)" entries, comma-separated.
func parseVerifyKeys(raw string) (map[string]ed25519.PublicKey, error) {
	out := map[string]ed25519.PublicKey{}
	for _, entry := range splitList(raw) {
		kid, enc, ok := strings.Cut(entry, ":")
		kid = strings.TrimSpace(kid)
		key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(enc))
		if !ok || kid == "" || err != nil || len(key) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("%s must be comma-separated kid:base64(32-byte public key) entries", EnvJWTVerifyKeys)
		}
		if _, dup := out[kid]; dup {
			return nil, fmt.Errorf("%s repeats a key id", EnvJWTVerifyKeys)
		}
		out[kid] = ed25519.PublicKey(key)
	}
	return out, nil
}

// parseCIDRs parses a comma-separated list of CIDR prefixes.
func parseCIDRs(raw string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, entry := range splitList(raw) {
		p, err := netip.ParsePrefix(entry)
		if err != nil {
			return nil, fmt.Errorf("%s must be comma-separated CIDRs, e.g. 10.0.0.0/8", EnvTrustedProxyCIDRs)
		}
		out = append(out, p.Masked())
	}
	return out, nil
}

// DatabaseURLFrom reads the database URL held in the environment variable
// name, for commands that need a connection URL other than DATABASE_URL
// (cmd/migrate uses MIGRATOR_DATABASE_URL). It applies the same rules as
// Load (required, url), names the variable in errors and never echoes the
// value, which carries credentials.
func DatabaseURLFrom(getenv func(string) string, name string) (string, error) {
	raw := strings.TrimSpace(getenv(name))
	err := envValidator().Var(raw, "required,url")
	if err == nil {
		return raw, nil
	}
	verrs, ok := errors.AsType[validator.ValidationErrors](err)
	if !ok || len(verrs) == 0 {
		return "", fmt.Errorf("invalid configuration: %w", err)
	}
	return "", fmt.Errorf("invalid configuration: %w", envError(name, true, verrs[0]))
}

// ListenAddrFrom reads the listen address held in the environment variable
// name, falling back to def when it is unset or blank, for commands that
// listen on an address other than HTTP_ADDR (cmd/worker, cmd/simulator) and
// for the -healthcheck probes. It applies the same rule as HTTP_ADDR in
// Load: "host:port" or ":port", port 0-65535.
func ListenAddrFrom(getenv func(string) string, name, def string) (string, error) {
	addr := valueOr(getenv(name), def)
	err := envValidator().Var(addr, "listen_addr")
	if err == nil {
		return addr, nil
	}
	verrs, ok := errors.AsType[validator.ValidationErrors](err)
	if !ok || len(verrs) == 0 {
		return "", fmt.Errorf("invalid configuration: %w", err)
	}
	return "", fmt.Errorf("invalid configuration: %w", envError(name, false, verrs[0]))
}

// envError turns a validator field error on the environment variable name
// into a message that names it. Values of secret variables are never
// included.
func envError(name string, secret bool, fe validator.FieldError) error {
	var msg string
	switch fe.Tag() {
	case "required":
		return fmt.Errorf("%s is required", name)
	case "url":
		msg = "must be a valid URL"
	case "oneof":
		msg = "must be one of " + strings.ReplaceAll(fe.Param(), " ", "|")
	case "min":
		msg = "must be >= " + fe.Param()
	case "max":
		msg = "must be <= " + fe.Param()
	case "listen_addr":
		msg = "must be host:port or :port"
	default:
		msg = "failed rule " + fe.Tag()
	}
	if secret {
		return fmt.Errorf("%s %s", name, msg)
	}
	return fmt.Errorf("%s %s, got %q", name, msg, fmt.Sprint(fe.Value()))
}

func parseLogLevel(raw string) (slog.Level, error) {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(raw)); err != nil {
		return 0, fmt.Errorf("%s: %w", EnvLogLevel, err)
	}
	return lvl, nil
}

// splitList splits a comma-separated list, trimming spaces and dropping
// empty entries.
func splitList(raw string) []string {
	var out []string
	for part := range strings.SplitSeq(raw, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func valueOr(v, def string) string {
	if v = strings.TrimSpace(v); v != "" {
		return v
	}
	return def
}
