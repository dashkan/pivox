-- Dev SSO seed for the acme org. Two-pass-safe: bootstraps the
-- SSO discovery surface (org + roles + verified domain + SsoConfig)
-- whether or not the founder identity exists yet, then binds the
-- founder as owner the moment they're present.
--
-- Single-pass-safe: bootstraps the SSO discovery surface AND the
-- founder identity in one apply, so a fresh `make db-up` + this
-- seed leaves ashkan@acme.com bound as owner of acme without
-- requiring an interactive sign-in first.
--
-- The identity row is pre-seeded with the founder's actual Firebase
-- localId (`firebase auth:export ... | jq '.users[] | select(.email
-- == "ashkan@acme.com") | .localId'`). On first sign-in the
-- `syncIdentityOnSignIn` blocking fn upserts on (firebase_uid) —
-- the conflict path leaves id stable and just refreshes
-- email_verified / display_name / last_login_time, so the owner
-- binding survives.
--
-- If the founder's Firebase user is recreated with a different UID
-- (e.g. you deleted + re-added them in the Firebase console), this
-- seed's pre-baked UID will go stale: the next sign-in will create
-- a NEW identity row, the email-uniqueness partial index will
-- block it, and SSO will surface as "Couldn't complete sign-in".
-- Fix: update `_ashkan_firebase_uid` below to match the new
-- localId, then re-run the seed (the existing identity row is
-- ON CONFLICT (firebase_uid) so the stale row survives — manually
-- delete it if you want a clean state).
--
-- Idempotent. Re-runnable. Safe to schedule.
--
-- Usage:
--   psql "$DATABASE_URL" -f scripts/seeds/dev_acme_sso.sql
--
-- IMPORTANT: this seed writes the IdP `client_secret` as PLAINTEXT
-- bytes into `sso_configs.client_secret_ciphertext`. That works
-- only when the server is built with `-tags dev` (NoOpEncryptor =
-- passthrough on Decrypt). When running pivox-cloud in prod mode
-- (KMS-backed encryptor), this seed's secret will fail to decrypt
-- and the broker will surface every sign-in as 404 unknown_provider.
--
-- For prod-mode local testing, set the secret via the proper write
-- path — `Organizations.UpdateSsoConfig` (encrypts on write + syncs
-- the OIDC provider in Firebase via the Admin SDK). Once Phase 6
-- ships an SSO settings UI, that's the canonical path.

\set ON_ERROR_STOP on

BEGIN;

DO $$
DECLARE
    ashkan_id UUID;
    acme_id   UUID;
    owner_id  UUID;
    -- Founder's Firebase localId. Must match the live Firebase Auth
    -- user record so the next sign-in's blocking-fn upsert hits ON
    -- CONFLICT (firebase_uid) and preserves the seeded row + owner
    -- binding. Sourced from `firebase auth:export` (see file header).
    _ashkan_firebase_uid CONSTANT TEXT := 'ScQytJWi2ycF3jiiBlRazncbfQB3';
BEGIN
    -- 0) Founder identity. Pre-seeded so the seed is single-pass —
    --    no need to interactively sign in before owner binding can
    --    land. ON CONFLICT (firebase_uid) means re-running the seed
    --    after a real sign-in won't clobber the live email_verified
    --    / display_name / last_login_time the blocking fn populated.
    INSERT INTO identities (id, firebase_uid, email, email_verified)
    VALUES (uuidv7(), _ashkan_firebase_uid, 'ashkan@acme.com', true)
    ON CONFLICT (firebase_uid) DO NOTHING;

    SELECT id INTO ashkan_id FROM identities
        WHERE firebase_uid = _ashkan_firebase_uid;

    -- 1) acme organization (idempotent). created_by is NULL on
    --    pass 1; back-filled on pass 2 once we know the identity.
    INSERT INTO organizations (id, name, display_name, created_by)
    VALUES (uuidv7(), 'acme', 'Acme Inc.', ashkan_id)
    ON CONFLICT (name) DO UPDATE SET
        created_by = COALESCE(organizations.created_by, EXCLUDED.created_by);

    SELECT id INTO acme_id FROM organizations WHERE name = 'acme';

    -- 2) System roles. UNIQUE(org_id, name) makes ON CONFLICT a safe
    --    no-op when the seed is re-run.
    INSERT INTO roles (id, org_id, name, display_name, description, is_system)
    VALUES
        (uuidv7(), acme_id, 'owner',  'Owner',  'Full administrative access including destruction-class operations (delete organization, transfer ownership, update SSO, delete users).', true),
        (uuidv7(), acme_id, 'admin',  'Admin',  'Day-to-day organization management — IAM, domains, SSO read, API keys, storage, invitations, and content.', true),
        (uuidv7(), acme_id, 'editor', 'Editor', 'Content management — assets, requests, line items, and AI conversations.', true),
        (uuidv7(), acme_id, 'viewer', 'Viewer', 'Read-only access across the organization.', true)
    ON CONFLICT (org_id, name) DO NOTHING;

    SELECT id INTO owner_id FROM roles
        WHERE org_id = acme_id AND name = 'owner' AND is_system = true;

    -- 3) Verified domain `acme.com`. Globally UNIQUE; CHECK enforces
    --    lowercase. Pre-staged so resolveProvider works on pass 1.
    INSERT INTO domains (id, org_id, domain, verification_token, state, verified_time)
    VALUES (uuidv7(), acme_id, 'acme.com', 'dev-seed-token', 'VERIFIED', now())
    ON CONFLICT (domain) DO NOTHING;

    -- 4) SsoConfig pointing at the manually-provisioned `oidc.acme`
    --    Firebase provider. The CHECK requires exactly one of
    --    oidc_config / saml_config — we set oidc_config, leave
    --    saml_config NULL. Dev mode uses NoOpEncryptor (passthrough)
    --    so client_secret_ciphertext stores plaintext bytes; prod
    --    runs the real KMS-backed Encryptor.
    INSERT INTO sso_configs (id, org_id, firebase_provider_id, display_name, enabled, oidc_config, client_secret_ciphertext)
    VALUES (
        uuidv7(),
        acme_id,
        'oidc.acme',
        'Acme SSO (Keycloak)',
        true,
        jsonb_build_object(
            'issuer',    'https://pivox.ngrok.app/keycloak/realms/acme',
            'client_id', 'pivox'
        ),
        convert_to('120MXaCwtOx7kcy2dFdfhIOTGxyPToXS', 'UTF8')
    )
    -- IMPORTANT: client_secret_ciphertext is INTENTIONALLY omitted
    -- from the conflict clause. On first INSERT we seed the
    -- plaintext bytes (works for dev-mode NoOpEncryptor). After
    -- that, the operator may have re-encrypted the column under
    -- prod-mode KMS via `cmd/encrypt-sso-secret`; re-running this
    -- seed must NOT silently revert that to plaintext (which would
    -- crash the broker with "ciphertext too short for wrapped DEK"
    -- on the next sign-in). To rotate the secret intentionally,
    -- either delete the row and re-run the seed, or use
    -- Organizations.UpdateSsoConfig which is the production path.
    ON CONFLICT (org_id) DO UPDATE SET
        firebase_provider_id     = EXCLUDED.firebase_provider_id,
        enabled                  = EXCLUDED.enabled,
        oidc_config              = EXCLUDED.oidc_config,
        update_time              = now();

    -- 5) Bind ashkan as owner of acme. The WHERE NOT EXISTS guard
    --    (rather than a UNIQUE-violation ON CONFLICT) is what makes
    --    this idempotent — re-running the seed after the row exists
    --    is a no-op rather than a constraint error.
    INSERT INTO org_members (id, org_id, role_id, user_id, created_by)
    SELECT uuidv7(), acme_id, owner_id, ashkan_id, ashkan_id
    WHERE NOT EXISTS (
        SELECT 1 FROM org_members
        WHERE org_id = acme_id
          AND user_id = ashkan_id
          AND role_id = owner_id
    );

    RAISE NOTICE 'Seeded acme SSO + bound ashkan as owner (org=%, ashkan=%).', acme_id, ashkan_id;
END $$;

COMMIT;
