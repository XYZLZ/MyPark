package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/apperr"
	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/auth"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
)

func TestAccounts_VerifyPassword(t *testing.T) {
	e := newEnv(t)
	u := e.insertUser("ana@example.com", "driver", "active", true)
	invitee := e.insertUser("invitee@example.com", "admin", "active", false)
	acc := auth.NewAccounts(e.svc)

	if err := acc.VerifyPassword(t.Context(), u.ID, secret.String("Original-pass-77")); err != nil {
		t.Errorf("correct password: %v", err)
	}
	for name, tc := range map[string]struct {
		id int64
		pw string
	}{
		"wrong password": {u.ID, "Wrong-pass-123"},
		"no password":    {invitee.ID, "anything-at-all"},
		"unknown user":   {u.ID + 9999, "anything-at-all"},
	} {
		err := acc.VerifyPassword(t.Context(), tc.id, secret.String(tc.pw))
		ae, ok := errors.AsType[*apperr.Error](err)
		if !ok || ae.Kind != apperr.KindForbidden || string(ae.Code) != "auth.password_incorrect" {
			t.Errorf("%s: err = %v, want 403 auth.password_incorrect", name, err)
		}
	}
}

func TestAccounts_EraseNeedsATransaction(t *testing.T) {
	e := newEnv(t)
	u := e.insertUser("ana@example.com", "driver", "active", true)
	err := auth.NewAccounts(e.svc).Erase(t.Context(), u.ID)
	if !errors.Is(err, database.ErrNoTx) {
		t.Fatalf("err = %v, want ErrNoTx", err)
	}
	if got := e.text(`select status from users where id = $1`, u.ID); got != "active" {
		t.Errorf("status = %q", got)
	}
}

func TestAccounts_EraseGoneUser(t *testing.T) {
	e := newEnv(t)
	u := e.insertUser("ana@example.com", "driver", "active", true)
	acc := auth.NewAccounts(e.svc)
	tx := database.NewTxRunner(e.db.App)
	if err := tx.InTx(t.Context(), func(ctx context.Context) error { return acc.Erase(ctx, u.ID) }); err != nil {
		t.Fatal(err)
	}
	err := tx.InTx(t.Context(), func(ctx context.Context) error { return acc.Erase(ctx, u.ID) })
	if !errors.Is(err, auth.ErrUserGone) {
		t.Errorf("second erase err = %v, want ErrUserGone", err)
	}
}
