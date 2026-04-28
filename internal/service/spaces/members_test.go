package spaces

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

// Space-scope Member handler tests. Org-scope variants live in
// `internal/service/organizations/members_test.go`.

var memberTestNow = time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

func newServerForMembers(q *mocks.MockQuerier) *SpacesServer {
	return &SpacesServer{queries: q}
}

func TestSpaces_GetMember_GroupPrincipal(t *testing.T) {
	spaceID := uuid.MustParse("0192a000-0020-7000-8000-000000000020")
	groupID := uuid.MustParse("0192a000-0030-7000-8000-000000000030")
	q := new(mocks.MockQuerier)
	q.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	q.On("GetSpaceByName", mock.Anything, db.GetSpaceByNameParams{
		OrgID: testOrg.ID,
		Name:  "news",
	}).Return(db.Space{ID: spaceID, OrgID: testOrg.ID, Name: "news"}, nil)
	q.On("GetSpaceMember", mock.Anything, db.GetSpaceMemberParams{
		SpaceID:       spaceID,
		PrincipalKind: db.PrincipalKindGroup,
		PrincipalID:   groupID,
	}).Return(db.GetSpaceMemberRow{
		ID:            uuid.New(),
		SpaceID:       spaceID,
		RoleID:        uuid.New(),
		PrincipalKind: db.PrincipalKindGroup,
		PrincipalID:   groupID,
		RoleName:      "editor",
		Etag:          "etag-sm",
		CreateTime:    memberTestNow,
		UpdateTime:    memberTestNow,
	}, nil)

	srv := newServerForMembers(q)
	resp, err := srv.GetMember(context.Background(), &iampb.GetMemberRequest{
		Name: "organizations/acme/spaces/news/members/group-" + groupID.String(),
	})
	require.NoError(t, err)
	assert.Equal(t, "organizations/acme/spaces/news/members/group-"+groupID.String(), resp.GetName())
	assert.Equal(t, "organizations/acme/roles/editor", resp.GetRole())
	assert.Equal(t, "organizations/acme/groups/"+groupID.String(), resp.GetGroup())
	assert.Empty(t, resp.GetUser())
}

func TestSpaces_GetMember_InvalidNameShape(t *testing.T) {
	cases := []string{
		// org-scope shape rejected at the space service.
		"organizations/acme/members/user-" + uuid.New().String(),
		"organizations/acme/spaces/news/members/no-prefix",
		"organizations/acme/spaces/news/members/user-not-a-uuid",
		"organizations/acme/spaces/news/members/",
		"organizations//spaces/news/members/user-x",
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

func TestSpaces_GetMember_OrgNotFound(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetOrganizationByName", mock.Anything, "ghost").
		Return(db.Organization{}, pgx.ErrNoRows)

	srv := newServerForMembers(q)
	_, err := srv.GetMember(context.Background(), &iampb.GetMemberRequest{
		Name: "organizations/ghost/spaces/news/members/user-" + uuid.New().String(),
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestSpaces_ListMembers_DirectBindingsOnly(t *testing.T) {
	spaceID := uuid.MustParse("0192a000-0050-7000-8000-000000000050")
	userA := uuid.MustParse("0192a000-0051-7000-8000-000000000051")
	q := new(mocks.MockQuerier)
	q.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	q.On("GetSpaceByName", mock.Anything, mock.Anything).Return(db.Space{ID: spaceID}, nil)
	q.On("ListSpaceMembers", mock.Anything, spaceID).Return([]db.ListSpaceMembersRow{
		{
			ID: uuid.New(), SpaceID: spaceID, RoleID: uuid.New(),
			PrincipalKind: db.PrincipalKindUser, PrincipalID: userA,
			RoleName: "viewer", Etag: "e", CreateTime: memberTestNow, UpdateTime: memberTestNow,
		},
	}, nil)

	srv := newServerForMembers(q)
	resp, err := srv.ListMembers(context.Background(), &iampb.ListMembersRequest{
		Parent: "organizations/acme/spaces/news",
	})
	require.NoError(t, err)
	require.Len(t, resp.GetMembers(), 1)
	assert.Equal(t, "organizations/acme/spaces/news/members/user-"+userA.String(),
		resp.GetMembers()[0].GetName())
}

func TestSpaces_ListMembers_InvalidParentShape(t *testing.T) {
	cases := []string{
		"",
		"organizations/acme",
		"organizations/acme/spaces",
		"organizations/acme/spaces/",
		"organizations/acme/spaces/news/extra",
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
