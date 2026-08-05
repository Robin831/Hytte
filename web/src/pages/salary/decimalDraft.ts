import { parseDecimal } from '../../utils/parseDecimal'

/**
 * A salary form field held as raw text and parsed on save. Text drafts are what
 * let Norwegian comma decimals ("7,5") reach parseDecimal at all — a native
 * `<input type="number">` reports an empty value for them in Chrome/Edge.
 */
export interface DecimalDraft {
  /** Parsed number — only meaningful when `error` is null. */
  value: number
  /** Why the draft could not be used, or null when it parsed cleanly. */
  error: 'required' | 'invalid' | null
}

/** Parse a required draft: blank and non-numeric text are both errors. */
export function parseRequiredDecimal(raw: string): DecimalDraft {
  if (raw.trim() === '') return { value: 0, error: 'required' }
  const value = parseDecimal(raw)
  return Number.isNaN(value) ? { value: 0, error: 'invalid' } : { value, error: null }
}

/** Parse an optional draft: blank falls back to `fallback`, garbage is an error. */
export function parseOptionalDecimal(raw: string, fallback = 0): DecimalDraft {
  if (raw.trim() === '') return { value: fallback, error: null }
  const value = parseDecimal(raw)
  return Number.isNaN(value) ? { value: 0, error: 'invalid' } : { value, error: null }
}

/**
 * Parse an optional whole-number draft (day counts). Fractions are rejected
 * rather than rounded — the API stores these as integers, so a silent round
 * would be the same class of bug as coercing an unparseable value to 0.
 */
export function parseOptionalInteger(raw: string, fallback = 0): DecimalDraft {
  const draft = parseOptionalDecimal(raw, fallback)
  if (draft.error || Number.isInteger(draft.value)) return draft
  return { value: 0, error: 'invalid' }
}

/**
 * Map parsed drafts to per-field error messages. Returns an empty object when
 * every draft parsed, so callers can gate the save on its size.
 */
export function collectDecimalErrors<K extends string>(
  drafts: Record<K, DecimalDraft>,
  messages: { required: string; invalid: string },
): Partial<Record<K, string>> {
  const errors: Partial<Record<K, string>> = {}
  for (const key of Object.keys(drafts) as K[]) {
    const error = drafts[key].error
    if (error) errors[key] = error === 'required' ? messages.required : messages.invalid
  }
  return errors
}
