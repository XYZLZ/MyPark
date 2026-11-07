package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"strings"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/audit"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/audit/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
	"github.com/XYZLZ/MyPark/apps/server/internal/transport/http/reqctx"
)

var errRollback = errors.New("rollback")

func TestRecord_FailsOutsideTx(t *testing.T) {
	err := postgres.NewRecorder().Record(t.Context(), audit.Entry{Action: audit.AdminSeeded, Actor: audit.ActorSystem})
	if !errors.Is(err, database.ErrNoTx) {
		t.Fatalf("err = %v, want ErrNoTx", err)
	}
}

func TestRecord_FillsIPAndRequestID(t *testing.T) {
	db := testsupport.NewTestDB(t)
	runner := database.NewTxRunner(db.App)
	ctx := reqctx.WithRequestID(t.Context(), "r1")
	ctx = reqctx.WithClientIP(ctx, netip.MustParseAddr("198.51.100.7"))
	uid := insertUser(t, db)
	err := runner.InTx(ctx, func(ctx context.Context) error {
		return postgres.NewRecorder().Record(ctx, audit.Entry{
			Action: audit.UserProfileUpdated, Actor: audit.ActorUser, ActorUserID: &uid,
			EntityType: "user", EntityID: "0190-public-id",
			After: map[string]any{"fields": []string{"first_name", "locale"}},
		})
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	var (
		action, kind, reqID, entityType, entityID, ip string
		actor                                         int64
		before, after                                 []byte
	)
	err = db.Super.QueryRow(t.Context(), `select action, actor_kind, actor_user_id, entity_type, entity_id,
		host(ip), request_id, before, after from audit_log`).
		Scan(&action, &kind, &actor, &entityType, &entityID, &ip, &reqID, &before, &after)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if action != "user.profile_updated" || kind != "user" || actor != uid || entityType != "user" || entityID != "0190-public-id" {
		t.Errorf("row = %s %s %d %s %s", action, kind, actor, entityType, entityID)
	}
	if ip != "198.51.100.7" || reqID != "r1" {
		t.Errorf("ip=%q request_id=%q", ip, reqID)
	}
	if before != nil {
		t.Errorf("before = %s, want NULL", before)
	}
	var got map[string][]string
	if err := json.Unmarshal(after, &got); err != nil || len(got["fields"]) != 2 {
		t.Errorf("after = %s (%v)", after, err)
	}
}

func TestRecord_NullsWhenContextIsBare(t *testing.T) {
	db := testsupport.NewTestDB(t)
	runner := database.NewTxRunner(db.App)
	err := runner.InTx(t.Context(), func(ctx context.Context) error {
		return postgres.NewRecorder().Record(ctx, audit.Entry{Action: audit.AdminSeeded, Actor: audit.ActorSystem})
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	var ipNull, reqNull, uidNull bool
	if err := db.Super.QueryRow(t.Context(),
		`select ip is null, request_id is null, actor_user_id is null from audit_log`).Scan(&ipNull, &reqNull, &uidNull); err != nil {
		t.Fatal(err)
	}
	if !ipNull || !reqNull || !uidNull {
		t.Errorf("ip/request_id/actor_user_id null = %v/%v/%v, want all true", ipNull, reqNull, uidNull)
	}
}

func TestRecord_RollsBackWithAction(t *testing.T) {
	db := testsupport.NewTestDB(t)
	runner := database.NewTxRunner(db.App)
	err := runner.InTx(t.Context(), func(ctx context.Context) error {
		if err := postgres.NewRecorder().Record(ctx, audit.Entry{Action: audit.AdminSeeded, Actor: audit.ActorSystem}); err != nil {
			return err
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("err = %v", err)
	}
	var n int
	if err := db.Super.QueryRow(t.Context(), `select count(*) from audit_log`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("rows = %d err=%v, want 0", n, err)
	}
}

func TestRecord_ErrorDoesNotLeakRowData(t *testing.T) {
	db := testsupport.NewTestDB(t)
	runner := database.NewTxRunner(db.App)
	err := runner.InTx(t.Context(), func(ctx context.Context) error {
		return postgres.NewRecorder().Record(ctx, audit.Entry{
			Action: "x", Actor: "root", EntityID: "secret-looking-value",
		})
	})
	if err == nil {
		t.Fatal("want error for unknown actor kind")
	}
	if strings.Contains(err.Error(), "secret-looking-value") {
		t.Errorf("error leaks row data: %q", err)
	}
}

func insertUser(t *testing.T, db *testsupport.TestDB) int64 {
	t.Helper()
	var id int64
	if err := db.App.QueryRow(t.Context(),
		`insert into users (email, first_name, last_name) values ('a@x.do', 'Ana', 'P') returning id`).Scan(&id); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return id
}
