package database_test

import (
	"context"
	"errors"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

var errBoom = errors.New("boom")

// Compile-time checks: DBTX is satisfied by dbgen's and the runner satisfies its port.
var (
	_ dbgen.DBTX            = database.DBTX(nil)
	_ database.TxRunnerPort = (*database.TxRunner)(nil)
)

// scratch creates a real scratch table (mypark_app has no TEMPORARY privilege).
func scratch(t *testing.T) (*testsupport.TestDB, *database.TxRunner) {
	t.Helper()
	db := testsupport.NewTestDB(t)
	for _, q := range []string{
		"create table tx_scratch (id int primary key)",
		"grant select, insert on tx_scratch to mypark_app",
	} {
		if _, err := db.Migrator.Exec(t.Context(), q); err != nil {
			t.Fatalf("setup %q: %v", q, err)
		}
	}
	return db, database.NewTxRunner(db.App)
}

func insert(ctx context.Context, db *testsupport.TestDB, id int) error {
	_, err := database.Conn(ctx, db.App).Exec(ctx, "insert into tx_scratch (id) values ($1)", id)
	return err
}

func count(t *testing.T, db *testsupport.TestDB) int {
	t.Helper()
	var n int
	if err := db.App.QueryRow(t.Context(), "select count(*) from tx_scratch").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestInTx_CommitsOnNil(t *testing.T) {
	db, tx := scratch(t)
	err := tx.InTx(t.Context(), func(ctx context.Context) error { return insert(ctx, db, 1) })
	if err != nil {
		t.Fatal(err)
	}
	if n := count(t, db); n != 1 {
		t.Errorf("rows = %d, want 1", n)
	}
}

func TestInTx_RollsBackOnError(t *testing.T) {
	db, tx := scratch(t)
	err := tx.InTx(t.Context(), func(ctx context.Context) error {
		if err := insert(ctx, db, 1); err != nil {
			return err
		}
		return errBoom
	})
	if !errors.Is(err, errBoom) {
		t.Fatalf("err = %v, want errBoom", err)
	}
	if n := count(t, db); n != 0 {
		t.Errorf("rows = %d, want 0", n)
	}
}

func TestInTx_RollsBackOnPanic(t *testing.T) {
	db, tx := scratch(t)
	func() {
		defer func() {
			if r := recover(); r != "kaboom" {
				t.Errorf("recover = %v, want kaboom", r)
			}
		}()
		_ = tx.InTx(t.Context(), func(ctx context.Context) error {
			if err := insert(ctx, db, 1); err != nil {
				return err
			}
			panic("kaboom")
		})
	}()
	if n := count(t, db); n != 0 {
		t.Errorf("rows = %d, want 0", n)
	}
}

func TestInTx_NestedReusesOuter(t *testing.T) {
	db, tx := scratch(t)
	err := tx.InTx(t.Context(), func(ctx context.Context) error {
		if err := insert(ctx, db, 1); err != nil {
			return err
		}
		outer, _ := database.MustTx(ctx)
		return tx.InTx(ctx, func(ctx context.Context) error {
			if inner, _ := database.MustTx(ctx); inner != outer {
				t.Error("nested InTx opened a new transaction")
			}
			if err := insert(ctx, db, 2); err != nil {
				return err
			}
			return errBoom
		})
	})
	if !errors.Is(err, errBoom) {
		t.Fatalf("err = %v, want errBoom", err)
	}
	if n := count(t, db); n != 0 {
		t.Errorf("rows = %d, want 0 (outer must roll back)", n)
	}
}

func TestInTx_NestedSuccessCommitsOnce(t *testing.T) {
	db, tx := scratch(t)
	err := tx.InTx(t.Context(), func(ctx context.Context) error {
		if err := tx.InTx(ctx, func(ctx context.Context) error { return insert(ctx, db, 1) }); err != nil {
			return err
		}
		return insert(ctx, db, 2)
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := count(t, db); n != 2 {
		t.Errorf("rows = %d, want 2", n)
	}
}

func TestMustTx_ErrorsOutsideTx(t *testing.T) {
	if _, err := database.MustTx(t.Context()); !errors.Is(err, database.ErrNoTx) {
		t.Errorf("err = %v, want ErrNoTx", err)
	}
}

func TestMustTx_ReturnsTxInside(t *testing.T) {
	_, tx := scratch(t)
	err := tx.InTx(t.Context(), func(ctx context.Context) error {
		got, err := database.MustTx(ctx)
		if err != nil || got == nil {
			t.Errorf("MustTx = %v, %v", got, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestConn_PrefersTx(t *testing.T) {
	db, tx := scratch(t)
	if got := database.Conn(t.Context(), db.App); got != database.DBTX(db.App) {
		t.Error("Conn outside a tx must return the pool")
	}
	err := tx.InTx(t.Context(), func(ctx context.Context) error {
		want, _ := database.MustTx(ctx)
		if got := database.Conn(ctx, db.App); got != database.DBTX(want) {
			t.Error("Conn inside a tx must return the tx")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
