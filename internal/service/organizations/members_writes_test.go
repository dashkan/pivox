package organizations

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	db "github.com/dashkan/pivox/internal/db/generated"
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

// org-scope Member writes — boundary semantics and TransferOwnership
// atomicity are the load-bearing tests here. Happy-path Create
// success is covered by integration tests (see
// `server_integration_test.go`); these focused unit tests exercise
// the security-critical paths against tx-mocked infrastructure.

var membersWriteTestNow = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

// newMemberWritesServer wires the minimum a write handler needs.
func newMemberWritesServer(pool *mockTxBeginner, q *mocks.MockQuerier) *OrganizationsServer {
	return &OrganizationsServer{pool: pool, queries: q}
}

// memberRow returns a mockRow whose Scan populates the column set
// returned by the various member-shaped queries used inside a tx.
// Tests that exercise multiple sequential queries within one tx use
// this to stage results in order.
//
// destWriters apply in order to the Scan dest slots; they're free
// to ignore the dest if they don't care about it (some scans return
// fewer destinations than members of the corresponding sqlc Row
// type).
func memberRow(destWriters ...func(dest interface{}) bool) *mockRow {
	return &mockRow{scanFunc: func(dest ...interface{}) error {
		for i, d := range dest {
			if i < len(destWriters) {
				if !destWriters[i](d) {
					return errors.New("scan dest type mismatch")
				}
			}
		}
		return nil
	}}
}

// scanGetOrgMemberRow stages a GetOrgMemberRow shape: 12 columns —
// id, org_id, role_id, principal_kind, principal_id, etag, revision,
// created_by, updated_by, create_time, update_time, role_name (joined).
func scanGetOrgMemberRow(row db.GetOrgMemberRow) *mockRow {
	return &mockRow{scanFunc: func(dest ...interface{}) error {
		if len(dest) != 12 {
			return errors.New("unexpected GetOrgMemberRow column count")
		}
		*dest[0].(*uuid.UUID) = row.ID
		*dest[1].(*uuid.UUID) = row.OrgID
		*dest[2].(*uuid.UUID) = row.RoleID
		*dest[3].(*db.PrincipalKind) = row.PrincipalKind
		*dest[4].(*uuid.UUID) = row.PrincipalID
		*dest[5].(*string) = row.Etag
		*dest[6].(*int32) = row.Revision
		*dest[7].(*string) = row.CreatedBy
		*dest[8].(*string) = row.UpdatedBy
		*dest[9].(*time.Time) = row.CreateTime
		*dest[10].(*time.Time) = row.UpdateTime
		*dest[11].(*string) = row.RoleName
		return nil
	}}
}

// scanInt64 stages a single int64 Scan (CountOwnersByOrg shape).
func scanInt64(v int64) *mockRow {
	return &mockRow{scanFunc: func(dest ...interface{}) error {
		if len(dest) != 1 {
			return errors.New("unexpected int64 column count")
		}
		*dest[0].(*int64) = v
		return nil
	}}
}

// scanUpdateRow stages an UpdateOrgMemberRoleRow shape: id, etag,
// create_time, update_time.
func scanUpdateRow(id uuid.UUID, etag string, ct, ut time.Time) *mockRow {
	return &mockRow{scanFunc: func(dest ...interface{}) error {
		if len(dest) != 4 {
			return errors.New("unexpected UpdateOrgMemberRoleRow column count")
		}
		*dest[0].(*uuid.UUID) = id
		*dest[1].(*string) = etag
		*dest[2].(*time.Time) = ct
		*dest[3].(*time.Time) = ut
		return nil
	}}
}

// ---------------------------------------------------------------------------
// UpdateMember boundary
// ---------------------------------------------------------------------------

// TestUpdateMember_BoundaryRejectsLastOwnerDemotion is the load-bearing
// test for the ≥1-owner invariant on org-scope Members. With exactly
// one owner currently, demoting them to admin would leave the org
// ownerless; the handler must refuse with FAILED_PRECONDITION.
func TestUpdateMember_BoundaryRejectsLastOwnerDemotion(t *testing.T) {
	q := new(mocks.MockQuerier)
	pool := new(mockTxBeginner)
	tx := new(mockTx)
	userID := uuid.MustParse("0192a000-1000-7000-8000-000000000001")
	memberID := uuid.MustParse("0192a000-1000-7000-8000-000000000002")
	ownerRoleID := uuid.MustParse("0192a000-1000-7000-8000-000000000003")
	adminRoleID := uuid.MustParse("0192a000-1000-7000-8000-000000000004")

	q.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	q.On("GetSystemRole", mock.Anything, db.GetSystemRoleParams{
		OrgID: testOrg.ID, Name: "admin",
	}).Return(db.Role{ID: adminRoleID, Name: "admin", IsSystem: true}, nil)

	pool.On("Begin", mock.Anything).Return(tx, nil)
	// Inside-tx: GetOrgMember returns current binding (owner role).
	tx.On("QueryRow", mock.Anything, mock.Anything, mock.Anything).
		Return(scanGetOrgMemberRow(db.GetOrgMemberRow{
			ID:            memberID,
			OrgID:         testOrg.ID,
			RoleID:        ownerRoleID,
			PrincipalKind: db.PrincipalKindUser,
			PrincipalID:   userID,
			RoleName:      "owner",
			Etag:          "etag-current",
			CreateTime:    membersWriteTestNow,
			UpdateTime:    membersWriteTestNow,
		})).Once()
	// Inside-tx: CountOwnersByOrg returns 1 — the boundary fires here.
	tx.On("QueryRow", mock.Anything, mock.Anything, mock.Anything).
		Return(scanInt64(1)).Once()
	// Tx rolls back (no commit on the boundary-rejection path).
	tx.On("Rollback", mock.Anything).Return(nil)

	srv := newMemberWritesServer(pool, q)
	_, err := srv.UpdateMember(context.Background(), &iampb.UpdateMemberRequest{
		Member: &iampb.Member{
			Name: "organizations/acme/members/user-" + userID.String(),
			Role: "organizations/acme/roles/admin",
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"role"}},
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code(),
		"demoting the last owner must return FailedPrecondition")
	assert.Contains(t, st.Message(), "last owner")

	// No UPDATE happened — only 2 QueryRow calls (the GetOrgMember
	// + CountOwnersByOrg lookups). The role mutation never fires.
	tx.AssertNumberOfCalls(t, "QueryRow", 2)
	tx.AssertNotCalled(t, "Commit", mock.Anything)
}

// TestUpdateMember_AllowsDemotionWhenMultipleOwners is the inverse
// case: with 2+ owners, demoting one is fine — the boundary holds.
func TestUpdateMember_AllowsDemotionWhenMultipleOwners(t *testing.T) {
	q := new(mocks.MockQuerier)
	pool := new(mockTxBeginner)
	tx := new(mockTx)
	userID := uuid.MustParse("0192a000-1100-7000-8000-000000000001")
	memberID := uuid.MustParse("0192a000-1100-7000-8000-000000000002")
	ownerRoleID := uuid.MustParse("0192a000-1100-7000-8000-000000000003")
	adminRoleID := uuid.MustParse("0192a000-1100-7000-8000-000000000004")

	q.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	q.On("GetSystemRole", mock.Anything, mock.Anything).
		Return(db.Role{ID: adminRoleID, Name: "admin", IsSystem: true}, nil)

	pool.On("Begin", mock.Anything).Return(tx, nil)
	tx.On("QueryRow", mock.Anything, mock.Anything, mock.Anything).
		Return(scanGetOrgMemberRow(db.GetOrgMemberRow{
			ID:            memberID,
			OrgID:         testOrg.ID,
			RoleID:        ownerRoleID,
			PrincipalKind: db.PrincipalKindUser,
			PrincipalID:   userID,
			RoleName:      "owner",
			Etag:          "etag-current",
			CreateTime:    membersWriteTestNow,
			UpdateTime:    membersWriteTestNow,
		})).Once()
	// 3 owners — boundary holds.
	tx.On("QueryRow", mock.Anything, mock.Anything, mock.Anything).
		Return(scanInt64(3)).Once()
	// UpdateOrgMemberRole returns the new etag/timestamps.
	tx.On("QueryRow", mock.Anything, mock.Anything, mock.Anything).
		Return(scanUpdateRow(memberID, "etag-new", membersWriteTestNow, membersWriteTestNow)).Once()
	tx.On("Commit", mock.Anything).Return(nil)
	tx.On("Rollback", mock.Anything).Return(pgx.ErrTxClosed)

	srv := newMemberWritesServer(pool, q)
	resp, err := srv.UpdateMember(context.Background(), &iampb.UpdateMemberRequest{
		Member: &iampb.Member{
			Name: "organizations/acme/members/user-" + userID.String(),
			Role: "organizations/acme/roles/admin",
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"role"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "organizations/acme/roles/admin", resp.GetRole())
	assert.Equal(t, "etag-new", resp.GetEtag())
}

// ---------------------------------------------------------------------------
// DeleteMember boundary
// ---------------------------------------------------------------------------

// TestDeleteMember_BoundaryRejectsLastOwner — same invariant via the
// delete path. 1 owner currently, the target IS the owner, refuse.
func TestDeleteMember_BoundaryRejectsLastOwner(t *testing.T) {
	q := new(mocks.MockQuerier)
	pool := new(mockTxBeginner)
	tx := new(mockTx)
	userID := uuid.MustParse("0192a000-1200-7000-8000-000000000001")
	memberID := uuid.MustParse("0192a000-1200-7000-8000-000000000002")
	ownerRoleID := uuid.MustParse("0192a000-1200-7000-8000-000000000003")

	q.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)

	pool.On("Begin", mock.Anything).Return(tx, nil)
	tx.On("QueryRow", mock.Anything, mock.Anything, mock.Anything).
		Return(scanGetOrgMemberRow(db.GetOrgMemberRow{
			ID:            memberID,
			OrgID:         testOrg.ID,
			RoleID:        ownerRoleID,
			PrincipalKind: db.PrincipalKindUser,
			PrincipalID:   userID,
			RoleName:      "owner",
			CreateTime:    membersWriteTestNow,
			UpdateTime:    membersWriteTestNow,
		})).Once()
	tx.On("QueryRow", mock.Anything, mock.Anything, mock.Anything).
		Return(scanInt64(1)).Once()
	tx.On("Rollback", mock.Anything).Return(nil)

	srv := newMemberWritesServer(pool, q)
	_, err := srv.DeleteMember(context.Background(), &iampb.DeleteMemberRequest{
		Name: "organizations/acme/members/user-" + userID.String(),
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
	assert.Contains(t, st.Message(), "last owner")
	tx.AssertNotCalled(t, "Exec", mock.Anything, mock.Anything, mock.Anything)
	tx.AssertNotCalled(t, "Commit", mock.Anything)
}

// TestDeleteMember_AllowsDeleteWhenMultipleOwners — boundary holds
// at 2+ owners.
func TestDeleteMember_AllowsDeleteWhenMultipleOwners(t *testing.T) {
	q := new(mocks.MockQuerier)
	pool := new(mockTxBeginner)
	tx := new(mockTx)
	userID := uuid.MustParse("0192a000-1300-7000-8000-000000000001")
	memberID := uuid.MustParse("0192a000-1300-7000-8000-000000000002")
	ownerRoleID := uuid.MustParse("0192a000-1300-7000-8000-000000000003")

	q.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)

	pool.On("Begin", mock.Anything).Return(tx, nil)
	tx.On("QueryRow", mock.Anything, mock.Anything, mock.Anything).
		Return(scanGetOrgMemberRow(db.GetOrgMemberRow{
			ID:            memberID,
			OrgID:         testOrg.ID,
			RoleID:        ownerRoleID,
			PrincipalKind: db.PrincipalKindUser,
			PrincipalID:   userID,
			RoleName:      "owner",
			CreateTime:    membersWriteTestNow,
			UpdateTime:    membersWriteTestNow,
		})).Once()
	tx.On("QueryRow", mock.Anything, mock.Anything, mock.Anything).
		Return(scanInt64(2)).Once()
	tx.On("Exec", mock.Anything, mock.Anything, mock.Anything).
		Return(pgconn.NewCommandTag("DELETE 1"), nil).Once()
	tx.On("Commit", mock.Anything).Return(nil)
	tx.On("Rollback", mock.Anything).Return(pgx.ErrTxClosed)

	srv := newMemberWritesServer(pool, q)
	_, err := srv.DeleteMember(context.Background(), &iampb.DeleteMemberRequest{
		Name: "organizations/acme/members/user-" + userID.String(),
	})
	require.NoError(t, err)
}
