package upload

import "testing"

func TestValidateSizeRejectsEveryNonPositiveSuppliedCategoryLimit(t *testing.T) {
	tests := []struct {
		name   string
		kind   Kind
		limits SizeLimits
	}{
		{name: "zero image limit for image", kind: JPEG, limits: SizeLimits{Global: 1, Image: 0, Document: 1}},
		{name: "negative document limit for document", kind: PDF, limits: SizeLimits{Global: 1, Image: 1, Document: -1}},
		{name: "zero unused document limit for image", kind: WebP, limits: SizeLimits{Global: 1, Image: 1, Document: 0}},
		{name: "negative unused image limit for document", kind: DOCX, limits: SizeLimits{Global: 1, Image: -1, Document: 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateSize(tt.kind, 0, tt.limits, nil); err == nil {
				t.Fatal("ValidateSize accepted non-positive supplied category limit")
			}
		})
	}
}
