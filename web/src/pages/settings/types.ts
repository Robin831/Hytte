// Shared types and helpers for the Settings page sections.

export interface HetznerTokenState {
  configured: boolean
  masked: string
}

export interface PushDevice {
  id: number
  endpoint: string
  created_at: string
}

export interface SessionInfo {
  id: string
  created_at: string
  expires_at: string
  current: boolean
}

export interface EventTypeInfo {
  key: string
  label: string
  description: string
}

export interface AIPrompt {
  key: string
  body: string
  default_prompt: string
  is_default: boolean
  updated_at: string
}

// Convert a sec/km integer string to "m:ss" display format.
export function secToMMSS(secStr: string): string {
  const sec = parseInt(secStr)
  if (isNaN(sec) || sec <= 0) return ''
  return `${Math.floor(sec / 60)}:${String(sec % 60).padStart(2, '0')}`
}

// Parse "m:ss" or "mm:ss" string back to sec/km integer string, or '' if invalid.
export function mmssToSec(pace: string): string {
  const parts = pace.trim().split(':')
  if (parts.length !== 2) return ''
  const mins = parseInt(parts[0])
  const secs = parseInt(parts[1])
  if (isNaN(mins) || isNaN(secs) || mins < 0 || secs < 0 || secs >= 60) return ''
  const total = mins * 60 + secs
  if (total < 120 || total > 1200) return '' // 2:00 – 20:00 per km range
  return String(total)
}

// Validate HH:MM:SS target time format.
export function isValidTargetTime(s: string): boolean {
  const trimmed = s.trim()
  const match = /^(\d+):(\d{1,2}):(\d{1,2})$/.exec(trimmed)
  if (!match) return false
  const h = Number(match[1])
  const m = Number(match[2])
  const sec = Number(match[3])
  return !Number.isNaN(h) && !Number.isNaN(m) && !Number.isNaN(sec) && h >= 0 && m >= 0 && m < 60 && sec >= 0 && sec < 60
}

// Olympiatoppen 5-zone model as percentages of max HR (matches backend hrzones package).
export const DEFAULT_ZONE_PCTS = [
  { minPct: 0.00, maxPct: 0.60 },
  { minPct: 0.60, maxPct: 0.72 },
  { minPct: 0.72, maxPct: 0.82 },
  { minPct: 0.82, maxPct: 0.92 },
  { minPct: 0.92, maxPct: 1.00 },
]

export const ZONE_NAME_KEYS = ['zoneName1', 'zoneName2', 'zoneName3', 'zoneName4', 'zoneName5']

export function computeDefaultZoneDrafts(maxHR: number): Array<{ min: string; max: string }> {
  return DEFAULT_ZONE_PCTS.map((p) => ({
    min: String(Math.round(maxHR * p.minPct)),
    max: String(Math.round(maxHR * p.maxPct)),
  }))
}

// Shared props passed from the Settings orchestrator to preference-editing sections.
export interface PreferenceSectionProps {
  preferences: Record<string, string>
  saving: boolean
  savePreference: (key: string, value: string, toast?: boolean) => Promise<void>
  savePreferences: (prefs: Record<string, string>, toast?: boolean) => Promise<void>
}
