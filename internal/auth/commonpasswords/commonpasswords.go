// Package commonpasswords provides exact membership checks against a pinned,
// public common-password corpus. It does not define password policy.
package commonpasswords

import (
	_ "embed"
	"strings"
)

//go:embed 10k-most-common.txt
var corpus string

var passwords = func() map[string]struct{} {
	lines := strings.Split(corpus, "\n")
	set := make(map[string]struct{}, len(lines))
	for _, password := range lines {
		if password != "" {
			set[password] = struct{}{}
		}
	}
	return set
}()

// Contains reports whether password is an exact, case-sensitive corpus entry.
// Input bytes are not normalized, trimmed, or otherwise transformed.
func Contains(password string) bool {
	_, ok := passwords[password]
	return ok
}
