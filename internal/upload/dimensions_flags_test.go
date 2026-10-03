package upload

import (
	"bytes"
	"testing"
)

func TestValidateDimensionsVP8XFeatureFlags(t *testing.T) {
	tests := []struct {
		name    string
		flag    byte
		wantErr bool
	}{
		{name: "icc_profile_bit_5", flag: 1 << 5, wantErr: false},
		{name: "reserved_bit_7", flag: 1 << 7, wantErr: true},
		{name: "reserved_bit_6", flag: 1 << 6, wantErr: true},
		{name: "reserved_bit_0", flag: 1 << 0, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			header := webPVP8XHeader(640, 480)
			header[20] = tc.flag

			err := ValidateDimensions(WebP, bytes.NewReader(header), DefaultDimensionLimits(), nil)
			if tc.wantErr && err == nil {
				t.Fatalf("VP8X feature byte %#02x with reserved bit accepted", tc.flag)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("VP8X feature byte %#02x with defined ICC profile flag rejected: %v", tc.flag, err)
			}
		})
	}
}
