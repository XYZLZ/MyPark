package config

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"

	"github.com/go-playground/validator/v10"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/secretbox"
)

// Seed admin variable names (spec 13.1, OC-20).
const (
	EnvSeedAdminEmail     = "SEED_ADMIN_EMAIL"
	EnvSeedAdminFirstName = "SEED_ADMIN_FIRST_NAME"
	EnvSeedAdminLastName  = "SEED_ADMIN_LAST_NAME"
	EnvSeedAdminPassword  = "SEED_ADMIN_PASSWORD"
)

// SeedAdmin is the first admin cmd/seed creates. Build it with LoadSeedAdmin.
// String, GoString and LogValue are redacted: it holds an email, names and a
// password.
type SeedAdmin struct {
	email, firstName, lastName string
	password                   secret.String
}

// Email returns the normalized (trimmed, lowercase) address.
func (s SeedAdmin) Email() string { return s.email }

// FirstName returns the trimmed first name.
func (s SeedAdmin) FirstName() string { return s.firstName }

// LastName returns the trimmed last name.
func (s SeedAdmin) LastName() string { return s.lastName }

// Password returns the initial password. It is a secret.String so it cannot
// be printed by accident.
func (s SeedAdmin) Password() secret.String { return s.password }

// String renders a redacted value, so %v, %+v and %s never print credentials.
func (SeedAdmin) String() string { return "SeedAdmin{" + redacted + "}" }

// GoString renders the same redacted value for %#v.
func (s SeedAdmin) GoString() string { return s.String() }

// LogValue implements slog.LogValuer.
func (SeedAdmin) LogValue() slog.Value { return slog.StringValue(redacted) }

// rawSeed holds the seed variables after trimming, before validation.
type rawSeed struct {
	Email     string `env:"SEED_ADMIN_EMAIL" validate:"required,email,max=254"`
	FirstName string `env:"SEED_ADMIN_FIRST_NAME" validate:"required,min=1,max=60"`
	LastName  string `env:"SEED_ADMIN_LAST_NAME" validate:"required,min=1,max=80"`
	Password  string `env:"SEED_ADMIN_PASSWORD" validate:"required"`
}

// LoadSeedAdmin reads the first-admin variables. It returns ok=false with no
// error when SEED_ADMIN_EMAIL is unset, so the caller skips the admin seed
// with a warning. Once the email is set the rest must be valid, and with
// APP_ENV=production the public development password is refused. Errors name
// variables and never echo a value. Password policy (length, common list) is
// enforced where the account is created.
func LoadSeedAdmin(getenv func(string) string) (SeedAdmin, bool, error) {
	email := strings.ToLower(strings.TrimSpace(getenv(EnvSeedAdminEmail)))
	if email == "" {
		return SeedAdmin{}, false, nil
	}
	raw := rawSeed{
		Email:     email,
		FirstName: strings.TrimSpace(getenv(EnvSeedAdminFirstName)),
		LastName:  strings.TrimSpace(getenv(EnvSeedAdminLastName)),
		Password:  getenv(EnvSeedAdminPassword),
	}
	var errs []error
	if err := envValidator().Struct(raw); err != nil {
		verrs, ok := errors.AsType[validator.ValidationErrors](err)
		if !ok {
			return SeedAdmin{}, false, fmt.Errorf("invalid seed configuration: %w", err)
		}
		for _, fe := range verrs {
			// Every seed variable can hold PII or a secret: never echo one.
			errs = append(errs, envError(fe.Field(), true, fe))
		}
	}
	// Compare the plain value, not a decoded form: the password is not base64.
	// A padded copy of the dev password is the same public value.
	if valueOr(getenv(EnvAppEnv), DefaultEnv) == "production" && strings.TrimSpace(raw.Password) == DevSeedAdminPassword {
		errs = append(errs, fmt.Errorf("%s must not be the development default in production", EnvSeedAdminPassword))
	}
	if len(errs) > 0 {
		return SeedAdmin{}, false, fmt.Errorf("invalid seed configuration: %w", errors.Join(errs...))
	}
	return SeedAdmin{
		email: raw.Email, firstName: raw.FirstName, lastName: raw.LastName,
		password: secret.String(raw.Password),
	}, true, nil
}

// Simulator fleet seed variable names (design M2C-25).
const (
	EnvSeedSimulatorFleet   = "SEED_SIMULATOR_FLEET"
	EnvSimulatorFleetSecret = "SIMULATOR_FLEET_SECRET"
)

// SeedFleet is the configuration cmd/seed needs to provision the simulator
// fleet: the secretbox keyring that seals the device secrets, the card UID
// pepper and the fleet secret the device secrets derive from. When the fleet
// seed is disabled only Enabled is meaningful. String, GoString and LogValue
// are redacted.
type SeedFleet struct {
	enabled bool
	current secretbox.Key
	old     []secretbox.Key
	pepper  []byte
	fleet   secret.Bytes
}

// Enabled reports whether SEED_SIMULATOR_FLEET is true.
func (s SeedFleet) Enabled() bool { return s.enabled }

// SecretboxKeys returns the key that seals new secrets and the rotated-out keys.
func (s SeedFleet) SecretboxKeys() (current secretbox.Key, old []secretbox.Key) {
	return s.current, slices.Clone(s.old)
}

// CardUIDPepper returns a copy of the card UID pepper. Never log it.
func (s SeedFleet) CardUIDPepper() []byte { return slices.Clone(s.pepper) }

// FleetSecret returns a copy of the fleet secret as a redacting type.
func (s SeedFleet) FleetSecret() secret.Bytes { return slices.Clone(s.fleet) }

// String renders a redacted value, so %v, %+v and %s never print key material.
func (SeedFleet) String() string { return "SeedFleet{" + redacted + "}" }

// GoString renders the same redacted value for %#v.
func (s SeedFleet) GoString() string { return s.String() }

// LogValue implements slog.LogValuer.
func (SeedFleet) LogValue() slog.Value { return slog.StringValue(redacted) }

// LoadSeedFleet reads the simulator fleet seed variables. With
// SEED_SIMULATOR_FLEET unset or false it returns a disabled SeedFleet and
// ignores every other variable. When true it requires the secretbox keyring,
// CARD_UID_PEPPER and SIMULATOR_FLEET_SECRET (base64, at least 32 bytes each,
// the pepper rule), and with APP_ENV=production it refuses the committed dev
// values. Errors name variables and never include a value.
func LoadSeedFleet(getenv func(string) string) (SeedFleet, error) {
	flag := strings.TrimSpace(getenv(EnvSeedSimulatorFleet))
	if flag == "" {
		return SeedFleet{}, nil
	}
	enabled, err := strconv.ParseBool(flag)
	if err != nil {
		return SeedFleet{}, fmt.Errorf("%s must be true or false", EnvSeedSimulatorFleet)
	}
	if !enabled {
		return SeedFleet{}, nil
	}
	production := valueOr(getenv(EnvAppEnv), DefaultEnv) == "production"
	var errs []error
	current, old, err := ParseSecretboxKeys(getenv(EnvSecretboxKeyID), getenv(EnvSecretboxKey), getenv(EnvSecretboxOldKeys), production)
	if err != nil {
		errs = append(errs, err)
	}
	pepper, err := loadSecretVar(getenv, EnvCardUIDPepper, DevCardUIDPepper, production)
	if err != nil {
		errs = append(errs, err)
	}
	fleet, err := loadSecretVar(getenv, EnvSimulatorFleetSecret, DevSimulatorFleetSecret, production)
	if err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return SeedFleet{}, fmt.Errorf("invalid seed configuration: %w", errors.Join(errs...))
	}
	return SeedFleet{enabled: true, current: current, old: old, pepper: pepper, fleet: secret.Bytes(fleet)}, nil
}

// loadSecretVar reads a required base64 variable of at least minPepperLen
// bytes and refuses the committed dev value in production.
func loadSecretVar(getenv func(string) string, name, dev string, production bool) ([]byte, error) {
	raw := strings.TrimSpace(getenv(name))
	if raw == "" {
		return nil, fmt.Errorf("%s is required", name)
	}
	b, err := decodeKey(name, raw, minPepperLen, false)
	if err != nil {
		return nil, err
	}
	if production && bytes.Equal(b, devBytes(dev)) {
		return nil, fmt.Errorf("%s must not be the development default in production", name)
	}
	return b, nil
}

// EnvSeedSimulatorDrivers enables the simulated driver seed (M3B-44).
const EnvSeedSimulatorDrivers = "SEED_SIMULATOR_DRIVERS"

// SeedDrivers is the configuration cmd/seed needs to provision the simulated
// drivers: the fleet secret their passwords derive from. When the seed is
// disabled only Enabled is meaningful. String, GoString and LogValue are
// redacted.
type SeedDrivers struct {
	enabled bool
	fleet   secret.Bytes
}

// Enabled reports whether SEED_SIMULATOR_DRIVERS is true.
func (s SeedDrivers) Enabled() bool { return s.enabled }

// FleetSecret returns a copy of the fleet secret as a redacting type.
func (s SeedDrivers) FleetSecret() secret.Bytes { return slices.Clone(s.fleet) }

// String renders a redacted value, so %v, %+v and %s never print the secret.
func (SeedDrivers) String() string { return "SeedDrivers{" + redacted + "}" }

// GoString renders the same redacted value for %#v.
func (s SeedDrivers) GoString() string { return s.String() }

// LogValue implements slog.LogValuer.
func (SeedDrivers) LogValue() slog.Value { return slog.StringValue(redacted) }

// LoadSeedDrivers reads the simulated driver seed variables. With
// SEED_SIMULATOR_DRIVERS unset or false it returns a disabled value and
// ignores every other variable. When true it requires SIMULATOR_FLEET_SECRET
// under the fleet seed's rule, and refuses the committed dev value when
// APP_ENV=production. Errors name variables and never include a value.
func LoadSeedDrivers(getenv func(string) string) (SeedDrivers, error) {
	flag := strings.TrimSpace(getenv(EnvSeedSimulatorDrivers))
	if flag == "" {
		return SeedDrivers{}, nil
	}
	enabled, err := strconv.ParseBool(flag)
	if err != nil {
		return SeedDrivers{}, fmt.Errorf("%s must be true or false", EnvSeedSimulatorDrivers)
	}
	if !enabled {
		return SeedDrivers{}, nil
	}
	production := valueOr(getenv(EnvAppEnv), DefaultEnv) == "production"
	fleet, err := loadSecretVar(getenv, EnvSimulatorFleetSecret, DevSimulatorFleetSecret, production)
	if err != nil {
		return SeedDrivers{}, fmt.Errorf("invalid seed configuration: %w", err)
	}
	return SeedDrivers{enabled: true, fleet: secret.Bytes(fleet)}, nil
}
