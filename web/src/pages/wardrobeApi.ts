import type { TFunction } from 'i18next'

// ApiError carries the HTTP status and the backend's `{"error": "..."}` message
// so callers can show something more useful than a generic failure notice.
export class ApiError extends Error {
  readonly status: number
  readonly serverMessage: string | null

  constructor(status: number, serverMessage: string | null, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.serverMessage = serverMessage
  }
}

export async function api(path: string, init?: RequestInit): Promise<Response> {
  const res = await fetch(`/api/wardrobe${path}`, {
    credentials: 'include',
    headers: init?.body ? { 'Content-Type': 'application/json' } : undefined,
    ...init,
  })
  if (!res.ok) {
    let serverMessage: string | null = null
    try {
      const body = await res.json()
      if (body && typeof body.error === 'string') serverMessage = body.error
    } catch {
      // Non-JSON body (an HTML error page, an empty 502) — fall back to the generic text.
    }
    throw new ApiError(res.status, serverMessage, `${init?.method ?? 'GET'} ${path} failed`)
  }
  return res
}

// Backend messages (internal/wardrobe/handlers.go) mapped to translation keys.
// Anything not listed here — 5xx internals, "invalid JSON" — falls back to the
// caller's generic key so no raw English reaches the UI.
const SERVER_MESSAGE_KEYS = {
  'name is required': 'errors.server.nameRequired',
  'birthdate must be YYYY-MM-DD': 'errors.server.birthdateFormat',
  'measured_at must be YYYY-MM-DD': 'errors.server.measuredAtFormat',
  'measurement out of range': 'errors.server.measurementOutOfRange',
  'at least one measurement is required': 'errors.server.measurementRequired',
  'quantity must be at most 99': 'errors.server.quantityMax',
  'invalid condition': 'errors.server.invalidCondition',
  'invalid status': 'errors.server.invalidStatus',
  'invalid location': 'errors.server.invalidLocation',
  'invalid season': 'errors.server.invalidSeason',
  'kid_id is required': 'errors.server.kidRequired',
  'kid not found': 'errors.server.kidNotFound',
  'category not found': 'errors.server.categoryNotFound',
  'target_qty must be between 0 and 99': 'errors.server.targetQtyRange',
  'invalid size_system': 'errors.server.invalidSizeSystem',
  'category has items': 'categories.inUse',
} as const

type ServerMessageKey = (typeof SERVER_MESSAGE_KEYS)[keyof typeof SERVER_MESSAGE_KEYS]
type FallbackKey = 'errors.failedToSave' | 'errors.failedToDelete' | 'errors.failedToLoad'
type WardrobeT = TFunction<['wardrobe', 'common']>

// messageFor translates a known backend message, or the generic fallback.
export function messageFor(err: unknown, t: WardrobeT, fallbackKey: FallbackKey): string {
  if (err instanceof ApiError && err.serverMessage) {
    const key: ServerMessageKey | undefined = SERVER_MESSAGE_KEYS[err.serverMessage as keyof typeof SERVER_MESSAGE_KEYS]
    if (key) return t(key)
  }
  return t(fallbackKey)
}
