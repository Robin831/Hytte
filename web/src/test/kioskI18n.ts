import enKiosk from '../../public/locales/en/kiosk.json'

// Translation helper for the KioskPage suites. Kept in its own module (free of
// React and of any KioskPage import) so a `vi.mock('react-i18next', ...)`
// factory can import it without pulling the component under test back through
// the mock that is still being registered.

type JsonValue = string | number | boolean | null | JsonObject | JsonValue[]
interface JsonObject { [key: string]: JsonValue }

function resolveKey(obj: JsonObject, parts: string[]): JsonValue | undefined {
  const [head, ...rest] = parts
  const val = obj[head]
  if (rest.length === 0) return val
  if (val && typeof val === 'object' && !Array.isArray(val)) {
    return resolveKey(val as JsonObject, rest)
  }
  return undefined
}

// Resolves against the shipped en/kiosk.json and deliberately ignores the
// inline `defaultValue` KioskPage passes, so a renamed, mis-nested or dropped
// key fails the copy assertions instead of silently falling back to the
// hardcoded English default.
//
// Defined once so it stays referentially stable across renders like the real
// react-i18next `t` — components may use it in effect dependency arrays.
export const kioskT = (key: string): string => {
  const val = resolveKey(enKiosk as unknown as JsonObject, key.split('.'))
  return typeof val === 'string' ? val : key
}
