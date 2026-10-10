import { useState } from 'react'
import { Link } from 'react-router'
import { Zap } from 'lucide-react'
import { type Watch, linkStride, parseDuration, unlinkStride } from './racesApi'
import { useRaceFormat } from './useRaceFormat'

/** Add a catalog race to Stride (training plan), or show and undo the link. */
export function StrideLink({ eventId, watch, onChange }: {
  eventId: number
  watch: Watch | null
  onChange: (w: Watch) => void
}) {
  const { t } = useRaceFormat()
  const [open, setOpen] = useState(false)
  const [priority, setPriority] = useState<'A' | 'B' | 'C'>('B')
  const [target, setTarget] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const linked = watch?.stride_race_id != null
  const targetSeconds = parseDuration(target)
  const targetInvalid = target.trim() !== '' && targetSeconds === null

  const add = async (e: React.FormEvent) => {
    e.preventDefault()
    if (targetInvalid) return
    setBusy(true)
    setError('')
    try {
      const res = await linkStride(eventId, priority, targetSeconds)
      onChange(res.watch)
      setOpen(false)
    } catch (err) {
      setError(err instanceof Error && err.message.includes('already') ? t('stride.already') : t('errors.save'))
    } finally {
      setBusy(false)
    }
  }

  const remove = async () => {
    const deleteRace = window.confirm(t('stride.confirmDelete'))
    setBusy(true)
    setError('')
    try {
      const res = await unlinkStride(eventId, deleteRace)
      onChange(res.watch)
    } catch {
      setError(t('errors.save'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="mt-3 border-t border-gray-700 pt-3">
      {linked ? (
        <div className="flex flex-wrap items-center gap-3 text-sm">
          <span className="inline-flex items-center gap-1.5 text-green-300"><Zap size={14} aria-hidden="true" /> {t('stride.linked')}</span>
          <Link to="/training/stride" className="text-blue-400 hover:text-blue-300">{t('stride.open')}</Link>
          <button type="button" onClick={remove} disabled={busy} className="text-gray-400 hover:text-red-300 disabled:opacity-50 cursor-pointer">
            {t('stride.remove')}
          </button>
        </div>
      ) : open ? (
        <form onSubmit={add} className="flex flex-wrap items-end gap-3 text-sm" aria-label={t('stride.add')}>
          <fieldset>
            <legend className="mb-1 text-xs font-semibold uppercase tracking-wide text-gray-500">{t('stride.priority')}</legend>
            <div className="flex gap-1">
              {(['A', 'B', 'C'] as const).map(p => (
                <button
                  key={p}
                  type="button"
                  aria-pressed={priority === p}
                  onClick={() => setPriority(p)}
                  title={t(`stride.priority${p}`)}
                  className={`h-9 w-9 rounded-lg border font-semibold cursor-pointer ${priority === p ? 'border-blue-600 bg-blue-600' : 'border-gray-700 bg-gray-800 hover:bg-gray-700'}`}
                >
                  {p}
                </button>
              ))}
            </div>
          </fieldset>
          <div>
            <label htmlFor="stride-target" className="mb-1 block text-xs font-semibold uppercase tracking-wide text-gray-500">{t('stride.target')}</label>
            <input
              id="stride-target"
              value={target}
              onChange={e => setTarget(e.target.value)}
              placeholder="3:30:00"
              aria-invalid={targetInvalid}
              className={`w-28 rounded-lg border bg-gray-800 px-3 py-2 tabular-nums focus:outline-none focus:ring-2 focus:ring-blue-500 ${targetInvalid ? 'border-red-500' : 'border-gray-700'}`}
            />
          </div>
          <button type="submit" disabled={busy || targetInvalid} className="rounded-lg bg-blue-600 px-3 py-2 font-medium hover:bg-blue-500 disabled:opacity-50 cursor-pointer">
            {t('stride.add')}
          </button>
          <button type="button" onClick={() => setOpen(false)} className="rounded-lg bg-gray-700 px-3 py-2 hover:bg-gray-600 cursor-pointer">
            {t('admin.cancel')}
          </button>
          <p className="basis-full text-xs text-gray-500">{t(`stride.priority${priority}`)}</p>
        </form>
      ) : (
        <div className="flex flex-wrap items-center gap-3 text-sm">
          <button type="button" onClick={() => setOpen(true)} className="inline-flex items-center gap-1.5 rounded-lg bg-gray-800 px-3 py-1.5 hover:bg-gray-700 cursor-pointer">
            <Zap size={14} aria-hidden="true" /> {t('stride.add')}
          </button>
          {watch && (watch.state === 'registered' || watch.state === 'got_place') && (
            <span className="text-xs text-gray-500">{t('stride.suggest')}</span>
          )}
        </div>
      )}
      {error && <p role="alert" className="mt-2 text-sm text-red-300">{error}</p>}
    </div>
  )
}
