import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { Trash2, Plus, Trophy, Zap } from 'lucide-react'
import { formatDate } from '../utils/formatDate'

interface Race {
  id: number
  user_id: number
  name: string
  date: string
  distance_m: number
  target_time: number | null
  priority: 'A' | 'B' | 'C'
  notes: string
  result_time: number | null
  created_at: string
}

interface Note {
  id: number
  user_id: number
  plan_id: number | null
  content: string
  created_at: string
}

function formatDistance(meters: number): string {
  if (meters >= 1000) {
    return `${(meters / 1000).toFixed(1)} km`
  }
  return `${meters} m`
}

function formatDuration(seconds: number | null): string {
  if (seconds === null) return '—'
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const s = seconds % 60
  if (h > 0) return `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
  return `${m}:${String(s).padStart(2, '0')}`
}

function priorityLabel(priority: string): { label: string; class: string } {
  switch (priority) {
    case 'A':
      return { label: 'A', class: 'bg-yellow-500/20 text-yellow-400 border border-yellow-500/30' }
    case 'B':
      return { label: 'B', class: 'bg-blue-500/20 text-blue-400 border border-blue-500/30' }
    case 'C':
      return { label: 'C', class: 'bg-gray-500/20 text-gray-400 border border-gray-500/30' }
    default:
      return { label: priority, class: 'bg-gray-500/20 text-gray-400' }
  }
}

function weeksUntil(dateStr: string): number {
  const target = new Date(`${dateStr}T00:00:00`)
  const now = new Date()
  const diff = target.getTime() - now.getTime()
  return Math.ceil(diff / (7 * 24 * 60 * 60 * 1000))
}

export default function StridePage() {
  const { t } = useTranslation('stride')

  const [races, setRaces] = useState<Race[]>([])
  const [notes, setNotes] = useState<Note[]>([])
  const [racesLoading, setRacesLoading] = useState(true)
  const [notesLoading, setNotesLoading] = useState(true)

  // Race form state
  const [showRaceForm, setShowRaceForm] = useState(false)
  const [raceName, setRaceName] = useState('')
  const [raceDate, setRaceDate] = useState('')
  const [raceDistanceKm, setRaceDistanceKm] = useState('')
  const [raceTargetTime, setRaceTargetTime] = useState('')
  const [racePriority, setRacePriority] = useState<'A' | 'B' | 'C'>('B')
  const [raceNotes, setRaceNotes] = useState('')
  const [raceSubmitting, setRaceSubmitting] = useState(false)
  const [raceError, setRaceError] = useState('')

  // Note form state
  const [noteContent, setNoteContent] = useState('')
  const [noteSubmitting, setNoteSubmitting] = useState(false)

  const loadRaces = useCallback(async (signal?: AbortSignal) => {
    try {
      const res = await fetch('/api/stride/races', { credentials: 'include', signal })
      if (!res.ok) {
        throw new Error(`Failed to load races: ${res.status} ${res.statusText}`)
      }
      const data = await res.json()
      if (!signal?.aborted) {
        setRaces(data.races ?? [])
      }
    } catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') return
      console.error('Failed to load races', error)
    } finally {
      if (!signal?.aborted) {
        setRacesLoading(false)
      }
    }
  }, [])

  const loadNotes = useCallback(async (signal?: AbortSignal) => {
    try {
      const res = await fetch('/api/stride/notes', { credentials: 'include', signal })
      if (!res.ok) {
        throw new Error(`Failed to load notes: ${res.status} ${res.statusText}`)
      }
      const data = await res.json()
      if (!signal?.aborted) {
        setNotes(data.notes ?? [])
      }
    } catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') return
      console.error('Failed to load notes', error)
    } finally {
      if (!signal?.aborted) {
        setNotesLoading(false)
      }
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    // eslint-disable-next-line react-hooks/set-state-in-effect -- async data fetch; AbortController prevents stale updates on unmount
    loadRaces(controller.signal)
    loadNotes(controller.signal)
    return () => { controller.abort() }
  }, [loadRaces, loadNotes])

  // Parse "H:MM:SS" or "M:SS" target time string to seconds
  function parseTargetTime(s: string): number | null {
    if (!s.trim()) return null
    const parts = s.trim().split(':').map(Number)
    if (parts.some(isNaN)) return null
    if (parts.length === 3) return parts[0] * 3600 + parts[1] * 60 + parts[2]
    if (parts.length === 2) return parts[0] * 60 + parts[1]
    return null
  }

  async function handleCreateRace(e: React.FormEvent) {
    e.preventDefault()
    setRaceError('')
    setRaceSubmitting(true)
    try {
      const distanceM = parseFloat(raceDistanceKm) * 1000
      if (isNaN(distanceM) || distanceM <= 0) {
        setRaceError(t('races.form.error.invalidDistance'))
        return
      }

      const targetTime = parseTargetTime(raceTargetTime)
      if (raceTargetTime.trim() !== '' && targetTime === null) {
        setRaceError(t('races.form.error.invalidTargetTime'))
        return
      }

      const payload = {
        name: raceName,
        date: raceDate,
        distance_m: distanceM,
        target_time: targetTime,
        priority: racePriority,
        notes: raceNotes,
      }

      const res = await fetch('/api/stride/races', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      })

      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        setRaceError(data.error ?? t('races.form.error.create'))
        return
      }

      setRaceName('')
      setRaceDate('')
      setRaceDistanceKm('')
      setRaceTargetTime('')
      setRacePriority('B')
      setRaceNotes('')
      setShowRaceForm(false)
      await loadRaces()
    } catch {
      setRaceError(t('races.form.error.create'))
    } finally {
      setRaceSubmitting(false)
    }
  }

  async function handleDeleteRace(id: number) {
    try {
      const res = await fetch(`/api/stride/races/${id}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        setRaceError(data.error ?? t('races.form.error.delete'))
        return
      }
      setRaces(prev => prev.filter(r => r.id !== id))
    } catch {
      setRaceError(t('races.form.error.delete'))
    }
  }

  async function handleCreateNote(e: React.FormEvent) {
    e.preventDefault()
    if (!noteContent.trim()) return
    setNoteSubmitting(true)
    try {
      const res = await fetch('/api/stride/notes', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content: noteContent }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        console.error('Failed to create note', data.error ?? res.statusText)
        return
      }
      setNoteContent('')
      await loadNotes()
    } catch (error) {
      console.error('Failed to create note', error)
    } finally {
      setNoteSubmitting(false)
    }
  }

  async function handleDeleteNote(id: number) {
    try {
      const res = await fetch(`/api/stride/notes/${id}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        console.error('Failed to delete note', data.error)
        return
      }
      setNotes(prev => prev.filter(n => n.id !== id))
    } catch (error) {
      console.error('Failed to delete note', error)
    }
  }

  const now = new Date()
  const today = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`
  const upcomingRaces = races.filter(r => r.date >= today)
  const pastRaces = races.filter(r => r.date < today)

  return (
    <div className="max-w-2xl mx-auto px-4 py-6 space-y-8">
      {/* Header */}
      <div className="flex items-center gap-3">
        <Zap size={28} className="text-yellow-400" />
        <div>
          <h1 className="text-2xl font-bold text-white">{t('title')}</h1>
          <p className="text-sm text-gray-400">{t('subtitle')}</p>
        </div>
      </div>

      {/* Race Calendar */}
      <section>
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-lg font-semibold text-white flex items-center gap-2">
            <Trophy size={18} className="text-yellow-400" />
            {t('races.title')}
          </h2>
          <button
            onClick={() => setShowRaceForm(v => !v)}
            className="flex items-center gap-1.5 px-3 py-1.5 text-sm bg-blue-600 hover:bg-blue-700 text-white rounded-lg transition-colors"
          >
            <Plus size={14} />
            {t('races.add')}
          </button>
        </div>

        {/* Race form */}
        {showRaceForm && (
          <form onSubmit={handleCreateRace} className="mb-4 p-4 bg-gray-800 rounded-xl border border-gray-700 space-y-3">
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <div>
                <label htmlFor="race-name" className="block text-xs text-gray-400 mb-1">{t('races.form.name')}</label>
                <input
                  id="race-name"
                  type="text"
                  value={raceName}
                  onChange={e => setRaceName(e.target.value)}
                  required
                  className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white placeholder-gray-400 focus:outline-none focus:border-blue-500"
                  placeholder={t('races.form.namePlaceholder')}
                />
              </div>
              <div>
                <label htmlFor="race-date" className="block text-xs text-gray-400 mb-1">{t('races.form.date')}</label>
                <input
                  id="race-date"
                  type="date"
                  value={raceDate}
                  onChange={e => setRaceDate(e.target.value)}
                  required
                  className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500"
                />
              </div>
              <div>
                <label htmlFor="race-distance" className="block text-xs text-gray-400 mb-1">{t('races.form.distance')}</label>
                <input
                  id="race-distance"
                  type="number"
                  step="0.001"
                  min="0.001"
                  value={raceDistanceKm}
                  onChange={e => setRaceDistanceKm(e.target.value)}
                  required
                  className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white placeholder-gray-400 focus:outline-none focus:border-blue-500"
                  placeholder="42.195"
                />
              </div>
              <div>
                <label htmlFor="race-target" className="block text-xs text-gray-400 mb-1">{t('races.form.targetTime')}</label>
                <input
                  id="race-target"
                  type="text"
                  value={raceTargetTime}
                  onChange={e => setRaceTargetTime(e.target.value)}
                  className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white placeholder-gray-400 focus:outline-none focus:border-blue-500"
                  placeholder="3:30:00"
                />
              </div>
              <div>
                <label htmlFor="race-priority" className="block text-xs text-gray-400 mb-1">{t('races.form.priority')}</label>
                <select
                  id="race-priority"
                  value={racePriority}
                  onChange={e => setRacePriority(e.target.value as 'A' | 'B' | 'C')}
                  className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500"
                >
                  <option value="A">{t('races.form.priorityA')}</option>
                  <option value="B">{t('races.form.priorityB')}</option>
                  <option value="C">{t('races.form.priorityC')}</option>
                </select>
              </div>
            </div>
            <div>
              <label htmlFor="race-notes" className="block text-xs text-gray-400 mb-1">{t('races.form.notes')}</label>
              <input
                id="race-notes"
                type="text"
                value={raceNotes}
                onChange={e => setRaceNotes(e.target.value)}
                className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white placeholder-gray-400 focus:outline-none focus:border-blue-500"
                placeholder={t('races.form.notesPlaceholder')}
              />
            </div>
            {raceError && <p className="text-sm text-red-400">{raceError}</p>}
            <div className="flex gap-2">
              <button
                type="submit"
                disabled={raceSubmitting}
                className="px-4 py-2 text-sm bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white rounded-lg transition-colors"
              >
                {raceSubmitting ? t('races.form.saving') : t('races.form.save')}
              </button>
              <button
                type="button"
                onClick={() => { setShowRaceForm(false); setRaceError('') }}
                className="px-4 py-2 text-sm bg-gray-700 hover:bg-gray-600 text-white rounded-lg transition-colors"
              >
                {t('races.form.cancel')}
              </button>
            </div>
          </form>
        )}

        {racesLoading ? (
          <p className="text-sm text-gray-400">{t('loading')}</p>
        ) : upcomingRaces.length === 0 ? (
          <p className="text-sm text-gray-500">{t('races.empty')}</p>
        ) : (
          <div className="space-y-2">
            {upcomingRaces.map(race => {
              const weeks = weeksUntil(race.date)
              const p = priorityLabel(race.priority)
              return (
                <div key={race.id} className="flex items-center gap-3 p-3 bg-gray-800 rounded-xl border border-gray-700 group">
                  <span className={`text-xs font-semibold px-2 py-0.5 rounded-full ${p.class}`}>{p.label}</span>
                  <div className="flex-1 min-w-0">
                    <p className="text-sm font-medium text-white truncate">{race.name}</p>
                    <p className="text-xs text-gray-400">
                      {formatDate(`${race.date}T00:00:00`, { dateStyle: 'medium' })}
                      {' · '}
                      {formatDistance(race.distance_m)}
                      {race.target_time != null && ` · ${formatDuration(race.target_time)}`}
                      {weeks > 0 && ` · ${t('races.weeksAway', { count: weeks })}`}
                    </p>
                  </div>
                  <button
                    onClick={() => handleDeleteRace(race.id)}
                    className="opacity-0 group-hover:opacity-100 p-1.5 text-gray-500 hover:text-red-400 transition-all"
                    aria-label={t('races.delete')}
                  >
                    <Trash2 size={14} />
                  </button>
                </div>
              )
            })}
          </div>
        )}

        {/* Past races */}
        {pastRaces.length > 0 && (
          <details className="mt-4">
            <summary className="text-sm text-gray-500 cursor-pointer hover:text-gray-300">{t('races.past', { count: pastRaces.length })}</summary>
            <div className="mt-2 space-y-2">
              {pastRaces.map(race => {
                const p = priorityLabel(race.priority)
                return (
                  <div key={race.id} className="flex items-center gap-3 p-3 bg-gray-800/50 rounded-xl border border-gray-700/50 group opacity-60">
                    <span className={`text-xs font-semibold px-2 py-0.5 rounded-full ${p.class}`}>{p.label}</span>
                    <div className="flex-1 min-w-0">
                      <p className="text-sm font-medium text-white truncate">{race.name}</p>
                      <p className="text-xs text-gray-400">
                        {formatDate(`${race.date}T00:00:00`, { dateStyle: 'medium' })}
                        {' · '}
                        {formatDistance(race.distance_m)}
                        {race.result_time != null && ` · ${t('races.result')}: ${formatDuration(race.result_time)}`}
                      </p>
                    </div>
                    <button
                      onClick={() => handleDeleteRace(race.id)}
                      className="opacity-0 group-hover:opacity-100 p-1.5 text-gray-500 hover:text-red-400 transition-all"
                      aria-label={t('races.delete')}
                    >
                      <Trash2 size={14} />
                    </button>
                  </div>
                )
              })}
            </div>
          </details>
        )}
      </section>

      {/* Coach Notes */}
      <section>
        <h2 className="text-lg font-semibold text-white mb-4">{t('notes.title')}</h2>
        <form onSubmit={handleCreateNote} className="mb-4">
          <textarea
            value={noteContent}
            onChange={e => setNoteContent(e.target.value)}
            placeholder={t('notes.placeholder')}
            aria-label={t('notes.title')}
            rows={3}
            className="w-full bg-gray-800 border border-gray-700 rounded-xl px-4 py-3 text-sm text-white placeholder-gray-500 focus:outline-none focus:border-blue-500 resize-none"
          />
          <div className="mt-2 flex justify-end">
            <button
              type="submit"
              disabled={noteSubmitting || !noteContent.trim()}
              className="px-4 py-2 text-sm bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white rounded-lg transition-colors"
            >
              {noteSubmitting ? t('notes.saving') : t('notes.add')}
            </button>
          </div>
        </form>

        {notesLoading ? (
          <p className="text-sm text-gray-400">{t('loading')}</p>
        ) : notes.length === 0 ? (
          <p className="text-sm text-gray-500">{t('notes.empty')}</p>
        ) : (
          <div className="space-y-2">
            {notes.map(note => (
              <div key={note.id} className="flex items-start gap-3 p-3 bg-gray-800 rounded-xl border border-gray-700 group">
                <p className="flex-1 text-sm text-gray-200 whitespace-pre-wrap">{note.content}</p>
                <div className="flex-shrink-0 flex flex-col items-end gap-1">
                  <button
                    onClick={() => handleDeleteNote(note.id)}
                    className="opacity-0 group-hover:opacity-100 p-1.5 text-gray-500 hover:text-red-400 transition-all"
                    aria-label={t('notes.delete')}
                  >
                    <Trash2 size={14} />
                  </button>
                  <span className="text-xs text-gray-500">
                    {new Intl.DateTimeFormat(undefined, { dateStyle: 'short', timeStyle: 'short' }).format(new Date(note.created_at))}
                  </span>
                </div>
              </div>
            ))}
          </div>
        )}
      </section>
    </div>
  )
}
