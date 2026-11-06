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

	var verr *validation.Error
	if !errors.As(err, &verr) {
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
	var verr *validation.Error
	if errors.As(err, &verr) {
		t.Fatalf("non-struct input reported as field errors: %v", err)
	}
}
