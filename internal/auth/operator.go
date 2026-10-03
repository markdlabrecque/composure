package auth

// OperatorPrincipal returns the fixed CLI operator's roles and eligibility.
// The CLI operator is separate from persisted web accounts and holds both
// roles for actions checked through AllowsRoleAction.
func OperatorPrincipal() (Roles, AccountState) {
	return Roles{Administrator: true, Editor: true}, AccountActive
}
