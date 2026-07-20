import { startTransition } from 'react';
import { hydrateRoot } from 'react-dom/client';
import { StartClient } from '@tanstack/react-start/client';

// TanStack Start's default client entry, without <StrictMode> (its dev
// double-mount double-fires queries on navigation).
startTransition(() => {
  hydrateRoot(document, <StartClient />);
});
