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
	// Type is the CEL type of the column, used by the compound-cursor keyset
	// path (BuildListQuery) to know whether a page-token's encoded sort value
	// must be re-parsed into a time.Time (TypeTimestamp) before it is bound as a
	// parameter. nil is treated as TypeString.
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

	// DefaultFields are the filter fields searched when a bare literal in an
	// AIP-160 filter has no field qualifier. Consumed by the transpiler.
	DefaultFields []string
}

// SpaceFilter returns the filter config for spaces. Consumed by the
// compound-cursor keyset path (filter.BuildListQuery) in ListSpaces — the base
// scope (org_id = $) is supplied by the handler via ListQuery.Base. Every
// Sortable column below is NOT NULL in the init migration
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
		DefaultFields: []string{"displayName"},
	}
}

// OrganizationFilter returns the filter config for organizations,
// consumed by the compound-cursor keyset path (filter.BuildListQuery) in
// ListOrganizations. The base scope ("orgs the caller is a member of")
// is supplied by the handler via ListQuery.Base — never baked in here.
//
// Every Sortable column is NOT NULL in the init migration (name UNIQUE
// NOT NULL, display_name NOT NULL DEFAULT ”, create_time NOT NULL),
// which the compound-cursor row comparison requires. DefaultOrder is
// "name" (name-ascending), matching the proto's documented default. Type
// MUST be TypeTimestamp on create_time so DecodeCursor reparses the
// page-token sort value back into a time.Time.
func OrganizationFilter() *ResourceFilter {
	return &ResourceFilter{
		Filterable: map[string]FilterableField{
			"displayName": {Column: "display_name", Type: filtering.TypeString, AllowPartial: true},
			"name":        {Column: "name", Type: filtering.TypeString},
			"state":       {Column: "state", Type: filtering.TypeString},
			"createTime":  {Column: "create_time", Type: filtering.TypeTimestamp},
		},
		Sortable: map[string]SortableField{
			"displayName": {Column: "display_name", Type: filtering.TypeString},
			"name":        {Column: "name", Type: filtering.TypeString},
			"createTime":  {Column: "create_time", Type: filtering.TypeTimestamp},
		},
		Table:         "organizations",
		SoftDelete:    true,
		DefaultOrder:  "name",
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
		DefaultFields: []string{"shortName"},
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
		DefaultFields: []string{"shortName"},
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
		DefaultFields: []string{"parentResource"},
	}
}

// ConversationFilter returns the filter config for AI chat conversations.
// Access-controlled by (org_id, created_by) — conversations are private to
// their creator (`identities.id` post-Phase-7). ListConversations uses the
// compound-cursor keyset path (filter.BuildListQuery): the base scope
// (org_id = $ AND created_by = $) is supplied by the handler via
// ListQuery.Base.
//
// DefaultOrder is "lastMessageTime desc" (recent-activity-first): with no
// client order_by the list surfaces the conversations the user most recently
// interacted with, which is the chat-sidebar default. It resolves to the
// compound (last_message_time, id) keyset. Every Sortable column below is NOT
// NULL in the init migration (title, create_time, last_message_time), which the
// compound-cursor row comparison requires: a nullable sort column goes UNKNOWN
// on NULLs and drops/duplicates rows across page boundaries. `last_message_time`
// became NOT NULL (DEFAULT now(), bumped on every message) precisely so it can
// serve as this default keyset column. DefaultPageSize is 50 (the pre-migration
// default).
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
			// last_message_time is NOT NULL now, so it's a valid compound-cursor
			// sort column — and it's the default sort (recent-activity-first).
			"lastMessageTime": {Column: "last_message_time", Type: filtering.TypeTimestamp},
		},
		Table:           "ai_conversations",
		SoftDelete:      false, // AI resources don't soft-delete
		DefaultOrder:    "lastMessageTime desc",
		DefaultPageSize: 50,
		DefaultFields:   []string{"title"},
	}
}

// MessageFilter returns the filter config for AI chat messages. ListMessages
// uses the compound-cursor keyset path (filter.BuildListQuery): the base scope
// (conversation_id = $) is supplied by the handler via ListQuery.Base after it
// verifies the authenticated user owns that conversation (access control
// happens at the parent layer). DefaultOrder is
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
		Table:        "ai_messages",
		DefaultOrder: "id desc",
	}
}

// ArtifactFilter returns the filter config for AI chat artifacts. ListArtifacts
// uses the compound-cursor keyset path (filter.BuildListQuery): the base scope
// (conversation_id = $) is supplied by the handler via ListQuery.Base.
// DefaultOrder is "id desc" (newest-first). Both Sortable columns (title,
// create_time) are NOT NULL in the init migration, as the compound-cursor row
// comparison requires.
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
		Table:         "ai_artifacts",
		DefaultOrder:  "id desc",
		DefaultFields: []string{"title"},
	}
}

// ArtifactVersionFilter returns the filter config for AI chat artifact versions.
// ListArtifactVersions uses the compound-cursor keyset path
// (filter.BuildListQuery): the base scope (artifact_id = $) is supplied by the
// handler via ListQuery.Base. DefaultOrder is "id desc" (newest version first, matching sequence
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
	}
}

// ConnectorFilter returns the filter config for workflow Connectors.
//
// Connectors are an org+space *leveled* resource: a row lives directly under an
// org (space_id NULL) or under a space (space_id set). That two-column
// partition — `org_id = … AND space_id IS NOT DISTINCT FROM …` — is supplied by
// the handler as a BuildListQuery base scope; this declaration supplies only the
// filterable + sortable surface (the transpiler + order_by whitelist). See
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
		DefaultFields: []string{"displayName"},
		// The org+space base scope is applied by the handler via BuildListQuery.Base.
	}
}

// WorkflowFilter returns the filter config for workflow containers (the
// ListWorkflows RPC), on the compound-cursor keyset path (filter.BuildListQuery).
//
// Workflows are an org+space *leveled* resource, exactly like connectors: a row
// lives directly under an org (space_id NULL) or under a space (space_id set).
// ListWorkflows preserves the pre-migration scope semantics by supplying that
// partition as a single BuildListQuery base predicate
// (space_id IS NOT DISTINCT FROM $), so an org-level list returns only the
// org-direct workflows (space_id NULL) and a space-level list returns that
// space's — NOT the connectors-style rollup. This declaration supplies only the
// filterable + sortable surface.
//
// workflows hard-deletes (no delete_time column), so SoftDelete is false. Every
// Sortable column is NOT NULL in the init migration (display_name TEXT NOT NULL
// DEFAULT ”, create_time / update_time TIMESTAMPTZ NOT NULL), which the
// compound-cursor row comparison requires — a nullable sort column would go
// UNKNOWN on NULLs and drop/duplicate rows across page boundaries. See
// docs/aip-list-transpiler-procedure.md.
func WorkflowFilter() *ResourceFilter {
	return &ResourceFilter{
		Filterable: map[string]FilterableField{
			"displayName": {Column: "display_name", Type: filtering.TypeString, AllowPartial: true},
			"description": {Column: "description", Type: filtering.TypeString, AllowPartial: true},
			"enabled":     {Column: "enabled", Type: filtering.TypeBool},
			"createTime":  {Column: "create_time", Type: filtering.TypeTimestamp},
			"updateTime":  {Column: "update_time", Type: filtering.TypeTimestamp},
			"annotations": {Column: "annotations", Type: filtering.TypeMap(filtering.TypeString, filtering.TypeString), JSONB: true},
		},
		Sortable: map[string]SortableField{
			"displayName": {Column: "display_name", Type: filtering.TypeString},
			// Type MUST be TypeTimestamp so DecodeCursor reparses the page-token
			// sort value back into a time.Time (RFC3339Nano round-trip).
			"createTime": {Column: "create_time", Type: filtering.TypeTimestamp},
			"updateTime": {Column: "update_time", Type: filtering.TypeTimestamp},
		},
		Table:         "workflows",
		SoftDelete:    false, // workflows hard-delete; no delete_time column
		DefaultFields: []string{"displayName"},
		// The org+space base scope is applied by the handler via BuildListQuery.Base.
	}
}

// WorkflowRunFilter returns the filter config for workflow runs (the
// ListWorkflowRuns RPC), on the compound-cursor keyset path (filter.BuildListQuery).
//
// A run's base scope is one of three predicates the handler selects from the
// parent (a single workflow's workflow_id, or — for the AIP-159 workflows/-
// wildcard — a space_id or an org_id rollup); this declaration supplies only the
// filter/sort surface.
//
// `state` is the only filterable field — it replaces the pre-migration bespoke
// `state = "VALUE"` parser, so `filter: "state = \"RUNNING\""` narrows via the
// generic AIP-160 transpiler (state is a TEXT column storing the enum label).
// NOTE: unlike the old parser, the generic engine does NOT validate the operand
// against the run-state enum, so an unknown state (e.g. state = "BOGUS") now
// yields an empty page rather than InvalidArgument — standard AIP-160 behavior.
//
// The lone Sortable column, create_time, is NOT NULL in the init migration, as
// the compound-cursor row comparison requires. With no DefaultOrder the list
// defaults to id ASC (creation order), preserving the pre-migration ordering.
func WorkflowRunFilter() *ResourceFilter {
	return &ResourceFilter{
		Filterable: map[string]FilterableField{
			"state": {Column: "state", Type: filtering.TypeString},
		},
		Sortable: map[string]SortableField{
			// Type MUST be TypeTimestamp so DecodeCursor reparses the page-token
			// sort value back into a time.Time (RFC3339Nano round-trip).
			"createTime": {Column: "create_time", Type: filtering.TypeTimestamp},
		},
		Table:      "workflow_runs",
		SoftDelete: false, // workflow_runs has no delete_time column
		// The per-parent base scope is applied by the handler via BuildListQuery.Base.
	}
}

// RequestFilter returns the filter config for asset requests (the ListRequests
// RPC), on the compound-cursor keyset path (filter.BuildListQuery).
//
// A request is a flat, single-parent resource: every row lives under exactly
// one space (space_id NOT NULL). ListRequests supplies that scope as a
// BuildListQuery base predicate (space_id = $), so this declaration provides
// only the filter/sort surface — matching the compound-cursor path
// connectors/spaces use. See docs/aip-list-transpiler-procedure.md.
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
		DefaultFields: []string{"displayName"},
		// The space base scope is applied by the handler via BuildListQuery.Base.
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
		DefaultFields: []string{"displayName"},
		// The space base scope is applied by the handler via BuildListQuery.Base.
	}
}

// SecretFilter returns the filter config for vault Secrets.
//
// Secrets are an org+space *leveled* resource, exactly like connectors: a row
// lives directly under an org (space_id NULL) or under a space (space_id set).
// That two-column partition is supplied by the handler as a BuildListQuery base
// scope; this declaration supplies only the filterable + sortable surface.
//
// Only `displayName` is filterable and only `displayName`/`createTime` are
// sortable — matching the proto's declared filter/order_by surface. The
// write-only `value` is never filterable/sortable, and `updateTime` is
// deliberately NOT sortable (the admin "Updated" column is display-only). Both
// Sortable columns are NOT NULL in the init migration (display_name TEXT NOT
// NULL DEFAULT ”, create_time TIMESTAMPTZ NOT NULL), which the compound-cursor
// row comparison requires. See docs/aip-list-transpiler-procedure.md.
func SecretFilter() *ResourceFilter {
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
		Table:         "secrets",
		SoftDelete:    false, // secrets hard-delete; no delete_time column
		DefaultFields: []string{"displayName"},
		// The org+space base scope is applied by the handler via BuildListQuery.Base.
	}
}

// DashboardFilter returns the filter config for space-scoped USER_MANAGED
// dashboards (the ListDashboards space branch), on the compound-cursor keyset
// path (filter.BuildListQuery).
//
// A dashboard is a flat, single-parent resource: every space-scoped row lives
// under exactly one space (space_id NOT NULL). ListDashboards supplies that
// scope as a BuildListQuery base predicate (space_id = $), so this declaration
// provides only the filter/sort surface — matching connectors/secrets. The
// org-level SYSTEM_MANAGED catalog is served from an in-memory registry and
// does NOT go through this declaration.
//
// The proto (dashboards.proto ListDashboardsRequest) advertises displayName +
// createTime as both filterable and sortable, with a default order of
// createTime descending — mirrored in DefaultOrder here. Both Sortable columns
// are NOT NULL in the init migration (display_name TEXT NOT NULL DEFAULT ”,
// create_time TIMESTAMPTZ NOT NULL), which the compound-cursor row comparison
// requires: a nullable sort column would go UNKNOWN on NULLs and drop/duplicate
// rows across page boundaries. dashboards carries delete_time (SoftDelete
// true), so soft-deleted rows are excluded by the engine.
func DashboardFilter() *ResourceFilter {
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
		Table:         "dashboards",
		SoftDelete:    true,
		DefaultOrder:  "createTime desc",
		DefaultFields: []string{"displayName"},
		// The space base scope is applied by the handler via BuildListQuery.Base.
	}
}

// StorageGatewayFilter returns the filter config for storage gateways. Consumed
// by the compound-cursor keyset path (filter.BuildListQuery) in
// ListStorageGateways — the base scope (org_id = $) is supplied by the handler
// via ListQuery.Base. The proto (storage_gateway.proto) advertises `state`,
// `displayName`, and `createTime` as filterable and `displayName`, `createTime`,
// `name` as order_by fields; only those appear below. `state` is the
// storage_gateway_state enum column, compared as text exactly as Space/Asset do.
// Every Sortable column is NOT NULL in the init migration (name, display_name,
// create_time), which the compound-cursor row comparison requires. DefaultOrder
// is "name" (the proto's documented default: name ascending). storage_gateways
// hard-deletes (no delete_time column), so SoftDelete is false.
func StorageGatewayFilter() *ResourceFilter {
	return &ResourceFilter{
		Filterable: map[string]FilterableField{
			"state":       {Column: "state", Type: filtering.TypeString},
			"displayName": {Column: "display_name", Type: filtering.TypeString, AllowPartial: true},
			"createTime":  {Column: "create_time", Type: filtering.TypeTimestamp},
		},
		Sortable: map[string]SortableField{
			"displayName": {Column: "display_name", Type: filtering.TypeString},
			// Type MUST be TypeTimestamp so DecodeCursor reparses the page-token
			// sort value back into a time.Time (RFC3339Nano round-trip).
			"createTime": {Column: "create_time", Type: filtering.TypeTimestamp},
			"name":       {Column: "name", Type: filtering.TypeString},
		},
		Table:         "storage_gateways",
		SoftDelete:    false, // storage_gateways hard-delete; no delete_time column
		DefaultOrder:  "name",
		DefaultFields: []string{"displayName"},
		// The org base scope is applied by the handler via BuildListQuery.Base.
	}
}

// EndpointFilter returns the filter config for storage endpoints. Consumed by
// the compound-cursor keyset path (filter.BuildListQuery) in ListEndpoints — the
// base scope (gateway_id = $) is supplied by the handler via ListQuery.Base. The
// proto (endpoint.proto) advertises `state` + `displayName` as filterable and
// `displayName`, `createTime`, `name` as order_by fields; only those appear
// below. `state` is the endpoint_state enum column, compared as text. Every
// Sortable column is NOT NULL in the init migration (display_name, create_time,
// name). DefaultOrder is "createTime" (the proto's documented default: createTime
// ascending). storage_endpoints hard-deletes (no delete_time column).
func EndpointFilter() *ResourceFilter {
	return &ResourceFilter{
		Filterable: map[string]FilterableField{
			"state":       {Column: "state", Type: filtering.TypeString},
			"displayName": {Column: "display_name", Type: filtering.TypeString, AllowPartial: true},
		},
		Sortable: map[string]SortableField{
			"displayName": {Column: "display_name", Type: filtering.TypeString},
			// Type MUST be TypeTimestamp so DecodeCursor reparses the page-token
			// sort value back into a time.Time (RFC3339Nano round-trip).
			"createTime": {Column: "create_time", Type: filtering.TypeTimestamp},
			"name":       {Column: "name", Type: filtering.TypeString},
		},
		Table:         "storage_endpoints",
		SoftDelete:    false, // storage_endpoints hard-delete; no delete_time column
		DefaultOrder:  "createTime",
		DefaultFields: []string{"displayName"},
		// The gateway base scope is applied by the handler via BuildListQuery.Base.
	}
}

// AgentFilter returns the filter config for storage agents. Consumed by the
// compound-cursor keyset path (filter.BuildListQuery) in ListAgents — the base
// scope (gateway_id = $) is supplied by the handler via ListQuery.Base. The
// proto (agent.proto) advertises `state`, `hostname`, `version` as filterable and
// `joinTime`, `lastSeenTime`, `hostname` as order_by fields; only those appear
// below. `state` is the agent_state enum column, compared as text. Every Sortable
// column is NOT NULL in the init migration (join_time, last_seen_time, hostname).
// DefaultOrder is "joinTime" (the proto's documented default: joinTime ascending).
// storage_agents has no delete_time column (agents self-register and are hard-
// removed), so SoftDelete is false. Agents carry no audit columns.
func AgentFilter() *ResourceFilter {
	return &ResourceFilter{
		Filterable: map[string]FilterableField{
			"state":    {Column: "state", Type: filtering.TypeString},
			"hostname": {Column: "hostname", Type: filtering.TypeString, AllowPartial: true},
			"version":  {Column: "version", Type: filtering.TypeString},
		},
		Sortable: map[string]SortableField{
			// Type MUST be TypeTimestamp so DecodeCursor reparses the page-token
			// sort value back into a time.Time (RFC3339Nano round-trip).
			"joinTime":     {Column: "join_time", Type: filtering.TypeTimestamp},
			"lastSeenTime": {Column: "last_seen_time", Type: filtering.TypeTimestamp},
			"hostname":     {Column: "hostname", Type: filtering.TypeString},
		},
		Table:         "storage_agents",
		SoftDelete:    false, // storage_agents has no delete_time column
		DefaultOrder:  "joinTime",
		DefaultFields: []string{"hostname"},
		// The gateway base scope is applied by the handler via BuildListQuery.Base.
	}
}

// OperationFilter returns the filter config for long-running operations (the
// AIP-151 ListOperations RPC), on the compound-cursor keyset path.
//
// Operations are unusual: their visibility is not a simple column-equality
// scope but a set-wise membership authorization (account-scoped ops the caller
// created, plus org/space ops the caller can read). ListOperations supplies
// that authorization as a BuildListQuery base predicate binding the caller;
// this declaration provides only the filter/sort surface.
//
// The standard google.longrunning.ListOperationsRequest declares no order_by
// and an opaque `filter` string, so the surface is intentionally minimal:
// `done` is the conventional AIP-151 operations filter (list pending vs
// completed). DefaultOrder is createTime descending (newest-first, matching the
// pre-migration ORDER BY); create_time is NOT NULL in the init migration, which
// the compound-cursor row comparison requires. operations has no delete_time
// column (SoftDelete false).
func OperationFilter() *ResourceFilter {
	return &ResourceFilter{
		Filterable: map[string]FilterableField{
			"done": {Column: "done", Type: filtering.TypeBool},
		},
		Sortable: map[string]SortableField{
			// Type MUST be TypeTimestamp so DecodeCursor reparses the page-token
			// sort value back into a time.Time (RFC3339Nano round-trip).
			"createTime": {Column: "create_time", Type: filtering.TypeTimestamp},
		},
		Table:        "operations",
		SoftDelete:   false, // operations has no delete_time column
		DefaultOrder: "createTime desc",
		// The caller-authorization base scope is applied by the handler via
		// BuildListQuery.Base.
	}
}

// ApiKeyFilter returns the filter config for API keys. Consumed by the
// compound-cursor keyset path (filter.BuildListQuery) in ListKeys — the base
// scope (org_id = $) is supplied by the handler via ListQuery.Base. Both
// Sortable columns below are NOT NULL in the init migration
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
		DefaultFields: []string{"displayName"},
	}
}

// OrgMemberFilter returns the filter config for org-scope IAM Members
// (the org ListMembers RPC), on the compound-cursor keyset path.
//
// Members are a JOINED resource: a role binding lives in `org_members`,
// but its wire shape carries the role's stable NAME (from `roles`), and
// its filter/order surface exposes the role's full RESOURCE NAME plus a
// virtual `principal_kind`. `BuildListQuery` emits `SELECT * FROM
// <Table>`, so Table is a server-controlled derived table that joins
// `roles` and exposes exactly the `db.ListOrgMembersRow` columns (om.* +
// role_name) — no extra columns, so `filter.ScanOrgMembers` matches it
// one-to-one. The filterable/virtual columns are computed inline as SQL
// expressions over that row (never user input):
//
//   - `role` — the full role resource name, reconstructed as
//     `organizations/<org slug>/roles/<role name>` via a correlated
//     subquery to `organizations` (the org slug is not a column of
//     org_members). Matches the proto's "exact match on a role resource
//     name" filter.
//   - `principal_kind` — `user` or `group`, from the user_id/group_id
//     XOR columns.
//
// Sortable exposes `role` (the role NAME slug — sorts identically to the
// resource name within one org, and is a real NOT-NULL column so the
// keyset row comparison is safe) and `createTime`. DefaultOrder is
// "createTime" (proto default: createTime ascending). DefaultPageSize/
// MaxPageSize (50/500) preserve the pre-migration members page policy.
// No bare-literal search surface (DefaultFields nil): the proto
// advertises only the `role` and `principal_kind` filter fields.
func OrgMemberFilter() *ResourceFilter {
	return &ResourceFilter{
		Filterable: map[string]FilterableField{
			"role":           {Column: "('organizations/' || (SELECT o.name FROM organizations o WHERE o.id = m.org_id) || '/roles/' || m.role_name)", Type: filtering.TypeString},
			"principal_kind": {Column: "(CASE WHEN m.user_id IS NOT NULL THEN 'user' ELSE 'group' END)", Type: filtering.TypeString},
		},
		Sortable: map[string]SortableField{
			"role":       {Column: "m.role_name", Type: filtering.TypeString},
			"createTime": {Column: "m.create_time", Type: filtering.TypeTimestamp},
		},
		Table:           "(SELECT om.*, r.name AS role_name FROM org_members om JOIN roles r ON r.id = om.role_id) AS m",
		SoftDelete:      false, // members hard-delete; no delete_time column
		DefaultOrder:    "createTime",
		DefaultPageSize: 50,
		MaxPageSize:     500,
		// The org base scope (org_id = $) is applied by the handler via
		// BuildListQuery.Base.
	}
}

// SpaceMemberFilter returns the filter config for space-scope IAM
// Members (the space ListMembers RPC). Structurally identical to
// OrgMemberFilter, over `space_members` instead of `org_members`. The
// `role` resource name is still org-scoped
// (`organizations/<org slug>/roles/<role name>`), so the correlated
// subquery reaches the org slug through spaces → organizations. The
// derived table exposes exactly the `db.ListSpaceMembersRow` columns
// (sm.* + role_name), matched by `filter.ScanSpaceMembers`.
func SpaceMemberFilter() *ResourceFilter {
	return &ResourceFilter{
		Filterable: map[string]FilterableField{
			"role":           {Column: "('organizations/' || (SELECT o.name FROM organizations o JOIN spaces sp ON sp.org_id = o.id WHERE sp.id = m.space_id) || '/roles/' || m.role_name)", Type: filtering.TypeString},
			"principal_kind": {Column: "(CASE WHEN m.user_id IS NOT NULL THEN 'user' ELSE 'group' END)", Type: filtering.TypeString},
		},
		Sortable: map[string]SortableField{
			"role":       {Column: "m.role_name", Type: filtering.TypeString},
			"createTime": {Column: "m.create_time", Type: filtering.TypeTimestamp},
		},
		Table:           "(SELECT sm.*, r.name AS role_name FROM space_members sm JOIN roles r ON r.id = sm.role_id) AS m",
		SoftDelete:      false, // members hard-delete; no delete_time column
		DefaultOrder:    "createTime",
		DefaultPageSize: 50,
		MaxPageSize:     500,
		// The space base scope (space_id = $) is applied by the handler
		// via BuildListQuery.Base.
	}
}
