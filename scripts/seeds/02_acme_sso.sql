-- Dev SSO seed for the acme org. Part of the standard seed chain
-- (`make db-seed` → `scripts/seed.sql`) — bootstraps the SSO
-- discovery surface (org + roles + verified domain + SsoConfig) and
-- binds ashkan@acme.com as owner so SSO sign-in works end-to-end on
-- a freshly-seeded dev DB.
--
-- `ashkan@acme.com` is the SSO-only test identity — its Firebase
-- localId below MUST match a real Firebase Auth user. Sourced from
-- `firebase auth:export ... | jq '.users[] | select(.email ==
-- "ashkan@acme.com") | .localId'`. On first sign-in the
-- `syncIdentityOnSignIn` blocking fn upserts on (firebase_uid) —
-- the conflict path leaves id stable and just refreshes
-- email_verified / display_name / last_login_time, so the owner
-- binding survives.
--
-- If the Firebase user is recreated with a different UID (e.g.
-- deleted + re-added in the Firebase console), this seed's pre-baked
-- UID goes stale: the next sign-in creates a NEW identity row, the
-- email-uniqueness partial index blocks it, and SSO surfaces as
-- "Couldn't complete sign-in". Fix: update `_ashkan_firebase_uid`
-- below to match the new localId, then re-run the seed (the
-- existing identity row is ON CONFLICT (firebase_uid) so the stale
-- row survives — manually delete it if you want a clean state).
--
-- Idempotent. Re-runnable.
--
-- The `client_secret_ciphertext` literal below is the dev IdP
-- client_secret PRE-ENCRYPTED through the standard KMS-backed
-- Encryptor. Plaintext was `120MXaCwtOx7kcy2dFdfhIOTGxyPToXS` (see
-- the Keycloak realm config); ciphertext was minted via
-- `go run ./cmd/encrypt-sso-secret --provider-id=oidc.acme
-- --secret=...` and copied here as hex.
--
-- Why pre-encrypted instead of plaintext + `-tags dev`: keeps a
-- fresh `make db-seed` working end-to-end against the same
-- pivox-cloud binary you'd run in normal dev (KMS encryptor, no
-- conditional build flag), so SSO sign-in works on a freshly seeded
-- DB without a manual encrypt-then-update step.
--
-- KMS key dependency: this ciphertext is bound to the KMS key the
-- encryptor was configured with at encrypt time. If your local
-- `PIVOX_GCP_KMS_KEY_NAME` differs from the one used to mint the
-- literal below, the broker fails on every SSO sign-in with
-- "ciphertext too short for wrapped DEK" → 404 unknown_provider.
--
-- Recovery (one command, paste-ready):
--
--   go run ./cmd/encrypt-sso-secret \
--     --provider-id=oidc.acme \
--     --secret=120MXaCwtOx7kcy2dFdfhIOTGxyPToXS
--
-- Then `psql ... -c "SELECT encode(client_secret_ciphertext, 'hex')
-- FROM sso_configs WHERE firebase_provider_id = 'oidc.acme';"` and
-- paste the hex into the `decode(...)` literal below. (177 bytes →
-- 354 hex chars; if you get something else, the encryptor is
-- misconfigured.)
--
-- For real secret rotation (not key rotation), go through
-- `Organizations.UpdateSsoConfig` — it bumps revision/etag and
-- syncs the secret to Firebase's OIDC provider config via the
-- Admin SDK. This seed bypasses both side effects and is dev-only.

\set ON_ERROR_STOP on

BEGIN;

DO $$
DECLARE
    -- Founder's Firebase localId. Must match the live Firebase Auth
    -- user record so the next sign-in's blocking-fn upsert hits ON
    -- CONFLICT (firebase_uid) and preserves the seeded row + owner
    -- binding. Sourced from `firebase auth:export` (see file header).
    _ashkan_firebase_uid CONSTANT TEXT := '2YCxpX5nmQXT5fmjri30SA3ra8t2';

    -- Pinned identity UUID for ashkan@acme.com — matches the value
    -- the live Firebase ID token's `pivox_user_id` custom claim
    -- already carries. See 15_dev_user_membership.sql header for
    -- the full motivation; DO NOT regenerate without forcing every
    -- dev to sign out + back in.
    _ashkan_id CONSTANT UUID := '019e7201-8066-73a9-947d-96d1039d99ab';

    -- Tier 0001 org UUID for acme. The dev orgs in
    -- 01_organizations.sql occupy suffixes 0001..000a; local-corp
    -- in 11_local_corp.sql owns 000b. Acme takes 000c — first free
    -- slot.
    _acme_id CONSTANT UUID := '0192a000-0001-7000-8000-00000000000c';

    -- Tier 0052 role UUIDs for acme (matches the convention used in
    -- 12_dev_org_roles.sql). Suffix OOOOOOOORRRR: org=000b,
    -- role index 0001..0004.
    _acme_owner_id  CONSTANT UUID := '0192a000-0052-7000-8000-0000000c0001';
    _acme_admin_id  CONSTANT UUID := '0192a000-0052-7000-8000-0000000c0002';
    _acme_editor_id CONSTANT UUID := '0192a000-0052-7000-8000-0000000c0003';
    _acme_viewer_id CONSTANT UUID := '0192a000-0052-7000-8000-0000000c0004';

    -- Tier 0054 domain, tier 0055 sso_config, tier 0053 org_members.
    _acme_domain_id     CONSTANT UUID := '0192a000-0054-7000-8000-0000000c0001';
    _acme_sso_config_id CONSTANT UUID := '0192a000-0055-7000-8000-0000000c0001';
    _acme_member_id     CONSTANT UUID := '0192a000-0053-7000-8000-0000000c0002';

    ashkan_id UUID := _ashkan_id;
    acme_id   UUID := _acme_id;
    owner_id  UUID := _acme_owner_id;
BEGIN
    -- 0) Founder identity. Pinned id + ON CONFLICT (firebase_uid)
    --    means a re-run after a real sign-in preserves the live
    --    email_verified / display_name / last_login_time that the
    --    blocking fn populated, without minting a fresh UUID.
    INSERT INTO identities (id, firebase_uid, email, email_verified)
    VALUES (_ashkan_id, _ashkan_firebase_uid, 'ashkan@acme.com', true)
    ON CONFLICT (firebase_uid) DO NOTHING;

    -- 1) acme organization (idempotent). created_by is back-filled
    --    on conflict so a re-seed after the founder identity exists
    --    populates it without overwriting a non-NULL value.
    INSERT INTO organizations (id, name, display_name, created_by)
    VALUES (_acme_id, 'acme', 'Acme Inc.', ashkan_id)
    ON CONFLICT (name) DO UPDATE SET
        created_by = COALESCE(organizations.created_by, EXCLUDED.created_by);

    -- 2) System roles. UNIQUE(org_id, name) makes ON CONFLICT a safe
    --    no-op when the seed is re-run.
    INSERT INTO roles (id, org_id, name, display_name, description, is_system)
    VALUES
        (_acme_owner_id,  acme_id, 'owner',  'Owner',  'Full administrative access including destruction-class operations (delete organization, transfer ownership, update SSO, delete users).', true),
        (_acme_admin_id,  acme_id, 'admin',  'Admin',  'Day-to-day organization management — IAM, domains, SSO read, API keys, storage, invitations, and content.', true),
        (_acme_editor_id, acme_id, 'editor', 'Editor', 'Content management — assets, requests, line items, and AI conversations.', true),
        (_acme_viewer_id, acme_id, 'viewer', 'Viewer', 'Read-only access across the organization.', true)
    ON CONFLICT (org_id, name) DO NOTHING;

    -- 3) Verified domain `acme.com`. Globally UNIQUE; CHECK enforces
    --    lowercase. Pre-staged so resolveProvider works on pass 1.
    INSERT INTO domains (id, org_id, domain, verification_token, state, verified_time)
    VALUES (_acme_domain_id, acme_id, 'acme.com', 'dev-seed-token', 'VERIFIED', now())
    ON CONFLICT (domain) DO NOTHING;

    -- 4) SsoConfig pointing at the manually-provisioned `oidc.acme`
    --    Firebase provider. The CHECK requires exactly one of
    --    oidc_config / saml_config — we set oidc_config, leave
    --    saml_config NULL.
    --
    --    `client_secret_ciphertext` is the pre-encrypted IdP client
    --    secret (see file header for plaintext + how to regenerate).
    --    Includes the wrapped DEK + AES-GCM ciphertext + nonce, all
    --    bound to the dev KMS key — the broker's standard Encryptor
    --    decrypts it on every SSO flow.
    INSERT INTO sso_configs (id, org_id, firebase_provider_id, display_name, enabled, oidc_config, client_secret_ciphertext)
    VALUES (
        _acme_sso_config_id,
        acme_id,
        'oidc.acme',
        'Acme SSO (Keycloak)',
        true,
        jsonb_build_object(
            'issuer',    'https://pivox.ngrok.app/realms/acme',
            'client_id', 'pivox'
        ),
        decode(
            '000000710a24003d85ede060d0fcefea5fa8cfaae0e0d24b1bff7f4fa79c609f57f81184ccf5ad1501e1124900c1072ff7fa5d83c77454a24b8b5fc465c373ddb1db30b921cc6a479691ff9680869214dd4f858d35deb4f141150eb318ed87e540b1647aacd1cae10a490b333d136f29b242cee205cef9534415f412469e0548810eea9c839e8fac889144d7e56d653ddebbb5af2b205def607b2ca5fe1d5e417d429ec94fa52806bc50e9d846fecfde9b',
            'hex'
        )
    )
    -- Conflict path includes client_secret_ciphertext so a re-seed
    -- after an SSO secret rotation lands the new ciphertext.
    --
    -- WARNING: this means `make db-seed` SILENTLY OVERWRITES any
    -- manual rotation done via `Organizations.UpdateSsoConfig`
    -- against your local DB. Intentional for dev — the seed is the
    -- authoritative source of the dev secret. If you're testing
    -- rotation flows, either (a) DELETE FROM sso_configs WHERE
    -- org_id = ... before each rotation test so the seed re-runs
    -- clean, or (b) accept that re-running the seed undoes your
    -- rotation.
    --
    -- For production rotation use `Organizations.UpdateSsoConfig`
    -- (encrypts on write + syncs Firebase's OIDC provider config
    -- via the Admin SDK).
    ON CONFLICT (org_id) DO UPDATE SET
        firebase_provider_id     = EXCLUDED.firebase_provider_id,
        enabled                  = EXCLUDED.enabled,
        oidc_config              = EXCLUDED.oidc_config,
        client_secret_ciphertext = EXCLUDED.client_secret_ciphertext,
        update_time              = now();

    -- 5) Bind ashkan as owner of acme. The WHERE NOT EXISTS guard
    --    (rather than a UNIQUE-violation ON CONFLICT) is what makes
    --    this idempotent — re-running the seed after the row exists
    --    is a no-op rather than a constraint error.
    INSERT INTO org_members (id, org_id, role_id, user_id, created_by)
    SELECT _acme_member_id, acme_id, owner_id, ashkan_id, ashkan_id
    WHERE NOT EXISTS (
        SELECT 1 FROM org_members
        WHERE org_id = acme_id
          AND user_id = ashkan_id
          AND role_id = owner_id
    );

    RAISE NOTICE 'Seeded acme SSO + bound ashkan as owner (org=%, ashkan=%).', acme_id, ashkan_id;
END $$;

COMMIT;
