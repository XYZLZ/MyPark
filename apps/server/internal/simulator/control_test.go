package simulator

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
)

const testToken = "control-token-for-tests-0123456789abcdef"

type controlRig struct {
	*runnerRig
	h    http.Handler
	logs *strings.Builder
}

func newControlRig(t *testing.T) *controlRig {
	t.Helper()
	rig := newRunnerRig(t, 100*time.Hour, nil)
	var logs strings.Builder
	l := slog.New(slog.NewJSONHandler(&logs, nil))
	return &controlRig{runnerRig: rig, h: NewControl(rig.run, rig.log, secret.String(testToken), l), logs: &logs}
}

type ctlResp struct {
	status int
	raw    string
	body   struct {
		Data  json.RawMessage `json:"data"`
		Error *struct {
			Code    string         `json:"code"`
			Details map[string]any `json:"details"`
		} `json:"error"`
		Meta struct {
			RequestID string `json:"request_id"`
		} `json:"meta"`
	}
}

func (r ctlResp) code() string {
	if r.body.Error == nil {
		return ""
	}
	return r.body.Error.Code
}

// call sends one control request with the test token unless token is given.
func (c *controlRig) call(t *testing.T, method, path, body string, token ...string) ctlResp {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	tok := testToken
	if len(token) > 0 {
		tok = token[0]
	}
	if tok != "" {
		req.Header.Set("X-Simulator-Token", tok)
	}
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, req)
	out := ctlResp{status: w.Code, raw: w.Body.String()}
	if w.Body.Len() > 0 {
		if err := json.Unmarshal(w.Body.Bytes(), &out.body); err != nil {
			t.Fatalf("%s %s: body %q is not JSON: %v", method, path, w.Body.String(), err)
		}
	}
	return out
}

func TestControl_TokenRequired(t *testing.T) {
	c := newControlRig(t)
	var bodies []string
	for _, tok := range []string{"", "wrong", testToken + "x", strings.ToUpper(testToken)} {
		r := c.call(t, http.MethodGet, "/control/status", "", tok)
		if r.status != http.StatusUnauthorized || r.code() != "simulator.unauthorized" {
			t.Errorf("token %q: %d %s, want 401 simulator.unauthorized", tok, r.status, r.raw)
		}
		bodies = append(bodies, strings.Replace(r.raw, r.body.Meta.RequestID, "", 1))
	}
	if len(slices.Compact(slices.Clone(bodies))) != 1 {
		t.Errorf("401 bodies differ by token (an oracle): %q", bodies)
	}
	// Every control route is guarded, the body is never read first.
	for _, rt := range ControlRoutes() {
		method, path, _ := strings.Cut(rt, " ")
		path = strings.NewReplacer("{car_id}", "car-000001", "{code}", "E010").Replace(path)
		if r := c.call(t, method, path, `{}`, ""); r.status != http.StatusUnauthorized {
			t.Errorf("%s without a token = %d, want 401", rt, r.status)
		}
	}
	if r := c.call(t, http.MethodGet, "/control/status", ""); r.status != http.StatusOK {
		t.Fatalf("right token = %d %s, want 200", r.status, r.raw)
	}
	if strings.Contains(c.logs.String(), testToken) {
		t.Error("the token was logged")
	}
}

func TestControl_StartStopFlow(t *testing.T) {
	c := newControlRig(t)
	r := c.call(t, http.MethodPost, "/control/scenarios/start", `{"scenario":"weekday","seed":42,"speed":120,"start_at":"2026-11-16T11:00:00Z"}`)
	if r.status != http.StatusAccepted {
		t.Fatalf("start = %d %s", r.status, r.raw)
	}
	var st Status
	_ = json.Unmarshal(r.body.Data, &st)
	if !st.Running || st.Seed != 42 || st.Speed != 120 || st.Scenario != ScenarioWeekday {
		t.Errorf("start status = %+v", st)
	}
	if r := c.call(t, http.MethodPost, "/control/scenarios/start", `{"scenario":"idle"}`); r.status != http.StatusConflict || r.code() != "simulator.already_running" {
		t.Errorf("second start = %d %s", r.status, r.raw)
	}
	r = c.call(t, http.MethodPost, "/control/scenarios/stop", `{"drain":true}`)
	if r.status != http.StatusAccepted {
		t.Fatalf("stop = %d %s", r.status, r.raw)
	}
	if r := c.call(t, http.MethodPost, "/control/scenarios/stop", `{"drain":true}`); r.status != http.StatusConflict || r.code() != "simulator.not_running" {
		t.Errorf("stop again = %d %s", r.status, r.raw)
	}
	eventually(t, "the drain to finish", func() bool { return !c.run.Status().Running })
	if r := c.call(t, http.MethodPost, "/control/scenarios/stop", `{}`); r.status != http.StatusConflict || r.code() != "simulator.not_running" {
		t.Errorf("stop after the run = %d %s", r.status, r.raw)
	}
}

func TestControl_StartDefaults(t *testing.T) {
	c := newControlRig(t)
	r := c.call(t, http.MethodPost, "/control/scenarios/start", `{"scenario":"fill_to_full"}`)
	if r.status != http.StatusAccepted {
		t.Fatalf("start = %d %s", r.status, r.raw)
	}
	var st Status
	_ = json.Unmarshal(r.body.Data, &st)
	if st.Speed != DefaultSpeed {
		t.Errorf("speed = %d, want the default %d", st.Speed, DefaultSpeed)
	}
	if st.Seed > 1<<63-1 {
		t.Errorf("seed %d is outside the contract's int64 range", st.Seed)
	}
	if got := c.run.Status().Seed; got != st.Seed {
		t.Errorf("status seed %d, start answered %d: the chosen seed must be recorded", got, st.Seed)
	}
}

func TestControl_StartValidation(t *testing.T) {
	c := newControlRig(t)
	cases := []struct {
		body       string
		status     int
		code, rule string
	}{
		{`{"scenario":"weekday","speed":0}`, 422, "validation.failed", "min"},
		{`{"scenario":"weekday","speed":121}`, 422, "validation.failed", "max"},
		{`{"scenario":"weekday","seed":9223372036854775808}`, 422, "validation.failed", "max"},
		{`{"scenario":"weekday","speed":"fast"}`, 400, "http.invalid_json", ""}, // PF7: a wrong JSON type is 400
		{`{"scenario":"weekday","speed":1e3}`, 400, "http.invalid_json", ""},
		{`{"scenario":"weekday","seed":-1}`, 400, "http.invalid_json", ""},
		{`{"scenario":"weekday","turbo":true}`, 422, "validation.failed", "unknown_field"},
		{`{"scenario":"weekday","Speed":30}`, 422, "validation.failed", "unknown_field"},
		{`{"scenario":"weekday","scenario":"idle"}`, 422, "validation.failed", "unknown_field"},
		{`{"scenario":"rush"}`, 422, "validation.failed", "oneof"},
		{`{}`, 422, "validation.failed", "required"},
		{`{"scenario":"weekday"`, 400, "http.invalid_json", ""},
		{`{"scenario":"reservation_arrivals"}`, 422, "simulator.scenario_unavailable", ""},
		{`{"scenario":"no_show"}`, 422, "simulator.scenario_unavailable", ""},
	}
	for _, tc := range cases {
		r := c.call(t, http.MethodPost, "/control/scenarios/start", tc.body)
		if r.status != tc.status || r.code() != tc.code {
			t.Errorf("%s = %d %s, want %d %s", tc.body, r.status, r.raw, tc.status, tc.code)
			continue
		}
		if tc.rule != "" && !strings.Contains(r.raw, `"rule":"`+tc.rule+`"`) {
			t.Errorf("%s: %s lacks rule %s", tc.body, r.raw, tc.rule)
		}
		if strings.Contains(r.raw, "fast") || strings.Contains(r.raw, "rush") {
			t.Errorf("%s: the value was echoed: %s", tc.body, r.raw)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/control/scenarios/start", strings.NewReader(`{"scenario":"idle"}`))
	req.Header.Set("X-Simulator-Token", testToken)
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, req)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain = %d, want 415", w.Code)
	}
	if c.run.Status().Running {
		t.Error("an invalid request started a run")
	}
}

func TestControl_CarsAndFaults(t *testing.T) {
	c := newControlRig(t)
	r := c.call(t, http.MethodPost, "/control/cars", `{"credential":"ticket","hold":true}`)
	if r.status != http.StatusAccepted || !strings.Contains(string(r.body.Data), `"car_id":"car-000001"`) {
		t.Fatalf("inject = %d %s", r.status, r.raw)
	}
	for _, body := range []string{
		`{"credential":"cash"}`, `{"credential":"ticket","dwell_minutes":0}`, `{"credential":"ticket","dwell_minutes":601}`,
		`{"credential":"employee_card","card":"EMP-7"}`, `{"credential":"ticket","card":"EMP-007"}`, `{"credential":"ticket","color":"red"}`,
	} {
		if r := c.call(t, http.MethodPost, "/control/cars", body); r.status != 422 || r.code() != "validation.failed" {
			t.Errorf("inject %s = %d %s, want 422 validation.failed", body, r.status, r.raw)
		}
	}
	if r := c.call(t, http.MethodPost, "/control/cars/car-999999/leave", `{}`); r.status != 404 || r.code() != "simulator.car_not_found" {
		t.Errorf("leave unknown = %d %s", r.status, r.raw)
	}
	if r := c.call(t, http.MethodPost, "/control/cars/car-000001/leave", `{"lose_ticket":true,"x":1}`); r.status != 422 {
		t.Errorf("leave with an unknown field = %d %s", r.status, r.raw)
	}
	if r := c.call(t, http.MethodPost, "/control/sensors/E010/fault", `{"fault":"stuck_occupied"}`); r.status != http.StatusNoContent || r.raw != "" {
		t.Errorf("fault = %d %q, want 204 with no body", r.status, r.raw)
	}
	for _, tc := range []struct{ path, body string }{
		{"/control/sensors/E010/fault", `{"fault":"boom"}`},
		{"/control/sensors/Z999/fault", `{"fault":"silent"}`},
		{"/control/sensors/E010/fault", `{}`},
	} {
		if r := c.call(t, http.MethodPost, tc.path, tc.body); r.status != 422 || r.code() != "validation.failed" {
			t.Errorf("fault %s %s = %d %s, want 422", tc.path, tc.body, r.status, r.raw)
		}
	}
	if r := c.call(t, http.MethodPost, "/control/cars/car-000001/leave", `{"lose_ticket":false}`); r.status != http.StatusAccepted {
		t.Errorf("leave = %d %s", r.status, r.raw)
	}
	// A stopping run takes no cars and no faults.
	if _, err := c.run.Stop(false); err != nil {
		t.Fatal(err)
	}
	c.run.Wait()
}

func TestControl_NotRunningOnAStoppedRun(t *testing.T) {
	c := newControlRig(t)
	if r := c.call(t, http.MethodPost, "/control/sensors/E010/fault", `{"fault":"none"}`); r.status != 409 || r.code() != "simulator.not_running" {
		t.Errorf("fault with no run = %d %s, want 409 simulator.not_running", r.status, r.raw)
	}
	if r := c.call(t, http.MethodPost, "/control/cars/car-000001/leave", `{}`); r.status != 409 || r.code() != "simulator.not_running" {
		t.Errorf("leave with no run = %d %s, want 409 simulator.not_running", r.status, r.raw)
	}
}

func TestControl_EventsAfter(t *testing.T) {
	c := newControlRig(t)
	for i := range 600 {
		c.log.Append(LogEntry{At: monday7.Add(time.Duration(i) * time.Second), Device: "dispenser-entry-1", Type: "entry.credential_presented", Result: "open", CarID: fmt.Sprintf("car-%06d", i)})
	}
	r := c.call(t, http.MethodGet, "/control/events?after=0", "")
	if r.status != http.StatusOK {
		t.Fatalf("events = %d %s", r.status, r.raw)
	}
	var got []LogEntry
	if err := json.Unmarshal(r.body.Data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || len(got) > 500 {
		t.Fatalf("%d entries, want 1-500", len(got))
	}
	if !slices.IsSortedFunc(got, func(a, b LogEntry) int { return int(a.Seq - b.Seq) }) {
		t.Error("entries are not in ascending seq")
	}
	if r := c.call(t, http.MethodGet, fmt.Sprintf("/control/events?after=%d", got[len(got)-1].Seq), ""); r.status != 200 || string(r.body.Data) == "null" {
		t.Errorf("events after the last = %d %s, want 200 with a list", r.status, r.raw)
	}
	if r := c.call(t, http.MethodGet, "/control/events", ""); r.status != 200 {
		t.Errorf("events without after = %d", r.status)
	}
	for _, q := range []string{"abc", "-1", "1.5", "99999999999999999999"} {
		if r := c.call(t, http.MethodGet, "/control/events?after="+q, ""); r.status != 422 || r.code() != "validation.failed" || strings.Contains(r.raw, q) && q != "-1" {
			t.Errorf("after=%s = %d %s, want 422 without the value", q, r.status, r.raw)
		}
	}

	// A real car's entries carry no ticket code.
	c.call(t, http.MethodPost, "/control/cars", `{"credential":"ticket"}`)
	eventually(t, "a ticket issued", func() bool {
		return len(c.api.payloads("entry.credential_presented")) > 0 && len(c.log.After(600)) >= 3
	})
	r = c.call(t, http.MethodGet, "/control/events?after=600", "")
	if strings.Contains(r.raw, "ticket_code") || strings.Contains(r.raw, "TICKET") {
		t.Errorf("events leak a ticket code: %s", r.raw)
	}
}
