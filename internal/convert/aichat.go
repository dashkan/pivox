package convert

import (
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"

	db "github.com/dashkan/pivox/internal/db/generated"
	aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
)

// ConversationToProto converts a DB conversation to proto.
// orgName is the organization segment (e.g. "acme"). The resource
// name encodes ownership via the creator's `firebase_identities.id`
// in the `users/{user}` segment — same uuid the handler enforces
// path-vs-caller against.
func ConversationToProto(row db.AiConversation, orgName string) *aiv1.Conversation {
	pb := &aiv1.Conversation{
		Name:         fmt.Sprintf("organizations/%s/users/%s/conversations/%s", orgName, row.CreatedBy, row.Name),
		Creator:      fmt.Sprintf("organizations/%s/users/%s", orgName, row.CreatedBy),
		Title:        row.Title,
		TitleUserSet: row.TitleUserSet,
		Description:  row.Description,
		Archived:     row.Archived,
		Pinned:       row.Pinned,
		MessageCount: row.MessageCount,
		Etag:         row.Etag,
		CreateTime:   timestamppb.New(row.CreateTime),
		UpdateTime:   timestamppb.New(row.UpdateTime),
	}
	if row.LastMessageTime.Valid {
		pb.LastMessageTime = timestamppb.New(row.LastMessageTime.Time)
	}
	return pb
}

// MessageToProto converts a DB message to proto.
// convName is the full conversation resource name.
func MessageToProto(row db.AiMessage, convName string) (*aiv1.Message, error) {
	pb := &aiv1.Message{
		Name:       fmt.Sprintf("%s/messages/%s", convName, row.Name),
		Role:       roleToProto(row.Role),
		CreateTime: timestamppb.New(row.CreateTime),
	}

	if len(row.Parts) > 0 && string(row.Parts) != "[]" {
		var rawParts []json.RawMessage
		if err := json.Unmarshal(row.Parts, &rawParts); err != nil {
			return nil, fmt.Errorf("unmarshal message parts: %w", err)
		}
		for _, raw := range rawParts {
			p := &aiv1.MessagePart{}
			if err := protojson.Unmarshal(raw, p); err != nil {
				return nil, fmt.Errorf("unmarshal message part: %w", err)
			}
			pb.Parts = append(pb.Parts, p)
		}
	}
	return pb, nil
}

// ArtifactToProto converts a DB artifact to proto.
// convName is the full conversation resource name.
func ArtifactToProto(row db.AiArtifact, convName string) *aiv1.Artifact {
	artName := fmt.Sprintf("%s/artifacts/%s", convName, row.Name)
	pb := &aiv1.Artifact{
		Name:        artName,
		Type:        row.Type,
		Title:       row.Title,
		Description: row.Description,
		CreateTime:  timestamppb.New(row.CreateTime),
		UpdateTime:  timestamppb.New(row.UpdateTime),
	}
	// LatestVersionID exists on `row` but the version's resource name
	// requires a separate lookup. Caller populates `pb.LatestVersion`
	// when it needs that field set.
	return pb
}

// ArtifactVersionToProtoAi converts a DB artifact version to proto.
// artName is the full artifact resource name.
func ArtifactVersionToProtoAi(row db.AiArtifactVersion, artName string) *aiv1.ArtifactVersion {
	pb := &aiv1.ArtifactVersion{
		Name:       fmt.Sprintf("%s/versions/%s", artName, row.Name),
		CreateTime: timestamppb.New(row.CreateTime),
	}

	if row.InlineContentType.Valid {
		pb.Content = &aiv1.ArtifactVersion_Inline{
			Inline: &aiv1.InlineContent{
				Data:      row.InlineData,
				MimeType:  row.InlineContentType.String,
				SizeBytes: row.InlineSizeBytes.Int64,
			},
		}
	} else if row.AssetVersionName.Valid {
		pb.Content = &aiv1.ArtifactVersion_AssetVersion{
			AssetVersion: row.AssetVersionName.String,
		}
	}

	return pb
}

func roleToProto(role string) aiv1.Role {
	switch role {
	case "user":
		return aiv1.Role_USER
	case "assistant":
		return aiv1.Role_ASSISTANT
	case "system":
		return aiv1.Role_SYSTEM
	case "tool":
		return aiv1.Role_TOOL
	default:
		return aiv1.Role_ROLE_UNSPECIFIED
	}
}

// RoleToString converts a proto role to the DB string representation.
func RoleToString(role aiv1.Role) string {
	switch role {
	case aiv1.Role_USER:
		return "user"
	case aiv1.Role_ASSISTANT:
		return "assistant"
	case aiv1.Role_SYSTEM:
		return "system"
	case aiv1.Role_TOOL:
		return "tool"
	default:
		return ""
	}
}
