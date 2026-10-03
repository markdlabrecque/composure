package image

import (
	"bytes"
	stdimage "image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/markdlabrecque/composure/internal/upload"
)

const (
	fixtureWidth  = 7
	fixtureHeight = 5
)

func TestMetadataFixturesAreValidImages(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		kind    upload.Kind
		format  string
	}{
		{name: "jpeg", fixture: "metadata.jpg", kind: upload.JPEG, format: "jpeg"},
		{name: "png", fixture: "metadata.png", kind: upload.PNG, format: "png"},
		{name: "webp", fixture: "metadata.webp", kind: upload.WebP},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			encoded := readFixture(t, tc.fixture)
			if !containsEXIF(encoded) || !containsGPSDirectory(encoded) {
				t.Fatal("fixture does not contain its EXIF GPS directory")
			}
			if err := upload.ValidateDimensions(tc.kind, bytes.NewReader(encoded), upload.DefaultDimensionLimits(), nil); err != nil {
				t.Fatalf("fixture has an invalid bounded image header: %v", err)
			}
			if tc.format == "" {
				return
			}
			config, format, err := stdimage.DecodeConfig(bytes.NewReader(encoded))
			if err != nil {
				t.Fatalf("fixture does not fully decode: %v", err)
			}
			if format != tc.format || config.Width != fixtureWidth || config.Height != fixtureHeight {
				t.Fatalf("fixture decoded as %q %dx%d, want %q %dx%d", format, config.Width, config.Height, tc.format, fixtureWidth, fixtureHeight)
			}
		})
	}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func containsEXIF(data []byte) bool {
	return bytes.Contains(data, []byte("Exif\x00\x00")) || bytes.Contains(data, []byte("eXIf")) || bytes.Contains(data, []byte("EXIF"))
}

func containsGPSDirectory(data []byte) bool {
	return bytes.Contains(data, []byte{0x25, 0x88, 0x04, 0x00, 0x01, 0x00, 0x00, 0x00})
}
