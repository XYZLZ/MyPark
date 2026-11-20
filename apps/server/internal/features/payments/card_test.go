package payments_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/payments"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/validation"
)

func TestNormalizeCardNumber(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"spaces", "4242 4242 4242 4242", "4242424242424242"},
		{"hyphens", "4242-4242-4242-4242", "4242424242424242"},
		{"mixed", " 4242 4242-4242 4242 ", "4242424242424242"},
		{"plain", "4242424242424242", "4242424242424242"},
		{"dots stay", "4242.4242", "4242.4242"},
		{"tabs stay", "4242\t4242", "4242\t4242"},
		{"empty", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := payments.NormalizeCardNumber(tc.in); got != tc.want {
				t.Errorf("NormalizeCardNumber(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestLuhnAndBrand(t *testing.T) {
	tests := []struct {
		number string
		luhn   bool
		brand  string
	}{
		{"4242424242424242", true, "visa"},
		{"4000000000000002", true, "visa"},
		{"5555555555554444", true, "mastercard"},
		{"2223003122003222", true, "mastercard"},
		{"378282246310005", true, "amex"},
		{"6011111111111117", true, "other"},
		{"4242424242424241", false, "visa"},
		{"5100000000000000", false, "mastercard"},
		{"2221000000000000", false, "mastercard"},
		{"2720999999999999", false, "mastercard"},
		{"2220999999999999", false, "other"},
		{"2721000000000000", false, "other"},
		{"5600000000000000", false, "other"},
		{"3400000000000000", true, "amex"},
		{"", false, "other"},
		{"4", false, "visa"},
		{"4242 4242", false, "visa"},
		{"abc", false, "other"},
	}
	for _, tc := range tests {
		t.Run(tc.number, func(t *testing.T) {
			if got := payments.Luhn(tc.number); got != tc.luhn {
				t.Errorf("Luhn(%q) = %v, want %v", tc.number, got, tc.luhn)
			}
			if got := payments.Brand(tc.number); got != tc.brand {
				t.Errorf("Brand(%q) = %q, want %q", tc.number, got, tc.brand)
			}
		})
	}
}

func TestExpiryValid(t *testing.T) {
	// 2026-12-01T03:00Z is Nov 30 23:00 in Santo Domingo (UTC-4).
	beforeMidnight := time.Date(2026, 12, 1, 3, 0, 0, 0, time.UTC)
	afterMidnight := time.Date(2026, 12, 1, 5, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		month, year int
		now         time.Time
		want        bool
	}{
		{"current month ok", 11, 2026, beforeMidnight, true},
		{"previous month not", 10, 2026, beforeMidnight, false},
		{"month just ended", 11, 2026, afterMidnight, false},
		{"next month ok", 12, 2026, afterMidnight, true},
		{"next year ok", 1, 2027, afterMidnight, true},
		{"past year not", 12, 2025, afterMidnight, false},
		{"month 0 invalid", 0, 2030, afterMidnight, false},
		{"month 13 invalid", 13, 2030, afterMidnight, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := payments.ExpiryValid(tc.month, tc.year, tc.now); got != tc.want {
				t.Errorf("ExpiryValid(%d, %d, %v) = %v, want %v", tc.month, tc.year, tc.now, got, tc.want)
			}
		})
	}
}

// validPay is a request every rule accepts at validNow.
func validPay() payments.PayRequest {
	return payments.PayRequest{CardNumber: "4242424242424242", ExpMonth: 12, ExpYear: 2030, CVC: "123"}
}

var validNow = time.Date(2026, 11, 20, 15, 0, 0, 0, time.UTC)

func validate(t *testing.T, r payments.PayRequest) error {
	t.Helper()
	return validation.New().StructCtx(validation.WithNow(t.Context(), validNow), r)
}

func TestCardRequest_Validation(t *testing.T) {
	// Sentinel values that must never come back in an error.
	const junkPAN, junkCVC = "4242abcd4242", "99887"
	tests := []struct {
		name      string
		mutate    func(*payments.PayRequest)
		field     string // "" = valid
		rule      string
		forbidden []string
	}{
		{"valid", func(*payments.PayRequest) {}, "", "", nil},
		{"spaces accepted after normalizing", func(r *payments.PayRequest) { r.CardNumber = payments.NormalizeCardNumber("4242 4242 4242 4242") }, "", "", nil},
		{"hyphens accepted after normalizing", func(r *payments.PayRequest) { r.CardNumber = payments.NormalizeCardNumber("4242-4242-4242-4242") }, "", "", nil},
		{"amex 15 digits", func(r *payments.PayRequest) { r.CardNumber = "378282246310005"; r.CVC = "1234" }, "", "", nil},
		{"two-digit year", func(r *payments.PayRequest) { r.ExpYear = 28 }, "exp_year", "min", []string{"28"}},
		{"month 13", func(r *payments.PayRequest) { r.ExpMonth = 13 }, "exp_month", "max", []string{"13"}},
		{"month 0 is missing", func(r *payments.PayRequest) { r.ExpMonth = 0 }, "exp_month", "required", nil},
		{"five-digit cvc", func(r *payments.PayRequest) { r.CVC = junkCVC }, "cvc", "max", []string{junkCVC}},
		{"short cvc", func(r *payments.PayRequest) { r.CVC = "12" }, "cvc", "min", []string{"12"}},
		{"signed cvc", func(r *payments.PayRequest) { r.CVC = "+123" }, "cvc", "number", []string{"+123"}},
		{"decimal cvc", func(r *payments.PayRequest) { r.CVC = "1.23" }, "cvc", "number", []string{"1.23"}},
		{"letters in number", func(r *payments.PayRequest) { r.CardNumber = junkPAN }, "card_number", "number", []string{junkPAN}},
		{"abc", func(r *payments.PayRequest) { r.CardNumber = "abc" }, "card_number", "number", []string{"abc"}},
		{"signed number", func(r *payments.PayRequest) { r.CardNumber = "+424242424242424" }, "card_number", "number", []string{"+4242"}},
		{"dotted number", func(r *payments.PayRequest) { r.CardNumber = "4242.4242" }, "card_number", "number", []string{"4242.4242"}},
		{"luhn fails", func(r *payments.PayRequest) { r.CardNumber = "4242424242424241" }, "card_number", "luhn", []string{"4242424242424241"}},
		{"too long", func(r *payments.PayRequest) { r.CardNumber = strings.Repeat("0", 20) }, "card_number", "max", nil},
		{"expired", func(r *payments.PayRequest) { r.ExpMonth, r.ExpYear = 10, 2026 }, "exp_year", "card_expiry", nil},
		{"current month ok", func(r *payments.PayRequest) { r.ExpMonth, r.ExpYear = 11, 2026 }, "", "", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := validPay()
			tc.mutate(&r)
			err := validate(t, r)
			if tc.field == "" {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				return
			}
			verr, ok := errors.AsType[*validation.Error](err)
			if !ok {
				t.Fatalf("err = %T %v, want *validation.Error", err, err)
			}
			if len(verr.Fields) != 1 || verr.Fields[0].Field != tc.field || verr.Fields[0].Rule != tc.rule {
				t.Fatalf("Fields = %+v, want one %s/%s", verr.Fields, tc.field, tc.rule)
			}
			dump := verr.Error() + fmt.Sprintf("%+v", verr.Fields)
			for _, f := range tc.forbidden {
				if strings.Contains(dump, f) {
					t.Errorf("error %q echoes input %q", dump, f)
				}
			}
		})
	}
}

func TestCardRequest_ExpiryNeedsNow(t *testing.T) {
	// Without a clock reading the expiry rule fails closed.
	if err := validation.New().Struct(validPay()); err == nil {
		t.Fatal("Struct without now accepted a card_expiry field")
	}
}

func TestCard_NeverPrintsPAN(t *testing.T) {
	c := payments.Card{Number: "4242424242424242", ExpMonth: 12, ExpYear: 2030, CVC: "123"}
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("charge", "card", c, "ptr", &c)
	outputs := map[string]string{
		"%v":     fmt.Sprintf("%v", c),
		"%+v":    fmt.Sprintf("%+v", c),
		"%#v":    fmt.Sprintf("%#v", c),
		"Sprint": fmt.Sprint(c),
		"%v*":    fmt.Sprintf("%v", &c),
		"%+v*":   fmt.Sprintf("%+v", &c),
		"slog":   buf.String(),
	}
	for name, out := range outputs {
		if !strings.Contains(out, "card(****4242)") {
			t.Errorf("%s = %q, want card(****4242)", name, out)
		}
		for _, secret := range []string{"4242424242424242", "123\"", "CVC", "2030"} {
			if strings.Contains(out, secret) {
				t.Errorf("%s = %q leaks %q", name, out, secret)
			}
		}
	}
	if c.Last4() != "4242" {
		t.Errorf("Last4() = %q", c.Last4())
	}
	if got := (payments.Card{Number: "12"}).String(); got != "card(****)" {
		t.Errorf("short number String() = %q", got)
	}
}

func TestFingerprint(t *testing.T) {
	r := uuid.MustParse("0197a1b2-0000-7000-8000-000000000001")
	base := payments.Fingerprint(r, "visa", "4242", 12, 2030)
	if len(base) != 32 {
		t.Fatalf("len = %d, want 32", len(base))
	}
	if !bytes.Equal(base, payments.Fingerprint(r, "visa", "4242", 12, 2030)) {
		t.Error("fingerprint is not stable")
	}
	variants := map[string][]byte{
		"brand":       payments.Fingerprint(r, "mastercard", "4242", 12, 2030),
		"last4":       payments.Fingerprint(r, "visa", "4243", 12, 2030),
		"exp_month":   payments.Fingerprint(r, "visa", "4242", 11, 2030),
		"exp_year":    payments.Fingerprint(r, "visa", "4242", 12, 2031),
		"reservation": payments.Fingerprint(uuid.MustParse("0197a1b2-0000-7000-8000-000000000002"), "visa", "4242", 12, 2030),
	}
	for name, v := range variants {
		if bytes.Equal(base, v) {
			t.Errorf("fingerprint unchanged when %s changes", name)
		}
	}
}

func TestCardAndPayRequest_NeverEncodePANOrCVC(t *testing.T) {
	c := payments.Card{Number: "4242424242424242", ExpMonth: 12, ExpYear: 2030, CVC: "987"}
	pay := payments.PayRequest{CardNumber: "4242424242424242", ExpMonth: 12, ExpYear: 2030, CVC: "987"}
	charge := payments.ChargeRequest{IdempotencyKey: "rsv-fee:x", AmountCents: 15000, Card: &c}

	var slogOut bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&slogOut, nil))
	log.Info("charge", "req", charge, "pay", pay, "payptr", &pay, "card", &c)

	marshal := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	outputs := map[string]string{
		"json ChargeRequest": marshal(charge),
		"json PayRequest":    marshal(pay),
		"json *PayRequest":   marshal(&pay),
		"json Card":          marshal(c),
		"%v":                 fmt.Sprintf("%v", pay),
		"%+v":                fmt.Sprintf("%+v", pay),
		"%#v":                fmt.Sprintf("%#v", pay),
		"%+v*":               fmt.Sprintf("%+v", &pay),
		"slog":               slogOut.String(),
	}
	for name, out := range outputs {
		if !strings.Contains(out, "****4242") {
			t.Errorf("%s = %q, want the masked last four", name, out)
		}
		for _, secret := range []string{"4242424242424242", "987"} {
			if strings.Contains(out, secret) {
				t.Errorf("%s = %q leaks %q", name, out, secret)
			}
		}
	}
}

func TestPayRequest_DecodingIsUnchanged(t *testing.T) {
	var got payments.PayRequest
	body := `{"card_number":"4242424242424242","exp_month":12,"exp_year":2030,"cvc":"123"}`
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	if got != validPay() {
		t.Errorf("decoded = %#v, want the full request", got.CardNumber)
	}
}
