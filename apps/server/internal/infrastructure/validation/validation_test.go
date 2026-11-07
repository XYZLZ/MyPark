package validation_test

import (
	"errors"
	"testing"

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
