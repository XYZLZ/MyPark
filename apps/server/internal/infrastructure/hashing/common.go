package hashing

import (
	_ "embed"
	"strings"
	"sync"
)

//go:embed common_passwords.txt
var commonPasswordsRaw string

// commonPasswords returns the lower-cased common-password set, built once.
// Lines starting with '#' (the source/license header) are skipped.
var commonPasswords = sync.OnceValue(func() map[string]struct{} {
	set := make(map[string]struct{}, 10_000)
	for line := range strings.Lines(commonPasswordsRaw) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		set[strings.ToLower(line)] = struct{}{}
	}
	return set
})

// IsCommonPassword reports whether pw appears in the embedded list of the
// 10k most common passwords, ignoring case.
func IsCommonPassword(pw string) bool {
	_, ok := commonPasswords()[strings.ToLower(pw)]
	return ok
}
