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
