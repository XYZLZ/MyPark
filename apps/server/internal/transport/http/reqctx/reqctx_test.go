package reqctx_test

import (
	"context"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/reqctx"
)

func TestRequestID_RoundTrip(t *testing.T) {
	if got := reqctx.RequestID(reqctx.WithRequestID(t.Context(), "r1")); got != "r1" {
		t.Errorf("RequestID = %q, want r1", got)
	}
	if got := reqctx.RequestID(context.Background()); got != "" {
		t.Errorf("RequestID(empty) = %q, want empty", got)
	}
}

func TestLogFields_PointerSharedThroughContext(t *testing.T) {
	ctx, f := reqctx.WithLogFields(t.Context())
	reqctx.Fields(ctx).UserPublicID = "u1"
	if f.UserPublicID != "u1" {
		t.Errorf("UserPublicID via returned pointer = %q, want u1", f.UserPublicID)
	}
}

func TestFields_NilWhenAbsent(t *testing.T) {
	if reqctx.Fields(t.Context()) != nil {
		t.Error("Fields on a bare context should be nil")
	}
}
