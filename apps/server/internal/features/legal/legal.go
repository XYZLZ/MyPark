// Package legal serves the current terms and privacy documents and checks
// that a user accepted their current versions.
package legal

import (
	"context"
	"time"
)

// Document kinds, as stored in legal_documents.kind.
const (
	KindTerms   = "terms"
	KindPrivacy = "privacy"
)

// Document is one published legal document.
type Document struct {
	Kind        string
	Version     string
	PublishedAt time.Time
	URLEs       string
	URLEn       string
}

// Versions holds the current document of each kind.
type Versions struct {
	Terms, Privacy Document
}

// Current is the use-case interface other features depend on.
type Current interface {
	// Versions returns the current terms and privacy documents.
	Versions(ctx context.Context) (Versions, error)
	// ValidateAcceptance returns a legal.version_outdated conflict, with the
	// current versions in its details, unless terms and privacy are both the
	// current versions.
	ValidateAcceptance(ctx context.Context, terms, privacy string) error
}
