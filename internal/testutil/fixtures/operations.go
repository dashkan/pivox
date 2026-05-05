package fixtures

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/dashkan/pivox/internal/db/generated"
)

// DefaultOperationID is the canonical UUID for the default test
// operation. Stable across calls.
var DefaultOperationID = uuid.MustParse("00000000-0000-7000-8000-000000000002")

// OperationOpt mutates a db.Operation.
type OperationOpt func(*db.Operation)

// Operation returns a default-populated db.Operation: a pending
// (done=false) op with parent "organizations/acme" and stable
// timestamps. Override with options.
func Operation(opts ...OperationOpt) db.Operation {
	o := db.Operation{
		ID:         DefaultOperationID,
		Parent:     "organizations/acme",
		Done:       false,
		CreateTime: DefaultTime,
		UpdateTime: DefaultTime,
		ExpireTime: DefaultTime.AddDate(0, 0, 30),
	}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// OpID overrides the operation's UUID.
func OpID(id uuid.UUID) OperationOpt { return func(o *db.Operation) { o.ID = id } }

// OpParent overrides the AIP-151 parent resource.
func OpParent(p string) OperationOpt { return func(o *db.Operation) { o.Parent = p } }

// OpDone marks the operation as completed.
func OpDone() OperationOpt { return func(o *db.Operation) { o.Done = true } }

// OpOrgID sets the reverse pointer to the org this LRO operates
// against.
func OpOrgID(id uuid.UUID) OperationOpt {
	return func(o *db.Operation) {
		o.OrgID = pgtype.UUID{Bytes: id, Valid: true}
	}
}

// OpFailed marks the operation done with an error code + message.
func OpFailed(code int32, msg string) OperationOpt {
	return func(o *db.Operation) {
		o.Done = true
		o.ErrorCode = pgtype.Int4{Int32: code, Valid: true}
		o.ErrorMessage = pgtype.Text{String: msg, Valid: true}
	}
}

// OpMetadata sets the operation's metadata blob (typically a
// proto-marshaled DeleteOrganizationMetadata, etc.).
func OpMetadata(b []byte) OperationOpt { return func(o *db.Operation) { o.Metadata = b } }

// OpResult sets the operation's result blob (set on success).
func OpResult(b []byte) OperationOpt { return func(o *db.Operation) { o.Result = b } }
