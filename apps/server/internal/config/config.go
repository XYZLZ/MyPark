// Package config loads and validates the server configuration from
// environment variables. It fails fast: every invalid or missing variable is
// reported in a single aggregated error that names each offending variable.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
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
)

// Default values applied when an optional variable is unset or empty.
// Required variables (DATABASE_URL) have no default.
const (
	DefaultEnv        = "development"
	DefaultHTTPAddr   = ":8080"
	DefaultLogLevel   = slog.LevelInfo
	DefaultDBMaxConns = int32(10)
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
}

// secretVars are never echoed back in error messages.
var secretVars = map[string]bool{EnvDatabaseURL: true}

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
	}
	var errs []error

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
	}, nil
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
