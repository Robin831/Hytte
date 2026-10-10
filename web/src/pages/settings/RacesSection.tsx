import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { PreferenceSectionProps } from './types'

type RacesSectionProps = Pick<PreferenceSectionProps, 'preferences' | 'saving' | 'savePreference'> & {
  /** The user has the calendar feature; the Google Calendar block is shown then. */
  calendarEnabled?: boolean
}

const TOGGLES = [
  { key: 'races_notify_deadlines', label: 'deadlines' },
  { key: 'races_notify_changes', label: 'changes' },
] as const

interface CalendarInfo {
  id: string
  summary: string
  primary?: boolean
}

function Switch({ checked, label, disabled, onToggle }: { checked: boolean; label: string; disabled?: boolean; onToggle: () => void }) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      onClick={onToggle}
      disabled={disabled}
      className={`relative inline-flex h-6 w-11 shrink-0 items-center rounded-full transition-colors cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed ${
        checked ? 'bg-blue-600' : 'bg-gray-600'
      }`}
    >
      <span className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${checked ? 'translate-x-6' : 'translate-x-1'}`} />
    </button>
  )
}

/** Google Calendar sync for tracked races: on/off, which calendar, sync now. */
function CalendarSync({ preferences, saving, savePreference }: Omit<RacesSectionProps, 'calendarEnabled'>) {
  const { t } = useTranslation('settings')
  const [connected, setConnected] = useState<boolean | null>(null)
  const [calendars, setCalendars] = useState<CalendarInfo[]>([])
  const [result, setResult] = useState('')
  const [syncing, setSyncing] = useState(false)
  const enabled = preferences.races_calendar_sync === 'true'
  const calendarId = preferences.races_calendar_id || 'primary'

  useEffect(() => {
    const controller = new AbortController()
    fetch('/api/calendar/calendars', { credentials: 'include', signal: controller.signal })
      .then(res => (res.ok ? res.json() : { connected: false, calendars: [] }))
      .then((data: { connected?: boolean; calendars?: CalendarInfo[] }) => {
        setConnected(data.connected !== false)
        setCalendars(data.calendars ?? [])
      })
      .catch(() => { if (!controller.signal.aborted) setConnected(false) })
    return () => controller.abort()
  }, [])

  const syncNow = async () => {
    setSyncing(true)
    setResult('')
    try {
      const res = await fetch('/api/races/calendar/sync', { method: 'POST', credentials: 'include' })
      const data = await res.json().catch(() => ({}))
      if (!res.ok) throw new Error()
      const r = data.result ?? {}
      setResult(t('races.calendarResult', { created: r.created ?? 0, updated: r.updated ?? 0, deleted: r.deleted ?? 0 }))
    } catch {
      setResult(t('races.calendarFailed'))
    } finally {
      setSyncing(false)
    }
  }

  if (connected === false) {
    return <p className="text-sm text-gray-400">{t('races.calendarNotConnected')}</p>
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between gap-4">
        <div>
          <p className="font-medium">{t('races.calendarTitle')}</p>
          <p className="text-sm text-gray-400">{t('races.calendarDescription')}</p>
        </div>
        <Switch
          checked={enabled}
          label={t('races.calendarTitle')}
          disabled={saving || connected === null}
          onToggle={async () => {
            await savePreference('races_calendar_sync', enabled ? 'false' : 'true')
            await syncNow()
          }}
        />
      </div>
      {enabled && (
        <div className="flex flex-wrap items-end gap-3">
          <div>
            <label htmlFor="races-calendar" className="mb-1 block text-xs font-semibold uppercase tracking-wide text-gray-400">{t('races.calendarWhich')}</label>
            <select
              id="races-calendar"
              value={calendarId}
              disabled={saving}
              onChange={async e => {
                await savePreference('races_calendar_id', e.target.value)
                await syncNow()
              }}
              className="rounded-lg border border-gray-700 bg-gray-800 px-3 py-2 text-sm"
            >
              {!calendars.some(c => c.id === calendarId) && <option value={calendarId}>{calendarId === 'primary' ? t('races.calendarPrimary') : calendarId}</option>}
              {calendars.map(c => <option key={c.id} value={c.id}>{c.primary ? t('races.calendarPrimary') : c.summary}</option>)}
            </select>
          </div>
          <button type="button" onClick={syncNow} disabled={syncing} className="rounded-lg bg-gray-700 px-3 py-2 text-sm hover:bg-gray-600 disabled:opacity-50 cursor-pointer">
            {t('races.calendarSyncNow')}
          </button>
        </div>
      )}
      {result && <p role="status" className="text-sm text-gray-400">{result}</p>}
    </div>
  )
}

// Push toggles for tracked races (on unless exactly 'false', matching the
// server-side notifier — tracking a race is the opt-in), Google Calendar
// sync, and the athlete profile used for qualifying-time checks.
function RacesSection({ preferences, saving, savePreference, calendarEnabled }: RacesSectionProps) {
  const { t } = useTranslation('settings')
  const [birthYear, setBirthYear] = useState(preferences.athlete_birth_year ?? '')

  return (
    <div className="space-y-4">
      {TOGGLES.map(({ key, label }) => {
        const enabled = preferences[key] !== 'false'
        return (
          <div key={key} className="flex items-center justify-between gap-4">
            <div>
              <p className="font-medium">{t(`races.${label}Title`)}</p>
              <p className="text-sm text-gray-400">{t(`races.${label}Description`)}</p>
            </div>
            <Switch
              checked={enabled}
              label={t(`races.${label}Title`)}
              disabled={saving}
              onToggle={async () => { await savePreference(key, enabled ? 'false' : 'true') }}
            />
          </div>
        )
      })}
      <p className="text-xs text-gray-500">{t('races.pushHint')}</p>

      {calendarEnabled && (
        <div className="border-t border-gray-700 pt-4">
          <CalendarSync preferences={preferences} saving={saving} savePreference={savePreference} />
        </div>
      )}

      <div className="border-t border-gray-700 pt-4">
        <p className="font-medium">{t('races.athleteTitle')}</p>
        <p className="text-sm text-gray-400">{t('races.athleteDescription')}</p>
        <div className="mt-2 flex flex-wrap gap-3">
          <div>
            <label htmlFor="athlete-birth-year" className="mb-1 block text-xs font-semibold uppercase tracking-wide text-gray-400">{t('races.birthYear')}</label>
            <input
              id="athlete-birth-year"
              inputMode="numeric"
              value={birthYear}
              onChange={e => setBirthYear(e.target.value.replace(/\D/g, '').slice(0, 4))}
              onBlur={async () => {
                if (birthYear === (preferences.athlete_birth_year ?? '')) return
                if (birthYear === '' || birthYear.length === 4) await savePreference('athlete_birth_year', birthYear)
              }}
              placeholder="1985"
              className="w-24 rounded-lg border border-gray-700 bg-gray-800 px-3 py-2 text-sm tabular-nums"
            />
          </div>
          <div>
            <label htmlFor="athlete-sex" className="mb-1 block text-xs font-semibold uppercase tracking-wide text-gray-400">{t('races.sex')}</label>
            <select
              id="athlete-sex"
              value={preferences.athlete_sex ?? ''}
              disabled={saving}
              onChange={e => savePreference('athlete_sex', e.target.value)}
              className="rounded-lg border border-gray-700 bg-gray-800 px-3 py-2 text-sm"
            >
              <option value="">—</option>
              <option value="female">{t('races.female')}</option>
              <option value="male">{t('races.male')}</option>
            </select>
          </div>
        </div>
      </div>
    </div>
  )
}

export default RacesSection
