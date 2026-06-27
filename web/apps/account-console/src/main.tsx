import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import "./index.css";

import { App } from "./app";
import { initAuth } from "./keycloak";

async function bootstrap() {
  // Redirects to the (Pivox-themed) Keycloak login if not authenticated, then
  // returns here with a token.
  await initAuth();
  const container = document.getElementById("app");
  if (!container) throw new Error("#app mount point missing");
  createRoot(container).render(
    <StrictMode>
      <App />
    </StrictMode>,
  );
}

void bootstrap();
