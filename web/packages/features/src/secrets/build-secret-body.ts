import type { components } from '@pivox/client/types';
import type { SecretFormValues } from '@pivox/ui/resource-admin';

import { entriesToMap } from '@/connectors/build-connector-body';

type SecretBody = components['schemas']['v1Secret'];

/**
 * Base64-encode a string for a proto `bytes` field. protojson (the JSON
 * gateway) requires `bytes` values base64-encoded and rejects the raw string.
 * Encodes as UTF-8 first so non-ASCII values survive the round-trip.
 */
function encodeBytes(value: string): string {
  const bytes = new TextEncoder().encode(value);
  let binary = '';
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary);
}

/** Create body — `value` is required (INPUT_ONLY) and always sent. */
export function buildSecretCreateBody(values: SecretFormValues): SecretBody {
  const body: SecretBody = {
    displayName: values.displayName,
    value: encodeBytes(values.value),
  };
  const annotations = entriesToMap(values.annotations);
  if (Object.keys(annotations).length > 0) body.annotations = annotations;
  return body;
}

/**
 * Update body for UpdateSecret. The REST gateway derives the field mask from
 * the fields present in this PATCH body, so `value` is included ONLY when
 * rotating — a metadata-only edit omits it and the stored value is untouched.
 */
export function buildSecretUpdateBody(input: {
  values: SecretFormValues;
  etag?: string;
}): SecretBody {
  const { values, etag } = input;
  const body: SecretBody = {
    displayName: values.displayName,
    annotations: entriesToMap(values.annotations),
  };
  if (etag) body.etag = etag;
  if (values.rotate) body.value = encodeBytes(values.value);
  return body;
}
