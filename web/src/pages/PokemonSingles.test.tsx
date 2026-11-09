// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import PokemonSingles from './PokemonSingles'
import { bestOffer, wantsListBlocks, type MissingCard } from '../components/pokemon/singles'

function mockT(key: string, opts?: Record<string, unknown>): string {
  if (!opts) return key
  const parts = Object.entries(opts).map(([k, v]) => `${k}=${v}`)
  return `${key}:${parts.join(',')}`
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: mockT, i18n: { language: 'en' } }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))
vi.mock('../i18n', () => ({ default: { language: 'en' } }))
vi.mock('../auth', () => ({ useAuth: () => ({ user: { id: 1, is_admin: true } }) }))

function card(over: Partial<MissingCard> = {}): MissingCard {
  return {
    id: 'me3-50', set_id: 'me3', name: 'Gengar', collector_no: '50', rarity: 'Rare',
    image_small_url: '', image_large_url: '', cardmarket_eur: 0.5, cardmarket_nok: 6, cardmarket_url: 'https://www.cardmarket.com/x',
    offers: [], ...over,
  }
}

const gengar = card({ offers: [
  { shop_id: 1, shop_name: 'Pokehandel', variant_id: '11', print: 'reverse', condition: 'NM', price_nok: 9, available: true, url: 'https://pokehandel.no/p' },
  { shop_id: 2, shop_name: 'Laboge', variant_id: '12', print: 'normal', condition: '', price_nok: 6, available: false, url: 'https://laboge.no/p' },
] })
const ariados = card({ id: 'me3-2', name: 'Ariados', collector_no: '2' })

const PAGE = {
  sets: [{ id: 'me3', name: 'Perfect Order', release_date: '2026/03/27', logo_url: '', symbol_url: '', total_cards: 124, missing_count: 2, set_code: 'POR' }],
  cards: [ariados, gengar],
  shops: [{ id: 1, name: 'Pokehandel', base_url: 'https://pokehandel.no', shipping_nok: 69, free_over_nok: 0, enabled: true, last_checked_at: '2026-11-08T17:20:00Z', last_error: '' }],
  refreshing: false,
}

function mockFetch() {
  const calls: { url: string; init?: RequestInit }[] = []
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    calls.push({ url, init })
    const json = (body: unknown) => new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } })
    if (url === '/api/pokemon/singles') return json(PAGE)
    if (url.startsWith('/api/pokemon/sets')) return json({ sets: [{ id: 'me3', name: 'Perfect Order', release_date: '' }, { id: 'me06', name: 'Delta Reign', release_date: '' }] })
    if (url === '/api/pokemon/singles/export') {
      const ids = (JSON.parse(String(init?.body)) as { card_ids: string[] }).card_ids
      const names: Record<string, string> = { 'me3-2': '1x Ariados Spider Trap', 'me3-50': '1x Gengar Shadow Bind' }
      return json({ lines: ids.map(id => names[id]) })
    }
    if (url === '/api/pokemon/singles/basket') {
      return json({
        shops: [{ shop_id: 1, shop_name: 'Pokehandel', items: [{ card_id: 'me3-50', name: 'Gengar #50', variant_id: '11', print: 'reverse', condition: 'NM', price_nok: 9, url: '' }],
          subtotal_nok: 9, shipping_nok: 69, total_nok: 78, cart_url: 'https://pokehandel.no/cart/11:1' }],
        unavailable: [], total_nok: 78,
      })
    }
    return json({})
  }))
  return calls
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('bestOffer', () => {
  it('returns the first offer only when it is in stock', () => {
    expect(bestOffer(gengar)?.variant_id).toBe('11')
    expect(bestOffer(card({ offers: [{ ...gengar.offers[1] }] }))).toBeUndefined()
  })
})

describe('PokemonSingles', () => {
  it('lists missing cards with the best in-stock offer and plans a basket', async () => {
    const calls = mockFetch()
    render(<MemoryRouter><PokemonSingles /></MemoryRouter>)

    expect(await screen.findByText('Perfect Order')).toBeTruthy()
    expect(screen.getByText(/Pokehandel/, { selector: 'a span' })).toBeTruthy()
    expect(screen.getByText('singles.notInStock')).toBeTruthy()

    // The filter hides cards nobody stocks.
    fireEvent.click(screen.getByRole('radio', { name: 'singles.filter.inStock' }))
    expect(screen.queryByText('Ariados', { exact: false })).toBeNull()

    fireEvent.click(screen.getByRole('checkbox', { name: 'singles.select:name=Gengar,number=50' }))
    fireEvent.click(screen.getByRole('button', { name: /singles.basket.plan/ }))
    const checkout = await screen.findByRole('link', { name: /singles.basket.checkout:shop=Pokehandel/ })
    expect(checkout.getAttribute('href')).toBe('https://pokehandel.no/cart/11:1')
    const basketCall = calls.find(c => c.url === '/api/pokemon/singles/basket')
    expect(JSON.parse(String(basketCall?.init?.body))).toEqual({ card_ids: ['me3-50'] })
  })

  it('adds a set from the picker', async () => {
    const calls = mockFetch()
    render(<MemoryRouter><PokemonSingles /></MemoryRouter>)
    await screen.findByText('Perfect Order')
    const select = screen.getByLabelText('singles.sets.addLabel') as HTMLSelectElement
    await waitFor(() => expect(select.querySelectorAll('option').length).toBe(2)) // placeholder + Delta Reign
    fireEvent.change(select, { target: { value: 'me06' } })
    fireEvent.click(screen.getByRole('button', { name: /singles.sets.add$/ }))
    await waitFor(() => expect(calls.some(c => c.url === '/api/pokemon/singles/sets' && c.init?.method === 'POST')).toBe(true))
    const add = calls.find(c => c.url === '/api/pokemon/singles/sets')
    expect(JSON.parse(String(add?.init?.body))).toEqual({ set_id: 'me06' })
  })

  it('exports cards no Norwegian shop has as a Cardmarket wants list', async () => {
    mockFetch()
    render(<MemoryRouter><PokemonSingles /></MemoryRouter>)
    await screen.findByText('Perfect Order')
    fireEvent.click(screen.getByRole('button', { name: /singles.export.button/ }))
    // Gengar is in stock in Norway, so only Ariados is exported by default.
    const list = await screen.findByLabelText('singles.export.listLabel:part=1') as HTMLTextAreaElement
    expect(list.value).toBe('1x Ariados Spider Trap')
    fireEvent.click(screen.getByRole('radio', { name: 'singles.export.scope.all' }))
    await waitFor(() => expect((screen.getByLabelText('singles.export.listLabel:part=1') as HTMLTextAreaElement).value)
      .toBe('1x Ariados Spider Trap\n1x Gengar Shadow Bind'))
  })
})

describe('Cardmarket version checklist', () => {
  it('links each card to its exact Cardmarket version and remembers ticks', async () => {
    mockFetch()
    window.localStorage.clear()
    render(<MemoryRouter><PokemonSingles /></MemoryRouter>)
    await screen.findByText('Perfect Order')
    fireEvent.click(screen.getByRole('button', { name: /singles.export.button/ }))
    fireEvent.click(screen.getByRole('radio', { name: 'singles.export.scope.all' }))
    const links = screen.getAllByRole('link', { name: /singles.export.openVersion/ })
    expect(links).toHaveLength(2)
    expect(links[0].getAttribute('href')).toBe('https://www.cardmarket.com/x')
    expect(screen.getByText('singles.export.checklistProgress:done=0,total=2')).toBeTruthy()
    fireEvent.click(screen.getByRole('checkbox', { name: 'singles.export.markAdded:name=Ariados,number=2' }))
    expect(screen.getByText('singles.export.checklistProgress:done=1,total=2')).toBeTruthy()
    expect(JSON.parse(window.localStorage.getItem('hytte.pokemon.singles.cardmarketAdded') ?? '[]')).toEqual(['me3-2'])
  })
})

describe('wantsListBlocks', () => {
  it('splits at 150 lines', () => {
    const lines = Array.from({ length: 151 }, (_, i) => `1x Card ${i + 1}`)
    const blocks = wantsListBlocks(lines)
    expect(blocks.length).toBe(2)
    expect(blocks[0].split('\n').length).toBe(150)
    expect(blocks[1]).toBe('1x Card 151')
  })
})
