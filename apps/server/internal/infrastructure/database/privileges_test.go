package database_test

import (
	"context"
	"crypto/rand"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

func TestDBPrivileges_AppCanConnect(t *testing.T) {
	db := testsupport.NewTestDB(t)
	for _, role := range []string{"mypark_app", "mypark_migrator"} {
		var ok bool
		err := db.Super.QueryRow(t.Context(),
			"select has_database_privilege($1, current_database(), 'CONNECT')", role).Scan(&ok)
		if err != nil {
			t.Fatalf("has_database_privilege(%s): %v", role, err)
		}
		if !ok {
			t.Errorf("%s lacks CONNECT on the database", role)
		}
	}
}

func TestDBPrivileges_PublicCannotConnectOrTemp(t *testing.T) {
	db := testsupport.NewTestDB(t)
	ctx := t.Context()
	probe := pgx.Identifier{"probe_" + strings.ToLower(rand.Text())}.Sanitize()
	if _, err := db.Super.Exec(ctx, "create role "+probe+" nologin"); err != nil {
		t.Fatalf("create probe role: %v", err)
	}
	t.Cleanup(func() {
		// t.Context() is canceled by now. Runs before the pool closes (LIFO).
		_, _ = db.Super.Exec(context.Background(), "drop role if exists "+probe)
	})

	for _, priv := range []string{"CONNECT", "TEMPORARY"} {
		var ok bool
		err := db.Super.QueryRow(ctx,
			"select has_database_privilege($1, current_database(), $2)", strings.Trim(probe, `"`), priv).Scan(&ok)
		if err != nil {
			t.Fatalf("has_database_privilege(%s): %v", priv, err)
		}
		if ok {
			t.Errorf("a role with no grants has %s on the database; PUBLIC must be revoked", priv)
		}
	}
}

func TestDBPrivileges_AppHasNoTemporary(t *testing.T) {
	db := testsupport.NewTestDB(t)
	var ok bool
	err := db.Super.QueryRow(t.Context(),
		"select has_database_privilege('mypark_app', current_database(), 'TEMPORARY')").Scan(&ok)
	if err != nil {
		t.Fatalf("has_database_privilege: %v", err)
	}
	if ok {
		t.Error("mypark_app has TEMPORARY; scratch tables must go through the migrator")
	}
}
