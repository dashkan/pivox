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
		if err := rows.Scan(
			&c.ID,
			&c.OrgID,
			&c.Name,
			&c.Title,
			&c.Description,
			&c.Archived,
			&c.Pinned,
			&c.MessageCount,
			&c.LastMessageTime,
			&c.Etag,
			&c.Revision,
			&c.CreatedBy,
			&c.UpdatedBy,
			&c.CreateTime,
			&c.UpdateTime,
			&c.TitleUserSet,
		); err != nil {
			return nil, err
		}
		results = append(results, c)
	}
	return results, rows.Err()
}

// ScanProjects scans rows into db.Project structs.
func ScanProjects(rows pgx.Rows) ([]db.Project, error) {
	defer rows.Close()
	var results []db.Project
	for rows.Next() {
		var p db.Project
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
			&o.TenantID,
			&o.OwnerID,
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
