package upload

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"io"
	"math"
	"testing"
)

// DimensionLimits is expected to use signed values, like SizeLimits, so that
// non-positive caller-supplied limits can be rejected as invalid input.
// ValidateDimensions must inspect only the encoded image header; callers retain
// ownership of decoding and re-encoding accepted pixels.

func TestValidateDimensionsAcceptsSupportedImageHeaders(t *testing.T) {
	limits := DefaultDimensionLimits()
	tests := []struct {
		name string
		kind Kind
		data []byte
	}{
		{"jpeg", JPEG, jpegHeader(640, 480)},
		{"png", PNG, pngHeader(640, 480)},
		{"webp_vp8x", WebP, webPVP8XHeader(640, 480)},
		{"webp_vp8", WebP, webPVP8Header(640, 480)},
		{"webp_vp8l", WebP, webPVP8LHeader(640, 480)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateDimensions(tc.kind, bytes.NewReader(tc.data), limits, nil); err != nil {
				t.Fatalf("valid %s header rejected: %v", tc.name, err)
			}
		})
	}
}

func TestValidateDimensionsDefaultBoundaries(t *testing.T) {
	limits := DefaultDimensionLimits()
	tests := []struct {
		name    string
		width   uint32
		height  uint32
		wantErr bool
	}{
		{"axis_equal", 16384, 1, false},
		{"width_one_over", 16385, 1, true},
		{"height_one_over", 1, 16385, true},
		{"pixels_equal", 5000, 8000, false},
		{"pixels_one_over", 5001, 8000, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateDimensions(PNG, bytes.NewReader(pngHeader(tc.width, tc.height)), limits, nil)
			if tc.wantErr && err == nil {
				t.Fatal("over-limit dimensions accepted")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("dimensions at the limit rejected: %v", err)
			}
		})
	}
}

func TestValidateDimensionsOptionalFieldLimitOnlyTightens(t *testing.T) {
	limits := DefaultDimensionLimits()
	tests := []struct {
		name       string
		fieldLimit *DimensionLimits
		width      uint32
		height     uint32
		wantErr    bool
	}{
		{"absent_uses_defaults", nil, 5000, 8000, false},
		{"tighter_axis_accepts_equal", dimensionLimits(4000, 3000, 12_000_000), 4000, 3000, false},
		{"tighter_axis_rejects_one_over", dimensionLimits(4000, 3000, 12_000_000), 4001, 1, true},
		{"tighter_pixels_rejects_one_over", dimensionLimits(6000, 6000, 12_000_000), 4001, 3000, true},
		{"equal_leaves_defaults", dimensionLimits(16384, 16384, 40_000_000), 5000, 8000, false},
		{"looser_cannot_raise_defaults", dimensionLimits(20000, 20000, 50_000_000), 5001, 8000, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateDimensions(PNG, bytes.NewReader(pngHeader(tc.width, tc.height)), limits, tc.fieldLimit)
			if tc.wantErr && err == nil {
				t.Fatal("dimensions above the effective field/default limit accepted")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("dimensions within the effective limit rejected: %v", err)
			}
		})
	}
}

func TestValidateDimensionsRejectsCraftedHugeHeaderWithoutOverflow(t *testing.T) {
	limits := DimensionLimits{MaxWidth: math.MaxInt64, MaxHeight: math.MaxInt64, MaxPixels: math.MaxInt64}
	data := pngHeader(math.MaxUint32, math.MaxUint32)
	if err := ValidateDimensions(PNG, bytes.NewReader(data), limits, nil); err == nil {
		t.Fatal("dimensions whose product exceeds MaxInt64 accepted")
	}
}

func TestValidateDimensionsFailsClosedForInvalidInput(t *testing.T) {
	limits := DefaultDimensionLimits()
	tests := []struct {
		name   string
		kind   Kind
		data   []byte
		limits DimensionLimits
		field  *DimensionLimits
	}{
		{"unsupported_kind", PDF, pngHeader(1, 1), limits, nil},
		{"unknown_kind", Kind("gif"), pngHeader(1, 1), limits, nil},
		{"malformed", PNG, []byte("not a png"), limits, nil},
		{"truncated_jpeg", JPEG, jpegHeader(1, 1)[:8], limits, nil},
		{"truncated_png", PNG, pngHeader(1, 1)[:20], limits, nil},
		{"truncated_webp", WebP, webPVP8XHeader(1, 1)[:24], limits, nil},
		{"zero_width", PNG, pngHeader(0, 1), limits, nil},
		{"zero_height", PNG, pngHeader(1, 0), limits, nil},
		{"zero_default_width_limit", PNG, pngHeader(1, 1), DimensionLimits{MaxWidth: 0, MaxHeight: 1, MaxPixels: 1}, nil},
		{"negative_default_pixel_limit", PNG, pngHeader(1, 1), DimensionLimits{MaxWidth: 1, MaxHeight: 1, MaxPixels: -1}, nil},
		{"zero_field_height_limit", PNG, pngHeader(1, 1), limits, dimensionLimits(1, 0, 1)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateDimensions(tc.kind, bytes.NewReader(tc.data), tc.limits, tc.field); err == nil {
				t.Fatal("invalid dimension input accepted")
			}
		})
	}
}

func TestValidateDimensionsBoundsHeaderReads(t *testing.T) {
	const maxObservedRead = int64(1 << 20)
	reader := &endlessJPEGHeaders{}

	if err := ValidateDimensions(JPEG, reader, DefaultDimensionLimits(), nil); err == nil {
		t.Fatal("JPEG with no dimension header accepted")
	}
	if reader.read > maxObservedRead {
		t.Fatalf("validator read %d header bytes; want at most %d", reader.read, maxObservedRead)
	}
}

func dimensionLimits(width, height, pixels int64) *DimensionLimits {
	return &DimensionLimits{MaxWidth: width, MaxHeight: height, MaxPixels: pixels}
}

func jpegHeader(width, height uint32) []byte {
	return []byte{
		0xff, 0xd8,
		0xff, 0xc0, 0x00, 0x11, 0x08,
		byte(height >> 8), byte(height), byte(width >> 8), byte(width),
		0x03, 0x01, 0x11, 0x00, 0x02, 0x11, 0x00, 0x03, 0x11, 0x00,
		0xff, 0xd9,
	}
}

func pngHeader(width, height uint32) []byte {
	data := make([]byte, 0, 33)
	data = append(data, "\x89PNG\r\n\x1a\n"...)
	chunk := make([]byte, 17)
	binary.BigEndian.PutUint32(chunk[0:4], 13)
	copy(chunk[4:8], "IHDR")
	binary.BigEndian.PutUint32(chunk[8:12], width)
	binary.BigEndian.PutUint32(chunk[12:16], height)
	chunk[16] = 8
	data = append(data, chunk...)
	data = append(data, 2, 0, 0, 0)
	crc := crc32.ChecksumIEEE(data[12:29])
	data = binary.BigEndian.AppendUint32(data, crc)
	return data
}

func webPVP8XHeader(width, height uint32) []byte {
	payload := make([]byte, 10)
	putUint24(payload[4:7], width-1)
	putUint24(payload[7:10], height-1)
	return webPChunk("VP8X", payload)
}

func webPVP8Header(width, height uint32) []byte {
	payload := []byte{0x00, 0x00, 0x00, 0x9d, 0x01, 0x2a, byte(width), byte(width >> 8), byte(height), byte(height >> 8)}
	return webPChunk("VP8 ", payload)
}

func webPVP8LHeader(width, height uint32) []byte {
	w := width - 1
	h := height - 1
	payload := []byte{0x2f, byte(w), byte(w>>8) | byte(h<<6), byte(h >> 2), byte(h >> 10)}
	return webPChunk("VP8L", payload)
}

func webPChunk(name string, payload []byte) []byte {
	data := make([]byte, 12, 20+len(payload))
	copy(data, "RIFF")
	copy(data[8:], "WEBP")
	data = append(data, name...)
	data = binary.LittleEndian.AppendUint32(data, uint32(len(payload)))
	data = append(data, payload...)
	if len(payload)%2 != 0 {
		data = append(data, 0)
	}
	binary.LittleEndian.PutUint32(data[4:8], uint32(len(data)-8))
	return data
}

func putUint24(dst []byte, value uint32) {
	dst[0] = byte(value)
	dst[1] = byte(value >> 8)
	dst[2] = byte(value >> 16)
}

type endlessJPEGHeaders struct {
	read int64
	pos  int
}

func (r *endlessJPEGHeaders) Read(p []byte) (int, error) {
	prefix := []byte{0xff, 0xd8}
	segment := []byte{0xff, 0xe0, 0x00, 0x02}
	for i := range p {
		if r.pos < len(prefix) {
			p[i] = prefix[r.pos]
		} else {
			p[i] = segment[(r.pos-len(prefix))%len(segment)]
		}
		r.pos++
	}
	r.read += int64(len(p))
	if r.read > 2<<20 {
		return len(p), io.ErrUnexpectedEOF
	}
	return len(p), nil
}

func TestValidateDimensionsEnforcesHardCeilingsWhenSuppliedLimitsAreLooser(t *testing.T) {
	loose := DimensionLimits{
		MaxWidth:  math.MaxInt64,
		MaxHeight: math.MaxInt64,
		MaxPixels: math.MaxInt64,
	}
	tests := []struct {
		name    string
		width   uint32
		height  uint32
		wantErr bool
	}{
		{"hard_limits_accept_equality", 5000, 8000, false},
		{"hard_width_one_over", 16385, 1, true},
		{"hard_height_one_over", 1, 16385, true},
		{"hard_pixels_one_over", 5001, 8000, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateDimensions(PNG, bytes.NewReader(pngHeader(tc.width, tc.height)), loose, nil)
			if tc.wantErr && err == nil {
				t.Fatal("dimensions above the hard image ceiling accepted")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("dimensions at the hard image ceiling rejected: %v", err)
			}
		})
	}
}
