import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import type { ParseKeys } from 'i18next'
import { Calculator, Timer, Zap, Target, Trophy, Award } from 'lucide-react'
import MathSummary from './MathSummary'

interface ModeTile {
  to: string
  icon: React.ReactNode
  titleKey: ParseKeys<'regnemester'>
  blurbKey: ParseKeys<'regnemester'>
}

const modes: ModeTile[] = [
  { to: '/math/play/marathon', icon: <Timer size={28} />, titleKey: 'modes.marathon.title', blurbKey: 'modes.marathon.blurb' },
  { to: '/math/play/blitz', icon: <Zap size={28} />, titleKey: 'modes.blitz.title', blurbKey: 'modes.blitz.blurb' },
  { to: '/math/play/practice', icon: <Target size={28} />, titleKey: 'modes.practice.title', blurbKey: 'modes.practice.blurb' },
  { to: '/math/heatmap', icon: <Trophy size={28} />, titleKey: 'modes.mastery.title', blurbKey: 'modes.mastery.blurb' },
]

export default function MathLanding() {
  const { t } = useTranslation('regnemester')

  return (
    <div className="max-w-4xl mx-auto p-4 sm:p-6">
      <header className="mb-6 sm:mb-8">
        <div className="flex items-center gap-3 mb-2">
          <Calculator size={28} className="text-blue-400 shrink-0" />
          <h1 className="text-2xl sm:text-3xl font-bold text-white">{t('title')}</h1>
        </div>
        <p className="text-gray-400 text-sm sm:text-base">{t('tagline')}</p>
      </header>

      <div className="mb-6 flex flex-wrap gap-2">
        <Link
          to="/math/leaderboard"
          className="inline-flex items-center gap-2 rounded-lg border border-gray-700 bg-gray-800 hover:border-yellow-500/40 hover:bg-gray-800/80 px-4 py-2 text-sm font-medium text-yellow-300 transition-colors"
        >
          <Trophy size={18} />
          {t('leaderboard.viewLink')}
        </Link>
        <Link
          to="/math/achievements"
          className="inline-flex items-center gap-2 rounded-lg border border-gray-700 bg-gray-800 hover:border-yellow-500/40 hover:bg-gray-800/80 px-4 py-2 text-sm font-medium text-yellow-300 transition-colors"
        >
          <Award size={18} />
          {t('achievements.viewLink')}
        </Link>
      </div>

      <MathSummary />

      <section aria-labelledby="modes-heading">
        <h2 id="modes-heading" className="sr-only">{t('modePickerHeading')}</h2>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 sm:gap-4">
          {modes.map(mode => (
            <Link
              key={mode.titleKey}
              to={mode.to}
              className="block rounded-lg border p-4 sm:p-5 transition-colors border-gray-700 bg-gray-800 hover:border-blue-500 hover:bg-gray-800/80 cursor-pointer"
            >
              <div className="flex items-start gap-3 sm:gap-4">
                <div className="p-2 rounded-md shrink-0 bg-blue-500/15 text-blue-400">
                  {mode.icon}
                </div>
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2 mb-1 flex-wrap">
                    <h3 className="text-base sm:text-lg font-semibold text-white">{t(mode.titleKey)}</h3>
                  </div>
                  <p className="text-sm text-gray-400">{t(mode.blurbKey)}</p>
                </div>
              </div>
            </Link>
          ))}
        </div>
      </section>
    </div>
  )
}
