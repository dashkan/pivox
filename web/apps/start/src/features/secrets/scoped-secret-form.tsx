import { SecretCreateFeature, SecretEditFeature } from '@pivox/features/secrets';

import { $api, apiClient } from '@/lib/api-client';
import { useSecretFormNav } from '@/lib/use-secret-form-nav';

/** The scoped secrets LIST path for this route's `?from=` fallback. */
export function scopedSecretsListRoute(orgSlug: string, spaceSlug?: string): string {
  return spaceSlug
    ? `/organizations/${orgSlug}/spaces/${spaceSlug}/secrets`
    : `/organizations/${orgSlug}/secrets`;
}

/** The shared "← Secrets" back link — a soft navigation to the return target. */
function BackLink({ returnTo, goBack }: { returnTo: string; goBack: () => void }) {
  return (
    <a
      href={returnTo}
      className="hover:underline"
      onClick={(e) => {
        e.preventDefault();
        goBack();
      }}
    >
      ← Secrets
    </a>
  );
}

/**
 * Shared secret CREATE page for the scope-in-URL routes — the secret twin of
 * `ScopedConnectorNew`. Both the org-rollup `/secrets/new` and the space
 * `.../spaces/$space/secrets/new` render this; `spaceSlug` only pins the return
 * target — the create form owns scope selection via its in-form space picker
 * (`SecretCreateFields`), so `parent` is always the org.
 */
export function ScopedSecretNew({
  orgSlug,
  spaceSlug,
  from,
}: {
  orgSlug: string;
  spaceSlug?: string;
  from?: string;
}) {
  const { returnTo, goBack, goBackAndRefresh, onDirtyChange } = useSecretFormNav(
    from,
    scopedSecretsListRoute(orgSlug, spaceSlug),
  );

  return (
    <SecretCreateFeature
      $api={$api}
      apiClient={apiClient}
      parent={`organizations/${orgSlug}`}
      back={<BackLink returnTo={returnTo} goBack={goBack} />}
      onCancel={goBack}
      onSubmitSuccess={goBackAndRefresh}
      onDirtyChange={onDirtyChange}
    />
  );
}

/**
 * Shared secret EDIT page for the scope-in-URL routes. The secret's scope is the
 * route's scope: the org-direct route passes no space; the space route passes its
 * `$space` param straight through to the edit feature.
 */
export function ScopedSecretEdit({
  orgSlug,
  spaceSlug,
  secretId,
  from,
}: {
  orgSlug: string;
  spaceSlug?: string;
  secretId: string;
  from?: string;
}) {
  const { returnTo, goBack, goBackAndRefresh, onDirtyChange } = useSecretFormNav(
    from,
    scopedSecretsListRoute(orgSlug, spaceSlug),
  );

  return (
    <SecretEditFeature
      $api={$api}
      apiClient={apiClient}
      parent={`organizations/${orgSlug}`}
      secretId={secretId}
      space={spaceSlug}
      back={<BackLink returnTo={returnTo} goBack={goBack} />}
      onCancel={goBack}
      onSubmitSuccess={goBackAndRefresh}
      onDirtyChange={onDirtyChange}
    />
  );
}
