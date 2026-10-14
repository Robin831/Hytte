import { useTranslation } from 'react-i18next'
import { AlertTriangle, Building2, Check, Pencil, Plus, Trash2, X } from 'lucide-react'
import { TimePicker } from '../../../components/ui/time-picker'
import { sessionMinutes } from '../calculateDay'
import { formatMins } from '../dateUtils'
import type { WorkSession } from '../types'
import type { ActiveSessionConflict, ConflictCause, SessionDraft, SessionEdit } from '../useDayEntry'

const CONFLICT_MESSAGE_KEYS = {
  save: 'workhours:sessionConflict',
  toggle: 'workhours:sessionConflictToggle',
  copy: 'workhours:sessionConflictCopy',
} as const satisfies Record<ConflictCause, string>

export interface SessionListProps {
  sessions: WorkSession[]
  overlaps: { ids: Set<number>; pairs: Array<[WorkSession, WorkSession]> }
  /** Gross minutes of the saved day; shown under the list when positive. */
  grossMinutes: number
  /** Id of the session open in the inline editor, if any. */
  editSessionId: number | null
  edit: SessionEdit
  draft: SessionDraft
  conflict: ActiveSessionConflict | null
  /** A day edit or punch action is in flight; locks the controls. */
  disabled: boolean
  onEditChange: (patch: Partial<SessionEdit>) => void
  onDraftChange: (patch: Partial<SessionDraft>) => void
  onStartEdit: (session: WorkSession) => void
  onCancelEdit: () => void
  onSaveEdit: (session: WorkSession) => void
  onToggleInternal: (session: WorkSession) => void
  onDelete: (sessionID: number) => void
  onAdd: () => void
}

/**
 * The day's sessions: stored-overlap warning, the session rows with their
 * inline editor, the gross total and the add-session row. Rendered inside the
 * sessions section, below its header.
 */
export default function SessionList({
  sessions,
  overlaps,
  grossMinutes,
  editSessionId,
  edit,
  draft,
  conflict,
  disabled: busy,
  onEditChange,
  onDraftChange,
  onStartEdit,
  onCancelEdit,
  onSaveEdit,
  onToggleInternal,
  onDelete,
  onAdd,
}: SessionListProps) {
  const { t } = useTranslation(['workhours', 'common'])

  // Inline message for a save rejected because it overlaps another session.
  const renderSessionConflict = (target: 'add' | number) => {
    if (!conflict || conflict.target !== target) return null
    return (
      <p role="alert" className="flex items-start gap-1.5 text-xs text-red-400">
        <AlertTriangle size={14} className="mt-0.5 shrink-0" aria-hidden="true" />
        <span>{t(CONFLICT_MESSAGE_KEYS[conflict.cause], { start: conflict.conflict.start_time, end: conflict.conflict.end_time })}</span>
      </p>
    )
  }

  return (
    <>
      {overlaps.pairs.length > 0 && (
        <div role="status" className="rounded-lg border border-amber-700/50 bg-amber-900/20 px-3 py-2 text-xs text-amber-300">
          <p className="flex items-center gap-1.5 font-medium">
            <AlertTriangle size={14} className="shrink-0" aria-hidden="true" />
            {t('workhours:storedOverlapTitle')}
          </p>
          <ul className="mt-1 space-y-0.5 pl-5">
            {overlaps.pairs.map(([a, b]) => (
              <li key={`${a.id}-${b.id}`} className="font-mono">
                {t('workhours:storedOverlapWarning', { startA: a.start_time, endA: a.end_time, startB: b.start_time, endB: b.end_time })}
              </li>
            ))}
          </ul>
        </div>
      )}

      {sessions.length > 0 && (
        <div className="space-y-2">
          {sessions.map(s => {
            const mins = sessionMinutes(s) ?? 0
            const overlapping = overlaps.ids.has(s.id)
            if (editSessionId === s.id) {
              return (
                <div key={s.id} className="space-y-1">
                  <div className={`flex items-center gap-2 flex-wrap rounded-lg border bg-gray-800 px-3 py-2 ${overlapping ? 'border-amber-500/60' : 'border-blue-700/40'}`}>
                    <TimePicker
                      value={edit.start}
                      onChange={v => onEditChange({ start: v })}
                      aria-label={t('workhours:startTime')}
                    />
                    <span className="text-gray-500 text-xs">→</span>
                    <TimePicker
                      value={edit.end}
                      onChange={v => onEditChange({ end: v })}
                      aria-label={t('workhours:endTime')}
                    />
                    <label className="flex items-center gap-1.5 cursor-pointer text-xs text-gray-400 select-none">
                      <input
                        type="checkbox"
                        checked={edit.crossesMidnight}
                        onChange={e => onEditChange({ crossesMidnight: e.target.checked })}
                        className="accent-blue-500"
                        aria-label={t('workhours:endsNextDay')}
                      />
                      <span className={edit.crossesMidnight ? 'text-blue-300' : ''}>{t('workhours:endsNextDay')}</span>
                    </label>
                    <button
                      type="button"
                      onClick={() => onSaveEdit(s)}
                      disabled={busy || !edit.start || !edit.end}
                      className="ml-auto text-green-400 hover:text-green-300 transition-colors disabled:opacity-40 cursor-pointer"
                      aria-label={t('workhours:saveSession')}
                      title={t('workhours:saveSession')}
                    >
                      <Check size={16} />
                    </button>
                    <button
                      type="button"
                      onClick={onCancelEdit}
                      disabled={busy}
                      className="text-gray-500 hover:text-white transition-colors disabled:opacity-40 cursor-pointer"
                      aria-label={t('workhours:cancelEditSession')}
                      title={t('workhours:cancelEditSession')}
                    >
                      <X size={16} />
                    </button>
                  </div>
                  {renderSessionConflict(s.id)}
                </div>
              )
            }
            return (
              <div key={s.id} className="space-y-1">
                <div className={`flex items-center gap-3 rounded-lg border px-3 py-2 ${s.is_internal ? 'bg-purple-900/40' : 'bg-gray-800'} ${overlapping ? 'border-amber-500/60' : s.is_internal ? 'border-purple-700/40' : 'border-transparent'}`}>
                  <span className="text-white font-mono text-sm">{s.start_time}</span>
                  <span className="text-gray-500 text-xs">→</span>
                  <span className="text-white font-mono text-sm">{s.end_time}</span>
                  {s.crosses_midnight && (
                    <span
                      className="rounded bg-blue-900/50 px-1 py-0.5 text-[0.65rem] font-medium text-blue-300"
                      title={t('workhours:endsNextDayTitle')}
                    >
                      {t('workhours:nextDayMarker')}
                    </span>
                  )}
                  <span className="text-gray-400 text-xs ml-auto">{formatMins(mins)}</span>
                  <button
                    type="button"
                    onClick={() => onStartEdit(s)}
                    disabled={busy}
                    className="text-gray-500 hover:text-blue-400 transition-colors disabled:opacity-40 cursor-pointer"
                    aria-label={t('workhours:editSession')}
                    title={t('workhours:editSession')}
                  >
                    <Pencil size={14} />
                  </button>
                  <button
                    type="button"
                    onClick={() => onToggleInternal(s)}
                    disabled={busy}
                    className={`transition-colors disabled:opacity-40 cursor-pointer ${s.is_internal ? 'text-purple-400 hover:text-purple-300' : 'text-gray-500 hover:text-purple-400'}`}
                    aria-label={s.is_internal ? t('workhours:markExternal') : t('workhours:markInternal')}
                    title={s.is_internal ? t('workhours:markExternal') : t('workhours:markInternal')}
                  >
                    <Building2 size={14} />
                  </button>
                  <button
                    type="button"
                    onClick={() => onDelete(s.id)}
                    disabled={busy}
                    className="text-gray-500 hover:text-red-400 transition-colors disabled:opacity-40 cursor-pointer"
                    aria-label={t('workhours:removeSession')}
                  >
                    <Trash2 size={14} />
                  </button>
                </div>
                {renderSessionConflict(s.id)}
              </div>
            )
          })}
        </div>
      )}

      {grossMinutes > 0 && (
        <p className="text-xs text-gray-400">
          {t('workhours:gross')}: <span className="font-mono">{formatMins(grossMinutes)}</span>
        </p>
      )}

      {/* Add session row */}
      <div className="flex items-center gap-2 flex-wrap">
        <TimePicker
          value={draft.start}
          onChange={v => onDraftChange({ start: v })}
          aria-label={t('workhours:startTime')}
        />
        <span className="text-gray-500 text-xs">→</span>
        <TimePicker
          value={draft.end}
          onChange={v => onDraftChange({ end: v })}
          aria-label={t('workhours:endTime')}
        />
        <label className="flex items-center gap-1.5 cursor-pointer text-xs text-gray-400 select-none">
          <input
            type="checkbox"
            checked={draft.crossesMidnight}
            onChange={e => onDraftChange({ crossesMidnight: e.target.checked })}
            className="accent-blue-500"
            aria-label={t('workhours:endsNextDay')}
          />
          <span className={draft.crossesMidnight ? 'text-blue-300' : ''}>{t('workhours:endsNextDay')}</span>
        </label>
        <label className="flex items-center gap-1.5 cursor-pointer text-xs text-gray-400 select-none">
          <input
            type="checkbox"
            checked={draft.isInternal}
            onChange={e => onDraftChange({ isInternal: e.target.checked })}
            className="accent-purple-500"
            aria-label={t('workhours:markInternal')}
          />
          <Building2 size={12} className={draft.isInternal ? 'text-purple-400' : 'text-gray-500'} />
          <span className={draft.isInternal ? 'text-purple-400' : ''}>{t('workhours:internal')}</span>
        </label>
        <button
          type="button"
          onClick={onAdd}
          disabled={!draft.start || !draft.end || busy}
          className="flex items-center gap-1 px-3 py-1.5 bg-blue-600 hover:bg-blue-500 disabled:opacity-40 disabled:cursor-not-allowed text-white text-sm rounded transition-colors cursor-pointer"
        >
          <Plus size={14} />
          {t('workhours:addSession')}
        </button>
      </div>
      {renderSessionConflict('add')}
    </>
  )
}
