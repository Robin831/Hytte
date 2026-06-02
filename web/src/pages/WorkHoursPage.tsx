import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Settings } from 'lucide-react'
import { buildNavHolidaySet, getInitialDate } from './workhours/dateUtils'
import type { ViewMode } from './workhours/types'
import DayView from './workhours/views/DayView'
import WeekView from './workhours/views/WeekView'
import MonthView from './workhours/views/MonthView'
import SettingsView from './workhours/views/SettingsView'

export default function WorkHoursPage() {
  const { t } = useTranslation(['workhours', 'common'])
  const [activeTab, setActiveTab] = useState<ViewMode>('day')
  const [currentDate, setCurrentDate] = useState<string>(() =>
    getInitialDate(buildNavHolidaySet(new Date().getFullYear()))
  )

  function handleSelectDay(date: string) {
    setCurrentDate(date)
    setActiveTab('day')
  }

  return (
    <div className="max-w-3xl mx-auto p-4 space-y-4">
      <h1 className="text-xl font-semibold text-white">{t('workhours:title')}</h1>

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
        <DayView currentDate={currentDate} setCurrentDate={setCurrentDate} onNavigateToSettings={() => setActiveTab('settings')} />
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
    </div>
  )
}
