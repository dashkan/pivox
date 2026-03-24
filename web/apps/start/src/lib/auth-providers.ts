import type { PivoxAuthProvider } from '@pivox/ui/auth';

export type { PivoxAuthProvider };

const defaultProviders: Array<PivoxAuthProvider> = ['google.com', 'github.com'];

const envProviders = import.meta.env.VITE_AUTH_PROVIDERS;

export const authProviders: Array<PivoxAuthProvider> = envProviders
  ? (envProviders.split(',') as Array<PivoxAuthProvider>)
  : defaultProviders;
