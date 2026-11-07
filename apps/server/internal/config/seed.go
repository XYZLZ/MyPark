package config

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/go-playground/validator/v10"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
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
