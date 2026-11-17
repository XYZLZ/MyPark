// Package email sends outbound mail over SMTP.
package email

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/domain/secret"
)

// ioTimeout bounds one whole SMTP exchange when the context has no deadline.
const ioTimeout = 30 * time.Second

// Message is one outbound email. From and To are RFC 5322 addresses
// ("Name <a@b>" or "a@b"); MessageID includes the angle brackets. Inline
// images are referenced from the HTML as cid:<ContentID>.
type Message struct {
	From, To, Subject, Text, HTML, MessageID string
	Inline                                   []Inline
}

// Inline is an image embedded in the message body.
type Inline struct {
	ContentID, ContentType string
	Data                   []byte
}

// Sender delivers a Message.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// MessageID derives the Message-ID of a message from its outbox dedupe key
// (OC-24), so a rare duplicate delivery is recognizable.
func MessageID(dedupeKey string) string {
	sum := sha256.Sum256([]byte(dedupeKey))
	return "<" + hex.EncodeToString(sum[:])[:32] + "@mypark.local>"
}

// SMTPConfig configures the SMTP sender. Username and Password are optional;
// StartTLS upgrades the connection before authenticating.
type SMTPConfig struct {
	Host     string
	Port     int
	Username secret.String
	Password secret.String
	StartTLS bool
}

// SMTP sends mail through net/smtp.
type SMTP struct {
	cfg SMTPConfig
	now func() time.Time
}

var _ Sender = (*SMTP)(nil)

// NewSMTP returns an SMTP sender.
func NewSMTP(cfg SMTPConfig) *SMTP { return &SMTP{cfg: cfg, now: time.Now} }

// Send implements Sender. Errors never carry the message content; an SMTP
// server reply can still name the recipient, so callers must not log it
// unredacted.
func (s *SMTP) Send(ctx context.Context, m Message) error {
	from, to, body, err := s.build(m)
	if err != nil {
		return err
	}
	addr := net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.cfg.Port))
	deadline := s.now().Add(ioTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	dialer := net.Dialer{Deadline: deadline}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("email: dial: %w", err)
	}
	// Closing the connection is what unblocks a canceled exchange.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	defer conn.Close()
	if err := conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("email: deadline: %w", err)
	}
	if err := s.deliver(conn, from, to, body); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	return nil
}

func (s *SMTP) deliver(conn net.Conn, from, to string, body []byte) error {
	c, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		return fmt.Errorf("email: greeting: %w", err)
	}
	defer c.Close()
	if s.cfg.StartTLS {
		if err := c.StartTLS(&tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("email: starttls: %w", err)
		}
	}
	if s.cfg.Username != "" {
		auth := smtp.PlainAuth("", s.cfg.Username.Reveal(), s.cfg.Password.Reveal(), s.cfg.Host)
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("email: auth: %w", err)
		}
	}
	if err := c.Mail(from); err != nil {
		return fmt.Errorf("email: mail from: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("email: rcpt to: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("email: data: %w", err)
	}
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("email: write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("email: data end: %w", err)
	}
	if err := c.Quit(); err != nil {
		// The message is already accepted; a failed QUIT is not a delivery failure.
		return nil
	}
	return nil
}

// build validates m and renders the RFC 5322 message. It rejects CR and LF in
// header values, which would allow header injection. Without inline images the
// body is multipart/alternative; with them it is multipart/related holding the
// alternative part and each image.
func (s *SMTP) build(m Message) (from, to string, body []byte, err error) {
	headers := []string{m.From, m.To, m.Subject, m.MessageID}
	for _, in := range m.Inline {
		headers = append(headers, in.ContentID, in.ContentType)
	}
	for _, v := range headers {
		if strings.ContainsAny(v, "\r\n") {
			return "", "", nil, errors.New("email: header value contains a line break")
		}
	}
	f, err := mail.ParseAddress(m.From)
	if err != nil {
		return "", "", nil, errors.New("email: invalid From address")
	}
	t, err := mail.ParseAddress(m.To)
	if err != nil {
		return "", "", nil, errors.New("email: invalid To address")
	}

	altType, alt, err := alternative(m)
	if err != nil {
		return "", "", nil, err
	}
	contentType, content := altType, alt
	if len(m.Inline) > 0 {
		if contentType, content, err = related(altType, alt, m.Inline); err != nil {
			return "", "", nil, err
		}
	}

	var buf bytes.Buffer
	hdr := func(k, v string) { fmt.Fprintf(&buf, "%s: %s\r\n", k, v) }
	hdr("From", f.String())
	hdr("To", t.String())
	hdr("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	hdr("Date", s.now().UTC().Format(time.RFC1123Z))
	if m.MessageID != "" {
		hdr("Message-ID", m.MessageID)
	}
	hdr("MIME-Version", "1.0")
	hdr("Content-Type", contentType)
	buf.WriteString("\r\n")
	buf.Write(content)
	return f.Address, t.Address, buf.Bytes(), nil
}

// alternative renders the text and HTML parts of m, as quoted-printable.
func alternative(m Message) (contentType string, body []byte, err error) {
	var buf bytes.Buffer
	mp := multipart.NewWriter(&buf)
	for _, part := range []struct{ typ, content string }{
		{"text/plain", m.Text}, {"text/html", m.HTML},
	} {
		w, err := mp.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {part.typ + "; charset=utf-8"},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		if err != nil {
			return "", nil, fmt.Errorf("email: part: %w", err)
		}
		qp := quotedprintable.NewWriter(w)
		if _, err := qp.Write([]byte(part.content)); err != nil {
			return "", nil, fmt.Errorf("email: encode: %w", err)
		}
		if err := qp.Close(); err != nil {
			return "", nil, fmt.Errorf("email: encode: %w", err)
		}
	}
	if err := mp.Close(); err != nil {
		return "", nil, fmt.Errorf("email: close: %w", err)
	}
	return "multipart/alternative; boundary=" + mp.Boundary(), buf.Bytes(), nil
}

// related wraps the alternative body and the inline images in multipart/related.
func related(altType string, alt []byte, images []Inline) (contentType string, body []byte, err error) {
	var buf bytes.Buffer
	mp := multipart.NewWriter(&buf)
	w, err := mp.CreatePart(textproto.MIMEHeader{"Content-Type": {altType}})
	if err != nil {
		return "", nil, fmt.Errorf("email: part: %w", err)
	}
	if _, err := w.Write(alt); err != nil {
		return "", nil, fmt.Errorf("email: part: %w", err)
	}
	for _, in := range images {
		w, err := mp.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {in.ContentType},
			"Content-Transfer-Encoding": {"base64"},
			"Content-ID":                {"<" + in.ContentID + ">"},
			"Content-Disposition":       {"inline"},
		})
		if err != nil {
			return "", nil, fmt.Errorf("email: part: %w", err)
		}
		if err := writeBase64(w, in.Data); err != nil {
			return "", nil, fmt.Errorf("email: encode: %w", err)
		}
	}
	if err := mp.Close(); err != nil {
		return "", nil, fmt.Errorf("email: close: %w", err)
	}
	return "multipart/related; boundary=" + mp.Boundary(), buf.Bytes(), nil
}

// writeBase64 writes data as base64 wrapped at 76 characters (RFC 2045), which
// keeps every line far below the 998-character SMTP limit.
func writeBase64(w io.Writer, data []byte) error {
	enc := base64.StdEncoding.EncodeToString(data)
	for len(enc) > 0 {
		n := min(76, len(enc))
		if _, err := io.WriteString(w, enc[:n]+"\r\n"); err != nil {
			return err
		}
		enc = enc[n:]
	}
	return nil
}
