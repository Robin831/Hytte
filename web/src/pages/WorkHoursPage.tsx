import { useState, useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { Settings } from 'lucide-react'
import { buildNavHolidaySet, getInitialDate, prevWeekday, nextWeekday } from './workhours/dateUtils'
import type { ViewMode } from './workhours/types'
import DayView from './workhours/views/DayView'
import WeekView from './workhours/views/WeekView'
import MonthView from './workhours/views/MonthView'
import SettingsView from './workhours/views/SettingsView'
import { WorkHoursShortcutsDialog } from '../components/WorkHoursShortcutsDialog'

export default function WorkHoursPage() {
  const { t } = useTranslation(['workhours', 'common'])
  const [activeTab, setActiveTab] = useState<ViewMode>('day')
  const [currentDate, setCurrentDate] = useState<string>(() =>
    getInitialDate(buildNavHolidaySet(new Date().getFullYear()))
  )
  const [shortcutsOpen, setShortcutsOpen] = useState(false)
  const [pendingPunch, setPendingPunch] = useState(false)
  const punchToggleRef = useRef<(() => void) | null>(null)

  function handleSelectDay(date: string) {
    setCurrentDate(date)
    setActiveTab('day')
  }

  useEffect(() => {
    if (pendingPunch && punchToggleRef.current) {
      punchToggleRef.current()
      setPendingPunch(false)
    }
  }, [pendingPunch])

  useEffect(() => {
    function isEditableTarget(el: Element | null): boolean {
      if (!el) return false
      const tag = el.tagName
      if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return true
      if ((el as HTMLElement).isContentEditable) return true
      if (el.getAttribute('role') === 'combobox') return true
      if (tag === 'BUTTON' && el.parentElement?.querySelector('[role="combobox"]')) return true
      return false
    }

    function handleKeyDown(e: KeyboardEvent) {
      if (e.defaultPrevented || e.ctrlKey || e.metaKey || e.altKey) return
      if (isEditableTarget(document.activeElement)) return
      if (document.querySelector('[role="dialog"]')) return

      const navYear = (d: string) => buildNavHolidaySet(parseInt(d.split('-')[0], 10))

      switch (e.key) {
        case '?':
          if (e.repeat) return
          e.preventDefault()
          setShortcutsOpen(true)
          break
        case 'p':
        case 'P':
          if (e.repeat) return
          e.preventDefault()
          if (punchToggleRef.current) {
            punchToggleRef.current()
          } else {
            setActiveTab('day')
            setPendingPunch(true)
          }
          break
        case 'j':
        case 'J':
        case 'ArrowLeft':
          e.preventDefault()
          setCurrentDate(prev => prevWeekday(prev, navYear(prev)))
          break
        case 'k':
        case 'K':
        case 'ArrowRight':
          e.preventDefault()
          setCurrentDate(prev => nextWeekday(prev, navYear(prev)))
          break
        case 't':
        case 'T':
          if (e.repeat) return
          e.preventDefault()
          setCurrentDate(getInitialDate(buildNavHolidaySet(new Date().getFullYear())))
          break
        case '1':
          if (e.repeat) return
          e.preventDefault()
          setActiveTab('day')
          break
        case '2':
          if (e.repeat) return
          e.preventDefault()
          setActiveTab('week')
          break
        case '3':
          if (e.repeat) return
          e.preventDefault()
          setActiveTab('month')
          break
        case '4':
          if (e.repeat) return
          e.preventDefault()
          setActiveTab('settings')
          break
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [])

  return (
    <div className="max-w-3xl mx-auto p-4 space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold text-white">{t('workhours:title')}</h1>
        <button
          type="button"
          onClick={() => setShortcutsOpen(true)}
          className="flex items-center justify-center w-7 h-7 rounded-full border border-gray-700 text-gray-400 hover:text-white hover:border-gray-500 transition-colors cursor-pointer"
          aria-label={t('workhours:shortcuts.hint')}
          title={t('workhours:shortcuts.hint')}
        >
          <span className="text-sm font-semibold">?</span>
        </button>
      </div>

      {/* Tab bar */}
      <div className="flex gap-1 bg-gray-800 rounded-lg p-1">
        <button
          type="button"
          onClick={() => setActiveTab('day')}
          className={`flex-1 py-1.5 text-sm font-medium rounded transition-colors cursor-pointer ${
            activeTab === 'day' ? 'bg-gray-700 text-white' : 'text-gray-400 hover:text-gray-200'
          }`}
        >
          {t('workhours:viewDay')}
        </button>
        <button
          type="button"
          onClick={() => setActiveTab('week')}
          className={`flex-1 py-1.5 text-sm font-medium rounded transition-colors cursor-pointer ${
            activeTab === 'week' ? 'bg-gray-700 text-white' : 'text-gray-400 hover:text-gray-200'
          }`}
        >
          {t('workhours:viewWeek')}
        </button>
        <button
          type="button"
          onClick={() => setActiveTab('month')}
          className={`flex-1 py-1.5 text-sm font-medium rounded transition-colors cursor-pointer ${
            activeTab === 'month' ? 'bg-gray-700 text-white' : 'text-gray-400 hover:text-gray-200'
          }`}
        >
          {t('workhours:viewMonth')}
        </button>
        <button
          type="button"
          onClick={() => setActiveTab('settings')}
          className={`py-1.5 px-2.5 text-sm font-medium rounded transition-colors cursor-pointer ${
            activeTab === 'settings' ? 'bg-gray-700 text-white' : 'text-gray-400 hover:text-gray-200'
          }`}
          aria-label={t('workhours:settings')}
        >
          <Settings size={16} />
        </button>
      </div>

      {activeTab === 'day' && (
        <DayView currentDate={currentDate} setCurrentDate={setCurrentDate} onNavigateToSettings={() => setActiveTab('settings')} punchToggleRef={punchToggleRef} />
      )}
      {activeTab === 'week' && (
        <WeekView currentDate={currentDate} setCurrentDate={setCurrentDate} onSelectDay={handleSelectDay} />
      )}
      {activeTab === 'month' && (
        <MonthView currentDate={currentDate} setCurrentDate={setCurrentDate} onSelectDay={handleSelectDay} />
      )}
      {activeTab === 'settings' && (
        <SettingsView />
      )}

      <WorkHoursShortcutsDialog open={shortcutsOpen} onClose={() => setShortcutsOpen(false)} />
    </div>
  )
}
