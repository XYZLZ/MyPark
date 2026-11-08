package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strconv"
	"strings"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
)

// Environment variable names read by LoadWorker, besides DATABASE_URL.
const (
	EnvWorkerHealthAddr = "WORKER_HEALTH_ADDR"
	EnvSMTPHost         = "SMTP_HOST"
	EnvSMTPPort         = "SMTP_PORT"
	EnvSMTPFrom         = "SMTP_FROM"
	EnvSMTPUsername     = "SMTP_USERNAME"
	EnvSMTPPassword     = "SMTP_PASSWORD"
	EnvSMTPStartTLS     = "SMTP_STARTTLS"

	// DefaultWorkerHealthAddr is used when WORKER_HEALTH_ADDR is unset.
	DefaultWorkerHealthAddr = ":8091"
)

// WorkerConfig is the validated configuration of cmd/worker. Build it with
// LoadWorker; the zero value is not valid.
type WorkerConfig struct {
	databaseURL  string
	healthAddr   string
	smtpHost     string
	smtpPort     int
	smtpFrom     string
	smtpUsername secret.String
	smtpPassword secret.String
	smtpStartTLS bool
}

// DatabaseURL returns the Postgres URL. It holds credentials: never log it.
func (c WorkerConfig) DatabaseURL() string { return c.databaseURL }

// HealthAddr returns the listen address of the health endpoints.
func (c WorkerConfig) HealthAddr() string { return c.healthAddr }

// SMTPHost returns the SMTP server host.
func (c WorkerConfig) SMTPHost() string { return c.smtpHost }

// SMTPPort returns the SMTP server port.
func (c WorkerConfig) SMTPPort() int { return c.smtpPort }

// SMTPFrom returns the From address, e.g. "MyPark <no-reply@mypark.local>".
func (c WorkerConfig) SMTPFrom() string { return c.smtpFrom }

// SMTPUsername returns the SMTP login, empty for none.
func (c WorkerConfig) SMTPUsername() secret.String { return c.smtpUsername }

// SMTPPassword returns the SMTP password, empty for none.
func (c WorkerConfig) SMTPPassword() secret.String { return c.smtpPassword }

// SMTPStartTLS reports whether the connection is upgraded with STARTTLS.
func (c WorkerConfig) SMTPStartTLS() bool { return c.smtpStartTLS }

// String renders the redacted LogValue group, so %v and %s never print
// credentials.
func (c WorkerConfig) String() string { return c.LogValue().String() }

// GoString renders the same redacted group for %#v.
func (c WorkerConfig) GoString() string { return c.String() }

// LogValue implements slog.LogValuer without credentials.
func (c WorkerConfig) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("database_url", redactURL(c.databaseURL)),
		slog.String("health_addr", c.healthAddr),
		slog.String("smtp_host", c.smtpHost),
		slog.Int("smtp_port", c.smtpPort),
		slog.Bool("smtp_auth", c.smtpUsername != ""),
		slog.Bool("smtp_starttls", c.smtpStartTLS),
	)
}

// LoadWorker builds a WorkerConfig using getenv. DATABASE_URL, SMTP_HOST,
// SMTP_PORT and SMTP_FROM are required (compose supplies mailpit, 1025 and
// "MyPark <no-reply@mypark.local>"). Every problem is reported at once, naming
// the variable and never echoing a value.
func LoadWorker(getenv func(string) string) (WorkerConfig, error) {
	var errs []error
	c := WorkerConfig{}

	url, err := DatabaseURLFrom(getenv, EnvDatabaseURL)
	errs = append(errs, err)
	c.databaseURL = url

	addr, err := ListenAddrFrom(getenv, EnvWorkerHealthAddr, DefaultWorkerHealthAddr)
	errs = append(errs, err)
	c.healthAddr = addr

	c.smtpHost = strings.TrimSpace(getenv(EnvSMTPHost))
	if c.smtpHost == "" || strings.ContainsAny(c.smtpHost, " \t/") {
		errs = append(errs, fmt.Errorf("%s is required and must be a host name", EnvSMTPHost))
	}

	switch raw := strings.TrimSpace(getenv(EnvSMTPPort)); raw {
	case "":
		errs = append(errs, fmt.Errorf("%s is required", EnvSMTPPort))
	default:
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 65535 {
			errs = append(errs, fmt.Errorf("%s must be an integer between 1 and 65535", EnvSMTPPort))
		}
		c.smtpPort = n
	}

	c.smtpFrom = strings.TrimSpace(getenv(EnvSMTPFrom))
	if c.smtpFrom == "" {
		errs = append(errs, fmt.Errorf("%s is required", EnvSMTPFrom))
	} else if _, err := mail.ParseAddress(c.smtpFrom); err != nil {
		errs = append(errs, fmt.Errorf("%s must be an email address, e.g. MyPark <no-reply@mypark.local>", EnvSMTPFrom))
	}

	user, pass := getenv(EnvSMTPUsername), getenv(EnvSMTPPassword)
	if (user == "") != (pass == "") {
		errs = append(errs, fmt.Errorf("%s and %s must be set together", EnvSMTPUsername, EnvSMTPPassword))
	}
	c.smtpUsername, c.smtpPassword = secret.String(user), secret.String(pass)

	if raw := strings.TrimSpace(getenv(EnvSMTPStartTLS)); raw != "" {
		b, err := strconv.ParseBool(raw)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s must be true or false", EnvSMTPStartTLS))
		}
		c.smtpStartTLS = b
	}

	if err := errors.Join(errs...); err != nil {
		return WorkerConfig{}, fmt.Errorf("invalid configuration: %w", err)
	}
	return c, nil
}
