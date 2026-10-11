import { CheckCircle2, Circle, ClipboardCheck, Clock } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../../auth'
import Widget from '../Widget'
import WidgetLink from './WidgetLink'
import { useWidgetData } from './useWidgetData'
import { isOpenChore, myChoresSource, pendingApprovalsSource } from './sources'

/** A child's chores for today. */
function MyChores() {
  const { t } = useTranslation('dashboard')
  const { data: chores, loaded } = useWidgetData(myChoresSource)
  if (!loaded || !chores || chores.length === 0) return null

  const left = chores.filter(isOpenChore).length
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
            ) : !isOpenChore(c) ? (
              <Clock size={16} className="shrink-0 text-amber-300" aria-hidden="true" />
            ) : (
              <Circle size={16} className="shrink-0 text-gray-500" aria-hidden="true" />
            )}
            <span className={`truncate ${isOpenChore(c) ? 'text-gray-200' : 'text-gray-500'}`}>{c.name}</span>
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
  const { data: pending, loaded } = useWidgetData(pendingApprovalsSource)
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
