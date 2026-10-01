package auth

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"io"
	"runtime"
	"strings"
	"testing"
)

// Generated with argon2 20190702-6:
// printf '%s' 'password' | argon2 '0123456789abcdef' -id -k 65536 -t 3 -p 4 -l 32 -v 13 -e
const passwordReferenceHash = "$argon2id$v=19$m=65536,t=3,p=4$MDEyMzQ1Njc4OWFiY2RlZg$uKZLaN6muIyoyIYr5waqw3y+zaDbe9aLSPj6Ln/rbz4"

// These tests must remain sequential: each derivation uses 64 MiB, and the
// entropy tests temporarily replace the shared crypto/rand.Reader.
func TestPasswordHashReferenceVector(t *testing.T) {
	entropy := strings.NewReader("0123456789abcdef")
	usePasswordEntropy(t, entropy)
	encoded, err := Hash("password")
	if err != nil {
		t.Fatal(err)
	}
	if encoded != passwordReferenceHash {
		t.Fatalf("Hash does not match the fixed Argon2id reference vector: %q", encoded)
	}
	if entropy.Len() != 0 {
		t.Fatal("Hash did not consume the 16-byte salt from the cryptographic random source")
	}
	assertPasswordVerification(t, "password", passwordReferenceHash, true)
	assertPasswordVerification(t, "wrong password", passwordReferenceHash, false)

	// A canonical, correctly sized but different tag is a successful mismatch,
	// not a malformed-hash error.
	fields := strings.Split(passwordReferenceHash, "$")
	digest, err := base64.RawStdEncoding.DecodeString(fields[5])
	if err != nil {
		t.Fatal(err)
	}
	digest[0] ^= 1
	fields[5] = base64.RawStdEncoding.EncodeToString(digest)
	assertPasswordVerification(t, "password", strings.Join(fields, "$"), false)
}

func TestPasswordHashRoundTripExactBytes(t *testing.T) {
	// Includes leading/trailing whitespace, mixed case, composed and decomposed
	// Unicode, a NUL, invalid UTF-8 and a suffix beyond common truncation limits.
	opaque := " \tMiXeD caf\u00e9 e\u0301\x00\xff\xfe" + strings.Repeat("x", 1024) + " tail\n"
	for _, tc := range []struct {
		name     string
		password string
	}{
		{"empty", ""},
		{"opaque", opaque},
	} {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := Hash(tc.password)
			if err != nil {
				t.Fatal(err)
			}
			passwordSalt(t, encoded)
			assertPasswordVerification(t, tc.password, encoded, true)
			if tc.name == "empty" {
				assertPasswordVerification(t, "\x00", encoded, false)
				return
			}
			for _, different := range []string{
				strings.TrimSpace(opaque),
				strings.ToLower(opaque),
				strings.Replace(opaque, "caf\u00e9", "cafe\u0301", 1),
				strings.Replace(opaque, "e\u0301", "\u00e9", 1),
				strings.SplitN(opaque, "\x00", 2)[0],
				strings.Replace(opaque, "\xff\xfe", "\ufffd\ufffd", 1),
				strings.Replace(opaque, " tail\n", " other\n", 1),
			} {
				assertPasswordVerification(t, different, encoded, false)
			}
		})
	}
}

func TestPasswordHashFreshSalts(t *testing.T) {
	var previous []byte
	for i := 0; i < 2; i++ {
		encoded, err := Hash("same password")
		if err != nil {
			t.Fatal(err)
		}
		salt := passwordSalt(t, encoded)
		if previous != nil && bytes.Equal(previous, salt) {
			t.Fatal("hashing the same password reused its salt")
		}
		previous = salt
	}
}

func TestPasswordHashEntropyFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reader io.Reader
	}{
		{"unavailable", passwordEntropyErrorReader{}},
		{"short_salt", strings.NewReader("only-fifteen!!!")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			usePasswordEntropy(t, tc.reader)
			if _, err := Hash("password"); err == nil {
				t.Fatal("Hash must return an error when a complete random salt is unavailable")
			}
		})
	}
}

func TestPasswordVerifyRejectsInvalidEncodingsBeforeDerivation(t *testing.T) {
	fields := strings.Split(passwordReferenceHash, "$")
	salt, digest := fields[4], fields[5]
	phc := func(algorithm, version, params, s, d string) string {
		return "$" + algorithm + "$" + version + "$" + params + "$" + s + "$" + d
	}
	withSalt := func(s string) string { return phc("argon2id", "v=19", "m=65536,t=3,p=4", s, digest) }
	withDigest := func(d string) string { return phc("argon2id", "v=19", "m=65536,t=3,p=4", salt, d) }
	cases := []struct {
		name    string
		encoded string
	}{
		{"empty", ""},
		{"plaintext", "password"},
		{"missing_leading_separator", passwordReferenceHash[1:]},
		{"missing_digest", strings.Join(fields[:5], "$")},
		{"missing_version", "$argon2id$m=65536,t=3,p=4$" + salt + "$" + digest},
		{"leading_whitespace", " " + passwordReferenceHash},
		{"trailing_whitespace", passwordReferenceHash + " "},
		{"trailing_newline", passwordReferenceHash + "\n"},
		{"trailing_nul", passwordReferenceHash + "\x00"},
		{"trailing_field", passwordReferenceHash + "$extra"},
		{"trailing_separator", passwordReferenceHash + "$"},
		{"second_hash", passwordReferenceHash + passwordReferenceHash},
		{"oversized_value", passwordReferenceHash + strings.Repeat("x", 1<<20)},
		{"empty_salt", withSalt("")},
		{"empty_digest", withDigest("")},
		{"salt_padding", withSalt(salt + "==")},
		{"digest_padding", withDigest(digest + "=")},
		{"salt_nonzero_padding_bits", withSalt(salt[:len(salt)-1] + "h")},
		{"digest_nonzero_padding_bits", withDigest(digest[:len(digest)-1] + "5")},
		{"salt_invalid_alphabet", withSalt("!" + salt[1:])},
		{"digest_invalid_alphabet", withDigest("!" + digest[1:])},
		{"salt_embedded_space", withSalt(salt[:4] + " " + salt[4:])},
		{"salt_embedded_crlf", withSalt(salt[:4] + "\r\n" + salt[4:])},
		{"digest_embedded_crlf", withDigest(digest[:4] + "\r\n" + digest[4:])},
		{"salt_invalid_utf8", withSalt("\xff" + salt[1:])},
		{"salt_url_alphabet", withSalt(base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0xfb}, 16)))},
		{"digest_url_alphabet", withDigest(strings.NewReplacer("+", "-", "/", "_").Replace(digest))},
		{"oversized_salt", withSalt(strings.Repeat("A", 1<<20))},
		{"oversized_digest", withDigest(strings.Repeat("A", 1<<20))},
		{"short_salt", withSalt(base64.RawStdEncoding.EncodeToString(make([]byte, 15)))},
		{"long_salt", withSalt(base64.RawStdEncoding.EncodeToString(make([]byte, 17)))},
		{"short_digest", withDigest(base64.RawStdEncoding.EncodeToString(make([]byte, 31)))},
		{"long_digest", withDigest(base64.RawStdEncoding.EncodeToString(make([]byte, 33)))},
	}
	for _, algorithm := range []string{"argon2i", "argon2d", "Argon2id", "bcrypt", ""} {
		cases = append(cases, struct{ name, encoded string }{"algorithm_" + algorithm, phc(algorithm, "v=19", "m=65536,t=3,p=4", salt, digest)})
	}
	for _, version := range []string{"v=16", "v=20", "v=019", "v=+19", "V=19", "v=19,extra=1", ""} {
		cases = append(cases, struct{ name, encoded string }{"version_" + version, phc("argon2id", version, "m=65536,t=3,p=4", salt, digest)})
	}
	for _, params := range []string{
		"m=8192,t=3,p=4", "m=65537,t=3,p=4", "m=0,t=3,p=4",
		"m=65536,t=2,p=4", "m=65536,t=4,p=4", "m=65536,t=0,p=4",
		"m=65536,t=3,p=1", "m=65536,t=3,p=0", "m=65536,t=3,p=256",
		"t=3,m=65536,p=4", "m=65536,p=4,t=3", "m=65536,t=3",
		"m=65536,t=3,p=4,extra=1", "m=65536,t=3,p=4,p=4",
		"m=065536,t=3,p=4", "m=+65536,t=3,p=4", "m=65536,t=03,p=4", "m=65536,t=3,p=04",
		"m=65536, t=3,p=4", "m=65536,t=3,p=4 ", "m=65536,t=3,p=4,",
		"m=-1,t=3,p=4", "m=4294967296,t=3,p=4", "m=65536,t=4294967296,p=4",
		"m=" + strings.Repeat("9", 100) + ",t=3,p=4", "",
	} {
		cases = append(cases, struct{ name, encoded string }{"parameters_" + params, phc("argon2id", "v=19", params, salt, digest)})
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// TotalAlloc is cumulative, so a GC cannot hide a derived workspace.
			// Leave ample parsing headroom but less than one 64 MiB derivation.
			// This checks work ordering without a machine-dependent time limit.
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			matched, err := Verify("password", tc.encoded)
			runtime.ReadMemStats(&after)
			if matched || err == nil {
				t.Errorf("invalid encoding: matched=%v, err=%v; want false and an error", matched, err)
			}
			if allocated := after.TotalAlloc - before.TotalAlloc; allocated >= 32<<20 {
				t.Errorf("invalid encoding allocated %d bytes; reject before Argon2 derivation", allocated)
			}
		})
	}
}

func assertPasswordVerification(t *testing.T, password, encoded string, want bool) {
	t.Helper()
	matched, err := Verify(password, encoded)
	if err != nil || matched != want {
		t.Fatalf("Verify: matched=%v, err=%v; want matched=%v, nil error", matched, err, want)
	}
}

func passwordSalt(t *testing.T, encoded string) []byte {
	t.Helper()
	fields := strings.Split(encoded, "$")
	if len(fields) != 6 || fields[0] != "" || fields[1] != "argon2id" || fields[2] != "v=19" || fields[3] != "m=65536,t=3,p=4" {
		t.Fatalf("Hash returned a non-contract PHC encoding: %q", encoded)
	}
	var salt []byte
	for i, size := range []int{16, 32} {
		field := fields[4+i]
		decoded, err := base64.RawStdEncoding.Strict().DecodeString(field)
		if err != nil || len(decoded) != size || base64.RawStdEncoding.EncodeToString(decoded) != field {
			t.Fatalf("PHC field %d is not canonical Base64 for %d bytes", 4+i, size)
		}
		if i == 0 {
			salt = decoded
		}
	}
	return salt
}

func usePasswordEntropy(t *testing.T, reader io.Reader) {
	t.Helper()
	previous := rand.Reader
	rand.Reader = reader
	t.Cleanup(func() { rand.Reader = previous })
}

type passwordEntropyErrorReader struct{}

func (passwordEntropyErrorReader) Read([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}
