package image

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	stdimage "image"
	"testing"

	"github.com/markdlabrecque/composure/internal/upload"
)

// losslessWebPBase64 is gopher-doc.1bpp.lossless.webp from the pinned
// golang.org/x/image v0.45.0 module testdata.
const losslessWebPBase64 = "UklGRrIBAABXRUJQVlA4TKUBAAAvSsAYAA8w//M///MfeJAkbXvaSG7m8Q3GfYSBJekwQztm/IcZlgwnmWImn2BK7aFmBtnVir6q//8VOkFE/xm4baTIu8c48ArEo6+B3zFKYln3pqClSCKX0begFTAXFOLXHSyF8cCNcZEG4OywuA4KVVfJCiArU7GAgJI8+lJP/OKMT/fBAjevg1cYB7YVkFuWga2lyPi5I0HFy5YTpWIHg0RZpkniRVW9odHAKOwosWuOGdxIyn2OvaCDvhg/we6TwadPBPbqBV58MsLmMJ8yZnOWk8SRz4N+QoyPL+MnamzMvcE1rHNEr91F9GKZPVUcS9w7PhhH36suB9qPeYb/oLk6cuTiJ0wOK3m5h1cKjW6EVZCYMK7dxcKCBdgP9HkKr9gkAO2P8GKZGWVdIAatQa+1IDpt6qyorVwdy01xdW8Jkfk6xjEXmVQQ+HQdFr6OKhIN34dXWq0+0qr6EJSCeeVLH9+gvGTLyqM65PQ44ihzlTXxQKjKbAvshXgir7Lil9w4L2bvMycmjQcqXaMCO6BlY28i+FOLzbfI1vEqxAhotocAAA=="

func TestReencodeRejectsTruncatedDeclaredWebPBodyAfterPixelChunk(t *testing.T) {
	lossless := losslessWebP(t)
	tests := []struct {
		name     string
		complete []byte
		pixelEnd int
	}{
		{
			name:     "vp8",
			complete: readFixture(t, "metadata.webp"),
			pixelEnd: webPChunkEnd(t, readFixture(t, "metadata.webp"), "VP8 "),
		},
		{
			name:     "vp8l",
			complete: losslessWebPWithEXIF(t, lossless),
			pixelEnd: webPChunkEnd(t, losslessWebPWithEXIF(t, lossless), "VP8L"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.pixelEnd >= len(tc.complete) {
				t.Fatal("test input does not declare data after its pixel chunk")
			}
			if _, _, err := Reencode("image.webp", tc.complete[:tc.pixelEnd], nil, nil); err == nil {
				t.Fatal("accepted WebP truncated after its complete pixel chunk while RIFF declares trailing data")
			}
		})
	}
}

func TestReencodeRejectsMalformedWebPChunkFraming(t *testing.T) {
	lossless := losslessWebP(t)

	lengthOverrunsBody := append(bytes.Clone(lossless), []byte("EXIF\x04\x00\x00\x00ab")...)
	binary.LittleEndian.PutUint32(lengthOverrunsBody[4:8], uint32(len(lengthOverrunsBody)-8))

	missingOddChunkPadding := append(bytes.Clone(lossless), []byte("EXIF\x01\x00\x00\x00x")...)
	binary.LittleEndian.PutUint32(missingOddChunkPadding[4:8], uint32(len(missingOddChunkPadding)-8))

	for _, tc := range []struct {
		name string
		data []byte
	}{
		{name: "chunk_length_overruns_riff_body", data: lengthOverrunsBody},
		{name: "odd_chunk_missing_padding_byte", data: missingOddChunkPadding},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := Reencode("image.webp", tc.data, nil, nil); err == nil {
				t.Fatal("accepted malformed WebP RIFF chunk framing")
			}
		})
	}
}

func TestReencodeAcceptsCompleteVP8AndVP8LWebPWithMetadata(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{name: "vp8", data: readFixture(t, "metadata.webp")},
		{name: "vp8l", data: losslessWebPWithEXIF(t, losslessWebP(t))},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			want, format, err := stdimage.DecodeConfig(bytes.NewReader(tc.data))
			if err != nil || format != "webp" {
				t.Fatalf("test input is not a valid WebP: format %q, error %v", format, err)
			}

			got, kind, err := Reencode("image.webp", tc.data, nil, nil)
			if err != nil {
				t.Fatalf("Reencode returned an error for complete WebP with metadata: %v", err)
			}
			if kind != upload.PNG {
				t.Fatalf("stored kind = %q, want %q", kind, upload.PNG)
			}
			config, outputFormat, err := stdimage.DecodeConfig(bytes.NewReader(got))
			if err != nil {
				t.Fatalf("stored bytes are not a decodable image: %v", err)
			}
			if outputFormat != "png" {
				t.Fatalf("stored format = %q, want png", outputFormat)
			}
			if config.Width != want.Width || config.Height != want.Height {
				t.Fatalf("stored dimensions = %dx%d, want %dx%d", config.Width, config.Height, want.Width, want.Height)
			}
		})
	}
}

func losslessWebP(t *testing.T) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(losslessWebPBase64)
	if err != nil {
		t.Fatalf("decode pinned lossless WebP fixture: %v", err)
	}
	return data
}

func appendWebPChunk(t *testing.T, webp []byte, fourCC string, payload []byte) []byte {
	t.Helper()
	if len(fourCC) != 4 {
		t.Fatalf("chunk FourCC %q does not have four bytes", fourCC)
	}
	data := bytes.Clone(webp)
	data = append(data, fourCC...)
	var size [4]byte
	binary.LittleEndian.PutUint32(size[:], uint32(len(payload)))
	data = append(data, size[:]...)
	data = append(data, payload...)
	if len(payload)%2 != 0 {
		data = append(data, 0)
	}
	binary.LittleEndian.PutUint32(data[4:8], uint32(len(data)-8))
	return data
}

func losslessWebPWithEXIF(t *testing.T, simple []byte) []byte {
	t.Helper()
	config, format, err := stdimage.DecodeConfig(bytes.NewReader(simple))
	if err != nil || format != "webp" {
		t.Fatalf("decode simple VP8L fixture: format %q, error %v", format, err)
	}
	canvas := make([]byte, 10)
	canvas[0] = 0x08 // EXIF metadata present.
	putUint24(canvas[4:7], config.Width-1)
	putUint24(canvas[7:10], config.Height-1)

	extended := append([]byte("RIFF\x00\x00\x00\x00WEBP"), []byte("VP8X\x0a\x00\x00\x00")...)
	extended = append(extended, canvas...)
	extended = append(extended, simple[12:]...)
	binary.LittleEndian.PutUint32(extended[4:8], uint32(len(extended)-8))
	return appendWebPChunk(t, extended, "EXIF", webPChunkPayload(t, readFixture(t, "metadata.webp"), "EXIF"))
}

func putUint24(dst []byte, value int) {
	dst[0] = byte(value)
	dst[1] = byte(value >> 8)
	dst[2] = byte(value >> 16)
}

func webPChunkEnd(t *testing.T, data []byte, fourCC string) int {
	t.Helper()
	offset := bytes.Index(data[12:], []byte(fourCC))
	if offset < 0 {
		t.Fatalf("WebP chunk %q not found", fourCC)
	}
	offset += 12
	if offset+8 > len(data) {
		t.Fatalf("WebP chunk %q has a truncated header", fourCC)
	}
	size := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
	end := offset + 8 + size + size%2
	if end > len(data) {
		t.Fatalf("WebP chunk %q overruns test input", fourCC)
	}
	return end
}

func webPChunkPayload(t *testing.T, data []byte, fourCC string) []byte {
	t.Helper()
	end := webPChunkEnd(t, data, fourCC)
	offset := bytes.Index(data[12:], []byte(fourCC)) + 12
	size := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
	return data[end-size-size%2 : end-size%2]
}
