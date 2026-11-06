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
	if c.HTTPAddr() != ":8080" {
		t.Errorf("HTTPAddr = %q, want %q", c.HTTPAddr(), ":8080")
	}
	if c.DBMaxConns() != 10 {
		t.Errorf("DBMaxConns = %d, want 10", c.DBMaxConns())
	}
	if c.LogLevel() != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want %v", c.LogLevel(), slog.LevelInfo)
	}
	if c.Env() != "development" {
		t.Errorf("Env = %q, want %q", c.Env(), "development")
	}
	if c.DatabaseURL() != "postgres://x" {
		t.Errorf("DatabaseURL = %q, want %q", c.DatabaseURL(), "postgres://x")
	}
	if len(c.CORSAllowedOrigins()) != 0 {
		t.Errorf("CORSAllowedOrigins = %v, want empty", c.CORSAllowedOrigins())
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
	if len(c.CORSAllowedOrigins()) != len(want) {
		t.Fatalf("CORSAllowedOrigins = %q, want %q", c.CORSAllowedOrigins(), want)
	}
	for i := range want {
		if c.CORSAllowedOrigins()[i] != want[i] {
			t.Errorf("CORSAllowedOrigins[%d] = %q, want %q", i, c.CORSAllowedOrigins()[i], want[i])
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
			if c.LogLevel() != tt.want {
				t.Errorf("LogLevel = %v, want %v", c.LogLevel(), tt.want)
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
	if c.Env() != "production" || c.HTTPAddr() != ":9000" || c.DatabaseURL() != "postgres://y" || c.DBMaxConns() != 25 {
		t.Errorf("overrides not applied: %+v", c)
	}
}

func TestLoad_NonPositiveMaxConns(t *testing.T) {
	_, err := config.Load(env(map[string]string{"DATABASE_URL": "postgres://x", "DB_MAX_CONNS": "0"}))
	if err == nil || !strings.Contains(err.Error(), "DB_MAX_CONNS") {
		t.Fatalf("want error naming DB_MAX_CONNS, got %v", err)
	}
}

func TestLoad_MaxConnsAboveLimit(t *testing.T) {
	_, err := config.Load(env(map[string]string{"DATABASE_URL": "postgres://x", "DB_MAX_CONNS": "1001"}))
	if err == nil || !strings.Contains(err.Error(), "DB_MAX_CONNS") {
		t.Fatalf("want error naming DB_MAX_CONNS, got %v", err)
	}
}

func TestLoad_InvalidDatabaseURL(t *testing.T) {
	_, err := config.Load(env(map[string]string{"DATABASE_URL": "not a url"}))
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("want error naming DATABASE_URL, got %v", err)
	}
	if strings.Contains(err.Error(), "not a url") {
		t.Errorf("error must not echo the DATABASE_URL value: %v", err)
	}
}

func TestLoad_InvalidAppEnv(t *testing.T) {
	_, err := config.Load(env(map[string]string{"DATABASE_URL": "postgres://x", "APP_ENV": "staging"}))
	if err == nil || !strings.Contains(err.Error(), "APP_ENV") {
		t.Fatalf("want error naming APP_ENV, got %v", err)
	}
}

func TestLoad_HTTPAddr(t *testing.T) {
	for _, addr := range []string{":8080", "127.0.0.1:0", "localhost:8080", "[::1]:8080", "0.0.0.0:80"} {
		t.Run("valid "+addr, func(t *testing.T) {
			c, err := config.Load(env(map[string]string{"DATABASE_URL": "postgres://x", "HTTP_ADDR": addr}))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if c.HTTPAddr() != addr {
				t.Errorf("HTTPAddr = %q, want %q", c.HTTPAddr(), addr)
			}
		})
	}
	for _, addr := range []string{"8080", "host:", ":http", ":70000", "bad host:80"} {
		t.Run("invalid "+addr, func(t *testing.T) {
			_, err := config.Load(env(map[string]string{"DATABASE_URL": "postgres://x", "HTTP_ADDR": addr}))
			if err == nil || !strings.Contains(err.Error(), "HTTP_ADDR") {
				t.Fatalf("want error naming HTTP_ADDR, got %v", err)
			}
		})
	}
}

func TestConfig_CORSAllowedOriginsReturnsCopy(t *testing.T) {
	c, err := config.Load(env(map[string]string{"DATABASE_URL": "postgres://x", "CORS_ALLOWED_ORIGINS": "http://a"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c.CORSAllowedOrigins()[0] = "mutated"
	if got := c.CORSAllowedOrigins()[0]; got != "http://a" {
		t.Fatalf("CORSAllowedOrigins exposes internal slice: got %q", got)
	}
}

func TestConfig_LogValueRedactsPassword(t *testing.T) {
	tests := []struct {
		name   string
		url    string
		leaked string // must not appear in the log
		want   string // redacted URL that must appear
	}{
		{
			name:   "userinfo password",
			url:    "postgres://mypark:s3cret@db:5432/mypark?sslmode=disable",
			leaked: "s3cret",
			want:   "postgres://mypark:xxxxx@db:5432/mypark?sslmode=disable",
		},
		{
			name:   "password query param",
			url:    "postgres://mypark@db:5432/mypark?password=s3cret",
			leaked: "s3cret",
			want:   "postgres://mypark@db:5432/mypark?password=xxxxx",
		},
		{
			name:   "uppercase key keeps other params and order",
			url:    "postgres://mypark@db/mypark?PASSWORD=s3cret&sslmode=disable",
			leaked: "s3cret",
			want:   "postgres://mypark@db/mypark?PASSWORD=xxxxx&sslmode=disable",
		},
		{
			name:   "sslpassword query param",
			url:    "postgres://mypark@db/mypark?sslmode=verify-full&sslpassword=k",
			leaked: "sslpassword=k",
			want:   "postgres://mypark@db/mypark?sslmode=verify-full&sslpassword=xxxxx",
		},
		{
			name:   "both userinfo and query password",
			url:    "postgres://mypark:s3cret@db/mypark?password=s3cret2",
			leaked: "s3cret",
			want:   "postgres://mypark:xxxxx@db/mypark?password=xxxxx",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := config.Load(env(map[string]string{"DATABASE_URL": tt.url}))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			var buf strings.Builder
			slog.New(slog.NewTextHandler(&buf, nil)).Info("start", "config", c)
			out := buf.String()
			if strings.Contains(out, tt.leaked) {
				t.Fatalf("log leaks %q: %s", tt.leaked, out)
			}
			if !strings.Contains(out, `config.database_url="`+tt.want+`"`) {
				t.Errorf("log = %s, want database_url=%s", out, tt.want)
			}
			if !strings.Contains(out, "config.http_addr=:8080") || !strings.Contains(out, "config.env=development") {
				t.Errorf("log = %s, want the other fields kept", out)
			}
		})
	}
}

func TestDatabaseURLFrom(t *testing.T) {
	const name = "MIGRATOR_DATABASE_URL"
	tests := []struct {
		desc    string
		value   string
		wantErr string
	}{
		{desc: "missing", value: "", wantErr: name + " is required"},
		{desc: "blank", value: "   ", wantErr: name + " is required"},
		{desc: "not a URL", value: "s3cret-not-a-url", wantErr: name + " must be a valid URL"},
		{desc: "valid", value: " postgres://m:s3cret@db:5432/mypark "},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got, err := config.DatabaseURLFrom(env(map[string]string{name: tt.value}), name)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				if want := strings.TrimSpace(tt.value); got != want {
					t.Errorf("url = %q, want %q", got, want)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want one containing %q", err, tt.wantErr)
			}
			if strings.Contains(err.Error(), "s3cret") {
				t.Errorf("error echoes the secret value: %v", err)
			}
		})
	}
}

func TestListenAddrFrom(t *testing.T) {
	const name = "WORKER_HEALTH_ADDR"
	for _, tc := range []struct {
		value, want string
	}{
		{"", ":8091"},
		{"  ", ":8091"},
		{"127.0.0.1:0", "127.0.0.1:0"},
		{":9000", ":9000"},
	} {
		got, err := config.ListenAddrFrom(env(map[string]string{name: tc.value}), name, ":8091")
		if err != nil || got != tc.want {
			t.Errorf("ListenAddrFrom(%q) = %q, %v; want %q", tc.value, got, err, tc.want)
		}
	}
	for _, value := range []string{"nope", ":99999", "host :80"} {
		_, err := config.ListenAddrFrom(env(map[string]string{name: value}), name, ":8091")
		if err == nil || !strings.Contains(err.Error(), name+" must be host:port or :port") {
			t.Errorf("ListenAddrFrom(%q) error = %v, want one naming %s", value, err, name)
		}
	}
}
