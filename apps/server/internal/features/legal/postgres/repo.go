// Package postgres is the PostgreSQL adapter of the legal feature.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/XYZLZ/MyPark/apps/server/internal/features/legal"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database"
	"github.com/XYZLZ/MyPark/apps/server/internal/infrastructure/database/dbgen"
)

// Repo implements legal.Repo over dbgen.
type Repo struct{ pool *pgxpool.Pool }

var _ legal.Repo = (*Repo)(nil)

// NewRepo returns a Repo that reads through pool, or through the transaction
// carried by the context when there is one.
func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// Current implements legal.Repo.
func (r *Repo) Current(ctx context.Context, now time.Time) (legal.Versions, error) {
	rows, err := dbgen.New(database.Conn(ctx, r.pool)).
		CurrentLegalDocuments(ctx, pgtype.Timestamptz{Time: now, Valid: true})
	if err != nil {
		return legal.Versions{}, fmt.Errorf("legal: current documents: %w", err)
	}
	var v legal.Versions
	var haveTerms, havePrivacy bool
	for _, row := range rows {
		d := legal.Document{
			Kind: row.Kind, Version: row.Version, PublishedAt: row.PublishedAt.Time,
			URLEs: row.UrlEs, URLEn: row.UrlEn,
		}
		switch row.Kind {
		case legal.KindTerms:
			v.Terms, haveTerms = d, true
		case legal.KindPrivacy:
			v.Privacy, havePrivacy = d, true
		}
	}
	if !haveTerms || !havePrivacy {
		return legal.Versions{}, fmt.Errorf("legal: no published terms or privacy document")
	}
	return v, nil
}
