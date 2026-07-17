package filter

import (
	"github.com/jackc/pgx/v5"

	db "github.com/dashkan/pivox/internal/db/generated"
)

// ScanMessages scans rows into db.AiMessage structs.
func ScanMessages(rows pgx.Rows) ([]db.AiMessage, error) {
	defer rows.Close()
	var results []db.AiMessage
	for rows.Next() {
		var m db.AiMessage
		if err := rows.Scan(
			&m.ID,
			&m.ConversationID,
			&m.Name,
			&m.Role,
			&m.Parts,
			&m.Sequence,
			&m.TokenCount,
			&m.CreateTime,
		); err != nil {
			return nil, err
		}
		results = append(results, m)
	}
	return results, rows.Err()
}

// ScanArtifacts scans rows into db.AiArtifact structs.
func ScanArtifacts(rows pgx.Rows) ([]db.AiArtifact, error) {
	defer rows.Close()
	var results []db.AiArtifact
	for rows.Next() {
		var a db.AiArtifact
		if err := rows.Scan(
			&a.ID,
			&a.ConversationID,
			&a.Name,
			&a.Type,
			&a.Title,
			&a.Description,
			&a.LatestVersionID,
			&a.CreatedBy,
			&a.UpdatedBy,
			&a.CreateTime,
			&a.UpdateTime,
		); err != nil {
			return nil, err
		}
		results = append(results, a)
	}
	return results, rows.Err()
}

// ScanArtifactVersions scans rows into db.AiArtifactVersion structs.
func ScanArtifactVersions(rows pgx.Rows) ([]db.AiArtifactVersion, error) {
	defer rows.Close()
	var results []db.AiArtifactVersion
	for rows.Next() {
		var v db.AiArtifactVersion
		if err := rows.Scan(
			&v.ID,
			&v.ArtifactID,
			&v.Name,
			&v.InlineData,
			&v.InlineContentType,
			&v.InlineSizeBytes,
			&v.AssetVersionName,
			&v.Sequence,
			&v.CreatedBy,
			&v.CreateTime,
		); err != nil {
			return nil, err
		}
		results = append(results, v)
	}
	return results, rows.Err()
}

// ScanConversations scans rows into db.AiConversation structs.
//
// The destination order MUST match the column order in
// `ai_conversations` exactly — this scans `SELECT *` from the
// filter query, so adding a new column to the table without adding
// a destination here causes pgx to fail with a column-count
// mismatch and the gRPC call to abort.
func ScanConversations(rows pgx.Rows) ([]db.AiConversation, error) {
	defer rows.Close()
	var results []db.AiConversation
	for rows.Next() {
		var c db.AiConversation
		// Order MUST match the `ai_conversations` column order from the
		// init migration: id, org_id, name, title, title_user_set,
		// description, archived, pinned, message_count,
		// last_message_time, etag, revision, created_by, updated_by,
		// lock_holder, lock_expires_at, create_time, update_time.
		if err := rows.Scan(
			&c.ID,
			&c.OrgID,
			&c.Name,
			&c.Title,
			&c.TitleUserSet,
			&c.Description,
			&c.Archived,
			&c.Pinned,
			&c.MessageCount,
			&c.LastMessageTime,
			&c.Etag,
			&c.Revision,
			&c.CreatedBy,
			&c.UpdatedBy,
			&c.LockHolder,
			&c.LockExpiresAt,
			&c.CreateTime,
			&c.UpdateTime,
		); err != nil {
			return nil, err
		}
		results = append(results, c)
	}
	return results, rows.Err()
}

// ScanSpaces scans rows into db.Space structs.
func ScanSpaces(rows pgx.Rows) ([]db.Space, error) {
	defer rows.Close()
	var results []db.Space
	for rows.Next() {
		var p db.Space
		if err := rows.Scan(
			&p.ID,
			&p.OrgID,
			&p.Name,
			&p.DisplayName,
			&p.Labels,
			&p.State,
			&p.Etag,
			&p.Revision,
			&p.CreatedBy,
			&p.UpdatedBy,
			&p.DeletedBy,
			&p.CreateTime,
			&p.UpdateTime,
			&p.DeleteTime,
			&p.PurgeTime,
		); err != nil {
			return nil, err
		}
		results = append(results, p)
	}
	return results, rows.Err()
}

// ScanOrganizations scans rows into db.Organization structs.
func ScanOrganizations(rows pgx.Rows) ([]db.Organization, error) {
	defer rows.Close()
	var results []db.Organization
	for rows.Next() {
		var o db.Organization
		if err := rows.Scan(
			&o.ID,
			&o.Name,
			&o.DisplayName,
			&o.Annotations,
			&o.State,
			&o.Etag,
			&o.Revision,
			&o.CreatedBy,
			&o.UpdatedBy,
			&o.DeletedBy,
			&o.CreateTime,
			&o.UpdateTime,
			&o.DeleteTime,
			&o.PurgeTime,
		); err != nil {
			return nil, err
		}
		results = append(results, o)
	}
	return results, rows.Err()
}

// ScanTagKeys scans rows into db.TagKey structs.
func ScanTagKeys(rows pgx.Rows) ([]db.TagKey, error) {
	defer rows.Close()
	var results []db.TagKey
	for rows.Next() {
		var tk db.TagKey
		if err := rows.Scan(
			&tk.ID,
			&tk.OrgID,
			&tk.ShortName,
			&tk.NamespacedName,
			&tk.Description,
			&tk.Annotations,
			&tk.Etag,
			&tk.Revision,
			&tk.CreatedBy,
			&tk.UpdatedBy,
			&tk.CreateTime,
			&tk.UpdateTime,
		); err != nil {
			return nil, err
		}
		results = append(results, tk)
	}
	return results, rows.Err()
}

// ScanTagValues scans rows into db.TagValue structs.
func ScanTagValues(rows pgx.Rows) ([]db.TagValue, error) {
	defer rows.Close()
	var results []db.TagValue
	for rows.Next() {
		var tv db.TagValue
		if err := rows.Scan(
			&tv.ID,
			&tv.TagKeyID,
			&tv.ShortName,
			&tv.NamespacedName,
			&tv.Description,
			&tv.Annotations,
			&tv.Etag,
			&tv.Revision,
			&tv.CreatedBy,
			&tv.UpdatedBy,
			&tv.CreateTime,
			&tv.UpdateTime,
		); err != nil {
			return nil, err
		}
		results = append(results, tv)
	}
	return results, rows.Err()
}

// ScanTagBindings scans rows into db.TagBinding structs.
func ScanTagBindings(rows pgx.Rows) ([]db.TagBinding, error) {
	defer rows.Close()
	var results []db.TagBinding
	for rows.Next() {
		var tb db.TagBinding
		if err := rows.Scan(
			&tb.ID,
			&tb.ParentResource,
			&tb.TagValueID,
			&tb.Origin,
			&tb.Annotations,
			&tb.Etag,
			&tb.CreatedBy,
			&tb.CreateTime,
		); err != nil {
			return nil, err
		}
		results = append(results, tb)
	}
	return results, rows.Err()
}

// ScanConnectors scans rows into db.Connector structs.
//
// The destination order MUST match the `connectors` column order from the init
// migration exactly — this scans `SELECT *` from BuildListQuery, so adding a
// column to the table without adding a destination here fails with a pgx
// column-count mismatch that aborts the RPC.
func ScanConnectors(rows pgx.Rows) ([]db.Connector, error) {
	defer rows.Close()
	var results []db.Connector
	for rows.Next() {
		var c db.Connector
		// Order MUST match `connectors`: id, org_id, space_id, slug,
		// display_name, description, config, agent, annotations, etag,
		// created_by, updated_by, create_time, update_time.
		if err := rows.Scan(
			&c.ID,
			&c.OrgID,
			&c.SpaceID,
			&c.Slug,
			&c.DisplayName,
			&c.Description,
			&c.Config,
			&c.Agent,
			&c.Annotations,
			&c.Etag,
			&c.CreatedBy,
			&c.UpdatedBy,
			&c.CreateTime,
			&c.UpdateTime,
		); err != nil {
			return nil, err
		}
		results = append(results, c)
	}
	return results, rows.Err()
}

// ScanRequests scans rows into db.AssetRequest structs.
//
// The destination order MUST match the `asset_requests` column order from the
// init migration exactly — this scans `SELECT *` from BuildListQuery, so adding
// a column to the table without adding a destination here fails with a pgx
// column-count mismatch that aborts the RPC.
func ScanRequests(rows pgx.Rows) ([]db.AssetRequest, error) {
	defer rows.Close()
	var results []db.AssetRequest
	for rows.Next() {
		var r db.AssetRequest
		// Order MUST match `asset_requests`: id, space_id, name, display_name,
		// description, priority, assignee, annotations, state, etag, revision,
		// created_by, updated_by, create_time, update_time, due_time,
		// delivered_time, approved_time.
		if err := rows.Scan(
			&r.ID,
			&r.SpaceID,
			&r.Name,
			&r.DisplayName,
			&r.Description,
			&r.Priority,
			&r.Assignee,
			&r.Annotations,
			&r.State,
			&r.Etag,
			&r.Revision,
			&r.CreatedBy,
			&r.UpdatedBy,
			&r.CreateTime,
			&r.UpdateTime,
			&r.DueTime,
			&r.DeliveredTime,
			&r.ApprovedTime,
		); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// ScanAssets scans rows into db.Asset structs.
//
// The destination order MUST match the `assets` column order from the init
// migration exactly — this scans `SELECT *` from BuildListQuery, so adding a
// column to the table without adding a destination here fails with a pgx
// column-count mismatch that aborts the RPC.
func ScanAssets(rows pgx.Rows) ([]db.Asset, error) {
	defer rows.Close()
	var results []db.Asset
	for rows.Next() {
		var a db.Asset
		// Order MUST match `assets`: id, space_id, endpoint_id, name,
		// display_name, import_path, filename, media_type, content_type,
		// checksum_sha256, size_bytes, technical_metadata, ai_description,
		// transcription, duration_seconds, width, height, annotations,
		// search_vector, embedding, state, etag, revision, created_by,
		// updated_by, deleted_by, create_time, update_time, delete_time,
		// purge_time, expire_time.
		if err := rows.Scan(
			&a.ID,
			&a.SpaceID,
			&a.EndpointID,
			&a.Name,
			&a.DisplayName,
			&a.ImportPath,
			&a.Filename,
			&a.MediaType,
			&a.ContentType,
			&a.ChecksumSha256,
			&a.SizeBytes,
			&a.TechnicalMetadata,
			&a.AiDescription,
			&a.Transcription,
			&a.DurationSeconds,
			&a.Width,
			&a.Height,
			&a.Annotations,
			&a.SearchVector,
			&a.Embedding,
			&a.State,
			&a.Etag,
			&a.Revision,
			&a.CreatedBy,
			&a.UpdatedBy,
			&a.DeletedBy,
			&a.CreateTime,
			&a.UpdateTime,
			&a.DeleteTime,
			&a.PurgeTime,
			&a.ExpireTime,
		); err != nil {
			return nil, err
		}
		results = append(results, a)
	}
	return results, rows.Err()
}

// ScanApiKeys scans rows into db.ApiKey structs.
func ScanApiKeys(rows pgx.Rows) ([]db.ApiKey, error) {
	defer rows.Close()
	var results []db.ApiKey
	for rows.Next() {
		var k db.ApiKey
		if err := rows.Scan(
			&k.ID,
			&k.OrgID,
			&k.KeyID,
			&k.KeyString,
			&k.DisplayName,
			&k.Annotations,
			&k.Restrictions,
			&k.Etag,
			&k.Revision,
			&k.CreatedBy,
			&k.UpdatedBy,
			&k.DeletedBy,
			&k.CreateTime,
			&k.UpdateTime,
			&k.DeleteTime,
			&k.PurgeTime,
		); err != nil {
			return nil, err
		}
		results = append(results, k)
	}
	return results, rows.Err()
}
