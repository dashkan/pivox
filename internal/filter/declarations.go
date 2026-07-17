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
	// Type is the CEL type of the column, used ONLY by the compound-cursor
	// keyset path (BuildListQuery) to know whether a page-token's encoded sort
	// value must be re-parsed into a time.Time (TypeTimestamp) before it is
	// bound as a parameter. nil is treated as TypeString. It does not affect
	// the simpler filter.Query / ParseOrderBy path.
	Type *expr.Type
}

// ResourceFilter holds per-resource metadata needed to translate AIP-132/160
// List RPC inputs into a pgx query. Filter and sort surfaces are declared
// independently: a field may be filterable only, sortable only, both, or
// neither. No implicit coupling.
type ResourceFilter struct {
	Filterable map[string]FilterableField
	Sortable   map[string]SortableField
	Table      string // SQL table name
	SoftDelete bool   // if true, adds "delete_time IS NULL"
	OrderBy    string // legacy default ORDER BY as raw SQL (e.g. "id ASC"), consumed ONLY by the filter.Query path; the compound-cursor path uses DefaultOrder instead

	// The engine's three per-resource "defaults" knobs, all consumed by the
	// compound-cursor path (PlanOrderBy / ClampPageSize / BuildListQuery). Each
	// is inert when unset, so a resource that declares none behaves exactly as
	// before the knobs existed.

	// DefaultOrder is the AIP-132 order applied when the client sends no
	// order_by (e.g. "id desc" for newest-first, or "createTime desc" for a
	// compound default). Parsed by planDefaultOrder; "id"/empty means the
	// id-only keyset with the declared direction, any other token must be a
	// registered Sortable field. Unset → historical id-ASC default.
	DefaultOrder string
	// DefaultPageSize is the page size used when the client omits page_size
	// (<= 0). Unset (0) → 100. Applied by ClampPageSize.
	DefaultPageSize int32
	// MaxPageSize caps the client-requested page_size. Unset (0) → 1000.
	// Applied by ClampPageSize.
	MaxPageSize int32
	// DefaultConditions are server-declared predicates ALWAYS ANDed into the
	// query (even with no client filter), reusing the same Predicate machinery
	// as ListQuery.Base — each must carry exactly one %s / one bound Arg.
	// Applied by BuildListQuery. Nil → none.
	DefaultConditions []Predicate

	CursorColumn    string   // column used for cursor pagination (e.g. "id")
	CursorDirection string   // "ASC" (default) or "DESC" — must match the direction of CursorColumn in OrderBy
	DefaultFields   []string // filter fields searched when bare literals have no field qualifier
	ParentColumn    string   // column name for parent filtering (default: "parent_id")
	UserColumn      string   // column name for implicit user-scoped access control (e.g. "created_by"); QueryParams.UserID is required when set
}

// SpaceFilter returns the filter config for spaces. Consumed by the
// compound-cursor keyset path (filter.BuildListQuery) in ListSpaces — the base
// scope (org_id = $) is supplied by the handler via ListQuery.Base, so
// ParentColumn is unused on that path (kept for documentation of the scope
// column). Every Sortable column below is NOT NULL in the init migration
// (name, display_name, create_time), which the compound-cursor row comparison
// requires: a nullable sort column would go UNKNOWN on NULLs and drop/duplicate
// rows across page boundaries, so such a column must be registered
// filterable-only, never Sortable. Spaces has no nullable sortable columns, so
// none are demoted.
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
			"displayName": {Column: "display_name", Type: filtering.TypeString},
			"name":        {Column: "name", Type: filtering.TypeString},
			// Type MUST be TypeTimestamp so DecodeCursor reparses the page-token
			// sort value back into a time.Time (RFC3339Nano round-trip).
			"createTime": {Column: "create_time", Type: filtering.TypeTimestamp},
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

// TagKeyFilter returns the filter config for tag keys. Consumed by the
// compound-cursor keyset path (filter.BuildListQuery) in ListTagKeys — the base
// scope (org_id = $) is supplied by the handler via ListQuery.Base. Every
// Sortable column is NOT NULL in the init migration (short_name, namespaced_name,
// create_time), which the compound-cursor row comparison requires (a nullable
// sort column would go UNKNOWN on NULLs and drop/duplicate rows across page
// boundaries). No nullable sortables here, so none are demoted. Type MUST be set
// on every sortable: TypeTimestamp on create_time so DecodeCursor reparses the
// page-token sort value into a time.Time; TypeString otherwise.
func TagKeyFilter() *ResourceFilter {
	return &ResourceFilter{
		Filterable: map[string]FilterableField{
			"shortName":      {Column: "short_name", Type: filtering.TypeString},
			"namespacedName": {Column: "namespaced_name", Type: filtering.TypeString},
			"createTime":     {Column: "create_time", Type: filtering.TypeTimestamp},
		},
		Sortable: map[string]SortableField{
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

// TagValueFilter returns the filter config for tag values. Consumed by the
// compound-cursor keyset path (filter.BuildListQuery) in ListTagValues — the base
// scope (tag_key_id = $) is supplied by the handler via ListQuery.Base. Every
// Sortable column is NOT NULL in the init migration (short_name, namespaced_name,
// create_time), so none are demoted. Type MUST be set on every sortable (see
// TagKeyFilter).
func TagValueFilter() *ResourceFilter {
	return &ResourceFilter{
		Filterable: map[string]FilterableField{
			"shortName":      {Column: "short_name", Type: filtering.TypeString},
			"namespacedName": {Column: "namespaced_name", Type: filtering.TypeString},
			"createTime":     {Column: "create_time", Type: filtering.TypeTimestamp},
		},
		Sortable: map[string]SortableField{
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

// TagBindingFilter returns the filter config for tag bindings. Consumed by the
// compound-cursor keyset path (filter.BuildListQuery) in ListTagBindings — the
// base scope (parent_resource = $) is supplied by the handler via ListQuery.Base.
// parent_resource is NOT NULL in the init migration, so it is a safe sortable.
// Note that within any single reachable list it is CONSTANT (it is also the base
// scope predicate), so an order_by=parentResource falls through to the id
// tiebreaker — which is precisely why the compound (parent_resource, id) cursor
// matters: the legacy ORDER BY had no tiebreaker and resumed on an id-only token.
// Type MUST be set on the sortable (TypeString here).
func TagBindingFilter() *ResourceFilter {
	return &ResourceFilter{
		Filterable: map[string]FilterableField{
			"parentResource": {Column: "parent_resource", Type: filtering.TypeString},
		},
		Sortable: map[string]SortableField{
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

// ConversationFilter returns the filter config for AI chat conversations.
// Access-controlled by (org_id, created_by) — conversations are private to
// their creator (`identities.id` post-Phase-7). ListConversations uses the
// compound-cursor keyset path (filter.BuildListQuery): the base scope
// (org_id = $ AND created_by = $) is supplied by the handler via
// ListQuery.Base, so ParentColumn/UserColumn/OrderBy/CursorColumn are inert on
// that path (no filter.Query consumer remains) — kept only to document the
// scope columns.
//
// DefaultOrder is "id desc" (newest-first): with no client order_by the list
// defaults to the id-only DESC keyset, restoring the pre-migration UX. Every
// Sortable column below is NOT NULL in the init migration (title, create_time),
// which the compound-cursor row comparison requires: a nullable sort column
// goes UNKNOWN on NULLs and drops/duplicates rows across page boundaries.
// `last_message_time` IS nullable, so it is registered filterable-only (DEMOTED
// from the legacy Sortable set) — a client that tries order_by=lastMessageTime
// now gets InvalidArgument from PlanOrderBy rather than a silently broken
// keyset. DefaultPageSize is 50 (the pre-migration default).
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
			"title": {Column: "title", Type: filtering.TypeString},
			// Type MUST be TypeTimestamp so DecodeCursor reparses the page-token
			// sort value back into a time.Time (RFC3339Nano round-trip).
			"createTime": {Column: "create_time", Type: filtering.TypeTimestamp},
			// lastMessageTime is DEMOTED to filterable-only: last_message_time is
			// nullable, so it cannot be a compound-cursor sort column.
		},
		Table:           "ai_conversations",
		SoftDelete:      false, // AI resources don't soft-delete
		DefaultOrder:    "id desc",
		DefaultPageSize: 50,
		OrderBy:         "id DESC",
		CursorColumn:    "id",
		CursorDirection: "DESC",
		DefaultFields:   []string{"title"},
		ParentColumn:    "org_id",
		UserColumn:      "created_by",
	}
}

// MessageFilter returns the filter config for AI chat messages. ListMessages
// uses the compound-cursor keyset path (filter.BuildListQuery): the base scope
// (conversation_id = $) is supplied by the handler via ListQuery.Base after it
// verifies the authenticated user owns that conversation (access control
// happens at the parent layer, not via UserColumn here), so
// ParentColumn/OrderBy/CursorColumn are inert on that path. DefaultOrder is
// "id desc" (newest-first). The lone Sortable column (create_time) is NOT NULL
// in the init migration, as the compound-cursor row comparison requires.
func MessageFilter() *ResourceFilter {
	return &ResourceFilter{
		Filterable: map[string]FilterableField{
			"role":       {Column: "role", Type: filtering.TypeString},
			"createTime": {Column: "create_time", Type: filtering.TypeTimestamp},
		},
		Sortable: map[string]SortableField{
			// Type MUST be TypeTimestamp so DecodeCursor reparses the page-token
			// sort value back into a time.Time (RFC3339Nano round-trip).
			"createTime": {Column: "create_time", Type: filtering.TypeTimestamp},
		},
		Table:           "ai_messages",
		DefaultOrder:    "id desc",
		OrderBy:         "id DESC",
		CursorColumn:    "id",
		CursorDirection: "DESC",
		ParentColumn:    "conversation_id",
	}
}

// ArtifactFilter returns the filter config for AI chat artifacts. ListArtifacts
// uses the compound-cursor keyset path (filter.BuildListQuery): the base scope
// (conversation_id = $) is supplied by the handler via ListQuery.Base, so
// ParentColumn/OrderBy/CursorColumn are inert on that path. DefaultOrder is
// "id desc" (newest-first). Both Sortable columns (title, create_time) are NOT
// NULL in the init migration, as the compound-cursor row comparison requires.
func ArtifactFilter() *ResourceFilter {
	return &ResourceFilter{
		Filterable: map[string]FilterableField{
			"title":      {Column: "title", Type: filtering.TypeString, AllowPartial: true},
			"type":       {Column: "type", Type: filtering.TypeString},
			"createTime": {Column: "create_time", Type: filtering.TypeTimestamp},
		},
		Sortable: map[string]SortableField{
			"title": {Column: "title", Type: filtering.TypeString},
			// Type MUST be TypeTimestamp so DecodeCursor reparses the page-token
			// sort value back into a time.Time (RFC3339Nano round-trip).
			"createTime": {Column: "create_time", Type: filtering.TypeTimestamp},
		},
		Table:           "ai_artifacts",
		DefaultOrder:    "id desc",
		OrderBy:         "id DESC",
		CursorColumn:    "id",
		CursorDirection: "DESC",
		DefaultFields:   []string{"title"},
		ParentColumn:    "conversation_id",
	}
}

// ArtifactVersionFilter returns the filter config for AI chat artifact versions.
// ListArtifactVersions uses the compound-cursor keyset path
// (filter.BuildListQuery): the base scope (artifact_id = $) is supplied by the
// handler via ListQuery.Base, so ParentColumn/OrderBy/CursorColumn are inert on
// that path. DefaultOrder is "id desc" (newest version first, matching sequence
// DESC under uuidv7). The lone Sortable column (create_time) is NOT NULL in the
// init migration, as the compound-cursor row comparison requires. DefaultPageSize
// is 50 and MaxPageSize 100 (the pre-migration page-size policy for versions).
func ArtifactVersionFilter() *ResourceFilter {
	return &ResourceFilter{
		Filterable: map[string]FilterableField{
			"createTime": {Column: "create_time", Type: filtering.TypeTimestamp},
		},
		Sortable: map[string]SortableField{
			// Type MUST be TypeTimestamp so DecodeCursor reparses the page-token
			// sort value back into a time.Time (RFC3339Nano round-trip).
			"createTime": {Column: "create_time", Type: filtering.TypeTimestamp},
		},
		Table:           "ai_artifact_versions",
		DefaultOrder:    "id desc",
		DefaultPageSize: 50,
		MaxPageSize:     100,
		OrderBy:         "id DESC",
		CursorColumn:    "id",
		CursorDirection: "DESC",
		ParentColumn:    "artifact_id",
	}
}

// ConnectorFilter returns the filter config for workflow Connectors.
//
// Connectors are an org+space *leveled* resource: a row lives directly under an
// org (space_id NULL) or under a space (space_id set). That two-column
// partition — `org_id = … AND space_id IS NOT DISTINCT FROM …` — is NOT
// expressible via ResourceFilter.ParentColumn (a single `col = $` predicate),
// so ListConnectors does NOT use filter.Query. It uses BuildListQuery with a
// handler-supplied base scope; this declaration supplies only the filterable +
// sortable surface (the transpiler + order_by whitelist). See
// docs/aip-list-transpiler-procedure.md.
func ConnectorFilter() *ResourceFilter {
	return &ResourceFilter{
		Filterable: map[string]FilterableField{
			"displayName": {Column: "display_name", Type: filtering.TypeString, AllowPartial: true},
			"description": {Column: "description", Type: filtering.TypeString, AllowPartial: true},
			"agent":       {Column: "agent", Type: filtering.TypeString},
			"createTime":  {Column: "create_time", Type: filtering.TypeTimestamp},
			"updateTime":  {Column: "update_time", Type: filtering.TypeTimestamp},
			"annotations": {Column: "annotations", Type: filtering.TypeMap(filtering.TypeString, filtering.TypeString), JSONB: true},
		},
		Sortable: map[string]SortableField{
			"displayName": {Column: "display_name", Type: filtering.TypeString},
			"createTime":  {Column: "create_time", Type: filtering.TypeTimestamp},
			"updateTime":  {Column: "update_time", Type: filtering.TypeTimestamp},
		},
		Table:         "connectors",
		SoftDelete:    false, // connectors hard-delete; no delete_time column
		OrderBy:       "id ASC",
		CursorColumn:  "id",
		DefaultFields: []string{"displayName"},
		// ParentColumn intentionally unset — the org+space base scope is applied
		// by the handler via BuildListQuery.Base, not by filter.Query.
	}
}

// RequestFilter returns the filter config for asset requests (the ListRequests
// RPC), on the compound-cursor keyset path (filter.BuildListQuery).
//
// A request is a flat, single-parent resource: every row lives under exactly
// one space (space_id NOT NULL). ListRequests supplies that scope as a
// BuildListQuery base predicate (space_id = $) rather than via ParentColumn, so
// this declaration provides only the filter/sort surface — matching the
// compound-cursor path connectors/spaces use. See
// docs/aip-list-transpiler-procedure.md.
//
// asset_requests has NO delete_time column (SoftDelete: false); the RPC's
// show_deleted flag is therefore inert. Every Sortable column is NOT NULL in the
// init migration (display_name, name, priority, create_time, update_time), which
// the compound-cursor row comparison requires — a nullable sort column would go
// UNKNOWN on NULLs and drop/duplicate rows across page boundaries. due_time is
// nullable, so although the proto advertises it as an order_by field it is
// registered filterable-only (a client ordering on it gets InvalidArgument, not
// a silently broken keyset).
func RequestFilter() *ResourceFilter {
	return &ResourceFilter{
		Filterable: map[string]FilterableField{
			"displayName": {Column: "display_name", Type: filtering.TypeString, AllowPartial: true},
			"state":       {Column: "state", Type: filtering.TypeString},
			"priority":    {Column: "priority", Type: filtering.TypeString},
			"assignee":    {Column: "assignee", Type: filtering.TypeString, AllowPartial: true},
			"createTime":  {Column: "create_time", Type: filtering.TypeTimestamp},
			"dueTime":     {Column: "due_time", Type: filtering.TypeTimestamp}, // nullable → filterable-only
		},
		Sortable: map[string]SortableField{
			"displayName": {Column: "display_name", Type: filtering.TypeString},
			"name":        {Column: "name", Type: filtering.TypeString},
			"priority":    {Column: "priority", Type: filtering.TypeString}, // enum text label round-trips
			// Type MUST be TypeTimestamp so DecodeCursor reparses the page-token
			// sort value back into a time.Time (RFC3339Nano round-trip).
			"createTime": {Column: "create_time", Type: filtering.TypeTimestamp},
			"updateTime": {Column: "update_time", Type: filtering.TypeTimestamp},
		},
		Table:         "asset_requests",
		SoftDelete:    false, // asset_requests has no delete_time column
		OrderBy:       "id ASC",
		CursorColumn:  "id",
		DefaultFields: []string{"displayName"},
		// ParentColumn intentionally unset — the space base scope is applied by
		// the handler via BuildListQuery.Base.
	}
}

// AssetFilter returns the filter config for assets over the PLAIN assets table
// (the ListAssets RPC), on the compound-cursor keyset path.
//
// This is deliberately independent of the dashboards service, which joins
// latest-version + endpoint columns via its own queries (ListAssetsBySpace /
// ListAssetsByOrg) and does NOT go through this declaration — ListAssets lists a
// single space's assets (space_id NOT NULL, supplied as a BuildListQuery base
// predicate).
//
// SoftDelete is true (assets carry delete_time); the RPC's show_deleted flag
// maps to ListQuery.ShowDeleted. Every Sortable column is NOT NULL in the init
// migration (display_name, name, size_bytes, create_time, update_time). size_bytes
// is a BIGINT: its Type is filtering.TypeInt so DecodeCursor reparses the token
// value into an int64 (a string bound against the bigint column would fail pgx
// encoding). expire_time is nullable, so it is registered filterable-only (the
// proto lists it only as a filter field, never an order_by).
func AssetFilter() *ResourceFilter {
	return &ResourceFilter{
		Filterable: map[string]FilterableField{
			"displayName": {Column: "display_name", Type: filtering.TypeString, AllowPartial: true},
			"state":       {Column: "state", Type: filtering.TypeString},
			"mediaType":   {Column: "media_type", Type: filtering.TypeString},
			"mimeType":    {Column: "content_type", Type: filtering.TypeString},
			"path":        {Column: "import_path", Type: filtering.TypeString, AllowPartial: true},
			"createTime":  {Column: "create_time", Type: filtering.TypeTimestamp},
			"expireTime":  {Column: "expire_time", Type: filtering.TypeTimestamp}, // nullable → filterable-only
		},
		Sortable: map[string]SortableField{
			"displayName": {Column: "display_name", Type: filtering.TypeString},
			"name":        {Column: "name", Type: filtering.TypeString},
			// size_bytes is BIGINT; TypeInt tells DecodeCursor to reparse the
			// token's sort value into an int64 (see token.go) so the keyset row
			// comparison binds a bigint, not a text literal.
			"sizeBytes": {Column: "size_bytes", Type: filtering.TypeInt},
			// TypeTimestamp: DecodeCursor reparses the token value to time.Time.
			"createTime": {Column: "create_time", Type: filtering.TypeTimestamp},
			"updateTime": {Column: "update_time", Type: filtering.TypeTimestamp},
		},
		Table:         "assets",
		SoftDelete:    true,
		OrderBy:       "id ASC",
		CursorColumn:  "id",
		DefaultFields: []string{"displayName"},
		// ParentColumn intentionally unset — the space base scope is applied by
		// the handler via BuildListQuery.Base.
	}
}

// ApiKeyFilter returns the filter config for API keys. Consumed by the
// compound-cursor keyset path (filter.BuildListQuery) in ListKeys — the base
// scope (org_id = $) is supplied by the handler via ListQuery.Base, so
// ParentColumn is unused on that path (kept for documentation of the scope
// column). Both Sortable columns below are NOT NULL in the init migration
// (display_name TEXT NOT NULL, create_time TIMESTAMPTZ NOT NULL), which the
// compound-cursor row comparison requires: a nullable sort column would go
// UNKNOWN on NULLs and drop/duplicate rows across page boundaries, so such a
// column must be registered filterable-only, never Sortable. API keys has no
// nullable sortable columns, so none are demoted.
func ApiKeyFilter() *ResourceFilter {
	return &ResourceFilter{
		Filterable: map[string]FilterableField{
			"displayName": {Column: "display_name", Type: filtering.TypeString, AllowPartial: true},
			"createTime":  {Column: "create_time", Type: filtering.TypeTimestamp},
		},
		Sortable: map[string]SortableField{
			"displayName": {Column: "display_name", Type: filtering.TypeString},
			// Type MUST be TypeTimestamp so DecodeCursor reparses the page-token
			// sort value back into a time.Time (RFC3339Nano round-trip).
			"createTime": {Column: "create_time", Type: filtering.TypeTimestamp},
		},
		Table:         "api_keys",
		SoftDelete:    true,
		OrderBy:       "id ASC",
		CursorColumn:  "id",
		DefaultFields: []string{"displayName"},
		ParentColumn:  "org_id",
	}
}
