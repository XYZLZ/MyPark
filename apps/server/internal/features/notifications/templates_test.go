package notifications_test

import (
	"strings"
	"testing"
	"time"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/notifications"
)

var allTemplates = []notifications.Template{
	notifications.TplVerifyEmail, notifications.TplPasswordReset, notifications.TplPasswordChanged,
	notifications.TplAccountExists, notifications.TplAdminInvite,
}

const brandLine = "MyPark · Banreservas Zona Colonial"

func TestTemplates_ParityAndRender(t *testing.T) {
	for _, tpl := range allTemplates {
		for _, locale := range []string{"es-DO", "en"} {
			t.Run(string(tpl)+"/"+locale, func(t *testing.T) {
				subject, text, html, err := notifications.Render(tpl, locale,
					map[string]string{"first_name": "Ana", "last_name": "Pérez", "code": "123456"})
				if err != nil {
					t.Fatalf("Render: %v", err)
				}
				if strings.TrimSpace(subject) == "" || strings.ContainsAny(subject, "\r\n") {
					t.Errorf("subject = %q, want one non-empty line", subject)
				}
				if !strings.Contains(text, brandLine) || !strings.Contains(html, brandLine) {
					t.Errorf("brand line missing (text %t, html %t)", strings.Contains(text, brandLine), strings.Contains(html, brandLine))
				}
				if !strings.Contains(html, "<html") {
					t.Errorf("html is not a document: %.60q", html)
				}
				if hasCode := tpl == notifications.TplVerifyEmail || tpl == notifications.TplPasswordReset || tpl == notifications.TplAdminInvite; hasCode {
					if !strings.Contains(text, "123456") || !strings.Contains(html, "123456") {
						t.Errorf("code missing from text or html")
					}
				}
				_, _, evil, err := notifications.Render(tpl, locale, map[string]string{"first_name": "<b>Ana</b>", "code": "123456"})
				if err != nil {
					t.Fatalf("Render evil: %v", err)
				}
				if strings.Contains(evil, "<b>Ana") {
					t.Errorf("html did not escape first_name")
				}
			})
		}
	}
}

func TestTemplates_LocalesDiffer(t *testing.T) {
	es, _, _, _ := notifications.Render(notifications.TplVerifyEmail, "es-DO", nil)
	en, _, _, _ := notifications.Render(notifications.TplVerifyEmail, "en", nil)
	if es == "" || en == "" || es == en {
		t.Errorf("subjects es=%q en=%q, want two different non-empty subjects", es, en)
	}
}

func TestTemplates_UnknownLocaleErrors(t *testing.T) {
	if _, _, _, err := notifications.Render(notifications.TplVerifyEmail, "fr", nil); err == nil {
		t.Error("unknown locale: want error")
	}
	if _, _, _, err := notifications.Render("nope", "en", nil); err == nil {
		t.Error("unknown template: want error")
	}
	if _, _, _, err := notifications.Render(notifications.TplVerifyEmail, "../en", nil); err == nil {
		t.Error("path-like locale: want error")
	}
}

func TestBackoff_Schedule(t *testing.T) {
	cases := map[int]time.Duration{
		1: 10 * time.Second, 2: 20 * time.Second, 7: 10*time.Minute + 40*time.Second,
		8: 15 * time.Minute, 100: 15 * time.Minute,
	}
	for attempts, want := range cases {
		if got := notifications.Backoff(attempts); got != want {
			t.Errorf("Backoff(%d) = %v, want %v", attempts, got, want)
		}
	}
}

func TestTemplates_ReservationConfirmedRenders(t *testing.T) {
	const token = "SECRET-QR-TOKEN-0123456789"
	data := map[string]string{
		"reservation_id": "0198f5a0-0000-7000-8000-000000000001", "arrival_at": "2026-12-01 11:02",
		"window": "2026-12-01 10:50 - 2026-12-01 11:25", "plate_display": "A123456", "fee_cents": "15000",
		"qr_token": token,
	}
	for _, locale := range []string{"es-DO", "en"} {
		t.Run(locale, func(t *testing.T) {
			subject, text, html, err := notifications.Render(notifications.TplReservationConfirmed, locale, data)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if strings.TrimSpace(subject) == "" || strings.ContainsAny(subject, "\r\n") {
				t.Errorf("subject = %q, want one non-empty line", subject)
			}
			for _, want := range []string{"2026-12-01 11:02", "2026-12-01 10:50 - 2026-12-01 11:25", "A123456", brandLine} {
				if !strings.Contains(text, want) || !strings.Contains(html, want) {
					t.Errorf("%q missing (text %t, html %t)", want, strings.Contains(text, want), strings.Contains(html, want))
				}
			}
			if !strings.Contains(html, `src="cid:qr"`) {
				t.Errorf("html lacks the inline image reference: %s", html)
			}
			for name, body := range map[string]string{"subject": subject, "text": text, "html": html} {
				if strings.Contains(body, token) {
					t.Errorf("%s contains the QR token", name)
				}
			}
		})
	}
	es, _, _, _ := notifications.Render(notifications.TplReservationConfirmed, "es-DO", data)
	en, _, _, _ := notifications.Render(notifications.TplReservationConfirmed, "en", data)
	if es == en {
		t.Error("the two locales must have different subjects")
	}
}

func TestTemplates_ReservationCancelled(t *testing.T) {
	for _, locale := range []string{"es-DO", "en"} {
		pending := map[string]string{"refund_status": "pending", "refund_reason": "cancelled_before_arrival", "fee_cents": "15000"}
		none := map[string]string{"refund_status": "none", "refund_reason": "none_after_arrival", "fee_cents": "15000"}
		sp, tp, hp, err := notifications.Render(notifications.TplReservationCancelled, locale, pending)
		if err != nil {
			t.Fatalf("%s pending: %v", locale, err)
		}
		sn, tn, hn, err := notifications.Render(notifications.TplReservationCancelled, locale, none)
		if err != nil {
			t.Fatalf("%s none: %v", locale, err)
		}
		if sp == "" || sn == "" || tp == tn || hp == hn {
			t.Errorf("%s: the refund wording must differ by refund_status (subject %q/%q)", locale, sp, sn)
		}
		for _, s := range []string{tp, hp, tn, hn} {
			if !strings.Contains(s, brandLine) {
				t.Errorf("%s: brand line missing", locale)
			}
		}
	}
}

func TestTemplates_RefundSucceeded(t *testing.T) {
	data := map[string]string{"reservation_id": "r", "amount_cents": "15000", "amount_display": "RD$150.00"}
	subjects := map[string]string{}
	for _, locale := range []string{"es-DO", "en"} {
		subject, text, html, err := notifications.Render(notifications.TplRefundSucceeded, locale, data)
		if err != nil {
			t.Fatalf("%s: %v", locale, err)
		}
		subjects[locale] = subject
		for _, body := range []string{text, html} {
			if !strings.Contains(body, "RD$150.00") || !strings.Contains(body, brandLine) {
				t.Errorf("%s: amount or brand line missing: %s", locale, body)
			}
		}
	}
	if subjects["es-DO"] == subjects["en"] || subjects["en"] == "" {
		t.Errorf("subjects = %v, want one per locale", subjects)
	}
}
