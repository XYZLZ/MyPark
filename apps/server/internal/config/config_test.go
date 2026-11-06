package config_test

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/config"
)

// env returns a getenv-style lookup backed by the given map.
func env(m map[string]string) func(string) string {
	return func(key string) string { return m[key] }
}

func TestLoad_MissingDatabaseURL(t *testing.T) {
	_, err := config.Load(func(string) string { return "" })
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("want error naming DATABASE_URL, got %v", err)
	}
}

func TestLoad_Defaults(t *testing.T) {
	c, err := config.Load(env(map[string]string{"DATABASE_URL": "postgres://x"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want %q", c.HTTPAddr, ":8080")
	}
	if c.DBMaxConns != 10 {
		t.Errorf("DBMaxConns = %d, want 10", c.DBMaxConns)
	}
	if c.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want %v", c.LogLevel, slog.LevelInfo)
	}
	if c.Env != "development" {
		t.Errorf("Env = %q, want %q", c.Env, "development")
	}
	if c.DatabaseURL != "postgres://x" {
		t.Errorf("DatabaseURL = %q, want %q", c.DatabaseURL, "postgres://x")
	}
	if len(c.CORSAllowedOrigins) != 0 {
		t.Errorf("CORSAllowedOrigins = %v, want empty", c.CORSAllowedOrigins)
	}
}

func TestLoad_InvalidMaxConns(t *testing.T) {
	_, err := config.Load(env(map[string]string{
		"DATABASE_URL": "postgres://x",
		"DB_MAX_CONNS": "abc",
	}))
	if err == nil || !strings.Contains(err.Error(), "DB_MAX_CONNS") {
		t.Fatalf("want error naming DB_MAX_CONNS, got %v", err)
	}
}

func TestLoad_CORSOriginsSplit(t *testing.T) {
	c, err := config.Load(env(map[string]string{
		"DATABASE_URL":         "postgres://x",
		"CORS_ALLOWED_ORIGINS": "http://localhost:3000, http://localhost:8081",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"http://localhost:3000", "http://localhost:8081"}
	if len(c.CORSAllowedOrigins) != len(want) {
		t.Fatalf("CORSAllowedOrigins = %q, want %q", c.CORSAllowedOrigins, want)
	}
	for i := range want {
		if c.CORSAllowedOrigins[i] != want[i] {
			t.Errorf("CORSAllowedOrigins[%d] = %q, want %q", i, c.CORSAllowedOrigins[i], want[i])
		}
	}
}

func TestLoad_LogLevel(t *testing.T) {
	tests := []struct {
		in   string
		want slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"INFO", slog.LevelInfo},
		{"Warn", slog.LevelWarn},
		{"error", slog.LevelError},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			c, err := config.Load(env(map[string]string{"DATABASE_URL": "postgres://x", "LOG_LEVEL": tt.in}))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if c.LogLevel != tt.want {
				t.Errorf("LogLevel = %v, want %v", c.LogLevel, tt.want)
			}
		})
	}
}

func TestLoad_InvalidLogLevel(t *testing.T) {
	_, err := config.Load(env(map[string]string{"DATABASE_URL": "postgres://x", "LOG_LEVEL": "verbose"}))
	if err == nil || !strings.Contains(err.Error(), "LOG_LEVEL") {
		t.Fatalf("want error naming LOG_LEVEL, got %v", err)
	}
}

func TestLoad_AggregatesErrors(t *testing.T) {
	_, err := config.Load(env(map[string]string{"DB_MAX_CONNS": "abc", "LOG_LEVEL": "nope"}))
	if err == nil {
		t.Fatal("want error, got nil")
	}
	for _, name := range []string{"DATABASE_URL", "DB_MAX_CONNS", "LOG_LEVEL"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not name %s", err, name)
		}
	}
}

func TestLoad_Overrides(t *testing.T) {
	c, err := config.Load(env(map[string]string{
		"APP_ENV":      "production",
		"HTTP_ADDR":    ":9000",
		"DATABASE_URL": "postgres://y",
		"DB_MAX_CONNS": "25",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Env != "production" || c.HTTPAddr != ":9000" || c.DatabaseURL != "postgres://y" || c.DBMaxConns != 25 {
		t.Errorf("overrides not applied: %+v", c)
	}
}

func TestLoad_NonPositiveMaxConns(t *testing.T) {
	_, err := config.Load(env(map[string]string{"DATABASE_URL": "postgres://x", "DB_MAX_CONNS": "0"}))
	if err == nil || !strings.Contains(err.Error(), "DB_MAX_CONNS") {
		t.Fatalf("want error naming DB_MAX_CONNS, got %v", err)
	}
}
