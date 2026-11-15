package config

import (
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
)

// Simulator variable names, besides SIMULATOR_FLEET_SECRET (seed.go).
const (
	EnvSimulatorAddr         = "SIMULATOR_ADDR"
	EnvSimulatorAPIURL       = "SIMULATOR_API_URL"
	EnvSimulatorControlToken = "SIMULATOR_CONTROL_TOKEN"

	DefaultSimulatorAddr   = ":8090"
	DefaultSimulatorAPIURL = "http://api:8080"

	// minControlTokenLen is the shortest control token accepted, in bytes.
	minControlTokenLen = 32
)

// SimulatorConfig is the validated configuration of cmd/simulator. Build it
// with LoadSimulator; the zero value is not valid. String, GoString and
// LogValue never print the fleet secret or the control token.
type SimulatorConfig struct {
	env    string
	addr   string
	apiURL string
	fleet  secret.Bytes
	token  secret.String
}

// Env returns APP_ENV.
func (c SimulatorConfig) Env() string { return c.env }

// Addr returns the listen address of the probes and the control API.
func (c SimulatorConfig) Addr() string { return c.addr }

// APIURL returns the API origin the simulated devices call.
func (c SimulatorConfig) APIURL() string { return c.apiURL }

// FleetSecret returns a copy of the secret every device secret derives from.
func (c SimulatorConfig) FleetSecret() secret.Bytes { return slices.Clone(c.fleet) }

// ControlToken returns the control API's shared secret.
func (c SimulatorConfig) ControlToken() secret.String { return c.token }

// String renders the redacted LogValue group.
func (c SimulatorConfig) String() string { return c.LogValue().String() }

// GoString renders the same redacted group for %#v.
func (c SimulatorConfig) GoString() string { return c.String() }

// LogValue implements slog.LogValuer without the secrets.
func (c SimulatorConfig) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("env", c.env),
		slog.String("addr", c.addr),
		slog.String("api_url", redactURL(c.apiURL)),
		slog.String("fleet_secret", redacted),
		slog.String("control_token", redacted),
	)
}

// LoadSimulator reads the simulator's variables. SIMULATOR_FLEET_SECRET
// follows the seed's rule (base64 of at least 32 bytes). The control token
// is used exactly as written (no decoding, no trimming: surrounding
// whitespace is an error) and must be at least 32 bytes. With
// APP_ENV=production the committed dev values of both are refused. Every
// problem is reported at once, naming the variable and never its value.
func LoadSimulator(getenv func(string) string) (SimulatorConfig, error) {
	var errs []error
	c := SimulatorConfig{env: valueOr(getenv(EnvAppEnv), DefaultEnv)}
	if err := envValidator().Var(c.env, "oneof=development test production"); err != nil {
		errs = append(errs, fmt.Errorf("%s must be one of development|test|production", EnvAppEnv))
	}
	production := c.env == "production"

	addr, err := ListenAddrFrom(getenv, EnvSimulatorAddr, DefaultSimulatorAddr)
	errs = append(errs, err)
	c.addr = addr

	// Without a trailing slash, so c.apiURL + "/readyz" is never "//readyz".
	c.apiURL = strings.TrimRight(valueOr(getenv(EnvSimulatorAPIURL), DefaultSimulatorAPIURL), "/")
	if err := envValidator().Var(c.apiURL, "http_url"); err != nil {
		errs = append(errs, fmt.Errorf("%s must be an http(s) URL", EnvSimulatorAPIURL))
	}

	fleet, err := loadSecretVar(getenv, EnvSimulatorFleetSecret, DevSimulatorFleetSecret, production)
	errs = append(errs, err)
	c.fleet = fleet

	token := getenv(EnvSimulatorControlToken)
	switch {
	case token == "":
		errs = append(errs, fmt.Errorf("%s is required", EnvSimulatorControlToken))
	case strings.TrimSpace(token) != token:
		// A padded value is a copy-paste slip; trimming it would hide which
		// token the BFF must send.
		errs = append(errs, fmt.Errorf("%s must not have leading or trailing whitespace", EnvSimulatorControlToken))
	case len(token) < minControlTokenLen:
		errs = append(errs, fmt.Errorf("%s must be at least %d bytes", EnvSimulatorControlToken, minControlTokenLen))
	case production && token == DevSimulatorControlToken:
		errs = append(errs, fmt.Errorf("%s must not be the development default in production", EnvSimulatorControlToken))
	}
	c.token = secret.String(token)

	if err := errors.Join(errs...); err != nil {
		return SimulatorConfig{}, fmt.Errorf("invalid simulator configuration: %w", err)
	}
	return c, nil
}
