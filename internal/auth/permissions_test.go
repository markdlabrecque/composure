package auth

import "testing"

// These are role-action eligibility checks, not complete authorization checks.
// The caller must independently validate credentials, sessions, CSRF, tokens,
// resource state and confirmations. No HTTP route or CLI operator is modeled.
var permissionSubjects = []struct {
	name          string
	roles         Roles
	state         AccountState
	administrator bool
	editor        bool
}{
	{"active_administrator", Roles{Administrator: true}, AccountState("active"), true, false},
	{"active_editor", Roles{Editor: true}, AccountState("active"), false, true},
	{"active_both", Roles{Administrator: true, Editor: true}, AccountState("active"), true, true},
	{"active_no_roles", Roles{}, AccountState("active"), false, false},
	{"deactivated_administrator", Roles{Administrator: true}, AccountState("deactivated"), false, false},
	{"deactivated_editor", Roles{Editor: true}, AccountState("deactivated"), false, false},
	{"deactivated_both", Roles{Administrator: true, Editor: true}, AccountState("deactivated"), false, false},
	{"deactivated_no_roles", Roles{}, AccountState("deactivated"), false, false},
	// The zero account state means no account, including a signed-out caller.
	// Role flags without an active account must never grant protected actions.
	{"no_account_no_roles", Roles{}, AccountState(""), false, false},
	{"no_account_administrator", Roles{Administrator: true}, AccountState(""), false, false},
	{"no_account_editor", Roles{Editor: true}, AccountState(""), false, false},
	{"no_account_both", Roles{Administrator: true, Editor: true}, AccountState(""), false, false},
}

func TestRoleActionEligibilityMatrix(t *testing.T) {
	// One abstract action per role-protected row in the approved #67 matrix,
	// docs/phase2/access-contract/06-roles.md. Grouped operations have identical
	// role eligibility; these labels do not invent routes or extra permissions.
	matrix := []struct {
		action        Action
		administrator bool
		editor        bool
	}{
		// List, search, filter, and open editorial content for editing or preview.
		{Action("content.read"), false, true},
		// Create content and save or revise drafts.
		{Action("content.draft.write"), false, true},
		// Preview, publish, and unpublish content.
		{Action("content.publish"), false, true},
		// View publication history and restore a snapshot as a draft.
		{Action("content.history"), false, true},
		// Move content to Trash and restore it as unpublished.
		{Action("content.trash"), false, true},
		// Only the trashed target and listed dependencies, not editorial browsing.
		{Action("content.deletion.preview"), true, false},
		// Role eligibility only; review and confirmation remain caller checks.
		{Action("content.delete.permanently"), true, false},
		// Upload/replace images and documents; focal point and placement text.
		{Action("content.media"), false, true},
		// Edit menu items; preview, publish/unpublish, restore a menu snapshot.
		{Action("menu.items"), false, true},
		// View, create, edit, and manually disable redirect rules.
		{Action("redirects"), false, true},
		// Define menus; edit types, fields, image styles, limits and configuration.
		// This action is explicitly development/staging, not production mutation.
		{Action("configuration.development_staging"), true, false},
		// Change site name, labels, logo, and colours.
		{Action("site.branding"), true, false},
		{Action("audit.review"), true, false},
		// View accounts, invite users, assign roles, and deactivate accounts.
		{Action("accounts.manage"), true, false},
	}

	for _, entry := range matrix {
		t.Run(string(entry.action), func(t *testing.T) {
			for _, subject := range permissionSubjects {
				t.Run(subject.name, func(t *testing.T) {
					want := entry.administrator && subject.administrator || entry.editor && subject.editor
					got := AllowsRoleAction(subject.roles, subject.state, entry.action)
					if got != want {
						t.Errorf("AllowsRoleAction(%+v, %q, %q) = %v, want %v", subject.roles, subject.state, entry.action, got, want)
					}
				})
			}
		})
	}
}

func TestRoleActionEligibilityRejectsUnknownAndEntryFlowActions(t *testing.T) {
	// Sign-in/reset/invitation entry flows have credential or token checks
	// independent of signed-in roles. They are outside this function, not
	// unconditional allow actions. Rejection here does not reject those flows
	// in their downstream handlers or grant any permission from role absence.
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
			for _, subject := range permissionSubjects {
				t.Run(subject.name, func(t *testing.T) {
					if AllowsRoleAction(subject.roles, subject.state, action) {
						t.Errorf("AllowsRoleAction(%+v, %q, %q) allowed an action outside the role-protected matrix", subject.roles, subject.state, action)
					}
				})
			}
		})
	}
}
