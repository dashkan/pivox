package fixtures

import (
	"time"

	"github.com/google/uuid"

	db "github.com/dashkan/pivox/internal/db/generated"
)

// DefaultOrgID is the canonical UUID for the default test org.
// Stable across calls so assertions can reference it directly.
var DefaultOrgID = uuid.MustParse("00000000-0000-7000-8000-000000000001")

// DefaultTime is the timestamp every fixture defaults to. Tests
// asserting on exact times can use this without time.Now() flakes.
var DefaultTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// OrgOpt mutates a db.Organization. Composed via Org(opt1, opt2, ...).
type OrgOpt func(*db.Organization)

// Org returns a default-populated db.Organization. The defaults
// represent an ACTIVE org named "acme" with stable ID and timestamps.
func Org(opts ...OrgOpt) db.Organization {
	o := db.Organization{
		ID:          DefaultOrgID,
		Name:        "acme",
		DisplayName: "Acme Corp",
		State:       db.ResourceStateACTIVE,
		Etag:        "etag-default",
		Revision:    1,
		CreateTime:  DefaultTime,
		UpdateTime:  DefaultTime,
	}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// OrgID overrides the org ID.
func OrgID(id uuid.UUID) OrgOpt { return func(o *db.Organization) { o.ID = id } }

// OrgName overrides the slug name (the URL-safe identifier).
func OrgName(name string) OrgOpt { return func(o *db.Organization) { o.Name = name } }

// OrgDisplayName overrides the human-readable display name.
func OrgDisplayName(name string) OrgOpt {
	return func(o *db.Organization) { o.DisplayName = name }
}

// OrgState sets the org's lifecycle state (ACTIVE,
// DELETE_REQUESTED, etc.).
func OrgState(s db.ResourceState) OrgOpt { return func(o *db.Organization) { o.State = s } }

// OrgEtag overrides the etag — tests that exercise If-Match
// preconditions or revision-pin guards typically need this.
func OrgEtag(etag string) OrgOpt { return func(o *db.Organization) { o.Etag = etag } }

// OrgRevision overrides the monotonic revision counter.
func OrgRevision(r int32) OrgOpt { return func(o *db.Organization) { o.Revision = r } }
