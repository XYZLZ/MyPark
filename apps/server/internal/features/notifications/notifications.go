// Package notifications defines the transactional email outbox port.
package notifications

import "context"

// Template names a message template. The set matches the outbox_messages
// template check.
type Template string

// Email templates available in M1.
const (
	TplVerifyEmail     Template = "verify_email"
	TplPasswordReset   Template = "password_reset"
	TplPasswordChanged Template = "password_changed"
	TplAccountExists   Template = "account_exists"
	TplAdminInvite     Template = "admin_invite"
)

// Email is one message to enqueue. Locale is the users.locale of the
// recipient. Data is the template data; a "code" entry is scrubbed once the
// message is sent. DedupeKey makes Enqueue idempotent, for example
// "verify_email:<auth_code id>".
type Email struct {
	Template  Template
	Locale    string
	To        string
	UserID    *int64
	Data      map[string]string
	DedupeKey string
}

// Notifier writes to the email outbox.
type Notifier interface {
	// Enqueue inserts e in the transaction carried by ctx, so the message
	// exists only if the action commits. A repeated DedupeKey is ignored.
	// It fails with database.ErrNoTx when ctx has no transaction.
	Enqueue(ctx context.Context, e Email) error
	// ScrubPendingForUser removes payload.code from the pending and sending
	// messages of a user (account deletion, spec 7.8). It fails with
	// database.ErrNoTx when ctx has no transaction.
	ScrubPendingForUser(ctx context.Context, userID int64) error
}
