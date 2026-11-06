// Package config loads and validates the server configuration from
// environment variables. It fails fast: every invalid or missing variable is
// reported in a single aggregated error.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
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

// Default values applied when a variable is unset or empty.
const (
	DefaultEnv        = "development"
	DefaultHTTPAddr   = ":8080"
	DefaultLogLevel   = slog.LevelInfo
	DefaultDBMaxConns = int32(10)
)

// Config is the validated server configuration.
type Config struct {
	Env                string
	HTTPAddr           string
	DatabaseURL        string
	LogLevel           slog.Level
	CORSAllowedOrigins []string
	DBMaxConns         int32
}

// Load builds a Config using getenv (typically os.Getenv). All validation
// errors are joined with errors.Join so every problem is reported at once,
// and each error names the offending variable.
func Load(getenv func(string) string) (Config, error) {
	c := Config{
		Env:        valueOr(getenv(EnvAppEnv), DefaultEnv),
		HTTPAddr:   valueOr(getenv(EnvHTTPAddr), DefaultHTTPAddr),
		LogLevel:   DefaultLogLevel,
		DBMaxConns: DefaultDBMaxConns,
	}
	var errs []error

	c.DatabaseURL = strings.TrimSpace(getenv(EnvDatabaseURL))
	if c.DatabaseURL == "" {
		errs = append(errs, fmt.Errorf("%s is required", EnvDatabaseURL))
	}

	if raw := strings.TrimSpace(getenv(EnvLogLevel)); raw != "" {
		lvl, err := parseLogLevel(raw)
		if err != nil {
			errs = append(errs, err)
		} else {
			c.LogLevel = lvl
		}
	}

	if raw := strings.TrimSpace(getenv(EnvDBMaxConns)); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 32)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("%s must be an integer, got %q", EnvDBMaxConns, raw))
		case n < 1:
			errs = append(errs, fmt.Errorf("%s must be >= 1, got %d", EnvDBMaxConns, n))
		default:
			c.DBMaxConns = int32(n)
		}
	}

	c.CORSAllowedOrigins = splitList(getenv(EnvCORSAllowedOrigins))

	if len(errs) > 0 {
		return Config{}, fmt.Errorf("invalid configuration: %w", errors.Join(errs...))
	}
	return c, nil
}

func parseLogLevel(raw string) (slog.Level, error) {
	switch strings.ToLower(raw) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("%s must be one of debug|info|warn|error, got %q", EnvLogLevel, raw)
	}
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
