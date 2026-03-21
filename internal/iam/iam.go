package iam

import (
	"context"
	"encoding/json"
	"strings"

	iampb "github.com/dashkan/pivox-server/internal/pkg/gen/pivox/iam/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/dashkan/pivox-server/internal/apierr"
	db "github.com/dashkan/pivox-server/internal/db/generated"
	"github.com/dashkan/pivox-server/internal/resource"
)

// Helper provides reusable IAM operations for any resource type.
type Helper struct {
	queries *db.Queries
}

// NewHelper creates a new IAM helper.
func NewHelper(queries *db.Queries) *Helper {
	return &Helper{queries: queries}
}

// GetIamPolicy retrieves the IAM policy for a resource.
// Returns an empty policy if none exists.
func (h *Helper) GetIamPolicy(ctx context.Context, req *iampb.GetIamPolicyRequest) (*iampb.Policy, error) {
	resourceID, err := h.resolveResourceID(ctx, req.GetName())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("name", err.Error()))
	}

	dbPolicy, err := h.queries.GetIamPolicy(ctx, resourceID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return &iampb.Policy{}, nil
		}
		return nil, apierr.Internal("failed to get IAM policy")
	}

	policy := &iampb.Policy{}
	if len(dbPolicy.Policy) > 0 {
		if err := protojson.Unmarshal(dbPolicy.Policy, policy); err != nil {
			return nil, apierr.Internal("failed to unmarshal IAM policy")
		}
	}
	policy.Etag = dbPolicy.Etag
	return policy, nil
}

// SetIamPolicy sets the IAM policy for a resource.
func (h *Helper) SetIamPolicy(ctx context.Context, req *iampb.SetIamPolicyRequest) (*iampb.Policy, error) {
	resourceID, err := h.resolveResourceID(ctx, req.GetResource())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("resource", err.Error()))
	}

	resourceType := resource.CollectionFromName(req.GetResource())

	policy := req.GetPolicy()
	if policy == nil {
		policy = &iampb.Policy{}
	}

	// Check etag if provided
	if policy.Etag != "" {
		existing, err := h.queries.GetIamPolicy(ctx, resourceID)
		if err != nil && err != pgx.ErrNoRows {
			return nil, apierr.Internal("failed to get existing IAM policy")
		}
		if err == nil && existing.Etag != policy.Etag {
			return nil, apierr.EtagMismatch(req.GetResource(), policy.Etag, existing.Etag)
		}
	}

	// Clear etag before marshaling (we store it separately)
	policy.Etag = ""
	policyJSON, err := protojson.Marshal(policy)
	if err != nil {
		return nil, apierr.Internal("failed to marshal IAM policy")
	}

	dbPolicy, err := h.queries.UpsertIamPolicy(ctx, db.UpsertIamPolicyParams{
		ResourceID:   resourceID,
		ResourceType: resourceType,
		Policy:       json.RawMessage(policyJSON),
		UpdatedBy:    "",
	})
	if err != nil {
		return nil, apierr.Internal("failed to set IAM policy")
	}

	result := &iampb.Policy{}
	if err := protojson.Unmarshal(dbPolicy.Policy, result); err != nil {
		return nil, apierr.Internal("failed to unmarshal IAM policy")
	}
	result.Etag = dbPolicy.Etag
	return result, nil
}

// TestIamPermissions returns all requested permissions.
func (h *Helper) TestIamPermissions(ctx context.Context, req *iampb.TestIamPermissionsRequest) (*iampb.TestIamPermissionsResponse, error) {
	return &iampb.TestIamPermissionsResponse{
		Permissions: req.GetPermissions(),
	}, nil
}

// resolveResourceID resolves a resource name to its UUID.
// Supports: organizations/{name}, organizations/{name}/projects/{name},
// tagKeys/{uuid}, tagKeys/{uuid}/tagValues/{uuid}
func (h *Helper) resolveResourceID(ctx context.Context, resourceName string) (uuid.UUID, error) {
	parts := strings.Split(resourceName, "/")

	switch {
	case len(parts) == 2 && parts[0] == "organizations":
		org, err := h.queries.GetOrganizationByName(ctx, parts[1])
		if err != nil {
			return uuid.Nil, err
		}
		return org.ID, nil

	case len(parts) == 4 && parts[0] == "organizations" && parts[2] == "projects":
		org, err := h.queries.GetOrganizationByName(ctx, parts[1])
		if err != nil {
			return uuid.Nil, err
		}
		project, err := h.queries.GetProjectByName(ctx, db.GetProjectByNameParams{OrgID: org.ID, Name: parts[3]})
		if err != nil {
			return uuid.Nil, err
		}
		return project.ID, nil

	case len(parts) == 2 && parts[0] == "tagKeys":
		id, err := uuid.Parse(parts[1])
		if err != nil {
			return uuid.Nil, err
		}
		return id, nil

	case len(parts) == 4 && parts[0] == "tagKeys" && parts[2] == "tagValues":
		id, err := uuid.Parse(parts[3])
		if err != nil {
			return uuid.Nil, err
		}
		return id, nil

	default:
		return uuid.Nil, apierr.InvalidArgument(apierr.FieldViolation("resource", "unknown resource type"))
	}
}
