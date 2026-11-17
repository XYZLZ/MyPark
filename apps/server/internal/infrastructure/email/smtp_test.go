package email_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
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

func buildMessage(t *testing.T, m email.Message) (header mail.Header, body []byte) {
	t.Helper()
	_, _, raw, err := email.NewSMTP(email.SMTPConfig{Host: "127.0.0.1", Port: 1}).Build(m)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	body, err = io.ReadAll(msg.Body)
	if err != nil {
		t.Fatal(err)
	}
	return msg.Header, body
}

func TestSMTPBuild_InlineImage(t *testing.T) {
	base := email.Message{From: "MyPark <no-reply@mypark.local>", To: "a@x.do", Subject: "s", Text: "plain", HTML: `<img src="cid:qr">`, MessageID: "<a@mypark.local>"}

	// Without an inline image the body stays multipart/alternative.
	hdr, _ := buildMessage(t, base)
	if ct := hdr.Get("Content-Type"); !strings.HasPrefix(ct, "multipart/alternative;") {
		t.Errorf("Content-Type without Inline = %q, want multipart/alternative", ct)
	}

	png := bytes.Repeat([]byte{0x89, 'P', 'N', 'G', 1, 2, 3, 4}, 400) // long enough to need line wrapping
	m := base
	m.Inline = []email.Inline{{ContentID: "qr", ContentType: "image/png", Data: png}}
	hdr, body := buildMessage(t, m)
	mt, params, err := mime.ParseMediaType(hdr.Get("Content-Type"))
	if err != nil || mt != "multipart/related" {
		t.Fatalf("Content-Type = %q (%v), want multipart/related", hdr.Get("Content-Type"), err)
	}
	mr := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	first, err := mr.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	if mt, _, _ := mime.ParseMediaType(first.Header.Get("Content-Type")); mt != "multipart/alternative" {
		t.Errorf("first related part = %q, want multipart/alternative", first.Header.Get("Content-Type"))
	}
	img, err := mr.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	if got := img.Header.Get("Content-ID"); got != "<qr>" {
		t.Errorf("Content-ID = %q, want <qr>", got)
	}
	if got := img.Header.Get("Content-Type"); got != "image/png" {
		t.Errorf("image Content-Type = %q", got)
	}
	if got := img.Header.Get("Content-Disposition"); !strings.HasPrefix(got, "inline") {
		t.Errorf("Content-Disposition = %q, want inline", got)
	}
	raw, _ := io.ReadAll(img)
	for line := range strings.SplitSeq(string(raw), "\r\n") {
		if len(line) > 76 {
			t.Fatalf("base64 line of %d characters, want at most 76", len(line))
		}
	}
	dec, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(string(raw), "\r\n", ""))
	if err != nil || !bytes.Equal(dec, png) {
		t.Errorf("image data does not round-trip (%v)", err)
	}
	if _, err := mr.NextPart(); err != io.EOF {
		t.Errorf("want exactly two related parts, next = %v", err)
	}

	m.Inline[0].ContentID = "qr\r\nBcc: evil@x.do"
	if _, _, _, err := email.NewSMTP(email.SMTPConfig{}).Build(m); err == nil {
		t.Error("a line break in the Content-ID must be rejected")
	}
}
