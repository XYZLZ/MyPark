// Package audit defines the append-only audit trail. Entries hold ids, action
// names and changed field names only: never emails, names, phones, plates,
// codes, tokens, hashes or any other value (OC-26).
package audit

import "context"

// ActorKind is who performed an audited action.
type ActorKind string

// Actor kinds stored in audit_log.actor_kind.
const (
	ActorUser      ActorKind = "user"
	ActorAdmin     ActorKind = "admin"
	ActorSystem    ActorKind = "system"
	ActorAnonymous ActorKind = "anonymous"
)

// Audited actions (spec section 12 and OC-39).
const (
	AuthRegistered             = "auth.registered"
	AuthRegistrationReplaced   = "auth.registration_replaced"
	AuthEmailVerified          = "auth.email_verified"
	AuthLoginSucceeded         = "auth.login_succeeded"
	AuthLoginFailed            = "auth.login_failed"
	AuthRefreshReuseDetected   = "auth.refresh_reuse_detected"
	AuthLogout                 = "auth.logout"
	AuthLogoutAll              = "auth.logout_all"
	AuthPasswordResetRequested = "auth.password_reset_requested"
	AuthPasswordReset          = "auth.password_reset"
	UserProfileUpdated         = "user.profile_updated"
	UserLegalAccepted          = "user.legal_accepted"
	UserDeleted                = "user.deleted"
	AdminCreated               = "admin.created"
	AdminInvitationAccepted    = "admin.invitation_accepted"
	AdminInvitationReissued    = "admin.invitation_reissued"
	AdminSeeded                = "admin.seeded"
)

// MetaReplacedPendingDriver is the metadata key set to true on admin.created
// when an invite took over a never-verified driver row (OC-39).
const MetaReplacedPendingDriver = "replaced_pending_driver"

// Entry is one audit record. EntityID is a public id, never an internal one
// or a personal value. Before and After hold non-PII metadata only; for
// profile changes that means field names, never values.
type Entry struct {
	Action      string
	Actor       ActorKind
	ActorUserID *int64
	EntityType  string
	EntityID    string
	Before      map[string]any
	After       map[string]any
}

// Recorder appends entries to the audit log.
type Recorder interface {
	// Record writes e in the transaction carried by ctx, filling the client
	// IP and request id from ctx. It fails with database.ErrNoTx when ctx has
	// no transaction, so an audit row always commits or rolls back with the
	// action it describes.
	Record(ctx context.Context, e Entry) error
}
