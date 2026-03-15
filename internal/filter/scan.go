package filter

import (
	"github.com/jackc/pgx/v5"

	db "github.com/pivoxai/pivox/internal/db/generated"
)

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
			&tb.Annotations,
			&tb.Etag,
			&tb.CreatedBy,
			&tb.CreateTime,
			&tb.UpdateTime,
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
