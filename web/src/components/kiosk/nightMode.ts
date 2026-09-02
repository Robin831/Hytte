// Night mode and burn-in pixel shift for the kiosk screen.
//
// Both are pure functions of the current time (plus, for night mode, the
// payload's sun times and the token's dim overrides), so KioskPage can
// re-evaluate them on its existing clock tick — no extra timer, no reload.
// They live outside the page component so the module only exports helpers,
// which keeps react-refresh happy and lets tests import them directly.

// Sun times as sent by /api/kiosk/data. `kind` is 'normal', 'polarDay' or
// 'polarNight'; sunrise/sunset are absent for the polar kinds and for a token
// without a location.
export interface SunTimes {
  kind: string
  sunrise?: string
  sunset?: string
}

// Optional night-mode overrides carried by the kiosk token config. Mirrors
// kiosk.DimConfig on the backend: every field is optional.
//
// `enabled: false` switches night mode off entirely; `enabled: true` is a
// no-op that keeps the sun-driven default (so a token with `dim: true`, no
// window and no location never dims — there is nothing to drive it).
//
// The backend only ever sends start/end together as a complete local "HH:MM"
// window, but a hand-edited or half-migrated config can still arrive with one
// edge missing or an unparsable value; see isNightMode for how that is
// handled.
export interface DimConfig {
  enabled?: boolean
  start?: string
  end?: string
}


// Convert a local "H:MM"/"HH:MM" string or an RFC3339 timestamp to minutes
// since local midnight. A single-digit hour is accepted, so a hand-written
// "7:30" behaves the same as "07:30". Returns null for anything unparsable so a
// malformed override can never wedge the kiosk into (or out of) night mode.
function minutesOfDay(value: string): number | null {
  const hhmm = /^(\d{1,2}):(\d{2})$/.exec(value)
  if (hhmm) {
    const hours = Number(hhmm[1])
    const minutes = Number(hhmm[2])
    if (hours > 23 || minutes > 59) return null
    return hours * 60 + minutes
  }
  const parsed = new Date(value)
  const ms = parsed.getTime()
  if (isNaN(ms)) return null
  return parsed.getHours() * 60 + parsed.getMinutes()
}

// Half-open [start, end) membership over minutes-of-day, handling windows that
// cross midnight (start > end). A zero-length window matches nothing.
function inWindow(minute: number, start: number, end: number): boolean {
  if (start === end) return false
  if (start < end) return minute >= start && minute < end
  return minute >= start || minute < end
}

// Warn once per distinct value when a dim window is configured but unusable —
// one edge missing, or a value we cannot parse. Without this the kiosk silently
// falls back to the sun window, which on a wall-mounted screen looks exactly
// like "night mode ignored my settings" with nothing anywhere to explain it.
// Deduped because isNightMode runs on every clock tick.
const warnedDimWindows = new Set<string>()

function warnUnusableDimWindow(dim: DimConfig): void {
  const key = `${dim.start ?? ''}|${dim.end ?? ''}`
  if (warnedDimWindows.has(key)) return
  warnedDimWindows.add(key)
  console.warn(
    '[kiosk] Ignoring the dim window override ' +
      `(start=${JSON.stringify(dim.start ?? null)}, end=${JSON.stringify(dim.end ?? null)}): ` +
      'both edges are required and must be a local "H:MM" time. ' +
      'Falling back to the sun-driven night window.',
  )
}

// Precedence, highest first:
//   1. dim.enabled === false — night mode is switched off entirely.
//      (dim.enabled === true is a no-op: it keeps the sun-driven default.)
//   2. An explicit dim.start/dim.end window replaces the sun window. A window
//      that is half-configured or unparsable is dropped — with a console
//      warning — rather than wedging the screen on or off.
//   3. polarDay is always day, polarNight is always night.
//   4. Sun-driven: night between sunset and sunrise.
//   5. No sun times and no window (a token without a location) — always day.
export function isNightMode(
  now: Date,
  sun?: SunTimes | null,
  dim?: DimConfig | null,
): boolean {
  if (dim?.enabled === false) return false

  const minute = now.getHours() * 60 + now.getMinutes()

  const overrideStart = dim?.start ? minutesOfDay(dim.start) : null
  const overrideEnd = dim?.end ? minutesOfDay(dim.end) : null
  if (overrideStart !== null && overrideEnd !== null) {
    return inWindow(minute, overrideStart, overrideEnd)
  }
  if (dim && (dim.start || dim.end)) warnUnusableDimWindow(dim)

  if (sun?.kind === 'polarDay') return false
  if (sun?.kind === 'polarNight') return true

  const sunset = sun?.sunset ? minutesOfDay(sun.sunset) : null
  const sunrise = sun?.sunrise ? minutesOfDay(sun.sunrise) : null
  if (sunset !== null && sunrise !== null) return inWindow(minute, sunset, sunrise)

  return false
}

// ── Pixel shift (burn-in mitigation) ─────────────────────────────────────────
// A wall-mounted screen shows the same clock in the same place all day, which
// burns static elements into OLED/LCD panels. Nudging the whole layout by a few
// pixels every few minutes spreads that wear without being visible to a viewer.

// Largest offset applied on either axis, in CSS pixels.
export const PIXEL_SHIFT_MAX_PX = 3
// Distinct offsets per axis: 0 … PIXEL_SHIFT_MAX_PX.
const PIXEL_SHIFT_STEPS = PIXEL_SHIFT_MAX_PX + 1
// How long each offset is held. With 4 steps per axis the full 4x4 lattice is
// visited every 7 * 16 = 112 minutes.
const PIXEL_SHIFT_HOLD_MINUTES = 7

// Offset for the given local time. Pure function of minutes-of-day, so like
// night mode it rides the existing clock tick.
export function pixelShift(now: Date): { x: number; y: number } {
  const minute = now.getHours() * 60 + now.getMinutes()
  const step = Math.floor(minute / PIXEL_SHIFT_HOLD_MINUTES)
  return {
    x: step % PIXEL_SHIFT_STEPS,
    y: Math.floor(step / PIXEL_SHIFT_STEPS) % PIXEL_SHIFT_STEPS,
  }
}
