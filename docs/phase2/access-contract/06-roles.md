# Roles

This is the proposed Phase 2 authorization contract. It describes the target behavior and does not imply that the current Phase 1 application enforces roles.

The [PRD's functional requirements](../../prd.md#functional-requirements), especially FR-01 through FR-18, define the v1 actions. The [account contract](01-accounts-and-sessions.md) defines account states and role storage. The [runtime and deployment ADR](../../adr/0001-runtime-content-and-deployment.md) defines configuration and recovery boundaries. The [Phase 1 work plan](../../work_plan.md), [Phase 2 work plan](../work_plan.md), [Phase 2 ticket plan](../tickets.md), and [test guidance](../../testing.md) establish sequencing and evidence; they do not change the PRD's role permissions.

## Permission matrix

An account may hold the administrator role, the editor role, or both. Each role grants only the actions marked for it. A user with both roles receives the union of those permissions. Being an administrator alone never grants editorial content permissions, and being an editor alone never grants administrator permissions. These rules apply to direct requests as well as controls shown in the interface. Unauthenticated requests and requests from deactivated accounts are denied every role-protected action, even if a deactivated account still has role flags.

| V1 action | Administrator role | Editor role |
| --- | --- | --- |
| Sign in and sign out while the account is active | Yes | Yes |
| Request and complete a self-service password reset through the email flow | Yes | Yes |
| Accept an invitation and set a password | Valid invitation token | Valid invitation token |
| List, search, and filter content; open content for editing or preview | No | Yes |
| Create content and save or revise drafts | No | Yes |
| Preview, publish, and unpublish content | No | Yes |
| View publication history and restore a content snapshot as a draft | No | Yes |
| Move content to Trash and restore it as unpublished | No | Yes |
| View the dependency preview for a proposed permanent deletion | Yes | No |
| Permanently delete trashed content after reviewing and confirming its listed effects | Yes | No |
| Upload or replace images and documents in content fields; set image focal point and placement alternative text or decorative choice | No | Yes |
| Edit menu items, preview a menu draft, publish or unpublish the menu, and restore a menu snapshot as a draft | No | Yes |
| View, create, edit, and manually disable redirect rules | No | Yes |
| Define menus and edit content types, fields, image styles, upload limits, and other site configuration in development or staging | Yes | No |
| Change site name, labels, logo, and colours | Yes | No |
| Review the audit log | Yes | No |
| View accounts, invite users, assign roles, and deactivate accounts | Yes | No |

Administrator-only configuration actions include defining a menu. They do not include editing menu items, which are editorial content. Administrators cannot list, read, edit, preview, upload to, publish, unpublish, trash, restore, or manage redirects for editorial content unless they also hold the editor role. Editors cannot define menus or alter site configuration, accounts, or audit records.

Sign-in and password-reset entry points do not require an existing signed-in role. A password reset is completed through the emailed link; requesting one does not grant account access.

Invitation acceptance is also an unauthenticated entry point. A valid invitation token authorizes the invited user to complete account setup; it does not grant permissions beyond the roles assigned to that account.

In production, configuration is deploy-only. The administrator role does not permit configuration changes through the interface or direct requests, including content type, field, menu-definition, or image-style changes. A reviewed configuration is applied through the explicit deployment process. The PRD's deploy, export, and restore commands are operator functions. CLI authorization uses its separately defined operator principal, which holds both roles; the complete command and route inventory belongs to [ticket #69](../tickets.md#p2-08-access-contract-cli-command-and-http-route-inventory-69).

For a proposed permanent deletion, an administrator may view only the trashed target and the dependency preview listing affected menu items, redirects, and relationship references. This narrow preview is not permission to browse or preview editorial content. The administrator may proceed only after reviewing and confirming the listed effects. Cancellation changes nothing; confirmation removes the listed dependent menu items and redirects and clears relationship links together. Other affected content items remain. User accounts are never permanently deleted in v1, and deactivation retains attribution on past content.

## Last active administrator

After site initialization, the site must always have at least one active account with the administrator role. The first account created by initialization and the account created by CLI access recovery receive both roles. The CLI operator principal also holds both roles for CLI authorization checks.

Before deactivating an account or removing its administrator role, the system must check and apply the change transactionally. If that change would leave no active administrator, reject it and leave account data unchanged. The check must serialize competing changes, including simultaneous self-demotions, so two administrators cannot both pass against the same earlier count. Apply the same invariant to account changes through the CLI, initialization, and full restore. A full restore that would leave no active administrator must fail validation before cutover.

The invariant preserves a valid operator path during recovery. It does not allow the administrator role to be stripped of its built-in permissions, and assigning both roles does not make either permission set configurable. A deactivated account cannot sign in, but its identity remains available for historical attribution.
