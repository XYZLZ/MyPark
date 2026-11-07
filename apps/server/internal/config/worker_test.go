package config_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/XYZLZ/MyPark/apps/server/internal/config"
)

func workerEnv(extra map[string]string) func(string) string {
	env := map[string]string{
		"DATABASE_URL": "postgres://u:pw@db:5432/mypark",
		"SMTP_HOST":    "mailpit",
		"SMTP_PORT":    "1025",
		"SMTP_FROM":    "MyPark <no-reply@mypark.local>",
	}
	for k, v := range extra {
		env[k] = v
	}
	return func(k string) string { return env[k] }
}

func TestLoadWorker_DefaultsAndRequired(t *testing.T) {
	c, err := config.LoadWorker(workerEnv(nil))
	if err != nil {
		t.Fatalf("LoadWorker: %v", err)
	}
	if c.HealthAddr() != ":8091" || c.SMTPHost() != "mailpit" || c.SMTPPort() != 1025 ||
		c.SMTPFrom() != "MyPark <no-reply@mypark.local>" || c.SMTPStartTLS() ||
		c.SMTPUsername().Reveal() != "" || c.SMTPPassword().Reveal() != "" ||
		c.DatabaseURL() != "postgres://u:pw@db:5432/mypark" {
		t.Errorf("defaults wrong: %v", c)
	}

	c, err = config.LoadWorker(workerEnv(map[string]string{
		"WORKER_HEALTH_ADDR": "127.0.0.1:0", "SMTP_USERNAME": "u", "SMTP_PASSWORD": "sm-secret", "SMTP_STARTTLS": "true",
	}))
	if err != nil || c.HealthAddr() != "127.0.0.1:0" || !c.SMTPStartTLS() || c.SMTPUsername().Reveal() != "u" || c.SMTPPassword().Reveal() != "sm-secret" {
		t.Fatalf("overrides: %v, %v", c, err)
	}
	if p := fmt.Sprintf("%v %+v %#v", c, c, c); strings.Contains(p, ":pw@") || strings.Contains(p, "sm-secret") {
		t.Errorf("config printing leaks credentials: %s", p)
	}

	_, err = config.LoadWorker(func(string) string { return "" })
	if err == nil {
		t.Fatal("empty env: want error")
	}
	for _, name := range []string{"DATABASE_URL", "SMTP_HOST", "SMTP_PORT", "SMTP_FROM"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not name %s", err, name)
		}
	}
}

func TestLoadWorker_RejectsInvalid(t *testing.T) {
	cases := map[string]map[string]string{
		"SMTP_PORT":          {"SMTP_PORT": "0"},
		"SMTP_PORT nan":      {"SMTP_PORT": "abc"},
		"SMTP_FROM":          {"SMTP_FROM": "not an address"},
		"SMTP_STARTTLS":      {"SMTP_STARTTLS": "maybe"},
		"SMTP_PASSWORD":      {"SMTP_USERNAME": "u"},
		"WORKER_HEALTH_ADDR": {"WORKER_HEALTH_ADDR": "nope"},
	}
	for name, extra := range cases {
		if _, err := config.LoadWorker(workerEnv(extra)); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	if _, err := config.LoadWorker(workerEnv(map[string]string{"SMTP_USERNAME": "u", "SMTP_PASSWORD": "topsecret", "SMTP_PORT": "x"})); err != nil && strings.Contains(err.Error(), "topsecret") {
		t.Errorf("error echoes the password: %v", err)
	}
}
