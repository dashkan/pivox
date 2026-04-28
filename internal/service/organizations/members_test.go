package organizations

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

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
	q.On("GetOrgMember", mock.Anything, db.GetOrgMemberParams{
		OrgID:         testOrg.ID,
		PrincipalKind: db.PrincipalKindUser,
		PrincipalID:   userID,
	}).Return(db.GetOrgMemberRow{
		ID:            uuid.New(),
		OrgID:         testOrg.ID,
		RoleID:        uuid.New(),
		PrincipalKind: db.PrincipalKindUser,
		PrincipalID:   userID,
		RoleName:      "admin",
		Etag:          "etag-m",
		CreateTime:    memberTestNow,
		UpdateTime:    memberTestNow,
	}, nil)

	srv := newServerForMembers(q)
	resp, err := srv.GetMember(context.Background(), &iampb.GetMemberRequest{
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
			_, err := srv.GetMember(context.Background(), &iampb.GetMemberRequest{Name: name})
			require.Error(t, err)
			st, _ := status.FromError(err)
			assert.Equal(t, codes.InvalidArgument, st.Code())
		})
	}
}

func TestGetMember_OrgNotFound(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetOrganizationByName", mock.Anything, "ghost").
		Return(db.Organization{}, pgx.ErrNoRows)

	srv := newServerForMembers(q)
	_, err := srv.GetMember(context.Background(), &iampb.GetMemberRequest{
		Name: "organizations/ghost/members/user-" + uuid.New().String(),
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestListMembers_OrgScope(t *testing.T) {
	userA := uuid.MustParse("0192a000-0040-7000-8000-000000000040")
	userB := uuid.MustParse("0192a000-0041-7000-8000-000000000041")
	q := new(mocks.MockQuerier)
	q.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	q.On("ListOrgMembers", mock.Anything, testOrg.ID).Return([]db.ListOrgMembersRow{
		{
			ID: uuid.New(), OrgID: testOrg.ID, RoleID: uuid.New(),
			PrincipalKind: db.PrincipalKindUser, PrincipalID: userA,
			RoleName: "owner", Etag: "e1", CreateTime: memberTestNow, UpdateTime: memberTestNow,
		},
		{
			ID: uuid.New(), OrgID: testOrg.ID, RoleID: uuid.New(),
			PrincipalKind: db.PrincipalKindUser, PrincipalID: userB,
			RoleName: "editor", Etag: "e2", CreateTime: memberTestNow, UpdateTime: memberTestNow,
		},
	}, nil)

	srv := newServerForMembers(q)
	resp, err := srv.ListMembers(context.Background(), &iampb.ListMembersRequest{
		Parent: "organizations/acme",
	})
	require.NoError(t, err)
	require.Len(t, resp.GetMembers(), 2)
	assert.Equal(t, "organizations/acme/roles/owner", resp.GetMembers()[0].GetRole())
	assert.Equal(t, "organizations/acme/roles/editor", resp.GetMembers()[1].GetRole())
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
			_, err := srv.ListMembers(context.Background(), &iampb.ListMembersRequest{Parent: p})
			require.Error(t, err)
			st, _ := status.FromError(err)
			assert.Equal(t, codes.InvalidArgument, st.Code())
		})
	}
}
