# SSO Config Expansion + IAM/Lifecycle Roll-overs

**Status**: open
**Owner**: Ashkan
**Started**: 2026-04-30
**Predecessor**: [iam-and-lifecycle-roadmap.md](./iam-and-lifecycle-roadmap.md)

This doc tracks two threads that emerged after the OAuth/OIDC broker
migration landed:

1. **SSO config expansion** — the broker currently hardcodes IdP-facing
   parameters (`prompt=login`, scopes, etc.). Different orgs / IdPs
   want different behavior; we should expose these as
   `SsoConfig.OidcConfig` / `SsoConfig.SamlConfig` fields with
   sensible defaults.
2. **IAM / lifecycle roll-overs** — items left unchecked when
   `iam-and-lifecycle-roadmap.md` was closed out. Captured here so
   the prior doc can be archived without losing work.

---

## Part 1 — SSO config expansion

### Why now

Hardcoding `prompt=login` in the broker (commit `73e4c01`) was the
right immediate fix — explicit "Sign in" clicks should re-auth at
the IdP, not silently reuse a stale session. But it's the wrong
forever-default for several reasons:

- **Multi-account users** want `prompt=select_account` ("let me
  pick which Google identity I'm signing in as"). Forcing
  `login` re-prompts even when the user only wanted to switch
  accounts.
- **SSO users on shared devices** (kiosks, library terminals) need
  `prompt=login` reliably — silent reauth is a security gap there.
- **High-assurance orgs** (banking, healthcare) want to set
  `acr_values` or `max_age` to enforce step-up MFA at sign-in,
  not just at IdP-side session creation.
- **Localized deployments** want `ui_locales` so the IdP login
  page matches the user's preferred language.

These are all OIDC-standard parameters every compliant IdP honors.
The broker already forwards `login_hint` from the native client;
the rest belong in per-org config.

### Proposed `OidcConfig` additions

Add to `api/proto/pivox/api/v1/sso.proto`'s `OidcConfig`:

```proto
message OidcConfig {
  // ... existing fields ...

  // Optional. Standard OIDC `prompt` behavior. Controls whether
  // the IdP shows credentials/account-picker/consent screens even
  // when an active session exists.
  enum Prompt {
    PROMPT_UNSPECIFIED = 0;  // Don't send a prompt param (let IdP decide).
    NONE = 1;                // Never prompt — fail if interaction is needed.
    LOGIN = 2;               // Always re-authenticate at the IdP.
    SELECT_ACCOUNT = 3;      // Show account picker; reuse credentials if possible.
    CONSENT = 4;             // Re-show the scope-grant consent screen.
  }
  Prompt prompt = 5 [(google.api.field_behavior) = OPTIONAL];

  // Optional. Additional scopes to request beyond `openid email profile`.
  // Repeated; deduplicated server-side; max 16 entries.
  repeated string extra_scopes = 6 [(google.api.field_behavior) = OPTIONAL];

  // Optional. Authentication Context Class Reference values, in
  // priority order. Used to enforce step-up auth (e.g. MFA, smart
  // card). IdP-defined; Pivox forwards verbatim.
  repeated string acr_values = 7 [(google.api.field_behavior) = OPTIONAL];

  // Optional. Maximum allowed elapsed time (seconds) since the
  // user's last IdP authentication. Forces re-auth at the IdP
  // even if the session is otherwise live. 0 = no constraint.
  int64 max_age_seconds = 8 [(google.api.field_behavior) = OPTIONAL];

  // Optional. Preferred language tags for the IdP UI, in priority
  // order (e.g. ["fr-CA", "en"]). IdP picks the best match.
  repeated string ui_locales = 9 [(google.api.field_behavior) = OPTIONAL];

  // Optional. Free-form key/value pairs forwarded as authorize-URL
  // query params. Escape hatch for IdP-specific extensions (Okta's
  // `idp_scoping`, Auth0 connections, etc.). Keys are normalized
  // to lowercase; reserved keys (client_id, redirect_uri,
  // response_type, scope, state, nonce, prompt, login_hint,
  // acr_values, max_age, ui_locales) are silently dropped to
  // prevent overriding security-critical params.
  map<string, string> extra_authorize_params = 10
      [(google.api.field_behavior) = OPTIONAL];

  // Optional. Map IdP id_token claims onto Firebase user-profile
  // fields. Keys are id_token claim names (e.g. "preferred_username",
  // "given_name"); values are Firebase profile field names (e.g.
  // "displayName", "photoURL"). Applied during the blocking-fn
  // identity sync. Only known Firebase target fields are accepted;
  // unknown targets surface as INVALID_ARGUMENT at write time.
  map<string, string> claim_mapping = 11
      [(google.api.field_behavior) = OPTIONAL];
}
```

### Proposed `SamlConfig` additions

```proto
message SamlConfig {
  // ... existing fields ...

  // Optional. SAML NameID format the SP requests from the IdP. Most
  // orgs want EMAIL_ADDRESS; PERSISTENT lets the IdP issue a stable
  // opaque pseudonym instead. UNSPECIFIED defers to the IdP's
  // default.
  enum NameIdFormat {
    NAME_ID_FORMAT_UNSPECIFIED = 0;
    EMAIL_ADDRESS = 1;
    PERSISTENT = 2;
    TRANSIENT = 3;
    UNSPECIFIED_FORMAT = 4;
  }
  NameIdFormat name_id_format = 7 [(google.api.field_behavior) = OPTIONAL];

  // Optional. Force re-authentication at the IdP regardless of
  // session state. SAML equivalent of OIDC `prompt=login`. Maps
  // to AuthnRequest's `ForceAuthn=true` attribute.
  bool force_authn = 8 [(google.api.field_behavior) = OPTIONAL];

  // Optional. Passive sign-on — IdP must not prompt the user.
  // Mutually exclusive with `force_authn`. Maps to AuthnRequest's
  // `IsPassive=true` attribute. Used for silent re-auth flows.
  bool is_passive = 9 [(google.api.field_behavior) = OPTIONAL];

  // Optional. Map SAML assertion attributes onto Firebase user-
  // profile fields. Keys are SAML attribute names (e.g.
  // "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/givenname");
  // values are Firebase profile field names. Same validation as
  // OidcConfig.claim_mapping.
  map<string, string> attribute_mapping = 10
      [(google.api.field_behavior) = OPTIONAL];

  // Optional. Preferred AuthnContextClassRef value for SAML's
  // RequestedAuthnContext element. Equivalent of OIDC `acr_values`.
  // Common values:
  //   urn:oasis:names:tc:SAML:2.0:ac:classes:Password
  //   urn:oasis:names:tc:SAML:2.0:ac:classes:PasswordProtectedTransport
  //   urn:oasis:names:tc:SAML:2.0:ac:classes:MultiFactor
  string requested_authn_context = 11
      [(google.api.field_behavior) = OPTIONAL];
}
```

### Storage shape

`oidc_config` and `saml_config` are JSONB columns; new fields slot
in without a migration. The Go-side `convert.OidcConfigRowFromProto`
+ `convert.SamlConfigRowFromProto` need to learn the new keys, plus
the corresponding read/round-trip paths.

### Broker changes

`internal/server/oauth_broker.go` `start` handler reads the new
`Prompt`, `ExtraScopes`, `AcrValues`, `MaxAgeSeconds`, `UiLocales`,
and `ExtraAuthorizeParams` fields off `providerConfig` and forwards
to the IdP's authorize URL. The hardcoded `q.Set("prompt", "login")`
becomes `q.Set("prompt", cfg.prompt)` (or omitted when
`PROMPT_UNSPECIFIED`).

Reserved-keys allowlist on `extra_authorize_params` — must filter
out `client_id`, `redirect_uri`, `response_type`, `scope`, `state`,
`nonce`, `prompt`, `login_hint`, `acr_values`, `max_age`,
`ui_locales` so a per-org config can't override security-critical
params (the same defense we already have for `extraAuthorizeParams`
in the static GitHub config).

### Validation

`validateOidc` / `validateSaml` (in
`internal/service/organizations/sso.go`) enforce:

- `extra_scopes` ≤ 16 entries, each ≤ 64 chars, no whitespace.
- `acr_values` ≤ 8 entries, each ≤ 256 chars.
- `max_age_seconds` ≥ 0.
- `ui_locales` ≤ 8 entries, each a valid BCP 47 tag.
- `extra_authorize_params` — keys must be `^[a-z][a-z0-9_]*$`,
  values ≤ 512 chars, total ≤ 16 entries, AND no key in the
  reserved set.
- `claim_mapping` / `attribute_mapping` — values must be one of
  the known Firebase profile fields (`displayName`, `photoURL`,
  `email`, `phoneNumber` for now).
- SAML: `force_authn` and `is_passive` are mutually exclusive.

### Migration / backwards compat

- All new fields are OPTIONAL with sensible defaults that match
  current behavior:
  - `prompt = PROMPT_UNSPECIFIED` → broker drops the prompt param,
    IdP decides (matches behavior pre-`73e4c01`). Operators who
    want today's `prompt=login` set it explicitly per-org.
  - `extra_scopes` empty → broker requests `openid email profile`.
  - `force_authn` / `is_passive` false → no AuthnRequest
    annotations.
- Existing rows pass validation unchanged; the JSONB shape
  accommodates absent keys natively.

### UI surface (Phase 6)

The Phase 6 SSO config form will need:

- Dropdown for `prompt` (4 options + "default").
- Multi-select / chip input for `extra_scopes` and `acr_values`.
- Numeric input for `max_age_seconds` (with "no constraint"
  null-state).
- Locale picker (multi-select BCP 47).
- Key/value table for `extra_authorize_params` and `claim_mapping`.
- For SAML: NameID dropdown, force/passive radio pair, attribute
  mapping table, AuthnContext dropdown.

### Decision needed

Whether to add an `enabled` toggle per *param*, or just rely on
"unset = default behavior." Leaning toward the latter — fewer
controls, less ambiguity. Operators who change their mind
overwrite or clear the value via UpdateSsoConfig.

---

## Part 2 — Lifecycle / IAM roll-overs

Carried forward from `iam-and-lifecycle-roadmap.md`. Grouped by
intended next-touch.

### Pre-Phase 1 ship gate

- [ ] `xcodebuild test -scheme PivoxTests` — macOS unit tests must
      pass before phase 1 cuts a release.

### Phase 4 — Step 5 (User lifecycle)

- [ ] `onUserDeleted` Firebase webhook handler. Idempotent — no-op
      if user already gone. Out-of-process, lives in
      `deployments/firebase/functions/`. **Blocked on a decision**:
      when Firebase deletes a user out-of-band (Console / gcloud)
      and they're the sole owner of one or more orgs, what should
      the webhook do? `Iam.DeleteUser` LRO refuses with
      FailedPrecondition; the webhook has no caller to surface
      that to. Options:
        (a) refuse — Pivox state diverges from Firebase, no recovery.
        (b) proceed, leave org ownerless, loud alert. Admin reconciles.
        (c) auto-promote next-admin. Implicit policy.
        (d) auto-soft-delete affected orgs — 30-day grace gives ops time.

### Phase 4 exit criteria

- [ ] `make build && go test ./... && make api-lint && make lint`
      clean. Open: `internal/service/aichat` build-fail and
      `internal/storageagent` runtime flake are pre-existing, not
      Phase-4-introduced. Tracked in "Pre-existing test failures"
      below.

### Phase 5 exit criteria

- [ ] All space-scoped RPCs pass integration tests. Spot-check vs
      current harness.
- [ ] Permission resolution honors org→space inheritance.
      Confirmed via `TestE2E_UndeleteSpace_RestoresSoftDeletedSpace`,
      pin-test before sign-off.
- [ ] Native macOS app can create + list + delete spaces. No
      spaces UI exists yet; collapses into Phase 6.

### Phase 6 — UI (entirely open)

#### Members management

- [ ] Members list view (per org, per space).
- [ ] Invite member: email lookup → existing user OR pending invite.
- [ ] Remove member.
- [ ] Update role.
- [ ] Group management: list, create, add/remove users.

#### Org settings

- [ ] Display name editing (already partial via OrgService).
- [ ] Slug shown as immutable.
- [ ] Transfer ownership flow.
- [ ] Delete org flow with slug-typed confirmation.
- [ ] Soft-delete state surfaced ("scheduled for deletion on …" banner).
- [ ] Undelete affordance during grace.

#### Account settings

- [ ] Account delete flow.
- [ ] Sole-owner blocking surfaces affected orgs with quick links to
      TransferOwnership / DeleteOrganization.
- [ ] Email change, profile editing.

#### SSO config

- [ ] Per-org SSO config form (OIDC + SAML variants). **Blocked on
      Part 1 above** — should ship the new fields together so the
      form doesn't need a follow-up redesign.
- [ ] Domain verification UI.
- [ ] Test login button.

#### Phase 6 exit criteria

- [ ] Account + org deletion possible end-to-end via UI.
- [ ] No "manual SQL to clear sole-owner block" required for normal flows.
- [ ] SSO config UI exercised end-to-end against real Firebase project.

### Future / unbacklogged

- [ ] `Organizations.LeaveOrganization` — member self-leave-org RPC.
- [ ] Custom roles: lift `UNIMPLEMENTED` on
      `CreateRole`/`UpdateRole`/`DeleteRole` when there's a real
      customer use case.
- [ ] Conditional bindings: re-import `google/type/expr.proto`,
      attach to `Member.condition`.
- [ ] Re-import `google/iam/v1/iam_policy.proto` for full
      `GetIamPolicy`/`SetIamPolicy` projection over `members` table —
      when fine-grain sharing arrives.
- [ ] `Group` cross-org? Today scoped to single org. Cross-org
      sharing is a future feature.
- [ ] Audit log for IAM mutations.

### From OAuth broker code review (post-migration)

Spilled out of the inline review during the broker migration. None
were blockers; tracked here so they don't get lost.

- [ ] Centralized macOS logger — partial. `PivoxLog` shipped, but
      categorized log subsystems still need `auth`/`sso`/`chat` /
      `transcript` rollups in Console.app to make filtering nicer
      across windows and engineers. Verify on real builds.
- [ ] Wider auth-mode story: dev-tag (`requireSecret`) vs prod
      (`requireGoogleIdentity`) caught us out once. Document the
      decision matrix in `docs/dev/auth-modes.md` so future
      contributors don't trip over it. Possibly add a fall-through
      so dev-mode also accepts OIDC tokens (eliminates the trap
      without compromising prod posture).
- [ ] `pivox-cloud serve` should fail-fast at startup if
      `PIVOX_APP_KEY` is unset. Currently boots fine and 500s on
      the first sign-in with `sign_state_failed`.
- [ ] `Organizations.UpdateSsoConfig` write-side validation already
      enforces https-issuer (mirror of `requireSecureIssuer`). When
      Phase 6's SSO settings UI ships, surface the validation
      error inline rather than as a modal toast.

### `DeleteUser` sole-owner concurrent race

- [ ] Two concurrent `Iam.DeleteUser` calls in the same org can
      both pass the `CountOrgOwnersExcludingUser > 0` check and
      proceed with their deletes, leaving the org with 0 owners.
      Surfaced in the Phase 7 audit. Fix: wrap the read+writes in
      a serializable tx, or use `SELECT … FOR UPDATE` on the
      owner role rows. Acceptable to defer until concurrent admin
      activity is realistic.

### Pre-existing test failures (still open)

#### `internal/service/aichat/server_integration_test.go` — compile failure (`-tags dev` only)

References proto + interface shapes that have since been refactored:

- `aiv1.ClientEvent`, `aiv1.ClientEvent_Message`, `aiv1.UserMessage`
  — old proto message names from before the bidi-stream conversion.
- `client.Stream` — old RPC name; current shape uses `Send`-style
  API.
- `*fixedModel does not implement model.LanguageModel (missing
  method Name)` — `LanguageModel` interface gained a `Name()`
  method; the test stub wasn't updated.

Gated by `//go:build dev` so `go test ./...` (no tags) skips it.
Surfaces only on `go test -tags dev`.

- [ ] Repair or rewrite against the current `AiChat` service shape
      and update `fixedModel` to satisfy the current
      `model.LanguageModel` interface.

#### `internal/storageagent/TestConnect_FullHandshake` — runtime failure

- [ ] Diagnose and fix `TestConnect_FullHandshake`,
      `TestConnect_ContextCancelDuringHeartbeat`,
      `TestConnect_ReconnectingAgent` (handshake stream closes
      before response arrives — likely a race in the test
      scaffolding, not the production handshake).

---

## How this doc retires

- Part 1 lands as a single proto + handler + UI commit. Move the
  bullet list under "Phase 6 — SSO config" in the parent roadmap
  and check it off there.
- Part 2 items each migrate to wherever they're picked up. When
  the last bullet is checked, this doc gets its `Status: closed`
  stamp and stays as historical context.
