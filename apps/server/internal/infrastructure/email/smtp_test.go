package email_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/email"
)

func TestMessageID_DerivedFromDedupeKey(t *testing.T) {
	id := email.MessageID("verify_email:1")
	if !regexp.MustCompile(`^<[0-9a-f]{32}@mypark\.local>$`).MatchString(id) {
		t.Errorf("MessageID = %q", id)
	}
	if id != email.MessageID("verify_email:1") || id == email.MessageID("verify_email:2") {
		t.Error("MessageID must be deterministic and key-specific")
	}
}

func TestSMTP_RejectsHeaderInjection(t *testing.T) {
	s := email.NewSMTP(email.SMTPConfig{Host: "127.0.0.1", Port: 1})
	base := email.Message{From: "MyPark <no-reply@mypark.local>", To: "a@x.do", Subject: "s", Text: "t", HTML: "<p>t</p>", MessageID: "<a@mypark.local>"}
	for name, mod := range map[string]func(*email.Message){
		"subject": func(m *email.Message) { m.Subject = "hi\r\nBcc: evil@x.do" },
		"to":      func(m *email.Message) { m.To = "a@x.do\r\nBcc: evil@x.do" },
		"from":    func(m *email.Message) { m.From = "a@x.do\nX: y" },
		"bad to":  func(m *email.Message) { m.To = "not an address" },
	} {
		m := base
		mod(&m)
		err := s.Send(context.Background(), m)
		if err == nil || strings.Contains(err.Error(), "evil") {
			t.Errorf("%s: err = %v, want a rejection before dialing", name, err)
		}
	}
}
