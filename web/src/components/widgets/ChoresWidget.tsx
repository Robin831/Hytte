import { CheckCircle2, Circle, ClipboardCheck, Clock } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../../auth'
import Widget from '../Widget'
import WidgetLink from './WidgetLink'
import { getJSON, useWidgetData } from './useWidgetData'

interface Chore {
  id: number
  name: string
  /** Absent = not done yet, "pending" = waiting for a parent, "approved" = done. */
  completion_status?: string | null
}

/** Still to do: never started, sent back by a parent, or waiting for a teammate. */
const isOpen = (c: Chore) =>
  !c.completion_status || c.completion_status === 'rejected' || c.completion_status === 'waiting_for_team'

const loadMyChores = (signal: AbortSignal) =>
  getJSON<{ chores: Chore[] | null }>('/api/allowance/my/chores', signal).then((d) => d.chores ?? [])

const loadPending = (signal: AbortSignal) =>
  getJSON<{ pending: unknown[] | null }>('/api/allowance/pending', signal).then((d) => (d.pending ?? []).length)

/** A child's chores for today. */
function MyChores() {
  const { t } = useTranslation('dashboard')
  const { data: chores, loaded } = useWidgetData(loadMyChores)
  if (!loaded || !chores || chores.length === 0) return null

  const left = chores.filter(isOpen).length
  return (
    <Widget title={t('widgets.chores.title')}>
      <p className="text-base font-semibold">
        {left > 0 ? t('widgets.chores.left', { count: left }) : t('widgets.chores.allDone')}
      </p>
      <ul className="mt-3 space-y-1.5">
        {chores.slice(0, 5).map((c) => (
          <li key={c.id} className="flex items-center gap-2 text-sm">
            {c.completion_status === 'approved' ? (
              <CheckCircle2 size={16} className="shrink-0 text-green-400" aria-hidden="true" />
            ) : !isOpen(c) ? (
              <Clock size={16} className="shrink-0 text-amber-300" aria-hidden="true" />
            ) : (
              <Circle size={16} className="shrink-0 text-gray-500" aria-hidden="true" />
            )}
            <span className={`truncate ${isOpen(c) ? 'text-gray-200' : 'text-gray-500'}`}>{c.name}</span>
          </li>
        ))}
      </ul>
      <WidgetLink to="/chores">{t('widgets.chores.open')}</WidgetLink>
    </Widget>
  )
}

/** A parent's queue of chores waiting for approval; nothing waiting, nothing shown. */
function PendingApprovals() {
  const { t } = useTranslation('dashboard')
  const { data: pending, loaded } = useWidgetData(loadPending)
  if (!loaded || !pending) return null

  return (
    <Widget title={t('widgets.chores.title')}>
      <p className="flex items-center gap-2 text-base font-semibold">
        <ClipboardCheck size={16} className="shrink-0 text-amber-300" aria-hidden="true" />
        {t('widgets.chores.toApprove', { count: pending })}
      </p>
      <WidgetLink to="/allowance">{t('widgets.chores.review')}</WidgetLink>
    </Widget>
  )
}

export default function ChoresWidget() {
  const { familyStatus } = useAuth()
  if (familyStatus?.is_child) return <MyChores />
  if (familyStatus?.is_parent) return <PendingApprovals />
  return null
}
