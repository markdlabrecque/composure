package audit

import "testing"

func TestValidateActionUsesCoveredCatalog(t *testing.T) {
	covered := []string{
		"account.sign_in",
		"account.created",
		"password.changed",
		"roles.changed",
		"account.deactivated",
		"configuration.deployed",
		"content.published",
		"content.unpublished",
		"menu.published",
		"menu.unpublished",
		"content.permanently_deleted",
		"configuration.exported",
		"site.exported",
		"site.restored",
	}

	for _, action := range covered {
		t.Run(action, func(t *testing.T) {
			if err := validCatalogEvent(action).Validate(); err != nil {
				t.Fatalf("catalog action %q rejected: %v", action, err)
			}
		})
	}

	if err := validCatalogEvent("secrets.exported").Validate(); err == nil {
		t.Fatal("machine-shaped action outside the covered catalog was accepted")
	}
}

func validCatalogEvent(action string) Event {
	return Event{
		ID:      "018f1f2e-7b3c-7abc-8def-0123456789ab",
		Time:    "2025-01-02T03:04:05.000Z",
		Action:  action,
		Outcome: "success",
		Count:   1,
		Target:  &Target{Kind: "operation", ID: action},
	}
}
