package upload

import "errors"

const (
	defaultGlobalSizeLimit   int64 = 25 * 1024 * 1024
	defaultImageSizeLimit    int64 = 10 * 1024 * 1024
	defaultDocumentSizeLimit int64 = 25 * 1024 * 1024
)

var errInvalidSizeLimit = errors.New("upload size limit must be positive")

// SizeLimits contains the global and per-category upload size ceilings.
type SizeLimits struct {
	Global   int64
	Image    int64
	Document int64
}

// DefaultSizeLimits returns the accepted upload size defaults, in bytes.
func DefaultSizeLimits() SizeLimits {
	return SizeLimits{
		Global:   defaultGlobalSizeLimit,
		Image:    defaultImageSizeLimit,
		Document: defaultDocumentSizeLimit,
	}
}

// ValidateSize reports whether size is within the applicable upload size
// limits. A field-specific limit can tighten the global/category ceiling.
func ValidateSize(kind Kind, size int64, limits SizeLimits, fieldLimit *int64) error {
	if size < 0 {
		return errors.New("upload size must not be negative")
	}
	if limits.Global <= 0 || limits.Image <= 0 || limits.Document <= 0 {
		return errInvalidSizeLimit
	}

	var categoryLimit int64
	switch kind {
	case JPEG, PNG, WebP:
		categoryLimit = limits.Image
	case PDF, DOCX:
		categoryLimit = limits.Document
	default:
		return errors.New("unsupported upload kind")
	}

	effectiveLimit := limits.Global
	if categoryLimit < effectiveLimit {
		effectiveLimit = categoryLimit
	}
	if fieldLimit != nil {
		if *fieldLimit <= 0 {
			return errInvalidSizeLimit
		}
		if *fieldLimit < effectiveLimit {
			effectiveLimit = *fieldLimit
		}
	}
	if size > effectiveLimit {
		return errors.New("upload exceeds size limit")
	}
	return nil
}
