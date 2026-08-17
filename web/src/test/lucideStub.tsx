/**
 * Shared inert stand-in for `lucide-react`.
 *
 * Suites that render whole pages otherwise have to mock the icon library —
 * importing the real one pulls ~30 MB of modules into every worker. The two
 * ways of doing that by hand both have downsides this Proxy avoids:
 *
 *   - enumerating the icons a page uses breaks the moment someone adds an icon
 *     anywhere in that page's component graph ("No X export is defined on the
 *     mock");
 *   - enumerating `importOriginal()`'s keys tolerates new icons, but still
 *     loads the real module just to read its export names.
 *
 * The trade-off is that `has: () => true` answers for *any* name, so an import
 * of an icon that does not exist in lucide-react no longer fails here; `tsc -b`
 * in the CI build is what catches that.
 *
 * Each icon renders an empty, aria-hidden `<span data-testid="icon-kebab-name">`
 * so tests can assert on which icon rendered (see LactateTests.test.tsx).
 *
 * Use from a test file with:
 *
 *   vi.mock('lucide-react', async () => (await import('../test/lucideStub')).lucideStub)
 *
 * The dynamic import matters: `vi.mock` factories are hoisted above the file's
 * imports, so a top-level `import { lucideStub }` would not be initialised yet.
 */

import type { ReactElement } from 'react'

const testId = (name: string) =>
  `icon-${name.replace(/([a-z0-9])([A-Z])/g, '$1-$2').toLowerCase()}`

// Cache per icon name so the component identity is stable across renders —
// a fresh function each time would remount the icon on every render.
const stubs = new Map<string, () => ReactElement>()

const stubFor = (name: string) => {
  let stub = stubs.get(name)
  if (!stub) {
    const id = testId(name)
    stub = () => <span data-testid={id} aria-hidden="true" />
    stubs.set(name, stub)
  }
  return stub
}

export const lucideStub = new Proxy({} as Record<string, unknown>, {
  get: (_target, prop) => {
    if (prop === '__esModule') return true
    // `then` must stay undefined or the module object looks like a thenable and
    // vitest's `await mock.resolve()` never settles.
    if (prop === 'then' || typeof prop === 'symbol') return undefined
    return stubFor(prop)
  },
  has: () => true,
})
