import { useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import {
  type Deadline, type DatePrecision, type Lang,
  daysUntil, formatFuzzyDate, formatInstant, toLang,
} from './racesApi'

/** Language + formatters bound to the current UI language. */
export function useRaceFormat() {
  const { t, i18n } = useTranslation('races')
  const lang: Lang = toLang(i18n.language)

  const fuzzy = useCallback(
    (date: string, precision: DatePrecision) =>
      formatFuzzyDate(date, precision, lang, (p, text) => t(`precision.${p}`, { date: text })),
    [lang, t],
  )

  /** A deadline's date: the exact local time when known, otherwise its (fuzzy) day. */
  const deadlineWhen = useCallback(
    (d: Deadline) => (d.due_at ? formatInstant(d.due_at, lang) : fuzzy(d.due_date, d.date_precision)),
    [fuzzy, lang],
  )

  const daysLeft = useCallback(
    (date: string) => {
      const n = daysUntil(date)
      if (n === 0) return t('when.today')
      if (n === 1) return t('when.tomorrow')
      if (n < 0) return t('when.ago', { count: -n })
      return t('when.inDays', { count: n })
    },
    [t],
  )

  return { t, lang, fuzzy, deadlineWhen, daysLeft }
}
