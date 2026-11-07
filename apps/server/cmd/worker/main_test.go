package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

// dummyDatabaseURL is never dialed by /healthz: the pool connects lazily.
const dummyDatabaseURL = "postgres://mypark_app:x@127.0.0.1:1/mypark?connect_timeout=1"

// smtpEnv is the SMTP part of a valid worker environment; nothing dials it
// unless a message is due.
var smtpEnv = map[string]string{"SMTP_HOST": "127.0.0.1", "SMTP_PORT": "1", "SMTP_FROM": "MyPark <no-reply@mypark.local>"}

func workerEnv(kv ...string) map[string]string {
	env := map[string]string{"DATABASE_URL": dummyDatabaseURL}
	for k, v := range smtpEnv {
		env[k] = v
	}
	for i := 0; i+1 < len(kv); i += 2 {
		env[kv[i]] = kv[i+1]
	}
	return env
}

func TestWorker_HealthzServes(t *testing.T) {
	base := testsupport.StartServer(t, run, workerEnv("WORKER_HEALTH_ADDR", "127.0.0.1:0"), "worker listening")

	resp, err := http.Get(base + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode /healthz body: %v", err)
	}
	data, _ := body["data"].(map[string]any)
	if resp.StatusCode != http.StatusOK || data["status"] != "ok" || data["service"] != "worker" {
		t.Fatalf("/healthz = %d %v, want 200 ok service=worker", resp.StatusCode, body)
	}
}

// TestWorker_ReadyzDBDown checks the worker reports an unreachable database
// on /readyz instead of failing to boot.
func TestWorker_ReadyzDBDown(t *testing.T) {
	base := testsupport.StartServer(t, run, workerEnv("WORKER_HEALTH_ADDR", "127.0.0.1:0"), "worker listening")

	resp, err := http.Get(base + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("/readyz status = %d, want 503", resp.StatusCode)
	}
}

func TestWorker_MissingDatabaseURL(t *testing.T) {
	err := run(t.Context(), func(string) string { return "" }, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL is required") {
		t.Fatalf("run error = %v, want one naming DATABASE_URL", err)
	}
}

func TestWorker_InvalidHealthAddr(t *testing.T) {
	env := workerEnv("WORKER_HEALTH_ADDR", "nope")
	err := run(t.Context(), func(k string) string { return env[k] }, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "WORKER_HEALTH_ADDR") {
		t.Fatalf("run error = %v, want one naming WORKER_HEALTH_ADDR", err)
	}
}

func TestWorker_HealthcheckFlag(t *testing.T) {
	for _, tc := range []struct {
		status  int
		wantErr bool
	}{{http.StatusOK, false}, {http.StatusServiceUnavailable, true}} {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			w.WriteHeader(tc.status)
		}))
		env := map[string]string{"WORKER_HEALTH_ADDR": strings.TrimPrefix(srv.URL, "http://")}
		err := execute(t.Context(), []string{"-healthcheck"}, func(k string) string { return env[k] }, io.Discard)
		srv.Close()
		if (err != nil) != tc.wantErr {
			t.Errorf("status %d: execute error = %v, wantErr %v", tc.status, err, tc.wantErr)
		}
		if gotPath != "/readyz" {
			t.Errorf("status %d: probed %q, want /readyz", tc.status, gotPath)
		}
	}
}

func TestWorker_MissingSMTPConfig(t *testing.T) {
	env := map[string]string{"DATABASE_URL": dummyDatabaseURL}
	err := run(t.Context(), func(k string) string { return env[k] }, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "SMTP_HOST") {
		t.Fatalf("run error = %v, want one naming SMTP_HOST", err)
	}
}

// TestWorker_DeliversOutbox runs the whole worker against a real database
// and Mailpit: a due outbox row is sent, then its code is scrubbed.
func TestWorker_DeliversOutbox(t *testing.T) {
	smtpAddr, api := testsupport.Mailpit(t)
	db := testsupport.NewTestDB(t)
	to := testsupport.UniqueRecipient(t, api)
	if _, err := db.Super.Exec(t.Context(), `insert into outbox_messages (template, locale, to_address, payload, dedupe_key)
		values ('verify_email', 'en', $1, '{"code":"424242"}', 'worker-test:1')`, to); err != nil {
		t.Fatal(err)
	}
	host, port := testsupport.SplitHostPort(t, smtpAddr)
	testsupport.StartServer(t, run, map[string]string{
		"WORKER_HEALTH_ADDR": "127.0.0.1:0", "DATABASE_URL": db.AppURL,
		"SMTP_HOST": host, "SMTP_PORT": strconv.Itoa(port), "SMTP_FROM": "MyPark <no-reply@mypark.local>",
	}, "worker listening")

	deadline := time.Now().Add(15 * time.Second)
	for {
		msgs := testsupport.MailpitMessages(t, api, to)
		if len(msgs) == 1 {
			if !strings.Contains(msgs[0].Text, "424242") {
				t.Errorf("message lacks the code")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no message delivered, mailpit has %d", len(msgs))
		}
		time.Sleep(100 * time.Millisecond)
	}
	var status string
	var payload []byte
	for {
		if err := db.Super.QueryRow(t.Context(), `select status, payload from outbox_messages where dedupe_key = 'worker-test:1'`).Scan(&status, &payload); err != nil {
			t.Fatal(err)
		}
		if status == "sent" || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status != "sent" || strings.Contains(string(payload), "424242") {
		t.Errorf("row status %q payload %s, want sent and scrubbed", status, payload)
	}
}
