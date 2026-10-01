package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	passwordTime        = 3
	passwordMemory      = 65536 // KiB
	passwordParallelism = 4
	passwordSaltSize    = 16
	passwordTagSize     = 32
	passwordSaltEncoded = 22
	passwordTagEncoded  = 43
	passwordPrefix      = "$argon2id$v=19$m=65536,t=3,p=4$"
	passwordEncodedSize = len(passwordPrefix) + passwordSaltEncoded + 1 + passwordTagEncoded
)

var errPasswordEncoding = errors.New("auth: malformed or unsupported password hash")

// Hash derives a fixed-profile Argon2id PHC hash from the exact password bytes
// with a fresh cryptographically random salt. Salt failures return no hash.
func Hash(password string) (string, error) {
	salt := make([]byte, passwordSaltSize)
	// rand.Read fatally aborts on errors in Go 1.27. Read the source directly
	// so an incomplete salt or reader error can be returned to the caller.
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return "", fmt.Errorf("auth: generate password salt: %w", err)
	}
	tag := argon2.IDKey([]byte(password), salt, passwordTime, passwordMemory, passwordParallelism, passwordTagSize)
	return passwordPrefix + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(tag), nil
}

// Verify compares the exact password bytes against a fixed-profile PHC hash.
// A password mismatch returns false, nil; invalid hashes return false, error
// before any Argon2 derivation.
func Verify(password, encoded string) (bool, error) {
	// Bound parsing and reject all unsupported profiles without interpreting
	// untrusted cost parameters or allocating a derivation workspace.
	if len(encoded) != passwordEncodedSize || !strings.HasPrefix(encoded, passwordPrefix) {
		return false, errPasswordEncoding
	}
	saltEnd := len(passwordPrefix) + passwordSaltEncoded
	if encoded[saltEnd] != '$' {
		return false, errPasswordEncoding
	}
	salt, err := decodePasswordField(encoded[len(passwordPrefix):saltEnd], passwordSaltSize)
	if err != nil {
		return false, err
	}
	tag, err := decodePasswordField(encoded[saltEnd+1:], passwordTagSize)
	if err != nil {
		return false, err
	}
	candidate := argon2.IDKey([]byte(password), salt, passwordTime, passwordMemory, passwordParallelism, passwordTagSize)
	return subtle.ConstantTimeCompare(candidate, tag) == 1, nil
}

func decodePasswordField(field string, size int) ([]byte, error) {
	decoded, err := base64.RawStdEncoding.Strict().DecodeString(field)
	// Strict decoding rejects nonzero padding bits but still ignores CR/LF.
	// Re-encoding also rules out those and any other noncanonical spelling.
	if err != nil || len(decoded) != size || base64.RawStdEncoding.EncodeToString(decoded) != field {
		return nil, errPasswordEncoding
	}
	return decoded, nil
}
