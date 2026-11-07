package legal

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
)

// InitialVersion is the version of the first terms and privacy documents.
const InitialVersion = "2026-11-16"

// initialPublishedAt is when the initial documents count as published. It is
// earlier than the InitialVersion label so the documents are current as soon
// as they are seeded, whatever day that is.
var initialPublishedAt = time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)

// webBase is where Track B renders the legal pages in development.
const webBase = "http://localhost:3000"

// SeedDocuments inserts the initial terms and privacy documents. It is
// idempotent: documents that already exist are left untouched.
func SeedDocuments(ctx context.Context, db database.DBTX) error {
	q := dbgen.New(db)
	for _, kind := range []string{KindTerms, KindPrivacy} {
		err := q.InsertLegalDocument(ctx, dbgen.InsertLegalDocumentParams{
			Kind:        kind,
			Version:     InitialVersion,
			PublishedAt: pgtype.Timestamptz{Time: initialPublishedAt, Valid: true},
			UrlEs:       fmt.Sprintf("%s/es-DO/legal/%s", webBase, kind),
			UrlEn:       fmt.Sprintf("%s/en/legal/%s", webBase, kind),
		})
		if err != nil {
			return fmt.Errorf("seed: insert %s document: %w", kind, err)
		}
	}
	return nil
}
