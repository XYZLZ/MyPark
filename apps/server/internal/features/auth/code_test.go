package auth

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestNewCode_RangeAndFormat(t *testing.T) {
	tests := []struct {
		name  string
		bytes []byte
		want  string
	}{
		{"zero", []byte{0, 0, 0, 0}, "000000"},
		{"max", []byte{0x00, 0x0F, 0x42, 0x3F}, "999999"},
		{"padded", []byte{0x00, 0x00, 0xA4, 0x99}, "042137"},
		// 0xFFFFFFFF is above the largest multiple of 10^6 that fits in
		// 32 bits, so it is rejected and the next four bytes are used.
		{"rejects biased top range", []byte{0xFF, 0xFF, 0xFF, 0xFF, 0x00, 0x00, 0xA4, 0x99}, "042137"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewCode(bytes.NewReader(tt.bytes))
			if err != nil {
				t.Fatalf("NewCode: %v", err)
			}
			if got != tt.want {
				t.Errorf("code = %q, want %q", got, tt.want)
			}
			if len(got) != 6 || strings.Trim(got, "0123456789") != "" {
				t.Errorf("code %q is not 6 digits", got)
			}
		})
	}
}

func TestNewCode_ReaderErrors(t *testing.T) {
	if _, err := NewCode(bytes.NewReader([]byte{1, 2})); err == nil {
		t.Fatal("short reader: want error")
	}
	if _, err := NewCode(errReader{}); err == nil {
		t.Fatal("failing reader: want error")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }
