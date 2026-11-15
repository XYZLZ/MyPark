package gate_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/authz"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/audit"
	"github.com/XYZLZ/MyPark/apps/server/internal/features/gate"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// passRepo is just enough of a gate.Repo for an entry pass; any other method
// panics through the nil embedded interface.
type passRepo struct {
	gate.Repo
	status gate.Status
}

func (r *passRepo) LockLotOccupancy(context.Context, int64) (gate.LotOccupancy, error) {
	return gate.LotOccupancy{Capacity: 10}, nil
}

func (r *passRepo) StayForUpdate(_ context.Context, _ int64, id uuid.UUID) (gate.Stay, error) {
	return gate.Stay{ID: 1, PublicID: id, Status: r.status}, nil
}

func (r *passRepo) ActivateStay(context.Context, int64, int64, time.Time) error { return nil }

func (r *passRepo) BumpOccupancy(context.Context, int64, int, time.Time) (int64, error) {
	return 2, nil
}

func (r *passRepo) NotifyLotChanged(context.Context, int64, int64) error { return nil }

type failingAudit struct{}

func (failingAudit) Record(context.Context, audit.Entry) error { return errors.New("audit down") }

func passService(status gate.Status, logs *bytes.Buffer) *gate.Service {
	return gate.NewService(gate.Deps{
		Repo:  &passRepo{status: status},
		Clock: testsupport.NewFakeClock(time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)),
		Log:   slog.New(slog.NewJSONHandler(logs, nil)),
		Audit: failingAudit{},
	})
}

func entryPassEvent() gate.Event {
	return gate.Event{ID: uuid.New(), Type: gate.EventBarrierPassed,
		Payload: gate.BarrierPassed{Direction: gate.DirectionEntry, StayID: uuid.New()}}
}

// The late-pass warning claims an audited reactivation, so it is logged only
// once the audit row is written.
func TestPass_LateEntryWarnsOnlyAfterAudit(t *testing.T) {
	var logs bytes.Buffer
	_, err := passService(gate.StatusVoided, &logs).Handle(t.Context(), authz.Device{LotID: 1}, entryPassEvent())
	if err == nil || !strings.Contains(err.Error(), "audit down") {
		t.Fatalf("err = %v, want the audit failure", err)
	}
	if strings.Contains(logs.String(), "late entry pass") {
		t.Errorf("warned before the audit succeeded:\n%s", logs.String())
	}
}

// A status the pass does not know is an error, never a silent rejection.
func TestPass_UnknownStatusIsAnError(t *testing.T) {
	var logs bytes.Buffer
	res, err := passService(gate.Status("bogus"), &logs).Handle(t.Context(), authz.Device{LotID: 1}, entryPassEvent())
	if err == nil || res != (gate.Result{}) {
		t.Fatalf("pass of a bogus status = %+v, %v; want an error", res, err)
	}
}

type okAudit struct{}

func (okAudit) Record(context.Context, audit.Entry) error { return nil }

// The gate runs inside the event's transaction, so it never logs the late
// pass itself: it marks the result, and LogCommitted logs it after commit.
func TestPass_LateEntryDefersWarnToCommit(t *testing.T) {
	var logs bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logs, nil))
	svc := gate.NewService(gate.Deps{
		Repo:  &passRepo{status: gate.StatusVoided},
		Clock: testsupport.NewFakeClock(time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)),
		Log:   log, Audit: okAudit{},
	})
	ev := entryPassEvent()
	stayID := ev.Payload.(gate.BarrierPassed).StayID
	dev := authz.Device{LotID: 1, PublicID: uuid.New()}
	res, err := svc.Handle(t.Context(), dev, ev)
	if err != nil || res.Result != gate.OutcomeAck || res.LateEntryStay != stayID {
		t.Fatalf("late pass = %+v, %v; want ack marked with stay %s", res, err, stayID)
	}
	if logs.Len() != 0 {
		t.Fatalf("the gate logged inside the transaction:\n%s", logs.String())
	}
	if b, _ := json.Marshal(res); strings.Contains(string(b), stayID.String()) {
		t.Errorf("the mark reached the wire: %s", b)
	}

	gate.LogCommitted(t.Context(), log, dev, res)
	if !strings.Contains(logs.String(), `"level":"WARN","msg":"gate: late entry pass on a voided stay"`) ||
		!strings.Contains(logs.String(), stayID.String()) {
		t.Errorf("LogCommitted = %s, want the late pass warning", logs.String())
	}
	logs.Reset()
	gate.LogCommitted(t.Context(), log, dev, gate.Result{Result: gate.OutcomeAck})
	if logs.Len() != 0 {
		t.Errorf("LogCommitted logged a plain result: %s", logs.String())
	}
}
