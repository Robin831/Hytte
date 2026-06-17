import { useTranslation } from 'react-i18next'
import type { FamilyRole } from '../../pages/TodayView'

export function TodayHeaderSkeleton() {
  const { t } = useTranslation('today')
  return (
    <header
      className="text-center mb-4 sm:mb-6 shrink-0"
      aria-busy="true"
      aria-label={t('loading')}
    >
      <div className="mx-auto h-10 sm:h-12 w-40 sm:w-48 rounded bg-gray-800 animate-pulse" />
      <div className="mx-auto mt-2 h-4 sm:h-5 w-48 sm:w-56 rounded bg-gray-800 animate-pulse" />
      <div className="mx-auto mt-2 h-3 w-32 rounded bg-gray-800 animate-pulse" />
      <div className="mx-auto mt-2 h-3 w-16 rounded bg-gray-800 animate-pulse" />
    </header>
  )
}

const cell = "rounded-xl bg-gray-800 animate-pulse"
const wide = `${cell} col-span-2`

function GuestGridCells() {
  return (
    <>
      <div className={wide} />
      <div className={wide} />
      <div className={wide} />
    </>
  )
}

function ChildGridCells() {
  return (
    <>
      <div className={wide} />
      <div className={cell} />
      <div className={cell} />
    </>
  )
}

function ParentGridCells() {
  return (
    <>
      <div className={wide} />
      <div className={cell} />
      <div className={cell} />
      <div className={cell} />
      <div className={cell} />
    </>
  )
}

export function TodayGridSkeleton({ role }: { role?: FamilyRole | null }) {
  const { t } = useTranslation('today')
  return (
    <div
      className="grid grid-cols-2 gap-3 sm:gap-4 flex-1 min-h-0 auto-rows-fr"
      aria-busy="true"
      aria-label={t('loadingWidgets')}
    >
      {role === 'child' ? <ChildGridCells /> :
       role === 'parent' ? <ParentGridCells /> :
       <GuestGridCells />}
    </div>
  )
}
