import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { centuryAnchor, monthDoomsday, parseIso, yearDoomsday } from './doomsday'
import { dayMonthText, weekdayName } from './format'
import { MiniCalendar, SolutionView } from './Parts'

const card = 'rounded-lg border border-gray-800 bg-gray-800/30 p-4 space-y-2'

export default function Learn() {
  const { t, i18n } = useTranslation('doomsday')
  const lang = i18n.language
  const year = new Date().getFullYear()
  const [input, setInput] = useState(`${year}-12-24`)
  const date = parseIso(input)

  return (
    <div className="space-y-4">
      <p className="text-sm text-gray-300">{t('learn.intro')}</p>

      <section className={card}>
        <h2 className="font-semibold">{t('learn.step1Title')}</h2>
        <p className="text-sm text-gray-300">{t('learn.step1Body')}</p>
        <div className="grid grid-cols-4 gap-2 text-sm">
          {[1600, 1700, 1800, 1900, 2000, 2100, 2200, 2300].map(c => (
            <div key={c} className="rounded bg-gray-900 px-2 py-1 text-center">
              <span className="text-gray-400">{c}</span> <span className="font-medium">{weekdayName(centuryAnchor(c), lang, 'short')}</span>
            </div>
          ))}
        </div>
      </section>

      <section className={card}>
        <h2 className="font-semibold">{t('learn.step2Title')}</h2>
        <p className="text-sm text-gray-300">{t('learn.step2Body')}</p>
        <ol className="list-decimal pl-5 text-sm text-gray-300 space-y-0.5">
          <li>{t('learn.oddRule1')}</li>
          <li>{t('learn.oddRule2')}</li>
          <li>{t('learn.oddRule3')}</li>
          <li>{t('learn.oddRule4')}</li>
          <li>{t('learn.oddRule5')}</li>
        </ol>
        <p className="text-sm text-gray-400 pt-1">{t('learn.step2Example', { year })}</p>
        <div className="rounded bg-gray-900 p-3">
          <SolutionView date={{ y: year, m: 4, d: 4 }} />
        </div>
      </section>

      <section className={card}>
        <h2 className="font-semibold">{t('learn.step3Title')}</h2>
        <p className="text-sm text-gray-300">{t('learn.step3Body')}</p>
        <ul className="list-disc pl-5 text-sm text-gray-300 space-y-0.5">
          <li>{t('learn.evenMonths')}</li>
          <li>{t('learn.nineToFive')}</li>
          <li>{t('learn.janFeb')}</li>
          <li>{t('learn.march')}</li>
        </ul>
      </section>

      <section className={card}>
        <h2 className="font-semibold">{t('learn.step4Title')}</h2>
        <p className="text-sm text-gray-300">{t('learn.step4Body')}</p>
      </section>

      <section className={card}>
        <h2 className="font-semibold">{t('learn.leapTitle')}</h2>
        <p className="text-sm text-gray-300">{t('learn.leapBody')}</p>
        <h2 className="font-semibold pt-2">{t('learn.numbersTitle')}</h2>
        <p className="text-sm text-gray-300">{t('learn.numbersBody')}</p>
      </section>

      <section className="rounded-lg border border-amber-700/50 bg-amber-900/10 p-4 space-y-2">
        <h2 className="font-semibold text-amber-200">{t('learn.cheatTitle')}</h2>
        <p className="text-sm">{t('learn.cheatThisYear', { year, day: weekdayName(yearDoomsday(year), lang) })}</p>
        <p className="text-xs text-gray-400">{t('learn.cheatAnchors')}</p>
        <p className="text-sm">
          {[1700, 1800, 1900, 2000, 2100].map(c => `${c} ${weekdayName(centuryAnchor(c), lang, 'short')}`).join(' · ')}
        </p>
        <p className="text-xs text-gray-400">{t('learn.cheatMonths')}</p>
        <p className="text-sm">
          {Array.from({ length: 12 }, (_, i) => i + 1).map(m => {
            const normal = monthDoomsday(m, false)
            const leapDay = monthDoomsday(m, true)
            return normal === leapDay ? dayMonthText(normal, m, lang) : `${dayMonthText(normal, m, lang)} (${leapDay})`
          }).join(' · ')}
        </p>
      </section>

      <section className={card}>
        <h2 className="font-semibold">{t('learn.tryTitle')}</h2>
        <label className="text-xs text-gray-400 flex items-center gap-2">
          {t('learn.tryLabel')}
          <input
            type="date"
            value={input}
            onChange={e => setInput(e.target.value)}
            min="1600-01-01"
            max="2299-12-31"
            className="bg-gray-800 border border-gray-700 rounded px-2 py-1 text-sm text-white"
          />
        </label>
        {date ? (
          <div className="flex flex-col sm:flex-row gap-4">
            <div className="flex-1"><SolutionView date={date} /></div>
            <MiniCalendar date={date} />
          </div>
        ) : (
          <p className="text-sm text-amber-300">{t('learn.tryInvalid')}</p>
        )}
      </section>
    </div>
  )
}
