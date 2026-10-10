import { createContext, useContext } from 'react'
import { type Lang, type RaceEvent, intlLocale } from './racesApi'

/** NOK per one unit of each currency ("EUR": 10.72), from the races API. */
export type Rates = Record<string, number>

export const RatesContext = createContext<Rates>({})
export const useRates = () => useContext(RatesContext)

// Currency markers as they appear in race texts.
const SYMBOLS: Record<string, string> = { '€': 'EUR', '£': 'GBP', '$': 'USD', '¥': 'JPY', '฿': 'THB' }
const CODES = ['EUR', 'GBP', 'USD', 'SEK', 'DKK', 'CHF', 'AUD', 'JPY', 'CZK', 'PLN', 'HUF', 'THB', 'NOK']
const WORDS: Record<string, string> = { yen: 'JPY', baht: 'THB', 'บาท': 'THB' }

// A number as written in nb/en/th texts: "59", "1 200", "15,000", "47,75", "43.50".
const NUM = String.raw`\d{1,3}(?:[ \u00a0,.]\d{3})+(?:[.,]\d{1,2})?|\d+(?:[.,]\d{1,2})?`
const SYM = String.raw`[€£$¥฿]`
const CODE = CODES.join('|')
const RANGE_SEP = String.raw`\s?[–-]\s?`

// Prefix form: "€59", "£56–£60", "AUD 330", "USD 1 300".
const PREFIX = new RegExp(
  String.raw`(${SYM}|(?:${CODE})\s?)(${NUM})(?:${RANGE_SEP}(?:${SYM}|(?:${CODE})\s?)?(${NUM}))?`,
  'g',
)
// Suffix form: "1 200 THB", "749 kr", "19 800 yen", "2,500–3,000 USD".
const SUFFIX = new RegExp(
  String.raw`(${NUM})(?:${RANGE_SEP}(${NUM}))?\s?(${CODE}|kr|yen|baht|บาท)(?![A-Za-z])`,
  'g',
)

/** Parses "1 200" / "15,000" / "47,75" / "43.50" into a number. */
export function parseAmount(raw: string): number {
  const s = raw.replace(/[\s\u00a0]/g, '')
  // A separator followed by exactly 1–2 trailing digits is a decimal mark;
  // a separator before a group of three is a thousands separator.
  const decimal = s.match(/^(.*?)[.,](\d{1,2})$/)
  const whole = (decimal ? decimal[1] : s).replace(/[.,]/g, '')
  return Number(whole + (decimal ? '.' + decimal[2] : ''))
}

function krCurrency(country: string): string {
  if (country === 'SE') return 'SEK'
  if (country === 'DK') return 'DKK'
  return 'NOK'
}

function formatNok(values: number[], lang: Lang): string {
  const fmt = new Intl.NumberFormat(intlLocale(lang), { maximumFractionDigits: 0 })
  const rounded = values.map(v => fmt.format(Math.round(v / 10) * 10))
  const amount = rounded.join('–')
  if (lang === 'nb') return `≈ ${amount} kr`
  if (lang === 'th') return `≈ ${amount} NOK`
  return `≈ NOK ${amount}`
}

/**
 * Adds an approximate kroner amount after every foreign-currency price in a
 * text: "€59 til 1. nov" → "€59 (≈ 630 kr) til 1. nov". Amounts already in
 * NOK, and currencies without a rate, are left as written.
 */
export function annotatePrices(text: string, rates: Rates, lang: Lang, country = ''): string {
  if (!text) return text
  const inserts: { at: number; text: string }[] = []
  const taken: [number, number][] = []
  const overlaps = (s: number, e: number) => taken.some(([a, b]) => s < b && e > a)

  const add = (start: number, end: number, currency: string, amounts: string[]) => {
    if (overlaps(start, end)) return
    taken.push([start, end])
    const rate = rates[currency]
    if (!rate || currency === 'NOK') return
    const values = amounts.map(a => parseAmount(a) * rate)
    if (values.some(v => !Number.isFinite(v) || v <= 0)) return
    inserts.push({ at: end, text: ` (${formatNok(values, lang)})` })
  }

  for (const m of text.matchAll(PREFIX)) {
    const marker = m[1].trim()
    const currency = SYMBOLS[marker] ?? marker
    add(m.index!, m.index! + m[0].length, currency, m[3] ? [m[2], m[3]] : [m[2]])
  }
  for (const m of text.matchAll(SUFFIX)) {
    const marker = m[3]
    const currency = marker === 'kr' ? krCurrency(country) : WORDS[marker] ?? marker
    add(m.index!, m.index! + m[0].length, currency, m[2] ? [m[1], m[2]] : [m[1]])
  }

  inserts.sort((a, b) => b.at - a.at)
  let out = text
  for (const ins of inserts) out = out.slice(0, ins.at) + ins.text + out.slice(ins.at)
  return out
}

/** Hook form bound to the current rates and a race's country. */
export function usePriceText(lang: Lang) {
  const rates = useRates()
  return (text: string | undefined, event?: Pick<RaceEvent, 'country'>) =>
    annotatePrices(text ?? '', rates, lang, event?.country)
}

/** Lowercases and strips accents, so "maraton" finds "Maratón" and "munchen" finds "München". */
export function fold(s: string): string {
  return s.normalize('NFD').replace(/\p{Diacritic}/gu, '').toLowerCase()
}

const DISTANCE_WORDS: Record<string, string> = {
  half: 'half halv halvmaraton halvmarathon half-marathon halfmarathon semi demi medio mitja meia ฮาล์ฟ 21k 21km',
  marathon: 'marathon maraton marato maratona full hel helmaraton มาราธอน 42k 42km',
  other: '',
}

/**
 * Every word of the query must appear somewhere in the race's name, places
 * (any language), country, distance words (any language) or series.
 */
export function matchesQuery(e: RaceEvent, query: string, distance: 'half' | 'marathon' | 'other'): boolean {
  const words = fold(query).split(/\s+/).filter(Boolean)
  if (words.length === 0) return true
  const hay = fold([
    e.name,
    e.country,
    ...Object.values(e.texts).map(t => t?.place ?? ''),
    DISTANCE_WORDS[distance],
    e.series.join(' '),
    e.series.includes('majors') ? 'world marathon majors wmm' : '',
    e.series.includes('emc') ? 'european marathon classics' : '',
  ].join(' '))
  return words.every(w => hay.includes(w))
}
