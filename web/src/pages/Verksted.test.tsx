// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import Verksted from './Verksted'
import { daysToChristmasEve, shareOf, type Gift, type Overview } from './verksted/api'

function mockT(key: string, opts?: Record<string, unknown>): string {
  if (key === 'gift.fromSuggestions') return 'A|B'
  if (!opts) return key
  return `${key}:${Object.entries(opts).map(([k, v]) => `${k}=${v}`).join(',')}`
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: mockT, i18n: { language: 'en' } }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))
vi.mock('../i18n', () => ({ default: { language: 'en' } }))
vi.mock('../auth', () => ({ useAuth: () => ({ user: { id: 1, is_admin: true } }) }))

const gift = (over: Partial<Gift>): Gift => ({
  id: 1, season: 2026, title: 'Sykkel', notes: '', url: '', shop: 'XXL', giver: 'Mamma og pappa', price_nok: 2400,
  status: 'ordered', wish_id: null, recipient_ids: [2], ...over,
})

const OVERVIEW: Overview = {
  season: 2026,
  seasons: [2026, 2025],
  total_budget_nok: 10000,
  people: [
    { id: 1, name: 'Robin', group: 'Familie', user_id: 1, birth_year: 1983, sizes: '', notes: '', archived: false, budget_nok: 0 },
    { id: 2, name: 'James', group: 'Familie', user_id: 2, birth_year: 2015, sizes: 'M', notes: '', archived: false, budget_nok: 3000 },
    { id: 3, name: 'Olivia', group: 'Familie', user_id: null, birth_year: 2023, sizes: '', notes: '', archived: false, budget_nok: 0 },
  ],
  wishes: [{ id: 7, person_id: 3, title: 'Dukke', url: '', notes: '', price_nok: 300, priority: 3, reserved: false, reserved_by: null, gift_id: null }],
  gifts: [gift({}), gift({ id: 2, title: 'Spill', status: 'idea', shop: '', price_nok: 400, recipient_ids: [2, 3] })],
}

function mockFetch() {
  const calls: { url: string; init?: RequestInit }[] = []
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    calls.push({ url, init })
    if (url.startsWith('/api/verksted?') || url === '/api/verksted') {
      return new Response(JSON.stringify(OVERVIEW), { status: 200, headers: { 'Content-Type': 'application/json' } })
    }
    return new Response(JSON.stringify({ id: 9 }), { status: 201, headers: { 'Content-Type': 'application/json' } })
  }))
  return calls
}

afterEach(() => vi.unstubAllGlobals())

describe('helpers', () => {
  it('splits shared gifts and counts down to Christmas Eve', () => {
    expect(shareOf(gift({ price_nok: 400, recipient_ids: [2, 3] }))).toBe(200)
    expect(daysToChristmasEve(2026, new Date(2026, 11, 20))).toBe(4)
    expect(daysToChristmasEve(2026, new Date(2026, 11, 24))).toBe(0)
  })
})

describe('Verksted', () => {
  it('shows people with budgets, and the own person without gifts', async () => {
    mockFetch()
    render(<Verksted />)
    expect(await screen.findByText('James')).toBeTruthy()
    expect(screen.getByText('summary.spent:spent=NOK 2,800')).toBeTruthy()
    // James: bike 2400 + half the game 200 of a 3000 budget.
    expect(screen.getByText('person.budgetUsed:spent=NOK 2,600,budget=NOK 3,000')).toBeTruthy()
    // Robin's own card shows only wishes, never gift counts or budget.
    fireEvent.click(screen.getByRole('button', { name: /^Robin/ }))
    expect(screen.queryByText('person.giftsTitle')).toBeNull()
  })

  it('plans a wish as a gift with the wish prefilled', async () => {
    const calls = mockFetch()
    render(<Verksted />)
    fireEvent.click(await screen.findByRole('button', { name: /^Olivia/ }))
    fireEvent.click(screen.getByRole('button', { name: /wish.makeGift/ }))
    expect((screen.getByLabelText('gift.titleLabel') as HTMLInputElement).value).toBe('Dukke')
    fireEvent.click(screen.getByRole('button', { name: 'form.save' }))
    await waitFor(() => expect(calls.some(c => c.url === '/api/verksted/gifts')).toBe(true))
    const body = JSON.parse(String(calls.find(c => c.url === '/api/verksted/gifts')?.init?.body))
    expect(body).toMatchObject({ title: 'Dukke', recipient_ids: [3], wish_id: 7, price_nok: 300, season: 2026 })
  })

  it('lists what is left to buy by shop', async () => {
    mockFetch()
    render(<Verksted />)
    fireEvent.click(await screen.findByRole('tab', { name: 'tabs.shopping' }))
    expect(screen.getByText('gift.noShop')).toBeTruthy()
    expect(screen.getByText('shopping.total:total=NOK 400')).toBeTruthy()
  })
})
