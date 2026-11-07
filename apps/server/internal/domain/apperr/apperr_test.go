package apperr_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/errcodes"
)

func TestApperr_ErrorsAsAndWrap(t *testing.T) {
	cause := errors.New("db down")
	e := apperr.NotFound(errcodes.HTTPNotFound).Wrap(cause)
	wrapped := fmt.Errorf("ctx: %w", e)

	got, ok := errors.AsType[*apperr.Error](wrapped)
	if !ok {
		t.Fatal("AsType did not find *apperr.Error")
	}
	if got.Kind != apperr.KindNotFound || got.Code != errcodes.HTTPNotFound {
		t.Errorf("got kind=%v code=%v", got.Kind, got.Code)
	}
	if !errors.Is(wrapped, cause) || got.Unwrap() != cause {
		t.Error("Unwrap does not return cause")
	}
	if got.Error() == "" {
		t.Error("empty Error()")
	}
}

func TestApperr_RateLimitedRoundsUp(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want int
	}{{1500 * time.Millisecond, 2}, {0, 1}, {time.Second, 1}, {61 * time.Second, 61}}
	for _, c := range cases {
		e := apperr.RateLimited(c.d)
		if e.Kind != apperr.KindRateLimited {
			t.Errorf("kind = %v", e.Kind)
		}
		if got := e.Details["retry_after_seconds"]; got != c.want {
			t.Errorf("RateLimited(%v) retry_after_seconds = %v, want %d", c.d, got, c.want)
		}
	}
}

func TestApperr_WithDetails(t *testing.T) {
	e := apperr.Invalid(errcodes.ValidationFailed).WithDetails(map[string]any{"a": 1})
	if e.Details["a"] != 1 {
		t.Errorf("details = %v", e.Details)
	}
}
