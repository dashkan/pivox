import { createInstance } from "i18next";
import FetchBackend from "i18next-fetch-backend";
import { initReactI18next } from "react-i18next";

import { authServerUrl, environment } from "./env";

/**
 * A SCOPED i18next instance for the Keycloak account message bundle — created
 * with `createInstance()` rather than the global i18next, deliberately.
 *
 * The workspace runs i18next@26 / react-i18next@17; account-ui was authored
 * against @25 / @16. Using the global instance would risk a dual-context
 * mismatch. A scoped instance mounted via its own `<I18nextProvider>` keeps the
 * KC bundle isolated from the app's own i18n, and our label helpers take `t` as
 * an argument so any instance works.
 *
 * Mirrors Keycloak's `js/apps/account-ui/src/i18n.ts`:
 *  - loadPath points at the theme *resources* endpoint (not the account REST
 *    API): `${serverBaseUrl}/resources/${realm}/account/{{lng}}`.
 *  - KC serves messages as a JSON array of `{key,value}`; i18next wants a flat
 *    object, hence the custom `parse`. Without it every key resolves to itself.
 *  - `nsSeparator: false` because KC keys contain `:`/`.`.
 */
const DEFAULT_LOCALE = "en";

type KeyValue = { key: string; value: string };

const locale = (environment as { locale?: string }).locale || DEFAULT_LOCALE;

export const i18n = createInstance({
  lng: locale,
  fallbackLng: DEFAULT_LOCALE,
  // KC message keys contain both `:` and `.` (e.g. option-label prefixes,
  // `profile.attributes.*`). The bundle is loaded flat, so BOTH separators must
  // be disabled or i18next does a nested lookup and returns the raw key.
  nsSeparator: false,
  keySeparator: false,
  interpolation: { escapeValue: false },
  backend: {
    loadPath: `${authServerUrl}/resources/${environment.realm}/account/{{lng}}`,
    parse(data: string) {
      const messages = JSON.parse(data) as KeyValue[];
      return Object.fromEntries(messages.map(({ key, value }) => [key, value]));
    },
  },
});

i18n.use(FetchBackend);
i18n.use(initReactI18next);
