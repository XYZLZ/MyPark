package testsupport

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Environment variables that locate the Mailpit test instance.
const (
	envSMTPAddr   = "TEST_SMTP_ADDR"
	envMailpitAPI = "TEST_MAILPIT_API"
)

// MailpitMessage is a message captured by Mailpit.
type MailpitMessage struct {
	Subject   string
	MessageID string
	Text      string
	HTML      string
}

// Mailpit returns the SMTP address and API base URL of the Mailpit test
// instance (TEST_SMTP_ADDR, TEST_MAILPIT_API). The inbox is shared by every
// test package running in parallel, so it is never cleared: address each test
// to its own UniqueRecipient and read only that. When they are unset the test is skipped, except with CI=true, where it fails:
// a CI run must never silently skip the delivery tests (OC-34).
func Mailpit(t *testing.T) (smtpAddr, apiURL string) {
	t.Helper()
	smtpAddr, apiURL = os.Getenv(envSMTPAddr), strings.TrimRight(os.Getenv(envMailpitAPI), "/")
	if smtpAddr == "" || apiURL == "" {
		if os.Getenv("CI") == "true" {
			t.Fatalf("testsupport: %s and %s must be set in CI", envSMTPAddr, envMailpitAPI)
		}
		t.Skipf("testsupport: %s and %s not set; skipping Mailpit test", envSMTPAddr, envMailpitAPI)
	}
	return smtpAddr, apiURL
}

// UniqueRecipient returns an address no other test uses, and deletes the
// messages sent to it when the test ends.
func UniqueRecipient(t *testing.T, apiURL string) string {
	t.Helper()
	to := fmt.Sprintf("t-%d-%d@example.com", time.Now().UnixNano(), rand.Uint32())
	t.Cleanup(func() { deleteTo(apiURL, to) })
	return to
}

// deleteTo removes the Mailpit messages addressed to to. Best effort.
func deleteTo(apiURL, to string) {
	var list struct {
		Messages []struct {
			ID string
			To []struct{ Address string }
		}
	}
	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(apiURL + "/api/v1/messages")
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if json.NewDecoder(resp.Body).Decode(&list) != nil {
		return
	}
	var ids []string
	for _, m := range list.Messages {
		if anyAddress(m.To, to) {
			ids = append(ids, m.ID)
		}
	}
	if len(ids) == 0 {
		return
	}
	body, _ := json.Marshal(map[string][]string{"IDs": ids})
	req, err := http.NewRequest(http.MethodDelete, apiURL+"/api/v1/messages", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if r, err := client.Do(req); err == nil {
		r.Body.Close()
	}
}

// SplitHostPort splits a "host:port" address into the pieces SMTPConfig takes.
func SplitHostPort(t *testing.T, addr string) (host string, port int) {
	t.Helper()
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("testsupport: bad address %q: %v", addr, err)
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		t.Fatalf("testsupport: bad port in %q: %v", addr, err)
	}
	return h, n
}

// MailpitMessages returns the captured messages addressed to to, with their
// bodies.
func MailpitMessages(t *testing.T, apiURL, to string) []MailpitMessage {
	t.Helper()
	var list struct {
		Messages []struct {
			ID string
			To []struct{ Address string }
		}
	}
	getJSON(t, apiURL+"/api/v1/messages", &list)
	var out []MailpitMessage
	for _, m := range list.Messages {
		if !anyAddress(m.To, to) {
			continue
		}
		var d struct {
			Subject   string
			MessageID string
			Text      string
			HTML      string
		}
		getJSON(t, apiURL+"/api/v1/message/"+m.ID, &d)
		out = append(out, MailpitMessage{Subject: d.Subject, MessageID: d.MessageID, Text: d.Text, HTML: d.HTML})
	}
	return out
}

func anyAddress(to []struct{ Address string }, want string) bool {
	for _, a := range to {
		if strings.EqualFold(a.Address, want) {
			return true
		}
	}
	return false
}

func getJSON(t *testing.T, url string, v any) {
	t.Helper()
	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("testsupport: GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("testsupport: GET %s: %s", url, fmt.Sprint(resp.StatusCode))
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("testsupport: decode %s: %v", url, err)
	}
}
