package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/notifications"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/notifications/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

func sample(key string, uid *int64) notifications.Email {
	return notifications.Email{
		Template: notifications.TplVerifyEmail, Locale: "es-DO", To: "Ana@x.do", UserID: uid,
		Data: map[string]string{"code": "123456", "first_name": "Ana"}, DedupeKey: key,
	}
}

func TestEnqueue_FailsOutsideTx(t *testing.T) {
	err := postgres.NewNotifier().Enqueue(t.Context(), sample("k", nil))
	if !errors.Is(err, database.ErrNoTx) {
		t.Fatalf("err = %v, want ErrNoTx", err)
	}
}

func TestScrubPendingForUser_FailsOutsideTx(t *testing.T) {
	err := postgres.NewNotifier().ScrubPendingForUser(t.Context(), 1)
	if !errors.Is(err, database.ErrNoTx) {
		t.Fatalf("err = %v, want ErrNoTx", err)
	}
}

func TestEnqueue_StoresRowAndRollsBack(t *testing.T) {
	db := testsupport.NewTestDB(t)
	runner := database.NewTxRunner(db.App)
	uid := insertUserEmail(t, db, "a@x.do")
	if err := runner.InTx(t.Context(), func(ctx context.Context) error {
		return postgres.NewNotifier().Enqueue(ctx, sample("verify_email:1", &uid))
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	var kind, tpl, locale, to, status string
	var payload []byte
	var userID int64
	var attempts int16
	if err := db.Super.QueryRow(t.Context(), `select kind, template, locale, to_address, user_id, payload, status, attempts
		from outbox_messages where dedupe_key = 'verify_email:1'`).
		Scan(&kind, &tpl, &locale, &to, &userID, &payload, &status, &attempts); err != nil {
		t.Fatalf("select: %v", err)
	}
	if kind != "email" || tpl != "verify_email" || locale != "es-DO" || to != "Ana@x.do" || userID != uid || status != "pending" || attempts != 0 {
		t.Errorf("row = %s %s %s %s %d %s %d", kind, tpl, locale, to, userID, status, attempts)
	}
	var data map[string]string
	if err := json.Unmarshal(payload, &data); err != nil || data["code"] != "123456" || data["first_name"] != "Ana" {
		t.Errorf("payload = %s (%v)", payload, err)
	}

	errRollback := errors.New("rollback")
	err := runner.InTx(t.Context(), func(ctx context.Context) error {
		if err := postgres.NewNotifier().Enqueue(ctx, sample("verify_email:2", nil)); err != nil {
			return err
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("err = %v", err)
	}
	assertCount(t, db, `dedupe_key = 'verify_email:2'`, 0)
}

func TestEnqueue_DedupeKeyIsIdempotent(t *testing.T) {
	db := testsupport.NewTestDB(t)
	runner := database.NewTxRunner(db.App)
	for range 2 {
		if err := runner.InTx(t.Context(), func(ctx context.Context) error {
			return postgres.NewNotifier().Enqueue(ctx, sample("same", nil))
		}); err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
	}
	assertCount(t, db, `dedupe_key = 'same'`, 1)
}

func TestEnqueue_NilDataStoresEmptyObject(t *testing.T) {
	db := testsupport.NewTestDB(t)
	runner := database.NewTxRunner(db.App)
	e := sample("nil-data", nil)
	e.Data = nil
	if err := runner.InTx(t.Context(), func(ctx context.Context) error {
		return postgres.NewNotifier().Enqueue(ctx, e)
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	assertCount(t, db, `dedupe_key = 'nil-data' and payload = '{}'::jsonb`, 1)
}

func TestEnqueue_ErrorDoesNotLeakAddressOrCode(t *testing.T) {
	db := testsupport.NewTestDB(t)
	runner := database.NewTxRunner(db.App)
	e := sample("bad", nil)
	e.Locale = "fr"
	e.To = "leaky.person@x.do"
	err := runner.InTx(t.Context(), func(ctx context.Context) error {
		return postgres.NewNotifier().Enqueue(ctx, e)
	})
	if err == nil {
		t.Fatal("want error for unknown locale")
	}
	for _, secret := range []string{"leaky.person", "123456"} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error leaks %q: %v", secret, err)
		}
	}
}

func TestScrubPendingForUser_RemovesCodeOnly(t *testing.T) {
	db := testsupport.NewTestDB(t)
	runner := database.NewTxRunner(db.App)
	uid := insertUserEmail(t, db, "a@x.do")
	other := insertUserEmail(t, db, "b@x.do")
	const ins = `insert into outbox_messages (kind, template, locale, to_address, user_id, payload, dedupe_key, status)
		values ('email', 'verify_email', 'es-DO', 'a@x.do', $1, '{"code":"123456","first_name":"Ana"}', $2, $3)`
	for _, r := range []struct {
		uid         int64
		key, status string
	}{{uid, "p", "pending"}, {uid, "s", "sending"}, {uid, "sent", "sent"}, {other, "other", "pending"}} {
		if _, err := db.Super.Exec(t.Context(), ins, r.uid, r.key, r.status); err != nil {
			t.Fatalf("seed %s: %v", r.key, err)
		}
	}
	if err := runner.InTx(t.Context(), func(ctx context.Context) error {
		return postgres.NewNotifier().ScrubPendingForUser(ctx, uid)
	}); err != nil {
		t.Fatalf("Scrub: %v", err)
	}
	assertCount(t, db, `dedupe_key in ('p','s') and not (payload ? 'code')`, 2)
	assertCount(t, db, `dedupe_key = 'p' and payload = '{"first_name":"Ana"}'::jsonb`, 1)
	assertCount(t, db, `dedupe_key in ('sent','other') and (payload ? 'code')`, 2)
}

func assertCount(t *testing.T, db *testsupport.TestDB, where string, want int) {
	t.Helper()
	var n int
	if err := db.Super.QueryRow(t.Context(), `select count(*) from outbox_messages where `+where).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != want {
		t.Errorf("count(%s) = %d, want %d", where, n, want)
	}
}

func insertUserEmail(t *testing.T, db *testsupport.TestDB, email string) int64 {
	t.Helper()
	var id int64
	if err := db.App.QueryRow(t.Context(),
		`insert into users (email, first_name, last_name) values ($1, 'Ana', 'P') returning id`, email).Scan(&id); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return id
}
