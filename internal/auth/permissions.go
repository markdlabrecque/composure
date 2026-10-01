package auth

// Roles identifies the independent roles held by an account.
type Roles struct {
	Administrator bool
	Editor        bool
}

// AccountState is the account's access state. The zero value represents no account.
type AccountState string

const (
	AccountActive      AccountState = "active"
	AccountDeactivated AccountState = "deactivated"
)

// Action identifies an action in the role-protected permission matrix.
type Action string

// AllowsRoleAction reports whether an active account's roles permit a protected
// action. It checks role eligibility only; callers must independently validate
// credentials, sessions, CSRF, tokens, resource state, and confirmations.
// Entry flows such as sign-in, password reset, and invitation acceptance are
// outside this matrix and are denied here; their handlers own their preconditions.
func AllowsRoleAction(roles Roles, state AccountState, action Action) bool {
	if state != AccountActive {
		return false
	}

	switch action {
	case "content.read", "content.draft.write", "content.publish",
		"content.history", "content.trash", "content.media",
		"menu.items", "redirects":
		return roles.Editor
	case "content.deletion.preview", "content.delete.permanently",
		"configuration.development_staging", "site.branding",
		"audit.review", "accounts.manage":
		return roles.Administrator
	default:
		return false
	}
}
