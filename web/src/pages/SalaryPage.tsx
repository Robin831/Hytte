import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Settings, ChevronDown, ChevronUp } from 'lucide-react'
import { addMonth } from './salary/types'
import type { Tab } from './salary/types'
import { useSalaryData } from './salary/useSalaryData'
import ConfigEditor from './salary/ConfigEditor'
import MonthView from './salary/MonthView'
import YearView from './salary/YearView'

export default function SalaryPage() {
  const { t, i18n } = useTranslation('salary')
  const locale = i18n.language

  const [activeTab, setActiveTab] = useState<Tab>('month')
  const [showConfig, setShowConfig] = useState(false)

  // Month/year navigation state.
  const currentYear = new Date().getFullYear()
  const currentMonthStr = (() => {
    const d = new Date()
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`
  })()
  const getYearFromMonth = (month: string) => {
    const parsedYear = Number.parseInt(month.split('-')[0] ?? '', 10)
    return Number.isNaN(parsedYear) ? currentYear : parsedYear
  }
  const [selectedMonth, setSelectedMonth] = useState(currentMonthStr)
  const [selectedYear, setSelectedYear] = useState(() => getYearFromMonth(currentMonthStr))

  // Move the selected month by `delta` while keeping the selected year in sync.
  // Updating both together in a single handler avoids the desync that used to
  // occur when crossing a year boundary (Dec → Jan / Jan → Dec).
  const changeMonth = (delta: number) => {
    const next = addMonth(selectedMonth, delta)
    setSelectedMonth(next)
    setSelectedYear(getYearFromMonth(next))
  }

  const salary = useSalaryData(selectedMonth, selectedYear, activeTab)
  const { estimate, loading, error } = salary

  if (loading) {
    return (
      <div className="p-6 text-gray-400">{t('title')}…</div>
    )
  }

  if (error) {
    return (
      <div className="p-6 text-red-400">{t('errors.failedToLoad')}: {error}</div>
    )
  }

  const noConfig = estimate === null && selectedMonth === currentMonthStr
  const noConfigPastMonth = estimate === null && selectedMonth !== currentMonthStr

  return (
    <div className="p-4 md:p-6 max-w-3xl mx-auto space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold text-white">{t('title')}</h1>
        {!noConfig && (
          <button
            onClick={() => setShowConfig(v => !v)}
            className="flex items-center gap-1.5 text-sm text-gray-400 hover:text-white transition-colors"
          >
            <Settings size={16} />
            {t('config.edit')}
            {showConfig ? <ChevronUp size={14} /> : <ChevronDown size={14} />}
          </button>
        )}
      </div>

      {/* Config panel */}
      {(showConfig || noConfig || noConfigPastMonth) && (
        <ConfigEditor
          salary={salary}
          noConfig={noConfig}
          noConfigPastMonth={noConfigPastMonth}
          onClose={() => setShowConfig(false)}
        />
      )}

      {/* Tab switcher */}
      {!noConfig && (
        <div className="flex gap-1 bg-gray-800/50 rounded-lg p-1 w-fit">
          <button
            type="button"
            onClick={() => setActiveTab('month')}
            className={`px-4 py-1.5 rounded-md text-sm font-medium transition-colors ${
              activeTab === 'month'
                ? 'bg-gray-700 text-white'
                : 'text-gray-400 hover:text-white'
            }`}
          >
            {t('year.tabs.month')}
          </button>
          <button
            type="button"
            onClick={() => setActiveTab('year')}
            className={`px-4 py-1.5 rounded-md text-sm font-medium transition-colors ${
              activeTab === 'year'
                ? 'bg-gray-700 text-white'
                : 'text-gray-400 hover:text-white'
            }`}
          >
            {t('year.tabs.year')}
          </button>
        </div>
      )}

      {/* Month view */}
      {activeTab === 'month' && estimate && (
        <MonthView
          salary={salary}
          selectedMonth={selectedMonth}
          currentMonthStr={currentMonthStr}
          locale={locale}
          onChangeMonth={changeMonth}
        />
      )}

      {/* Year view */}
      {activeTab === 'year' && (
        <YearView
          salary={salary}
          selectedYear={selectedYear}
          currentYear={currentYear}
          locale={locale}
          setSelectedYear={setSelectedYear}
        />
      )}
    </div>
  )
}
