import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Trash2, Plus, Trophy, Pencil } from 'lucide-react'
import { formatDate } from '../../utils/formatDate'
import { formatDistance, formatDuration } from '../../utils/training'
import { formatTargetTime, parseTargetTime } from '../../pages/strideUtils'
import type { Race, StrideMutationResult } from '../../types/stride'
import type { RaceInput } from '../../hooks/useStrideRaces'

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

interface RaceCalendarSectionProps {
  races: Race[]
  loading: boolean
  /** Create (no id) or replace (with id) a race. */
  onSave: (input: RaceInput, id?: number) => Promise<StrideMutationResult>
  onDelete: (id: number) => Promise<StrideMutationResult>
  /** Re-reads the race list after a successful save. */
  onReload: () => Promise<void>
}

export function RaceCalendarSection({ races, loading, onSave, onDelete, onReload }: RaceCalendarSectionProps) {
  const { t } = useTranslation('stride')
  // Bound to the `training` namespace so the shared distance/duration helpers
  // resolve their `units.*` keys (the section's own strings use the `stride` ns).
  const { t: tTraining } = useTranslation('training')

  // Race form state. The one form serves both create and edit: editingRace
  // holds the row being edited (null while creating), and is also what carries
  // result_time across an edit — the form does not expose it, and PUT
  // /api/stride/races/{id} replaces every column it is sent.
  const [showRaceForm, setShowRaceForm] = useState(false)
  const [editingRace, setEditingRace] = useState<Race | null>(null)
  const [raceName, setRaceName] = useState('')
  const [raceDate, setRaceDate] = useState('')
  const [raceDistanceKm, setRaceDistanceKm] = useState('')
  const [raceTargetTime, setRaceTargetTime] = useState('')
  const [racePriority, setRacePriority] = useState<'A' | 'B' | 'C'>('B')
  const [raceNotes, setRaceNotes] = useState('')
  const [raceSubmitting, setRaceSubmitting] = useState(false)
  const [raceError, setRaceError] = useState('')

  function resetRaceForm() {
    setEditingRace(null)
    setRaceName('')
    setRaceDate('')
    setRaceDistanceKm('')
    setRaceTargetTime('')
    setRacePriority('B')
    setRaceNotes('')
    setRaceError('')
  }

  // Loads a race into the shared form and switches it to edit mode. The
  // distance is stored in metres but entered in kilometres, and the target time
  // is stored in seconds but entered as H:MM:SS, so both are converted here and
  // back again on submit.
  function startEditRace(race: Race) {
    setEditingRace(race)
    setRaceName(race.name)
    setRaceDate(race.date)
    setRaceDistanceKm(String(race.distance_m / 1000))
    setRaceTargetTime(formatTargetTime(race.target_time))
    setRacePriority(race.priority)
    setRaceNotes(race.notes)
    setRaceError('')
    setShowRaceForm(true)
  }

  function toggleRaceForm() {
    if (showRaceForm) {
      setShowRaceForm(false)
      resetRaceForm()
      return
    }
    resetRaceForm()
    setShowRaceForm(true)
  }

  async function handleSubmitRace(e: React.FormEvent) {
    e.preventDefault()
    const editing = editingRace
    const failureKey = editing ? 'races.form.error.update' : 'races.form.error.create'
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

      const input: RaceInput = {
        name: raceName,
        date: raceDate,
        distance_m: distanceM,
        target_time: targetTime,
        priority: racePriority,
        notes: raceNotes,
        ...(editing ? { result_time: editing.result_time } : {}),
      }

      const result = await onSave(input, editing?.id)
      if (!result.ok) {
        setRaceError(result.error ?? t(failureKey))
        return
      }

      setShowRaceForm(false)
      resetRaceForm()
      await onReload()
    } finally {
      setRaceSubmitting(false)
    }
  }

  async function handleDeleteRace(id: number) {
    const result = await onDelete(id)
    if (!result.ok) {
      setRaceError(result.error ?? t('races.form.error.delete'))
      return
    }
    // Deleting the race the form is editing would leave it pointing at a row
    // that no longer exists, so the form goes with it.
    if (editingRace?.id === id) {
      setShowRaceForm(false)
      resetRaceForm()
    }
  }

  const now = new Date()
  const today = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`
  const upcomingRaces = races.filter(r => r.date >= today)
  const pastRaces = races.filter(r => r.date < today)

  return (
    <section>
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-lg font-semibold text-white flex items-center gap-2">
          <Trophy size={18} className="text-yellow-400" />
          {t('races.title')}
        </h2>
        <button
          type="button"
          onClick={toggleRaceForm}
          className="flex items-center gap-1.5 px-3 py-1.5 text-sm bg-blue-600 hover:bg-blue-700 text-white rounded-lg transition-colors"
        >
          <Plus size={14} />
          {t('races.add')}
        </button>
      </div>

      {/* Race form — create when editingRace is null, edit otherwise */}
      {showRaceForm && (
        <form onSubmit={handleSubmitRace} className="mb-4 p-4 bg-gray-800 rounded-xl border border-gray-700 space-y-3">
          <p className="text-sm font-medium text-white">
            {editingRace ? t('races.form.editTitle') : t('races.form.createTitle')}
          </p>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <div>
              <label htmlFor="race-name" className="block text-xs text-gray-400 mb-1">{t('races.form.name')}</label>
              <input
                id="race-name"
                type="text"
                value={raceName}
                onChange={e => setRaceName(e.target.value)}
                required
                className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-base sm:text-sm text-white placeholder-gray-400 focus:outline-none focus:border-blue-500"
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
                className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-base sm:text-sm text-white focus:outline-none focus:border-blue-500"
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
                className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-base sm:text-sm text-white placeholder-gray-400 focus:outline-none focus:border-blue-500"
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
                className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-base sm:text-sm text-white placeholder-gray-400 focus:outline-none focus:border-blue-500"
                placeholder="3:30:00"
              />
            </div>
            <div>
              <label htmlFor="race-priority" className="block text-xs text-gray-400 mb-1">{t('races.form.priority')}</label>
              <select
                id="race-priority"
                value={racePriority}
                onChange={e => setRacePriority(e.target.value as 'A' | 'B' | 'C')}
                className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-base sm:text-sm text-white focus:outline-none focus:border-blue-500"
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
              className="w-full bg-gray-700 border border-gray-600 rounded-lg px-3 py-2 text-base sm:text-sm text-white placeholder-gray-400 focus:outline-none focus:border-blue-500"
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
              {raceSubmitting
                ? t('races.form.saving')
                : editingRace
                  ? t('races.form.update')
                  : t('races.form.save')}
            </button>
            <button
              type="button"
              onClick={() => { setShowRaceForm(false); resetRaceForm() }}
              className="px-4 py-2 text-sm bg-gray-700 hover:bg-gray-600 text-white rounded-lg transition-colors"
            >
              {t('races.form.cancel')}
            </button>
          </div>
        </form>
      )}

      {loading ? (
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
                    {formatDistance(race.distance_m, tTraining, { fractionDigits: 1 })}
                    {race.target_time != null && ` · ${t('races.target')}: ${formatDuration(race.target_time, tTraining)}`}
                    {race.result_time != null && ` · ${t('races.result')}: ${formatDuration(race.result_time, tTraining)}`}
                    {weeks > 0 && ` · ${t('races.weeksAway', { count: weeks })}`}
                  </p>
                </div>
                <button
                  type="button"
                  onClick={() => startEditRace(race)}
                  className="sm:opacity-0 sm:group-hover:opacity-100 p-1.5 text-gray-500 hover:text-blue-400 transition-all"
                  aria-label={t('races.edit', { name: race.name })}
                >
                  <Pencil size={14} />
                </button>
                <button
                  type="button"
                  onClick={() => handleDeleteRace(race.id)}
                  className="sm:opacity-0 sm:group-hover:opacity-100 p-1.5 text-gray-500 hover:text-red-400 transition-all"
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
                      {formatDistance(race.distance_m, tTraining, { fractionDigits: 1 })}
                      {race.target_time != null && ` · ${t('races.target')}: ${formatDuration(race.target_time, tTraining)}`}
                      {race.result_time != null && ` · ${t('races.result')}: ${formatDuration(race.result_time, tTraining)}`}
                    </p>
                  </div>
                  <button
                    type="button"
                    onClick={() => startEditRace(race)}
                    className="sm:opacity-0 sm:group-hover:opacity-100 p-1.5 text-gray-500 hover:text-blue-400 transition-all"
                    aria-label={t('races.edit', { name: race.name })}
                  >
                    <Pencil size={14} />
                  </button>
                  <button
                    type="button"
                    onClick={() => handleDeleteRace(race.id)}
                    className="sm:opacity-0 sm:group-hover:opacity-100 p-1.5 text-gray-500 hover:text-red-400 transition-all"
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
  )
}
