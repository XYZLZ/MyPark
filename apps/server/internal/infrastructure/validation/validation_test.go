package validation_test

import (
	"errors"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/validation"
)

type createVehicle struct {
	Plate string `json:"plate" validate:"required"`
	Seats int    `json:"seats,omitempty" validate:"min=1"`
	Owner owner  `json:"owner"`
}

type owner struct {
	Email string `json:"email" validate:"required,email"`
}

func TestStruct_ReportsJSONFieldNames(t *testing.T) {
	err := validation.New().Struct(createVehicle{Seats: 0, Owner: owner{Email: "nope"}})

	verr, ok := errors.AsType[*validation.Error](err)
	if !ok {
		t.Fatalf("err = %T %v, want *validation.Error", err, err)
	}
	want := []validation.FieldError{
		{Field: "plate", Rule: "required", Param: ""},
		{Field: "seats", Rule: "min", Param: "1"},
		{Field: "owner.email", Rule: "email", Param: ""},
	}
	if len(verr.Fields) != len(want) {
		t.Fatalf("Fields = %+v, want %+v", verr.Fields, want)
	}
	for i := range want {
		if verr.Fields[i] != want[i] {
			t.Errorf("Fields[%d] = %+v, want %+v", i, verr.Fields[i], want[i])
		}
	}
	if verr.Error() == "" {
		t.Error("Error() is empty")
	}
}

func TestStruct_ValidPasses(t *testing.T) {
	err := validation.New().Struct(createVehicle{Plate: "A123456", Seats: 4, Owner: owner{Email: "a@b.do"}})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
}

func TestStruct_NonStructIsNotAValidationError(t *testing.T) {
	err := validation.New().Struct(42)
	if err == nil {
		t.Fatal("err = nil, want an error for a non-struct value")
	}
	if _, ok := errors.AsType[*validation.Error](err); ok {
		t.Fatalf("non-struct input reported as field errors: %v", err)
	}
}

func TestValidator_PasswordCommonTag(t *testing.T) {
	type dto struct {
		P string `json:"password" validate:"required,min=10,max=128,password_common"`
	}
	err := validation.New().Struct(dto{P: "1234567890"})
	verr, ok := errors.AsType[*validation.Error](err)
	if !ok || len(verr.Fields) != 1 || verr.Fields[0].Rule != "password_common" {
		t.Fatalf("err = %v; want single password_common failure", err)
	}
	if err := validation.New().Struct(dto{P: "Zx9!kelp-orbit"}); err != nil {
		t.Fatalf("strong password rejected: %v", err)
	}
}

func TestValidator_PhoneTagMatchesDBCheck(t *testing.T) {
	type dto struct {
		P string `json:"phone" validate:"omitempty,phone"`
	}
	for phone, valid := range map[string]bool{
		"": true, "+18095551234": true, "+1234567": true, "+123456789012345": true,
		"+0123456789": false, "+123456": false, "+1234567890123456": false,
		"18095551234": false, "+1 809 555 1234": false, " +18095551234": false,
	} {
		err := validation.New().Struct(dto{P: phone})
		if (err == nil) != valid {
			t.Errorf("phone %q: err = %v, want valid=%v", phone, err, valid)
		}
	}
}

func TestLuhn(t *testing.T) {
	for number, want := range map[string]bool{
		"4242424242424242": true, "4242424242424241": false, "": false, "42 42": false, "0": true,
	} {
		if got := validation.Luhn(number); got != want {
			t.Errorf("Luhn(%q) = %v, want %v", number, got, want)
		}
	}
}

func TestCardExpiryValid_LotTimeZone(t *testing.T) {
	// 03:00Z on Dec 1 is still Nov 30 in Santo Domingo.
	if !validation.CardExpiryValid(11, 2026, time.Date(2026, 12, 1, 3, 0, 0, 0, time.UTC)) {
		t.Error("11/2026 rejected on Nov 30 local time")
	}
	if validation.CardExpiryValid(11, 2026, time.Date(2026, 12, 1, 4, 0, 0, 0, time.UTC)) {
		t.Error("11/2026 accepted on Dec 1 local time")
	}
}
