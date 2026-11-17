// Package validation validates decoded client input (request DTOs) with
// go-playground/validator and reports failures by their JSON field names, so
// the HTTP layer can return them to clients as-is.
package validation

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-playground/validator/v10"

	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/hashing"
)

// FieldError describes one failed rule on one field.
type FieldError struct {
	// Field is the JSON path of the field, e.g. "plate" or "owner.email".
	Field string `json:"field"`
	// Rule is the validator tag that failed, e.g. "required" or "min".
	Rule string `json:"rule"`
	// Param is the rule parameter, e.g. "1" for min=1; empty when none.
	Param string `json:"param"`
}

// Error is returned by Validator.Struct when one or more fields are invalid.
type Error struct {
	Fields []FieldError
}

func (e *Error) Error() string {
	parts := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		parts[i] = f.Field + ": " + f.Rule
		if f.Param != "" {
			parts[i] += "=" + f.Param
		}
	}
	return "validation failed: " + strings.Join(parts, ", ")
}

// Validator validates structs using their `validate` tags.
type Validator struct {
	v *validator.Validate
}

// shared is built once: validator.Validate caches struct metadata and is safe
// for concurrent use, so every Validator shares one instance.
// phonePattern is the users.phone CHECK, so a value that passes validation
// can never fail the constraint.
var phonePattern = regexp.MustCompile(`^\+[1-9][0-9]{6,14}$`)

// codePattern is the one-time code of the OpenAPI contract. go-playground's
// numeric also accepts signs and decimals.
var codePattern = regexp.MustCompile(`^[0-9]{6}$`)

var shared = sync.OnceValue(func() *validator.Validate {
	v := validator.New(validator.WithRequiredStructEnabled())
	v.RegisterTagNameFunc(jsonName)
	// password_common fails when the password is in the common-password list.
	_ = v.RegisterValidation("password_common", func(fl validator.FieldLevel) bool {
		return !hashing.IsCommonPassword(fl.Field().String())
	})
	// code6 passes exactly six ASCII digits.
	_ = v.RegisterValidation("code6", func(fl validator.FieldLevel) bool {
		return codePattern.MatchString(fl.Field().String())
	})
	_ = v.RegisterValidation("phone", func(fl validator.FieldLevel) bool {
		return phonePattern.MatchString(fl.Field().String())
	})
	// luhn passes a digits-only string whose Luhn checksum is valid.
	_ = v.RegisterValidation("luhn", func(fl validator.FieldLevel) bool {
		return Luhn(fl.Field().String())
	})
	// card_expiry goes on the expiry-year field and names the month field as
	// its parameter: `card_expiry=ExpMonth`. It reads "now" from the context
	// (WithNow) and fails closed without one. An out-of-range month passes:
	// the month's own rules report it.
	_ = v.RegisterValidationCtx("card_expiry", func(ctx context.Context, fl validator.FieldLevel) bool {
		now, ok := ctx.Value(nowKey{}).(time.Time)
		if !ok {
			return false
		}
		m := fl.Parent().FieldByName(fl.Param())
		if !m.IsValid() || !m.CanInt() {
			return false
		}
		if m.Int() < 1 || m.Int() > 12 {
			return true
		}
		return CardExpiryValid(int(m.Int()), int(fl.Field().Int()), now)
	})
	return v
})

// New returns a Validator backed by the shared, cached validator instance.
func New() *Validator {
	return &Validator{v: shared()}
}

// Struct validates s. It returns nil when s is valid, *Error when one or more
// fields fail their rules, and another error when s cannot be validated at
// all (for example, s is not a struct).
func (v *Validator) Struct(s any) error {
	return v.StructCtx(context.Background(), s)
}

// StructCtx is Struct for structs with time-dependent rules: ctx should carry
// the business time (WithNow).
func (v *Validator) StructCtx(ctx context.Context, s any) error {
	err := v.v.StructCtx(ctx, s)
	if err == nil {
		return nil
	}
	verrs, ok := errors.AsType[validator.ValidationErrors](err)
	if !ok {
		return fmt.Errorf("validation: %w", err)
	}
	out := &Error{Fields: make([]FieldError, len(verrs))}
	for i, fe := range verrs {
		out.Fields[i] = FieldError{Field: fieldPath(fe), Rule: fe.Tag(), Param: fe.Param()}
	}
	return out
}

// fieldPath drops the root struct name from the namespace, which the tag name
// func has already mapped to JSON names: "createVehicle.owner.email" becomes
// "owner.email".
func fieldPath(fe validator.FieldError) string {
	ns := fe.Namespace()
	if _, rest, ok := strings.Cut(ns, "."); ok {
		return rest
	}
	return fe.Field()
}

// jsonName returns the JSON name of a struct field, falling back to the Go
// name when the field has no json tag.
func jsonName(f reflect.StructField) string {
	name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
	switch name {
	case "-":
		return ""
	case "":
		return f.Name
	}
	return name
}

type nowKey struct{}

// WithNow returns a context carrying the business "now" for time-dependent
// rules such as card_expiry. Callers pass their injected clock's reading.
func WithNow(ctx context.Context, now time.Time) context.Context {
	return context.WithValue(ctx, nowKey{}, now)
}

// lotZone is the lot's civil time (America/Santo_Domingo, no DST).
var lotZone = time.FixedZone("America/Santo_Domingo", -4*60*60)

// CardExpiryValid reports whether a card expiring at the end of month/year is
// still valid at now, judged in the lot's time zone: the card works through
// the last day of its expiry month.
func CardExpiryValid(month, year int, now time.Time) bool {
	if month < 1 || month > 12 {
		return false
	}
	n := now.In(lotZone)
	cy, cm := n.Year(), int(n.Month())
	return year > cy || (year == cy && month >= cm)
}

// Luhn reports whether s is a non-empty string of ASCII digits whose Luhn
// checksum is valid. It is the only Luhn implementation (payments delegates).
func Luhn(s string) bool {
	if s == "" {
		return false
	}
	sum, double := 0, false
	for i := len(s) - 1; i >= 0; i-- {
		c := s[i]
		if c < '0' || c > '9' {
			return false
		}
		d := int(c - '0')
		if double {
			if d *= 2; d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return sum%10 == 0
}
