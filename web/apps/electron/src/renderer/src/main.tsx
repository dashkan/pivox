import './assets/main.css'
import { ensureFirebase } from './lib/firebase'

import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { RouterProvider, createHashHistory, createRouter } from '@tanstack/react-router'

import { routeTree } from './routeTree.gen'

ensureFirebase()
const hashHistory = createHashHistory()

const router = createRouter({
  routeTree,
  history: hashHistory
})

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <RouterProvider router={router} />
  </StrictMode>
)
