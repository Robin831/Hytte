import { useState } from 'react'
import { Link } from 'react-router'
import { useTranslation } from 'react-i18next'
import { ArrowLeft, CalendarDays } from 'lucide-react'
import Learn from './doomsday/Learn'
import Practice from './doomsday/Practice'
import Stats from './doomsday/Stats'

type Tab = 'learn' | 'practice' | 'stats'

export default function Doomsday() {
  const { t } = useTranslation('doomsday')
  const [tab, setTab] = useState<Tab>('practice')
  const [version, setVersion] = useState(0)

  return (
    <div className="max-w-4xl mx-auto p-4 sm:p-6 space-y-5 text-white">
      <header className="space-y-2">
        <Link to="/math" className="inline-flex items-center gap-1.5 text-sm text-gray-400 hover:text-white">
          <ArrowLeft size={16} aria-hidden="true" /> {t('back')}
        </Link>
        <div className="flex items-center gap-3">
          <CalendarDays size={28} className="text-blue-400 shrink-0" aria-hidden="true" />
          <div>
            <h1 className="text-2xl sm:text-3xl font-bold">{t('title')}</h1>
            <p className="text-sm text-gray-400">{t('tagline')}</p>
          </div>
        </div>
      </header>

      <div role="tablist" aria-label={t('tabs.label')} className="flex gap-1 border-b border-gray-800">
        {(['learn', 'practice', 'stats'] as Tab[]).map(k => (
          <button
            key={k}
            role="tab"
            type="button"
            aria-selected={tab === k}
            onClick={() => setTab(k)}
            className={`px-3 py-2 text-sm border-b-2 cursor-pointer ${tab === k ? 'border-blue-400 text-white' : 'border-transparent text-gray-400 hover:text-white'}`}
          >
            {k === 'learn' ? t('tabs.learn') : k === 'practice' ? t('tabs.practice') : t('tabs.stats')}
          </button>
        ))}
      </div>

      {tab === 'learn' && <Learn />}
      {tab === 'practice' && <Practice onSaved={() => setVersion(v => v + 1)} />}
      {tab === 'stats' && <Stats version={version} />}
    </div>
  )
}
