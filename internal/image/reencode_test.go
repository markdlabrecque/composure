package image

import (
	"bytes"
	stdimage "image"
	_ "image/jpeg"
	_ "image/png"
	"testing"

	"github.com/markdlabrecque/composure/internal/upload"
)

func TestReencodeStripsMetadataAndPreservesDimensions(t *testing.T) {
	tests := []struct {
		name       string
		filename   string
		fixture    string
		wantKind   upload.Kind
		wantFormat string
	}{
		{name: "jpeg_to_jpeg", filename: "photo.jpg", fixture: "metadata.jpg", wantKind: upload.JPEG, wantFormat: "jpeg"},
		{name: "png_to_png", filename: "graphic.png", fixture: "metadata.png", wantKind: upload.PNG, wantFormat: "png"},
		{name: "webp_to_png", filename: "picture.webp", fixture: "metadata.webp", wantKind: upload.PNG, wantFormat: "png"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			encoded := readFixture(t, tc.fixture)
			if !containsEXIF(encoded) || !containsGPSDirectory(encoded) {
				t.Fatal("source fixture does not contain the required EXIF GPS metadata")
			}

			got, kind, err := Reencode(tc.filename, encoded, nil, nil)
			if err != nil {
				t.Fatalf("Reencode returned an error: %v", err)
			}
			if kind != tc.wantKind {
				t.Fatalf("Reencode kind = %q, want %q", kind, tc.wantKind)
			}
			config, format, err := stdimage.DecodeConfig(bytes.NewReader(got))
			if err != nil {
				t.Fatalf("stored bytes are not a decodable image: %v", err)
			}
			if format != tc.wantFormat {
				t.Fatalf("stored format = %q, want %q", format, tc.wantFormat)
			}
			if config.Width != fixtureWidth || config.Height != fixtureHeight {
				t.Fatalf("stored dimensions = %dx%d, want %dx%d", config.Width, config.Height, fixtureWidth, fixtureHeight)
			}
			if containsEXIF(got) || containsGPSDirectory(got) {
				t.Fatal("stored bytes retain EXIF GPS metadata")
			}
			assertNoMetadataContainer(t, kind, got)
		})
	}
}

func TestReencodeRejectsMalformedOrUnsupportedInput(t *testing.T) {
	jpeg := readFixture(t, "metadata.jpg")
	png := readFixture(t, "metadata.png")
	webp := readFixture(t, "metadata.webp")
	tests := []struct {
		name     string
		filename string
		data     []byte
	}{
		{name: "jpeg_truncated_after_valid_header", filename: "broken.jpg", data: jpeg[:len(jpeg)/2]},
		{name: "png_truncated_after_valid_header", filename: "broken.png", data: png[:40]},
		{name: "webp_truncated_after_valid_header", filename: "broken.webp", data: webp[:30]},
		{name: "mismatched_extension", filename: "photo.png", data: jpeg},
		{name: "unsupported_kind", filename: "document.pdf", data: []byte("%PDF-1.7\n")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := Reencode(tc.filename, tc.data, nil, nil); err == nil {
				t.Fatal("invalid image accepted")
			}
		})
	}
}

func TestReencodeHonorsTighterCallerLimitsBeforePixelDecode(t *testing.T) {
	encoded := readFixture(t, "metadata.png")

	sizeLimit := int64(len(encoded) - 1)
	if _, _, err := Reencode("graphic.png", encoded, &sizeLimit, nil); err == nil {
		t.Fatal("image above the field size limit accepted")
	}

	dimensions := &upload.DimensionLimits{
		MaxWidth:  fixtureWidth - 1,
		MaxHeight: fixtureHeight,
		MaxPixels: fixtureWidth * fixtureHeight,
	}
	if _, _, err := Reencode("graphic.png", encoded, nil, dimensions); err == nil {
		t.Fatal("image above the field dimension limit accepted")
	}

	zeroSize := int64(0)
	if _, _, err := Reencode("graphic.png", encoded, &zeroSize, nil); err == nil {
		t.Fatal("non-positive field size limit accepted")
	}
	invalidDimensions := &upload.DimensionLimits{MaxWidth: fixtureWidth, MaxHeight: 0, MaxPixels: fixtureWidth * fixtureHeight}
	if _, _, err := Reencode("graphic.png", encoded, nil, invalidDimensions); err == nil {
		t.Fatal("non-positive field dimension limit accepted")
	}
}

func assertNoMetadataContainer(t *testing.T, kind upload.Kind, data []byte) {
	t.Helper()
	switch kind {
	case upload.JPEG:
		for offset := 2; offset+4 <= len(data) && data[offset] == 0xff; {
			marker := data[offset+1]
			if marker == 0xda || marker == 0xd9 {
				return
			}
			if marker >= 0xe1 && marker <= 0xef || marker == 0xfe {
				t.Fatalf("stored JPEG retains metadata marker %#x", marker)
			}
			length := int(data[offset+2])<<8 | int(data[offset+3])
			if length < 2 || offset+2+length > len(data) {
				t.Fatal("stored JPEG has an invalid segment while checking metadata")
			}
			offset += 2 + length
		}
	case upload.PNG:
		for offset := 8; offset+12 <= len(data); {
			length := int(data[offset])<<24 | int(data[offset+1])<<16 | int(data[offset+2])<<8 | int(data[offset+3])
			end := offset + 12 + length
			if length < 0 || end > len(data) {
				t.Fatal("stored PNG has an invalid chunk while checking metadata")
			}
			chunk := string(data[offset+4 : offset+8])
			switch chunk {
			case "eXIf", "iTXt", "tEXt", "zTXt":
				t.Fatalf("stored PNG retains metadata chunk %s", chunk)
			}
			offset = end
			if chunk == "IEND" {
				return
			}
		}
	default:
		t.Fatalf("unexpected stored image kind %q", kind)
	}
}
