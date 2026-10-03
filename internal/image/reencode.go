// Package image provides bounded image processing for uploaded content.
package image

import (
	"bytes"
	"errors"
	stdimage "image"
	"image/jpeg"
	"image/png"

	"github.com/markdlabrecque/composure/internal/upload"
	_ "golang.org/x/image/webp"
)

// Reencode validates and decodes an uploaded JPEG, PNG, or WebP image, then
// re-encodes its pixels without carrying metadata into the stored bytes. WebP
// input is stored as PNG because the standard library does not encode WebP.
func Reencode(filename string, encoded []byte, fieldSizeLimit *int64, fieldDimensionLimit *upload.DimensionLimits) ([]byte, upload.Kind, error) {
	kind, err := upload.DetectKind(filename, encoded)
	if err != nil {
		return nil, "", err
	}

	if err := upload.ValidateSize(kind, int64(len(encoded)), upload.DefaultSizeLimits(), fieldSizeLimit); err != nil {
		return nil, "", err
	}
	limits := upload.DefaultDimensionLimits()
	if err := upload.ValidateDimensions(kind, bytes.NewReader(encoded), limits, fieldDimensionLimit); err != nil {
		return nil, "", err
	}

	config, format, err := stdimage.DecodeConfig(bytes.NewReader(encoded))
	if err != nil {
		return nil, "", err
	}
	if !matchesKind(kind, format) {
		return nil, "", errUnexpectedImageFormat
	}
	if err := validateDecodedDimensions(config.Width, config.Height, limits, fieldDimensionLimit); err != nil {
		return nil, "", err
	}

	decoded, decodedFormat, err := stdimage.Decode(bytes.NewReader(encoded))
	if err != nil {
		return nil, "", err
	}
	if decodedFormat != format {
		return nil, "", errUnexpectedImageFormat
	}
	bounds := decoded.Bounds()
	if bounds.Dx() != config.Width || bounds.Dy() != config.Height {
		return nil, "", errUnexpectedImageDimensions
	}

	var output bytes.Buffer
	storedKind := kind
	switch kind {
	case upload.JPEG:
		err = jpeg.Encode(&output, decoded, nil)
	case upload.PNG, upload.WebP:
		err = png.Encode(&output, decoded)
		storedKind = upload.PNG
	default:
		return nil, "", errUnexpectedImageFormat
	}
	if err != nil {
		return nil, "", err
	}
	return output.Bytes(), storedKind, nil
}

func matchesKind(kind upload.Kind, format string) bool {
	switch kind {
	case upload.JPEG:
		return format == "jpeg"
	case upload.PNG:
		return format == "png"
	case upload.WebP:
		return format == "webp"
	default:
		return false
	}
}

func validateDecodedDimensions(width, height int, limits upload.DimensionLimits, fieldLimit *upload.DimensionLimits) error {
	if width <= 0 || height <= 0 {
		return errUnexpectedImageDimensions
	}
	effective := upload.DefaultDimensionLimits()
	if limits.MaxWidth < effective.MaxWidth {
		effective.MaxWidth = limits.MaxWidth
	}
	if limits.MaxHeight < effective.MaxHeight {
		effective.MaxHeight = limits.MaxHeight
	}
	if limits.MaxPixels < effective.MaxPixels {
		effective.MaxPixels = limits.MaxPixels
	}
	if fieldLimit != nil {
		if fieldLimit.MaxWidth <= 0 || fieldLimit.MaxHeight <= 0 || fieldLimit.MaxPixels <= 0 {
			return errInvalidDimensionLimit
		}
		if fieldLimit.MaxWidth < effective.MaxWidth {
			effective.MaxWidth = fieldLimit.MaxWidth
		}
		if fieldLimit.MaxHeight < effective.MaxHeight {
			effective.MaxHeight = fieldLimit.MaxHeight
		}
		if fieldLimit.MaxPixels < effective.MaxPixels {
			effective.MaxPixels = fieldLimit.MaxPixels
		}
	}
	if int64(width) > effective.MaxWidth || int64(height) > effective.MaxHeight {
		return errImageDimensionsExceedLimit
	}
	if int64(width) > effective.MaxPixels/int64(height) {
		return errImagePixelCountExceedsLimit
	}
	return nil
}

var (
	errUnexpectedImageFormat       = errors.New("decoded image format does not match its detected kind")
	errUnexpectedImageDimensions   = errors.New("decoded image dimensions do not match its header")
	errInvalidDimensionLimit       = errors.New("image dimension limits must be positive")
	errImageDimensionsExceedLimit  = errors.New("image dimensions exceed limit")
	errImagePixelCountExceedsLimit = errors.New("image pixel count exceeds limit")
)
