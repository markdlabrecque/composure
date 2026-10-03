package upload

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
)

const (
	defaultMaxImageAxis   int64 = 16_384
	defaultMaxImagePixels int64 = 40_000_000
	maxDimensionHeader          = 1 << 20
)

var errInvalidDimensionLimit = errors.New("image dimension limits must be positive")

// DimensionLimits contains the maximum width, height, and pixel count allowed
// for an uploaded image.
type DimensionLimits struct {
	MaxWidth  int64
	MaxHeight int64
	MaxPixels int64
}

// DefaultDimensionLimits returns the image dimension ceilings from the upload
// contract.
func DefaultDimensionLimits() DimensionLimits {
	return DimensionLimits{
		MaxWidth:  defaultMaxImageAxis,
		MaxHeight: defaultMaxImageAxis,
		MaxPixels: defaultMaxImagePixels,
	}
}

// ValidateDimensions inspects a bounded image header and rejects dimensions
// above the supplied limits. A field limit can only tighten each ceiling.
func ValidateDimensions(kind Kind, encoded io.Reader, limits DimensionLimits, fieldLimit *DimensionLimits) error {
	if encoded == nil {
		return errors.New("image data is required")
	}
	if err := validateDimensionLimit(limits); err != nil {
		return err
	}
	// The values supplied by callers can tighten the contract but cannot raise
	// its immutable per-axis and total-pixel ceilings.
	effective := DefaultDimensionLimits()
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
		if err := validateDimensionLimit(*fieldLimit); err != nil {
			return err
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

	bounded := &io.LimitedReader{R: encoded, N: maxDimensionHeader}
	var width, height uint32
	var err error
	switch kind {
	case JPEG:
		width, height, err = readJPEGDimensions(bounded)
	case PNG:
		width, height, err = readPNGDimensions(bounded)
	case WebP:
		width, height, err = readWebPDimensions(bounded)
	default:
		return errors.New("unsupported image kind")
	}
	if err != nil {
		return err
	}
	if width == 0 || height == 0 {
		return errors.New("image dimensions must be positive")
	}
	if int64(width) > effective.MaxWidth || int64(height) > effective.MaxHeight {
		return errors.New("image dimensions exceed limit")
	}
	// Dividing before multiplying keeps this comparison safe even for crafted
	// headers whose dimensions are near the format's maximum values.
	if int64(width) > effective.MaxPixels/int64(height) {
		return errors.New("image pixel count exceeds limit")
	}
	return nil
}

func validateDimensionLimit(limit DimensionLimits) error {
	if limit.MaxWidth <= 0 || limit.MaxHeight <= 0 || limit.MaxPixels <= 0 {
		return errInvalidDimensionLimit
	}
	return nil
}

func readJPEGDimensions(r io.Reader) (uint32, uint32, error) {
	var soi [2]byte
	if _, err := io.ReadFull(r, soi[:]); err != nil || soi != [2]byte{0xff, 0xd8} {
		return 0, 0, errors.New("malformed JPEG header")
	}
	for {
		prefix, err := readByte(r)
		if err != nil || prefix != 0xff {
			return 0, 0, errors.New("malformed JPEG marker")
		}
		marker, err := readByte(r)
		for err == nil && marker == 0xff { // marker fill bytes
			marker, err = readByte(r)
		}
		if err != nil || marker == 0x00 {
			return 0, 0, errors.New("malformed JPEG marker")
		}
		switch {
		case marker == 0xd9 || marker == 0xda:
			return 0, 0, errors.New("JPEG has no frame dimensions before image data")
		case marker == 0xd8 || marker == 0x01 || marker >= 0xd0 && marker <= 0xd7:
			continue
		}
		var lengthBytes [2]byte
		if _, err := io.ReadFull(r, lengthBytes[:]); err != nil {
			return 0, 0, errors.New("truncated JPEG segment")
		}
		length := int(binary.BigEndian.Uint16(lengthBytes[:]))
		if length < 2 {
			return 0, 0, errors.New("invalid JPEG segment length")
		}
		if isJPEGStartOfFrame(marker) {
			if length < 8 {
				return 0, 0, errors.New("invalid JPEG frame header")
			}
			var frame [6]byte
			if _, err := io.ReadFull(r, frame[:]); err != nil {
				return 0, 0, errors.New("truncated JPEG frame header")
			}
			components := int(frame[5])
			if components == 0 || length != 8+3*components {
				return 0, 0, errors.New("invalid JPEG frame components")
			}
			if _, err := io.CopyN(io.Discard, r, int64(length-8)); err != nil {
				return 0, 0, errors.New("truncated JPEG frame header")
			}
			return uint32(binary.BigEndian.Uint16(frame[3:5])), uint32(binary.BigEndian.Uint16(frame[1:3])), nil
		}
		if _, err := io.CopyN(io.Discard, r, int64(length-2)); err != nil {
			return 0, 0, errors.New("truncated JPEG segment")
		}
	}
}

func isJPEGStartOfFrame(marker byte) bool {
	return marker >= 0xc0 && marker <= 0xcf && marker != 0xc4 && marker != 0xc8 && marker != 0xcc
}

func readPNGDimensions(r io.Reader) (uint32, uint32, error) {
	var header [8]byte
	if _, err := io.ReadFull(r, header[:]); err != nil || string(header[:]) != "\x89PNG\r\n\x1a\n" {
		return 0, 0, errors.New("malformed PNG signature")
	}
	var chunkHeader [8]byte
	if _, err := io.ReadFull(r, chunkHeader[:]); err != nil || binary.BigEndian.Uint32(chunkHeader[:4]) != 13 || string(chunkHeader[4:]) != "IHDR" {
		return 0, 0, errors.New("PNG must begin with a 13-byte IHDR chunk")
	}
	var ihdr [13]byte
	var crc [4]byte
	if _, err := io.ReadFull(r, ihdr[:]); err != nil {
		return 0, 0, errors.New("truncated PNG IHDR")
	}
	if _, err := io.ReadFull(r, crc[:]); err != nil {
		return 0, 0, errors.New("truncated PNG IHDR checksum")
	}
	hash := crc32.NewIEEE()
	_, _ = hash.Write(chunkHeader[4:])
	_, _ = hash.Write(ihdr[:])
	if binary.BigEndian.Uint32(crc[:]) != hash.Sum32() {
		return 0, 0, errors.New("invalid PNG IHDR checksum")
	}
	w, h := binary.BigEndian.Uint32(ihdr[:4]), binary.BigEndian.Uint32(ihdr[4:8])
	if !validPNGEncoding(ihdr[8], ihdr[9]) || ihdr[10] != 0 || ihdr[11] != 0 || ihdr[12] > 1 {
		return 0, 0, errors.New("invalid PNG IHDR fields")
	}
	return w, h, nil
}

func validPNGEncoding(depth, color byte) bool {
	switch color {
	case 0:
		return depth == 1 || depth == 2 || depth == 4 || depth == 8 || depth == 16
	case 2, 4, 6:
		return depth == 8 || depth == 16
	case 3:
		return depth == 1 || depth == 2 || depth == 4 || depth == 8
	default:
		return false
	}
}

func readWebPDimensions(r io.Reader) (uint32, uint32, error) {
	var riff [12]byte
	if _, err := io.ReadFull(r, riff[:]); err != nil || string(riff[:4]) != "RIFF" || string(riff[8:]) != "WEBP" {
		return 0, 0, errors.New("malformed WebP RIFF header")
	}
	remaining := int64(binary.LittleEndian.Uint32(riff[4:8]))
	if remaining < 4 {
		return 0, 0, errors.New("invalid WebP RIFF length")
	}
	remaining -= 4 // WEBP form type
	for remaining >= 8 {
		var chunk [8]byte
		if _, err := io.ReadFull(r, chunk[:]); err != nil {
			return 0, 0, errors.New("truncated WebP chunk")
		}
		length := int64(binary.LittleEndian.Uint32(chunk[4:]))
		padded := length + length%2
		if padded > remaining-8 {
			return 0, 0, errors.New("invalid WebP chunk length")
		}
		remaining -= 8
		switch string(chunk[:4]) {
		case "VP8X":
			if length != 10 {
				return 0, 0, errors.New("invalid WebP VP8X header length")
			}
			var data [10]byte
			if _, err := io.ReadFull(r, data[:]); err != nil {
				return 0, 0, errors.New("truncated WebP VP8X header")
			}
			if data[0]&0xc1 != 0 || data[1] != 0 || data[2] != 0 || data[3] != 0 {
				return 0, 0, errors.New("invalid WebP VP8X reserved bits")
			}
			return readUint24(data[4:7]) + 1, readUint24(data[7:10]) + 1, nil
		case "VP8 ":
			if length < 10 {
				return 0, 0, errors.New("invalid WebP VP8 frame header length")
			}
			var data [10]byte
			if _, err := io.ReadFull(r, data[:]); err != nil {
				return 0, 0, errors.New("truncated WebP VP8 frame header")
			}
			if data[0]&1 != 0 || data[3] != 0x9d || data[4] != 0x01 || data[5] != 0x2a {
				return 0, 0, errors.New("invalid WebP VP8 frame header")
			}
			width := binary.LittleEndian.Uint16(data[6:8]) & 0x3fff
			height := binary.LittleEndian.Uint16(data[8:10]) & 0x3fff
			return uint32(width), uint32(height), nil
		case "VP8L":
			if length < 5 {
				return 0, 0, errors.New("invalid WebP VP8L header length")
			}
			var data [5]byte
			if _, err := io.ReadFull(r, data[:]); err != nil {
				return 0, 0, errors.New("truncated WebP VP8L header")
			}
			if data[0] != 0x2f || data[4]&0xe0 != 0 {
				return 0, 0, errors.New("invalid WebP VP8L frame header")
			}
			width := uint32(data[1]) | uint32(data[2]&0x3f)<<8
			height := uint32(data[2]>>6) | uint32(data[3])<<2 | uint32(data[4]&0x0f)<<10
			return width + 1, height + 1, nil
		default:
			if _, err := io.CopyN(io.Discard, r, padded); err != nil {
				return 0, 0, errors.New("truncated WebP chunk")
			}
			remaining -= padded
		}
	}
	return 0, 0, errors.New("WebP has no image dimensions")
}

func readUint24(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16
}

func readByte(r io.Reader) (byte, error) {
	var b [1]byte
	_, err := io.ReadFull(r, b[:])
	return b[0], err
}
