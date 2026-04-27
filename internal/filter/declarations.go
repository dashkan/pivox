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
	Filterable      map[string]FilterableField
	Sortable        map[string]SortableField
	Table           string   // SQL table name
	SoftDelete      bool     // if true, adds "delete_time IS NULL"
	OrderBy         string   // default ORDER BY (e.g. "id ASC")
	CursorColumn    string   // column used for cursor pagination (e.g. "id")
	CursorDirection string   // "ASC" (default) or "DESC" — must match the direction of CursorColumn in OrderBy
	DefaultFields   []string // filter fields searched when bare literals have no field qualifier
	ParentColumn    string   // column name for parent filtering (default: "parent_id")
	UserColumn      string   // column name for implicit user-scoped access control (e.g. "created_by"); QueryParams.UserID is required when set
}

// SpaceFilter returns the filter config for spaces.
func SpaceFilter() *ResourceFilter {
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
		Table:         "spaces",
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

// ConversationFilter returns the filter config for AI chat conversations.
// Access-controlled by (org_id, created_by) — conversations are private to
// their creator.
func ConversationFilter() *ResourceFilter {
	return &ResourceFilter{
		Filterable: map[string]FilterableField{
			"title":           {Column: "title", Type: filtering.TypeString, AllowPartial: true},
			"archived":        {Column: "archived", Type: filtering.TypeBool},
			"pinned":          {Column: "pinned", Type: filtering.TypeBool},
			"createTime":      {Column: "create_time", Type: filtering.TypeTimestamp},
			"lastMessageTime": {Column: "last_message_time", Type: filtering.TypeTimestamp},
		},
		Sortable: map[string]SortableField{
			"title":           {Column: "title"},
			"createTime":      {Column: "create_time"},
			"lastMessageTime": {Column: "last_message_time"},
		},
		Table:           "ai_conversations",
		SoftDelete:      false, // AI resources don't soft-delete
		OrderBy:         "id DESC",
		CursorColumn:    "id",
		CursorDirection: "DESC",
		DefaultFields:   []string{"title"},
		ParentColumn:    "org_id",
		UserColumn:      "created_by",
	}
}

// MessageFilter returns the filter config for AI chat messages. Scoped to a
// parent conversation; the handler must verify the authenticated user owns
// that conversation before calling filter.Query (access control happens at
// the parent layer, not via UserColumn here).
func MessageFilter() *ResourceFilter {
	return &ResourceFilter{
		Filterable: map[string]FilterableField{
			"role":       {Column: "role", Type: filtering.TypeString},
			"createTime": {Column: "create_time", Type: filtering.TypeTimestamp},
		},
		Sortable: map[string]SortableField{
			"createTime": {Column: "create_time"},
		},
		Table:           "ai_messages",
		OrderBy:         "id DESC",
		CursorColumn:    "id",
		CursorDirection: "DESC",
		ParentColumn:    "conversation_id",
	}
}

// ArtifactFilter returns the filter config for AI chat artifacts.
func ArtifactFilter() *ResourceFilter {
	return &ResourceFilter{
		Filterable: map[string]FilterableField{
			"title":      {Column: "title", Type: filtering.TypeString, AllowPartial: true},
			"type":       {Column: "type", Type: filtering.TypeString},
			"createTime": {Column: "create_time", Type: filtering.TypeTimestamp},
		},
		Sortable: map[string]SortableField{
			"title":      {Column: "title"},
			"createTime": {Column: "create_time"},
		},
		Table:           "ai_artifacts",
		OrderBy:         "id DESC",
		CursorColumn:    "id",
		CursorDirection: "DESC",
		DefaultFields:   []string{"title"},
		ParentColumn:    "conversation_id",
	}
}

// ArtifactVersionFilter returns the filter config for AI chat artifact versions.
// Sorted newest version first (id DESC, which matches sequence DESC under
// uuidv7).
func ArtifactVersionFilter() *ResourceFilter {
	return &ResourceFilter{
		Filterable: map[string]FilterableField{
			"createTime": {Column: "create_time", Type: filtering.TypeTimestamp},
		},
		Sortable: map[string]SortableField{
			"createTime": {Column: "create_time"},
		},
		Table:           "ai_artifact_versions",
		OrderBy:         "id DESC",
		CursorColumn:    "id",
		CursorDirection: "DESC",
		ParentColumn:    "artifact_id",
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
