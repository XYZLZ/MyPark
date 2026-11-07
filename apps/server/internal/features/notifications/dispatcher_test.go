package notifications_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/notifications"
	notificationspg "github.com/XYZLZ/MyPark/apps/server/internal/features/notifications/postgres"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/email"
	"github.com/XYZLZ/MyPark/apps/server/internal/testsupport"
)

const testFrom = "MyPark <no-reply@mypark.local>"

var t0 = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

// recorder is a Sender that records messages, optionally failing or blocking.
type recorder struct {
	mu    sync.Mutex
	sent  []email.Message
	err   error
	block chan struct{}
}

func (r *recorder) Send(ctx context.Context, m email.Message) error {
	if r.block != nil {
		select {
		case <-r.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.sent = append(r.sent, m)
	return nil
}

func (r *recorder) messages() []email.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]email.Message(nil), r.sent...)
}

type fixture struct {
	db  *testsupport.TestDB
	clk *testsupport.FakeClock
	to  string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	return fixture{db: testsupport.NewTestDB(t), clk: testsupport.NewFakeClock(t0), to: "ana@example.com"}
}

func (f fixture) dispatcher(s email.Sender, log *slog.Logger) *notifications.Dispatcher {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return notifications.NewDispatcher(f.db.App, s, f.clk, testFrom, log)
}

// enqueue goes through the real Notifier so the row gets available_at = fake now.
func (f fixture) enqueue(t *testing.T, key, locale string) {
	t.Helper()
	err := database.NewTxRunner(f.db.App).InTx(t.Context(), func(ctx context.Context) error {
		return notificationspg.NewNotifier(f.clk).Enqueue(ctx, notifications.Email{
			Template: notifications.TplVerifyEmail, Locale: locale, To: f.to,
			Data: map[string]string{"code": "123456", "first_name": "Ana"}, DedupeKey: key,
		})
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
}

type outboxRow struct {
	Status    string
	Attempts  int
	Payload   map[string]string
	LastError string
	AvailAt   time.Time
	SentAt    *time.Time
}

func (f fixture) row(t *testing.T, key string) outboxRow {
	t.Helper()
	var r outboxRow
	var payload []byte
	var lastErr *string
	err := f.db.Super.QueryRow(t.Context(), `select status, attempts, payload, last_error, available_at, sent_at
		from outbox_messages where dedupe_key = $1`, key).Scan(&r.Status, &r.Attempts, &payload, &lastErr, &r.AvailAt, &r.SentAt)
	if err != nil {
		t.Fatalf("select %s: %v", key, err)
	}
	if lastErr != nil {
		r.LastError = *lastErr
	}
	if err := json.Unmarshal(payload, &r.Payload); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestDispatcher_SendsAndScrubs(t *testing.T) {
	f := newFixture(t)
	f.enqueue(t, "verify_email:1", "es-DO")
	s := &recorder{}
	n, err := f.dispatcher(s, nil).Tick(t.Context())
	if err != nil || n != 1 {
		t.Fatalf("Tick = %d, %v; want 1, nil", n, err)
	}
	r := f.row(t, "verify_email:1")
	if r.Status != "sent" || r.SentAt == nil || !r.SentAt.Equal(t0) {
		t.Errorf("row = %+v, want sent at fake now", r)
	}
	if _, has := r.Payload["code"]; has || r.Payload["first_name"] != "Ana" {
		t.Errorf("payload = %v, want code scrubbed and first_name kept", r.Payload)
	}
	msgs := s.messages()
	if len(msgs) != 1 {
		t.Fatalf("sent %d messages", len(msgs))
	}
	m := msgs[0]
	if m.MessageID != email.MessageID("verify_email:1") || m.From != testFrom || m.To != "ana@example.com" {
		t.Errorf("message = %+v", m)
	}
	if !strings.Contains(m.Text, "123456") || !strings.Contains(m.HTML, "123456") || m.Subject == "" {
		t.Errorf("rendered message lacks code or subject")
	}
	if n, _ := f.dispatcher(s, nil).Tick(t.Context()); n != 0 {
		t.Errorf("second Tick sent %d, want 0", n)
	}
}

func TestDispatcher_NotClaimableBeforeAvailableAt(t *testing.T) {
	f := newFixture(t)
	f.clk.Advance(time.Hour)
	f.enqueue(t, "future", "en") // available_at = t0+1h
	f.clk.Set(t0)
	if n, _ := f.dispatcher(&recorder{}, nil).Tick(t.Context()); n != 0 {
		t.Errorf("claimed a row before its available_at")
	}
	f.clk.Set(t0.Add(time.Hour))
	if n, _ := f.dispatcher(&recorder{}, nil).Tick(t.Context()); n != 1 {
		t.Errorf("row enqueued at fake now not claimable at fake now")
	}
}

func TestDispatcher_RetryWithBackoff(t *testing.T) {
	f := newFixture(t)
	f.enqueue(t, "k", "en")
	s := &recorder{err: errors.New("550 mailbox <x@y.com> unavailable " + strings.Repeat("z", 800))}
	var logs bytes.Buffer
	n, err := f.dispatcher(s, slog.New(slog.NewTextHandler(&logs, nil))).Tick(t.Context())
	if err != nil || n != 0 {
		t.Fatalf("Tick = %d, %v; want 0, nil", n, err)
	}
	r := f.row(t, "k")
	if r.Status != "pending" || r.Attempts != 1 || !r.AvailAt.Equal(t0.Add(10*time.Second)) {
		t.Errorf("row = %+v, want pending attempts=1 available_at=now+10s", r)
	}
	if len(r.LastError) == 0 || len(r.LastError) > 500 {
		t.Errorf("last_error length = %d, want 1..500", len(r.LastError))
	}
	for _, leak := range []string{"123456", "Ana", "ana@example.com", "x@y.com"} {
		if strings.Contains(r.LastError, leak) || strings.Contains(logs.String(), leak) {
			t.Errorf("last_error or log leaks %q", leak)
		}
	}
	if r.Payload["code"] != "123456" {
		t.Errorf("code scrubbed while the message can still be retried")
	}
}

func TestDispatcher_DeadAfterMax(t *testing.T) {
	f := newFixture(t)
	f.enqueue(t, "k", "en")
	s := &recorder{err: errors.New("boom")}
	var logs bytes.Buffer
	d := f.dispatcher(s, slog.New(slog.NewJSONHandler(&logs, nil)))
	for i := 1; i <= 8; i++ {
		if _, err := d.Tick(t.Context()); err != nil {
			t.Fatal(err)
		}
		if i < 8 {
			if r := f.row(t, "k"); r.Status != "pending" || r.Attempts != i {
				t.Fatalf("tick %d: row = %+v", i, r)
			}
			f.clk.Advance(notifications.Backoff(i))
		}
	}
	r := f.row(t, "k")
	if r.Status != "dead" || r.Attempts != 8 {
		t.Errorf("row = %+v, want dead after 8 attempts", r)
	}
	if _, has := r.Payload["code"]; has {
		t.Error("code not scrubbed on dead")
	}
	if !strings.Contains(logs.String(), `"level":"ERROR"`) {
		t.Errorf("no error log line for the dead message: %s", logs.String())
	}
	f.clk.Advance(24 * time.Hour)
	if n, _ := d.Tick(t.Context()); n != 0 || f.row(t, "k").Attempts != 8 {
		t.Error("dead message was claimed again")
	}
}

func TestDispatcher_LeaseReclaimAfterCrash(t *testing.T) {
	f := newFixture(t)
	f.enqueue(t, "k", "en")
	blocker := &recorder{block: make(chan struct{})}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = f.dispatcher(blocker, nil).Tick(ctx)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for f.row(t, "k").Status != "sending" {
		if time.Now().After(deadline) {
			t.Fatal("row never reached sending")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	if r := f.row(t, "k"); r.Status != "sending" {
		t.Fatalf("after canceled send status = %s, want sending (lease)", r.Status)
	}
	s := &recorder{}
	if n, _ := f.dispatcher(s, nil).Tick(t.Context()); n != 0 {
		t.Error("row reclaimed before its lease expired")
	}
	f.clk.Advance(61 * time.Second)
	if n, err := f.dispatcher(s, nil).Tick(t.Context()); err != nil || n != 1 {
		t.Fatalf("Tick after lease = %d, %v; want 1", n, err)
	}
	if r := f.row(t, "k"); r.Status != "sent" || r.Attempts != 2 {
		t.Errorf("row = %+v, want sent on attempt 2", r)
	}
}

func TestDispatcher_ConcurrentTicksNoDoubleSend(t *testing.T) {
	f := newFixture(t)
	for i := range 10 {
		f.enqueue(t, fmt.Sprintf("k%d", i), "en")
	}
	s := &recorder{}
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			for range 3 {
				if _, err := f.dispatcher(s, nil).Tick(t.Context()); err != nil {
					t.Errorf("Tick: %v", err)
				}
			}
		})
	}
	wg.Wait()
	seen := map[string]int{}
	for _, m := range s.messages() {
		seen[m.MessageID]++
	}
	if len(seen) != 10 {
		t.Errorf("distinct messages = %d, want 10", len(seen))
	}
	for id, c := range seen {
		if c != 1 {
			t.Errorf("message %s sent %d times", id, c)
		}
	}
}

func TestPurgeOutbox(t *testing.T) {
	f := newFixture(t)
	ins := func(key, status string, age time.Duration) {
		t.Helper()
		_, err := f.db.Super.Exec(t.Context(), `insert into outbox_messages (template, locale, to_address, payload, dedupe_key, status, created_at)
			values ('verify_email', 'en', 'a@x.do', '{}', $1, $2, $3)`, key, status, f.clk.Now().Add(-age))
		if err != nil {
			t.Fatal(err)
		}
	}
	day := 24 * time.Hour
	ins("sent-old", "sent", 91*day)
	ins("dead-old", "dead", 91*day)
	ins("sent-new", "sent", 89*day)
	ins("pending-old", "pending", 91*day)
	n, err := notifications.PurgeOutbox(t.Context(), f.db.App, f.clk)
	if err != nil || n != 2 {
		t.Fatalf("PurgeOutbox = %d, %v; want 2", n, err)
	}
	var left []string
	rows, err := f.db.Super.Query(t.Context(), `select dedupe_key from outbox_messages order by 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		_ = rows.Scan(&k)
		left = append(left, k)
	}
	if strings.Join(left, ",") != "pending-old,sent-new" {
		t.Errorf("left = %v, want pending-old and sent-new", left)
	}
}

func TestDispatcher_DeliversToMailpit(t *testing.T) {
	smtpAddr, api := testsupport.Mailpit(t)
	f := newFixture(t)
	f.to = testsupport.UniqueRecipient(t, api)
	f.enqueue(t, "mp:es", "es-DO")
	f.enqueue(t, "mp:en", "en")
	host, port := testsupport.SplitHostPort(t, smtpAddr)
	sender := email.NewSMTP(email.SMTPConfig{Host: host, Port: port})
	n, err := f.dispatcher(sender, nil).Tick(t.Context())
	if err != nil || n != 2 {
		t.Fatalf("Tick = %d, %v; want 2", n, err)
	}
	msgs := testsupport.MailpitMessages(t, api, f.to)
	if len(msgs) != 2 {
		t.Fatalf("mailpit has %d messages, want 2", len(msgs))
	}
	esSubject, _, _, _ := notifications.Render(notifications.TplVerifyEmail, "es-DO", nil)
	enSubject, _, _, _ := notifications.Render(notifications.TplVerifyEmail, "en", nil)
	got := map[string]bool{}
	for _, m := range msgs {
		got[m.Subject] = true
		if !strings.Contains(m.Text, "123456") || !strings.Contains(m.HTML, "123456") {
			t.Errorf("message %q lacks the code", m.Subject)
		}
		if m.MessageID == "" {
			t.Error("Message-ID not set")
		}
	}
	if !got[esSubject] || !got[enSubject] {
		t.Errorf("subjects %v, want es %q and en %q", got, esSubject, enSubject)
	}
	for _, key := range []string{"mp:es", "mp:en"} {
		r := f.row(t, key)
		if _, has := r.Payload["code"]; has || r.Status != "sent" {
			t.Errorf("%s row = %+v, want sent and scrubbed", key, r)
		}
	}
}

func TestDispatcher_LeaseCoversSlowSendAndLaterRowsAreNotLeased(t *testing.T) {
	f := newFixture(t)
	f.enqueue(t, "a", "en")
	f.enqueue(t, "b", "en")
	slow := &recorder{block: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = f.dispatcher(slow, nil).Tick(t.Context())
	}()
	waitStatus(t, f, "a", "sending")
	// "b" waits behind the slow send but holds no lease: still pending, untouched.
	if r := f.row(t, "b"); r.Status != "pending" || r.Attempts != 0 || !r.AvailAt.Equal(t0) {
		t.Fatalf("row b = %+v, want untouched pending while a is being sent", r)
	}
	// 59 s into the slow send, a second worker takes b but must not take a.
	f.clk.Advance(59 * time.Second)
	other := &recorder{}
	if n, err := f.dispatcher(other, nil).Tick(t.Context()); err != nil || n != 1 {
		t.Fatalf("second worker Tick = %d, %v; want 1", n, err)
	}
	if ms := other.messages(); len(ms) != 1 || ms[0].MessageID != email.MessageID("b") {
		t.Fatalf("second worker sent %+v, want only b", ms)
	}
	close(slow.block)
	<-done
	if r := f.row(t, "a"); r.Status != "sent" || r.Attempts != 1 {
		t.Errorf("row a = %+v, want sent on its single attempt", r)
	}
}

func TestDispatcher_StaleFinishChangesNothing(t *testing.T) {
	f := newFixture(t)
	f.enqueue(t, "k", "en")
	stale := &recorder{block: make(chan struct{}), err: errors.New("boom")}
	var staleLogs bytes.Buffer
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = f.dispatcher(stale, slog.New(slog.NewTextHandler(&staleLogs, nil))).Tick(t.Context())
	}()
	waitStatus(t, f, "k", "sending")
	// The lease expires and a second worker delivers the message.
	f.clk.Advance(61 * time.Second)
	good := &recorder{}
	if n, err := f.dispatcher(good, nil).Tick(t.Context()); err != nil || n != 1 {
		t.Fatalf("second worker Tick = %d, %v; want 1", n, err)
	}
	before := f.row(t, "k")
	// The first worker now fails: its retry must not overwrite "sent".
	close(stale.block)
	<-done
	after := f.row(t, "k")
	if after.Status != "sent" || after.Attempts != 2 || after.LastError != before.LastError ||
		!after.AvailAt.Equal(before.AvailAt) || before.SentAt == nil || after.SentAt == nil || !after.SentAt.Equal(*before.SentAt) {
		t.Errorf("row changed by a stale finish: before %+v after %+v", before, after)
	}
	if !strings.Contains(staleLogs.String(), "lease lost") || strings.Contains(staleLogs.String(), "ana@example.com") {
		t.Errorf("stale finish log = %s", staleLogs.String())
	}
}

func waitStatus(t *testing.T, f fixture, key, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for f.row(t, key).Status != want {
		if time.Now().After(deadline) {
			t.Fatalf("row %s never reached %s", key, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
