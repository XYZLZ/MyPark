package notifications

import (
	"bytes"
	"embed"
	"fmt"
	htmltemplate "html/template"
	"slices"
	"strings"
	texttemplate "text/template"
)

// Files starting with "_" (the shared brand partials) are skipped by a plain
// embed pattern, hence "all:".
//
//go:embed all:templates
var templateFS embed.FS

var (
	locales  = []string{"es-DO", "en"}
	allNames = []Template{TplVerifyEmail, TplPasswordReset, TplPasswordChanged, TplAccountExists, TplAdminInvite}
)

// Render renders template t for locale (es-DO or en) with data. subject is a
// single trimmed line; text is plain text; html is a document in which every
// data value is escaped. Both bodies carry the brand line from the shared
// partial. Missing data keys render as empty strings.
func Render(t Template, locale string, data map[string]string) (subject, text, html string, err error) {
	if !slices.Contains(locales, locale) {
		return "", "", "", fmt.Errorf("notifications: unsupported locale %q", locale)
	}
	if !slices.Contains(allNames, t) {
		return "", "", "", fmt.Errorf("notifications: unknown template %q", t)
	}
	base := "templates/" + locale + "/" + string(t)

	subject, err = renderText(templateFS, data, base+".subject.txt")
	if err != nil {
		return "", "", "", err
	}
	subject = strings.TrimSpace(subject)

	text, err = renderText(templateFS, data, "templates/_brand.txt", base+".txt")
	if err != nil {
		return "", "", "", err
	}

	hb, err := htmltemplate.New("").Option("missingkey=zero").
		ParseFS(templateFS, "templates/_brand.html", base+".html")
	if err != nil {
		return "", "", "", fmt.Errorf("notifications: parse %s.html: %w", t, err)
	}
	var buf bytes.Buffer
	if err := hb.ExecuteTemplate(&buf, string(t)+".html", data); err != nil {
		return "", "", "", fmt.Errorf("notifications: render %s.html: %w", t, err)
	}
	return subject, text, buf.String(), nil
}

// renderText parses the files (the last one is executed) as text templates.
func renderText(fsys embed.FS, data map[string]string, files ...string) (string, error) {
	tb, err := texttemplate.New("").Option("missingkey=zero").ParseFS(fsys, files...)
	if err != nil {
		return "", fmt.Errorf("notifications: parse %s: %w", files[len(files)-1], err)
	}
	name := files[len(files)-1][strings.LastIndexByte(files[len(files)-1], '/')+1:]
	var buf bytes.Buffer
	if err := tb.ExecuteTemplate(&buf, name, data); err != nil {
		return "", fmt.Errorf("notifications: render %s: %w", name, err)
	}
	return buf.String(), nil
}
