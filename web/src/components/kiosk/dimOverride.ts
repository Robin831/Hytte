// Helpers for the per-token night-mode override ("dim", "dim_start",
// "dim_end") edited in the kiosk token admin UI. The keys and their meaning
// mirror kiosk.buildDimConfig on the backend:
//   - "dim" absent: follow the sun (default); true: enabled; false: disabled.
//   - "dim_start"/"dim_end": a zero-padded local "HH:MM" window that replaces
//     the sun window. Only honoured when both are set.

export type DimMode = 'auto' | 'on' | 'off'

export interface DimOverride {
  mode: DimMode
  start: string
  end: string
}

export const EMPTY_DIM_OVERRIDE: DimOverride = { mode: 'auto', start: '', end: '' }

// Same shape kiosk.parseHHMM accepts: fixed width, hour 00-23, minute 00-59.
const HHMM_RE = /^([01]\d|2[0-3]):[0-5]\d$/

export function isValidHHMM(s: string): boolean {
  return HHMM_RE.test(s)
}

export type DimOverrideError = 'invalidTime' | 'windowIncomplete' | null

// Validates the form values before submit. Empty fields are allowed, but the
// window needs both edges — the backend ignores a half-configured window.
export function validateDimOverride(dim: DimOverride): DimOverrideError {
  const start = dim.start.trim()
  const end = dim.end.trim()
  if ((start && !isValidHHMM(start)) || (end && !isValidHHMM(end))) return 'invalidTime'
  if (!start !== !end) return 'windowIncomplete'
  return null
}

// Reads the override from a token config, falling back to defaults for
// missing or wrongly-typed values (the same values the backend ignores).
export function dimOverrideFromConfig(config: unknown): DimOverride {
  if (!config || typeof config !== 'object' || Array.isArray(config)) return EMPTY_DIM_OVERRIDE
  const cfg = config as Record<string, unknown>
  const mode: DimMode = cfg.dim === true ? 'on' : cfg.dim === false ? 'off' : 'auto'
  const start = typeof cfg.dim_start === 'string' ? cfg.dim_start : ''
  const end = typeof cfg.dim_end === 'string' ? cfg.dim_end : ''
  return { mode, start, end }
}

// Request body for PUT /api/kiosk/tokens/{id}/dim: null clears "dim", empty
// strings clear the window.
export function dimOverrideToRequest(dim: DimOverride): {
  dim: boolean | null
  dim_start: string
  dim_end: string
} {
  return {
    dim: dim.mode === 'auto' ? null : dim.mode === 'on',
    dim_start: dim.start.trim(),
    dim_end: dim.end.trim(),
  }
}

// Writes the override into a config object being built for token creation,
// omitting keys that represent the default.
export function applyDimOverride(config: Record<string, unknown>, dim: DimOverride): void {
  if (dim.mode !== 'auto') config.dim = dim.mode === 'on'
  const start = dim.start.trim()
  const end = dim.end.trim()
  if (start && end) {
    config.dim_start = start
    config.dim_end = end
  }
}
