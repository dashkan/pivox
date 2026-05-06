# Firebase Cloud Functions — agent conventions

Scope: `deployments/firebase/functions/`. Pivox uses Firebase blocking
functions for authentication-time hooks (`syncIdentityOnCreate`,
`syncIdentityOnSignIn`) — these run synchronously during Firebase
Auth user-create / sign-in events and create/update the corresponding
Pivox `identities` row before the user can call any other RPC.

Stack: TypeScript + `firebase-functions/v2/identity` blocking
triggers. Built with `tsc`; deployed via `firebase deploy --only
functions` (or `make firebase-deploy` from repo root).

## Function naming

- Cloud-function exports represent runtime endpoints. Export name =
  Firebase function name in the console. **Renaming an export
  requires deleting the old function in Firebase before deploying
  the new one** — Firebase doesn't garbage-collect old function
  names automatically. Use `firebase functions:delete <oldName>
  --region us-central1 --force`.
- Don't include "Firebase" in export names — these are Pivox identity
  hooks that happen to use Firebase as the auth provider, not
  Firebase-specific functions. `syncIdentityOnCreate`, not
  `syncFirebaseIdentityOnCreate`.

## Authentication for the Pivox callback

- The Firebase Function calls Pivox via a Google Cloud OIDC
  identity token minted from the function's runtime service
  account; the Go server validates it against Google's JWKS in
  `internal/server/internal_hooks_sync_auth.go`.
- The function code lives in `src/index.ts`; the Authorization
  header is built in `getAuthorizationHeader`.

## Custom claims

`pivox_user_id` custom claim is set on every token issuance — both
on user create AND on every sign-in (so older tokens get the claim
on their next refresh). This is the Pivox-side identity UUID;
backend handlers read it via `server.MustPivoxUserID(ctx)`.

If the claim is empty / missing on a token reaching the backend, the
auth interceptor rejects with `Unauthenticated`. Clients refresh
their ID token to re-mint with the current claim.

## When you change the function shape

After any change to `src/index.ts`:

1. `npm run lint` — eslint + tsc strict.
2. `npm run build` — tsc.
3. `make firebase-deploy` from repo root.
4. If you renamed an export, delete the old function in Firebase
   first (see above).
5. Smoke test with a sign-in via the Pivox app + verify the
   `identity synced` log lands in the cloud-controller logs.

## Out of scope

OAuth / SSO flows are NOT Firebase blocking-function territory.
Those go through the Pivox OAuth broker
(`internal/server/oauth_broker.go`). Don't add OAuth code here.
