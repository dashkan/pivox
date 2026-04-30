package organizations

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

// Org-scope Member handler tests. Space-scope variants live in
// `internal/service/spaces/members_test.go`.

var memberTestNow = time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

// newServerForMembers builds an OrganizationsServer with only the
// fields needed by Member handlers wired (queries). pool/auth/codec/
// readUID/resolver/caller stay nil — handler tests don't exercise them.
func newServerForMembers(q *mocks.MockQuerier) *OrganizationsServer {
	return &OrganizationsServer{queries: q}
}

func TestGetMember_OrgScope_UserPrincipal(t *testing.T) {
	q := new(mocks.MockQuerier)
	userID := uuid.MustParse("0192a000-0010-7000-8000-000000000010")
	q.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	q.On("GetOrgMemberByUser", mock.Anything, db.GetOrgMemberByUserParams{
		OrgID:  testOrg.ID,
		UserID: convert.PgUUID(userID),
	}).Return(db.GetOrgMemberByUserRow{
		ID:         uuid.New(),
		OrgID:      testOrg.ID,
		RoleID:     uuid.New(),
		UserID:     convert.PgUUID(userID),
		RoleName:   "admin",
		Etag:       "etag-m",
		CreateTime: memberTestNow,
		UpdateTime: memberTestNow,
	}, nil)

	srv := newServerForMembers(q)
	resp, err := srv.GetMember(memberTestCtx(), &iampb.GetMemberRequest{
		Name: "organizations/acme/members/user-" + userID.String(),
	})
	require.NoError(t, err)
	assert.Equal(t, "organizations/acme/members/user-"+userID.String(), resp.GetName())
	assert.Equal(t, "organizations/acme/roles/admin", resp.GetRole())
	assert.Equal(t, "organizations/acme/users/"+userID.String(), resp.GetUser())
	assert.Empty(t, resp.GetGroup())
}

func TestGetMember_InvalidNameShape(t *testing.T) {
	cases := []string{
		"organizations/acme/members/no-prefix",
		"organizations/acme/members/user-not-a-uuid",
		"organizations/acme/members/owner-1234",
		"organizations/acme/members/",
		"members/user-x",
		"organizations//members/user-" + uuid.New().String(),
		// space-scope path is invalid AT THE ORG service: type-narrowed
		// out by URL pattern, but the handler also rejects defensively.
		"organizations/acme/spaces/news/members/user-" + uuid.New().String(),
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			q := new(mocks.MockQuerier)
			srv := newServerForMembers(q)
			_, err := srv.GetMember(memberTestCtx(), &iampb.GetMemberRequest{Name: name})
			require.Error(t, err)
			st, _ := status.FromError(err)
			assert.Equal(t, codes.InvalidArgument, st.Code())
		})
	}
}

// TestGetMember_OrgSlugMismatch defends against a path/scope drift —
// if the resource name's slug doesn't match the resolved scope (which
// the interceptor already gated), the handler refuses rather than
// silently operating on the wrong org. In production this never fires
// because the interceptor 404s on an unknown org before we get here;
// the assertion is paranoia against gate-vs-handler skew.
func TestGetMember_OrgSlugMismatch(t *testing.T) {
	q := new(mocks.MockQuerier)
	srv := newServerForMembers(q)
	_, err := srv.GetMember(memberTestCtx(), &iampb.GetMemberRequest{
		Name: "organizations/ghost/members/user-" + uuid.New().String(),
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestListMembers_OrgScope(t *testing.T) {
	userA := uuid.MustParse("0192a000-0040-7000-8000-000000000040")
	userB := uuid.MustParse("0192a000-0041-7000-8000-000000000041")
	q := new(mocks.MockQuerier)
	q.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	q.On("ListOrgMembers", mock.Anything, mock.MatchedBy(func(p db.ListOrgMembersParams) bool {
		return p.OrgID == testOrg.ID
	})).Return([]db.ListOrgMembersRow{
		{
			ID: uuid.New(), OrgID: testOrg.ID, RoleID: uuid.New(),
			UserID:   convert.PgUUID(userA),
			RoleName: "owner", Etag: "e1", CreateTime: memberTestNow, UpdateTime: memberTestNow,
		},
		{
			ID: uuid.New(), OrgID: testOrg.ID, RoleID: uuid.New(),
			UserID:   convert.PgUUID(userB),
			RoleName: "editor", Etag: "e2", CreateTime: memberTestNow, UpdateTime: memberTestNow,
		},
	}, nil)

	srv := newServerForMembers(q)
	resp, err := srv.ListMembers(memberTestCtx(), &iampb.ListMembersRequest{
		Parent: "organizations/acme",
	})
	require.NoError(t, err)
	require.Len(t, resp.GetMembers(), 2)
	assert.Equal(t, "organizations/acme/roles/owner", resp.GetMembers()[0].GetRole())
	assert.Equal(t, "organizations/acme/roles/editor", resp.GetMembers()[1].GetRole())
}

// TestListMembers_PaginationCursor pins the offset-token round trip.
// 60 rows + page_size=50 → first page returns 50 rows + next_page_token=50.
// Caller passes that token back; server returns the remaining 10 rows
// with no next_page_token.
func TestListMembers_PaginationCursor(t *testing.T) {
	q := new(mocks.MockQuerier)
	pageSize := int32(50)
	// First-page request: handler asks for limit+1 = 51, gets 51 (more
	// pages exist). Build 51 rows so handler sees the truncation signal.
	firstPage := make([]db.ListOrgMembersRow, 51)
	for i := range firstPage {
		firstPage[i] = db.ListOrgMembersRow{
			ID: uuid.New(), OrgID: testOrg.ID, RoleID: uuid.New(),
			UserID:   convert.PgUUID(uuid.New()),
			RoleName: "viewer", Etag: "e", CreateTime: memberTestNow, UpdateTime: memberTestNow,
		}
	}
	q.On("ListOrgMembers", mock.Anything, mock.MatchedBy(func(p db.ListOrgMembersParams) bool {
		return p.Offset == 0 && p.Limit == 51
	})).Return(firstPage, nil).Once()

	// Second page from offset=50: 10 rows remaining, fewer than limit+1
	// so no truncation signal.
	secondPage := firstPage[:10]
	q.On("ListOrgMembers", mock.Anything, mock.MatchedBy(func(p db.ListOrgMembersParams) bool {
		return p.Offset == 50 && p.Limit == 51
	})).Return(secondPage, nil).Once()

	srv := newServerForMembers(q)
	resp, err := srv.ListMembers(memberTestCtx(), &iampb.ListMembersRequest{
		Parent: "organizations/acme", PageSize: pageSize,
	})
	require.NoError(t, err)
	assert.Len(t, resp.GetMembers(), 50)
	assert.Equal(t, "50", resp.GetNextPageToken())

	resp, err = srv.ListMembers(memberTestCtx(), &iampb.ListMembersRequest{
		Parent: "organizations/acme", PageSize: pageSize, PageToken: resp.GetNextPageToken(),
	})
	require.NoError(t, err)
	assert.Len(t, resp.GetMembers(), 10)
	assert.Empty(t, resp.GetNextPageToken(), "last page should not advertise more")
}

func TestListMembers_RejectsBadPageToken(t *testing.T) {
	q := new(mocks.MockQuerier)
	srv := newServerForMembers(q)
	_, err := srv.ListMembers(memberTestCtx(), &iampb.ListMembersRequest{
		Parent: "organizations/acme", PageToken: "not-an-int",
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestListMembers_InvalidParentShape(t *testing.T) {
	cases := []string{
		"",
		"organizations",
		"organizations/",
		// space-shape parent must be rejected at org service.
		"organizations/acme/spaces/news",
		"members",
	}
	for _, p := range cases {
		t.Run(p, func(t *testing.T) {
			q := new(mocks.MockQuerier)
			srv := newServerForMembers(q)
			_, err := srv.ListMembers(memberTestCtx(), &iampb.ListMembersRequest{Parent: p})
			require.Error(t, err)
			st, _ := status.FromError(err)
			assert.Equal(t, codes.InvalidArgument, st.Code())
		})
	}
}
