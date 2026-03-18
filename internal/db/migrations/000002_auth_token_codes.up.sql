-- 000002_auth_token_codes.up.sql
-- Short-lived, single-use opaque codes that wrap a Firebase ID token.
-- Used by the Electron provider-linking flow to avoid passing the raw
-- ID token in a URL query parameter (AUTHN-04).

CREATE TABLE auth_token_codes (
    code        UUID PRIMARY KEY DEFAULT uuidv7(),
    id_token    TEXT NOT NULL,
    consumed    BOOLEAN NOT NULL DEFAULT false,
    create_time TIMESTAMPTZ NOT NULL DEFAULT now(),
    expire_time TIMESTAMPTZ NOT NULL DEFAULT now() + INTERVAL '60 seconds'
);

CREATE INDEX idx_auth_token_codes_expire ON auth_token_codes (expire_time);
