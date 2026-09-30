// Package content defines persisted public content and its repository boundary.
package content

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"
)

var ErrNotFound = errors.New("content not found")

// Snapshot contains the immutable values visible to visitors.
type Snapshot struct {
	ID, ItemID, Title, Path string
	Fields                  map[string]string
}

// Repository resolves published snapshots without exposing storage details.
type Repository interface {
	PublishedByPath(context.Context, string) (Snapshot, error)
}

// NewID generates a UUIDv7 using the supplied clock value and secure randomness.
func NewID(at time.Time) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	ms := uint64(at.UnixMilli())
	for i := 5; i >= 0; i-- {
		b[i] = byte(ms)
		ms >>= 8
	}
	b[6] = (b[6] & 0x0f) | 0x70
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}
