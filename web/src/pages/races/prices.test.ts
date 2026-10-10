import { describe, it, expect } from 'vitest'
import { annotatePrices, fold, matchesQuery, parseAmount } from './prices'
import type { RaceEvent } from './racesApi'

const rates = { NOK: 1, EUR: 10.7155, GBP: 12.6417, USD: 9.5623, SEK: 0.9595, AUD: 6.6755, JPY: 0.060423, THB: 0.29 }
// Intl uses (narrow) no-break spaces as group separators; compare on plain spaces.
const plain = (s: string) => s.replace(/[\s\u00a0\u202f]/g, ' ')

describe('parseAmount', () => {
  it('reads nb and en number formats', () => {
    expect(parseAmount('59')).toBe(59)
    expect(parseAmount('1 200')).toBe(1200)
    expect(parseAmount('15,000')).toBe(15000)
    expect(parseAmount('47,75')).toBe(47.75)
    expect(parseAmount('43.50')).toBe(43.5)
    expect(parseAmount('19 800')).toBe(19800)
  })
})

describe('annotatePrices', () => {
  it('adds kroner after euro tiers in Norwegian text', () => {
    const out = annotatePrices('€59 til 1. nov, €69 til 7. jan, pluss €4 dagslisens', rates, 'nb')
    expect(plain(out)).toBe('€59 (≈ 630 kr) til 1. nov, €69 (≈ 740 kr) til 7. jan, pluss €4 (≈ 40 kr) dagslisens')
  })

  it('handles ranges, decimal commas, codes and suffixes', () => {
    expect(plain(annotatePrices('ca. £56–£60', rates, 'nb'))).toBe('ca. £56–£60 (≈ 710–760 kr)')
    expect(plain(annotatePrices('ca. £70 pluss £3,50 i gebyr', rates, 'nb'))).toBe('ca. £70 (≈ 880 kr) pluss £3,50 (≈ 40 kr) i gebyr')
    expect(plain(annotatePrices('AUD 330 for international runners', rates, 'en'))).toBe('AUD 330 (≈ NOK 2,200) for international runners')
    expect(plain(annotatePrices('¥19,800 for Japanese runners', rates, 'en'))).toBe('¥19,800 (≈ NOK 1,200) for Japanese runners')
    expect(plain(annotatePrices('1 200 THB for utlendinger', rates, 'nb'))).toBe('1 200 THB (≈ 350 kr) for utlendinger')
  })

  it('reads "kr" by the race country and leaves NOK alone', () => {
    expect(plain(annotatePrices('800 kr til 31. jan', rates, 'nb', 'NO'))).toBe('800 kr til 31. jan')
    expect(plain(annotatePrices('749 kr', rates, 'nb', 'SE'))).toBe('749 kr (≈ 720 kr)')
    expect(plain(annotatePrices('NOK 990', rates, 'en'))).toBe('NOK 990')
  })

  it('leaves text untouched without a rate and ignores non-prices', () => {
    expect(annotatePrices('CHF 50', rates, 'en')).toBe('CHF 50')
    expect(annotatePrices('15 000 plasser, start 08.30', rates, 'nb')).toBe('15 000 plasser, start 08.30')
    expect(annotatePrices('', rates, 'nb')).toBe('')
  })
})

function race(name: string, distance_m: number, places: string[], series: RaceEvent['series'] = []): RaceEvent {
  return {
    id: 1, slug: 'x', name, edition_year: 2027, race_date: '2027-10-24', date_precision: 'day', country: 'ES',
    distance_m, status: 'later', entry_type: 'fcfs', travel: 'nearby', url: '', series,
    texts: { nb: { place: places[0], participants: '', course: '', travel: '', how: '', price: '' },
      en: { place: places[1] ?? places[0], participants: '', course: '', travel: '', how: '', price: '' } },
    scope: 'away', distances: [], place: '', lat: null, lng: null, source: '', source_id: '',
    checked_at: '', created_at: '', updated_at: '', deadlines: [],
  }
}

describe('matchesQuery', () => {
  const valenciaHalf = race('Medio Maratón Valencia', 21097, ['Valencia, Spania', 'Valencia, Spain'])
  const munich = race('Generali München Marathon', 42195, ['München, Tyskland', 'Munich, Germany'], ['majors'])

  it('matches every word anywhere, including distance words in any language', () => {
    expect(matchesQuery(valenciaHalf, 'valencia half', 'half')).toBe(true)
    expect(matchesQuery(valenciaHalf, 'Valencia halvmaraton', 'half')).toBe(true)
    expect(matchesQuery(valenciaHalf, 'valencia full', 'half')).toBe(false)
    expect(matchesQuery(valenciaHalf, 'spain', 'half')).toBe(true)
  })

  it('ignores accents and case', () => {
    expect(fold('Maratón München')).toBe('maraton munchen')
    expect(matchesQuery(valenciaHalf, 'maraton', 'half')).toBe(true)
    expect(matchesQuery(munich, 'munchen full marathon', 'marathon')).toBe(true)
    expect(matchesQuery(munich, 'world marathon majors', 'marathon')).toBe(true)
  })
})
