import type { ReactElement } from 'react'
import { render } from '@testing-library/react'
import { createMemoryRouter, RouterProvider, type RouteObject } from 'react-router'

interface Options {
  /** Path the element is mounted at, and the router's initial entry. */
  path?: string
  /** Additional routes, so a test can navigate away from the page under test. */
  extraRoutes?: RouteObject[]
}

/**
 * Renders `ui` under a data router. Required by hooks that only work with a
 * data router — notably `useBlocker`, which Notes uses to guard unsaved edits.
 * Returns the router alongside the usual RTL result so tests can drive and
 * assert on navigation.
 *
 * The page mounts under a `path: '*'` splat route to match the production
 * router shape in main.tsx, where App handles all paths via a single splat.
 * Extra routes are siblings so tests can navigate away from the page under test.
 */
export function renderWithDataRouter(ui: ReactElement, { path = '/', extraRoutes = [] }: Options = {}) {
  const router = createMemoryRouter([{ path: '*', element: ui }, ...extraRoutes], {
    initialEntries: [path],
  })
  return { router, ...render(<RouterProvider router={router} />) }
}
