// whatwg-fetch polyfills the fetch Web API for Android 5 / old Firefox.
// Must be the first import so the polyfill runs before i18next-http-backend
// attempts its first fetch() call to load locale JSON files.
import 'whatwg-fetch'
import { StrictMode, Suspense } from 'react'
import { createRoot } from 'react-dom/client'
import { createBrowserRouter, RouterProvider } from 'react-router-dom'
import './i18n'
import './index.css'
import App from './App.tsx'
import { AuthProvider } from './auth.tsx'

// A data router (rather than <BrowserRouter>) is required for `useBlocker`,
// which pages such as Notes use to guard unsaved edits against navigation.
// The single splat route keeps every existing <Route> declaration in App.tsx
// working as descendant routes — blocking is router-global either way.
const router = createBrowserRouter([{ path: '*', element: <App /> }])

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <Suspense fallback={<div className="flex items-center justify-center min-h-screen bg-gray-900" />}>
      <AuthProvider>
        <RouterProvider router={router} />
      </AuthProvider>
    </Suspense>
  </StrictMode>,
)
