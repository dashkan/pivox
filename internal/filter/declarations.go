package filter

import (
	"go.einride.tech/aip/filtering"
	expr "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

// FilterableField maps an AIP-160 filter field name to its SQL column and
// the CEL type + modifiers that govern how filter expressions translate.
type FilterableField struct {
	Column       string     // SQL column name or JSONB expression
	Type         *expr.Type // filtering.TypeString, TypeTimestamp, etc.
	AllowPartial bool       // if true, "=" with wildcards becomes ILIKE
	JSONB        bool       // if true, use JSONB operators for has/traversal
}

// SortableField maps an AIP-132 order_by field name to its SQL column.
// Kept deliberately narrow — ordering has fewer semantic knobs than filtering.
type SortableField struct {
	Column string
}

// ResourceFilter holds per-resource metadata needed to translate AIP-132/160
// List RPC inputs into a pgx query. Filter and sort surfaces are declared
// independently: a field may be filterable only, sortable only, both, or
// neither. No implicit coupling.
type ResourceFilter struct {
	Filterable    map[string]FilterableField
	Sortable      map[string]SortableField
	Table         string   // SQL table name
	SoftDelete    bool     // if true, adds "delete_time IS NULL"
	OrderBy       string   // default ORDER BY (e.g. "id ASC")
	CursorColumn  string   // column used for cursor pagination (e.g. "id")
	DefaultFields []string // filter fields searched when bare literals have no field qualifier
	ParentColumn  string   // column name for parent filtering (default: "parent_id")
}

// ProjectFilter returns the filter config for projects.
func ProjectFilter() *ResourceFilter {
	return &ResourceFilter{
		Filterable: map[string]FilterableField{
			"displayName": {Column: "display_name", Type: filtering.TypeString, AllowPartial: true},
			"state":       {Column: "state", Type: filtering.TypeString},
			"name":        {Column: "name", Type: filtering.TypeString},
			"labels":      {Column: "labels", Type: filtering.TypeMap(filtering.TypeString, filtering.TypeString), JSONB: true},
			"createTime":  {Column: "create_time", Type: filtering.TypeTimestamp},
		},
		Sortable: map[string]SortableField{
			"displayName": {Column: "display_name"},
			"name":        {Column: "name"},
			"createTime":  {Column: "create_time"},
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
		Filterable: map[string]FilterableField{
			"displayName": {Column: "display_name", Type: filtering.TypeString, AllowPartial: true},
			"name":        {Column: "name", Type: filtering.TypeString},
			"state":       {Column: "state", Type: filtering.TypeString},
			"createTime":  {Column: "create_time", Type: filtering.TypeTimestamp},
		},
		Sortable: map[string]SortableField{
			"displayName": {Column: "display_name"},
			"name":        {Column: "name"},
			"createTime":  {Column: "create_time"},
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
		Filterable: map[string]FilterableField{
			"shortName":      {Column: "short_name", Type: filtering.TypeString},
			"namespacedName": {Column: "namespaced_name", Type: filtering.TypeString},
			"createTime":     {Column: "create_time", Type: filtering.TypeTimestamp},
		},
		Sortable: map[string]SortableField{
			"shortName":      {Column: "short_name"},
			"namespacedName": {Column: "namespaced_name"},
			"createTime":     {Column: "create_time"},
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
		Filterable: map[string]FilterableField{
			"shortName":      {Column: "short_name", Type: filtering.TypeString},
			"namespacedName": {Column: "namespaced_name", Type: filtering.TypeString},
			"createTime":     {Column: "create_time", Type: filtering.TypeTimestamp},
		},
		Sortable: map[string]SortableField{
			"shortName":      {Column: "short_name"},
			"namespacedName": {Column: "namespaced_name"},
			"createTime":     {Column: "create_time"},
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
		Filterable: map[string]FilterableField{
			"parentResource": {Column: "parent_resource", Type: filtering.TypeString},
		},
		Sortable: map[string]SortableField{
			"parentResource": {Column: "parent_resource"},
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
		Filterable: map[string]FilterableField{
			"displayName": {Column: "display_name", Type: filtering.TypeString, AllowPartial: true},
			"createTime":  {Column: "create_time", Type: filtering.TypeTimestamp},
		},
		Sortable: map[string]SortableField{
			"displayName": {Column: "display_name"},
			"createTime":  {Column: "create_time"},
		},
		Table:         "api_keys",
		SoftDelete:    true,
		OrderBy:       "id ASC",
		CursorColumn:  "id",
		DefaultFields: []string{"displayName"},
		ParentColumn:  "org_id",
	}
}
