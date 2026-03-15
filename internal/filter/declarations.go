package filter

import (
	"go.einride.tech/aip/filtering"
	expr "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

// FieldMapping maps an AIP-160 field name to a database column.
type FieldMapping struct {
	Column       string     // SQL column name or JSONB expression
	Type         *expr.Type // filtering.TypeString, TypeTimestamp, etc.
	AllowPartial bool       // if true, "=" with wildcards becomes ILIKE
	JSONB        bool       // if true, use JSONB operators for has/traversal
}

// ResourceFilter holds column mappings and query metadata for a resource.
type ResourceFilter struct {
	Fields        map[string]FieldMapping
	Table         string   // SQL table name
	SoftDelete    bool     // if true, adds "delete_time IS NULL"
	OrderBy       string   // e.g. "id ASC"
	CursorColumn  string   // column used for cursor pagination (e.g. "id")
	DefaultFields []string // fields searched when bare literals have no field qualifier
	ParentColumn  string   // column name for parent filtering (default: "parent_id")
}

// ProjectFilter returns the filter config for projects.
func ProjectFilter() *ResourceFilter {
	return &ResourceFilter{
		Fields: map[string]FieldMapping{
			"displayName": {Column: "display_name", Type: filtering.TypeString, AllowPartial: true},
			"state":       {Column: "state", Type: filtering.TypeString},
			"name":        {Column: "name", Type: filtering.TypeString},
			"labels":      {Column: "labels", Type: filtering.TypeMap(filtering.TypeString, filtering.TypeString), JSONB: true},
			"createTime":  {Column: "create_time", Type: filtering.TypeTimestamp},
		},
		Table:         "projects",
		SoftDelete:    true,
		OrderBy:       "id ASC",
		CursorColumn:  "id",
		DefaultFields: []string{"displayName"},
		ParentColumn:  "org_id",
	}
}

// OrganizationFilter returns the filter config for organizations.
func OrganizationFilter() *ResourceFilter {
	return &ResourceFilter{
		Fields: map[string]FieldMapping{
			"displayName": {Column: "display_name", Type: filtering.TypeString, AllowPartial: true},
			"name":        {Column: "name", Type: filtering.TypeString},
			"state":       {Column: "state", Type: filtering.TypeString},
			"createTime":  {Column: "create_time", Type: filtering.TypeTimestamp},
		},
		Table:         "organizations",
		SoftDelete:    true,
		OrderBy:       "id ASC",
		CursorColumn:  "id",
		DefaultFields: []string{"displayName"},
	}
}

// TagKeyFilter returns the filter config for tag keys.
func TagKeyFilter() *ResourceFilter {
	return &ResourceFilter{
		Fields: map[string]FieldMapping{
			"shortName":      {Column: "short_name", Type: filtering.TypeString},
			"namespacedName": {Column: "namespaced_name", Type: filtering.TypeString},
			"createTime":     {Column: "create_time", Type: filtering.TypeTimestamp},
		},
		Table:         "tag_keys",
		SoftDelete:    false,
		OrderBy:       "id ASC",
		CursorColumn:  "id",
		DefaultFields: []string{"shortName"},
		ParentColumn:  "org_id",
	}
}

// TagValueFilter returns the filter config for tag values.
func TagValueFilter() *ResourceFilter {
	return &ResourceFilter{
		Fields: map[string]FieldMapping{
			"shortName":      {Column: "short_name", Type: filtering.TypeString},
			"namespacedName": {Column: "namespaced_name", Type: filtering.TypeString},
			"createTime":     {Column: "create_time", Type: filtering.TypeTimestamp},
		},
		Table:         "tag_values",
		SoftDelete:    false,
		OrderBy:       "id ASC",
		CursorColumn:  "id",
		DefaultFields: []string{"shortName"},
		ParentColumn:  "tag_key_id",
	}
}

// TagBindingFilter returns the filter config for tag bindings.
func TagBindingFilter() *ResourceFilter {
	return &ResourceFilter{
		Fields: map[string]FieldMapping{
			"parentResource": {Column: "parent_resource", Type: filtering.TypeString},
		},
		Table:         "tag_bindings",
		SoftDelete:    false,
		OrderBy:       "id ASC",
		CursorColumn:  "id",
		DefaultFields: []string{"parentResource"},
		ParentColumn:  "parent_resource",
	}
}

// ApiKeyFilter returns the filter config for API keys.
func ApiKeyFilter() *ResourceFilter {
	return &ResourceFilter{
		Fields: map[string]FieldMapping{
			"displayName": {Column: "display_name", Type: filtering.TypeString, AllowPartial: true},
			"createTime":  {Column: "create_time", Type: filtering.TypeTimestamp},
		},
		Table:         "api_keys",
		SoftDelete:    true,
		OrderBy:       "id ASC",
		CursorColumn:  "id",
		DefaultFields: []string{"displayName"},
		ParentColumn:  "org_id",
	}
}
