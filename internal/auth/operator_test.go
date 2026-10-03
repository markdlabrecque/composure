package auth

import "testing"

func TestOperatorPrincipalHasBothRolesAndActiveEligibility(t *testing.T) {
	roles, state := OperatorPrincipal()

	if !roles.Administrator || !roles.Editor {
		t.Fatalf("OperatorPrincipal roles = %+v, want both administrator and editor", roles)
	}
	if state != AccountActive {
		t.Fatalf("OperatorPrincipal state = %q, want %q", state, AccountActive)
	}

	for _, action := range []Action{
		Action("content.read"),
		Action("content.draft.write"),
		Action("content.publish"),
		Action("content.history"),
		Action("content.trash"),
		Action("content.deletion.preview"),
		Action("content.delete.permanently"),
		Action("content.media"),
		Action("menu.items"),
		Action("redirects"),
		Action("configuration.development_staging"),
		Action("site.branding"),
		Action("audit.review"),
		Action("accounts.manage"),
	} {
		t.Run(string(action), func(t *testing.T) {
			if !AllowsRoleAction(roles, state, action) {
				t.Errorf("operator principal denied %q", action)
			}
		})
	}
}

func TestOperatorPrincipalRejectsActionsOutsideRoleMatrix(t *testing.T) {
	roles, state := OperatorPrincipal()

	for _, action := range []Action{
		Action(""),
		Action("not_in_the_approved_matrix"),
		Action("sign_in"),
		Action("sign_out"),
		Action("password_reset.request"),
		Action("password_reset.complete"),
		Action("invitation.accept"),
	} {
		t.Run(string(action), func(t *testing.T) {
			if AllowsRoleAction(roles, state, action) {
				t.Errorf("operator principal allowed action %q outside the role-protected matrix", action)
			}
		})
	}
}
