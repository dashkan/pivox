import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { I18nextProvider } from "react-i18next";
import { RouterProvider } from "react-router-dom";

import "./index.css";

import { i18n } from "./i18n";
import { initAuth } from "./keycloak";
import { router } from "./router";

async function bootstrap() {
  // Redirects to the (Pivox-themed) Keycloak login if not authenticated, then
  // returns here with a token. account-ui's data functions consume this same
  // instance via the hand-built context in `kc-context.ts` (no KeycloakProvider
  // — see that file for why its React components can't be mounted).
  await initAuth();
  // Load the KC account message bundle into the scoped i18n instance so UP
  // attribute labels (`${profile.attributes.*}`) resolve.
  await i18n.init();

  const container = document.getElementById("app");
  if (!container) throw new Error("#app mount point missing");
  createRoot(container).render(
    <StrictMode>
      <I18nextProvider i18n={i18n}>
        <RouterProvider router={router} />
      </I18nextProvider>
    </StrictMode>,
  );
}

void bootstrap();
