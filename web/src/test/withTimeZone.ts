import { beforeAll, afterAll, vi } from 'vitest'

/**
 * Pins the TZ env var for the enclosing describe block, then restores only TZ
 * afterwards so other env stubs in the file are left alone.
 */
export function withTimeZone(tz: string) {
  let prev: string | undefined
  beforeAll(() => {
    prev = import.meta.env.TZ
    vi.stubEnv('TZ', tz)
  })
  afterAll(() => { vi.stubEnv('TZ', prev) })
}
