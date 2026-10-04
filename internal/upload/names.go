package upload

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

var errUnsupportedStorageKind = errors.New("unsupported stored upload kind")

// StorageName returns a content-addressed name for validated bytes that are
// actually stored. Image callers must pass the re-encoded stored bytes (WebP
// uploads are stored as PNG). Validation and re-encoding are prerequisites
// handled by the caller; this function does neither.
func StorageName(storedBytes []byte, storedKind Kind) (string, error) {
	var extension string
	switch storedKind {
	case JPEG:
		extension = ".jpg"
	case PNG:
		extension = ".png"
	case PDF:
		extension = ".pdf"
	case DOCX:
		extension = ".docx"
	default:
		return "", errUnsupportedStorageKind
	}

	digest := sha256.Sum256(storedBytes)
	return hex.EncodeToString(digest[:]) + extension, nil
}
