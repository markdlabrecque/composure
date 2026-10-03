package upload

import (
	"math"
	"testing"
)

const testMiB int64 = 1024 * 1024

func TestValidateSizeDefaultLimitsForEveryKind(t *testing.T) {
	limits := DefaultSizeLimits()
	tests := []struct {
		name  string
		kind  Kind
		limit int64
	}{
		{"jpeg", JPEG, 10 * testMiB},
		{"png", PNG, 10 * testMiB},
		{"webp", WebP, 10 * testMiB},
		{"pdf", PDF, 25 * testMiB},
		{"docx", DOCX, 25 * testMiB},
	}

	for _, tc := range tests {
		t.Run(tc.name+"_at_limit", func(t *testing.T) {
			if err := ValidateSize(tc.kind, tc.limit, limits, nil); err != nil {
				t.Fatalf("size at default limit rejected: %v", err)
			}
		})
		t.Run(tc.name+"_one_over", func(t *testing.T) {
			if err := ValidateSize(tc.kind, tc.limit+1, limits, nil); err == nil {
				t.Fatal("size one byte above default limit accepted")
			}
		})
	}
}

func TestValidateSizeUsesLowerGlobalOrCategoryLimit(t *testing.T) {
	tests := []struct {
		name   string
		kind   Kind
		limits SizeLimits
		limit  int64
	}{
		{
			name:   "global_lower_for_image",
			kind:   JPEG,
			limits: SizeLimits{Global: 8 * testMiB, Image: 12 * testMiB, Document: 25 * testMiB},
			limit:  8 * testMiB,
		},
		{
			name:   "image_lower_than_global",
			kind:   PNG,
			limits: SizeLimits{Global: 30 * testMiB, Image: 12 * testMiB, Document: 25 * testMiB},
			limit:  12 * testMiB,
		},
		{
			name:   "global_lower_for_document",
			kind:   PDF,
			limits: SizeLimits{Global: 18 * testMiB, Image: 10 * testMiB, Document: 20 * testMiB},
			limit:  18 * testMiB,
		},
		{
			name:   "document_lower_than_global",
			kind:   DOCX,
			limits: SizeLimits{Global: 30 * testMiB, Image: 10 * testMiB, Document: 20 * testMiB},
			limit:  20 * testMiB,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateSize(tc.kind, tc.limit, tc.limits, nil); err != nil {
				t.Fatalf("size at effective limit rejected: %v", err)
			}
			if err := ValidateSize(tc.kind, tc.limit+1, tc.limits, nil); err == nil {
				t.Fatal("size one byte above effective limit accepted")
			}
		})
	}
}

func TestValidateSizeOptionalFieldLimitOnlyTightens(t *testing.T) {
	limits := SizeLimits{Global: 25 * testMiB, Image: 10 * testMiB, Document: 25 * testMiB}
	tests := []struct {
		name       string
		fieldLimit *int64
		wantLimit  int64
	}{
		{"absent", nil, 10 * testMiB},
		{"tighter", sizeLimit(6 * testMiB), 6 * testMiB},
		{"equal", sizeLimit(10 * testMiB), 10 * testMiB},
		{"looser", sizeLimit(20 * testMiB), 10 * testMiB},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateSize(WebP, tc.wantLimit, limits, tc.fieldLimit); err != nil {
				t.Fatalf("size at effective limit rejected: %v", err)
			}
			if err := ValidateSize(WebP, tc.wantLimit+1, limits, tc.fieldLimit); err == nil {
				t.Fatal("size one byte above effective limit accepted")
			}
		})
	}
}

func TestValidateSizeHandlesLargeValuesWithoutOverflow(t *testing.T) {
	limits := SizeLimits{Global: math.MaxInt64, Image: math.MaxInt64, Document: math.MaxInt64}

	if err := ValidateSize(JPEG, math.MaxInt64, limits, nil); err != nil {
		t.Fatalf("maximum representable size at limit rejected: %v", err)
	}
	fieldLimit := int64(math.MaxInt64 - 1)
	if err := ValidateSize(PDF, fieldLimit, limits, &fieldLimit); err != nil {
		t.Fatalf("large size at tighter field limit rejected: %v", err)
	}
	if err := ValidateSize(PDF, math.MaxInt64, limits, &fieldLimit); err == nil {
		t.Fatal("large size above tighter field limit accepted")
	}
}

func TestValidateSizeFailsClosedForInvalidInput(t *testing.T) {
	limits := DefaultSizeLimits()
	tests := []struct {
		name string
		kind Kind
		size int64
	}{
		{"negative_size", JPEG, -1},
		{"empty_kind", Kind(""), 0},
		{"unknown_kind", Kind("gif"), 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateSize(tc.kind, tc.size, limits, nil); err == nil {
				t.Fatal("invalid input accepted")
			}
		})
	}
}

func sizeLimit(limit int64) *int64 {
	return &limit
}
