package image

import (
	"encoding/binary"
	"errors"
)

// validateWebPRIFF checks the complete WebP RIFF container before the decoder
// can return after its image chunk. Reencode calls this only after applying the
// upload byte limit, so the framing walk is bounded by that existing limit.
func validateWebPRIFF(encoded []byte) error {
	if len(encoded) < 12 || string(encoded[:4]) != "RIFF" || string(encoded[8:12]) != "WEBP" {
		return errors.New("malformed WebP RIFF header")
	}

	declaredLength := uint64(binary.LittleEndian.Uint32(encoded[4:8]))
	if declaredLength+8 != uint64(len(encoded)) {
		return errors.New("WebP RIFF length does not match input length")
	}

	for offset, end := uint64(12), uint64(len(encoded)); offset < end; {
		if end-offset < 8 {
			return errors.New("truncated WebP chunk header")
		}

		chunkLength := uint64(binary.LittleEndian.Uint32(encoded[offset+4 : offset+8]))
		paddedLength := chunkLength + chunkLength%2
		if paddedLength > end-offset-8 {
			return errors.New("invalid WebP chunk length or padding")
		}
		offset += 8 + paddedLength
	}

	return nil
}
