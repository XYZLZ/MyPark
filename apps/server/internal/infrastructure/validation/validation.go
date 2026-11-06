// Package validation validates decoded client input (request DTOs) with
// go-playground/validator and reports failures by their JSON field names, so
// the HTTP layer can return them to clients as-is.
package validation

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/go-playground/validator/v10"
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
var shared = sync.OnceValue(func() *validator.Validate {
	v := validator.New(validator.WithRequiredStructEnabled())
	v.RegisterTagNameFunc(jsonName)
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
	err := v.v.Struct(s)
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
