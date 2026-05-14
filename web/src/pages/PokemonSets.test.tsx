// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import PokemonSets from './PokemonSets'

const TRANSLATIONS: Record<string, string> = {
  'pageTitle': 'Pokémon Sets',
  'showOlder': 'Show older sets',
  'hideOlder': 'Hide older sets',
  'olderSets': 'Older sets',
  'errors.failedToLoad': 'Failed to load Pokémon sets',
  'retry': 'Retry',
  'setDetail.comingSoon': 'Set detail coming soon',
}

function mockT(key: string, opts?: Record<string, string | number>): string {
  if (key === 'tile.ownership') return `${opts?.owned ?? 0} / ${opts?.total ?? 0} — ${opts?.percent ?? 0}%`
  if (key === 'tile.openSet') return `Open ${opts?.name ?? ''}`
  if (key === 'tile.totalCards') {
    const count = Number(opts?.count ?? 0)
    return count === 1 ? `${count} card` : `${count} cards`
  }
  return TRANSLATIONS[key] ?? key
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: mockT,
    i18n: { language: 'en' },
  }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

function makeSet(overrides: Partial<{
  id: string
  name: string
  series: string
  release_date: string
  total_cards: number
  symbol_url: string
  logo_url: string
  owned_count: number
}> = {}) {
  return {
    id: 'sv1',
    name: 'Scarlet & Violet Base',
    series: 'Scarlet & Violet',
    release_date: '2023/03/31',
    total_cards: 198,
    symbol_url: '',
    logo_url: '',
    owned_count: 0,
    ...overrides,
  }
}

type SetShape = ReturnType<typeof makeSet>

function setsResponse(sets: SetShape[]) {
  return { ok: true, json: () => Promise.resolve({ sets }) }
}

function renderPage() {
  return render(
    <MemoryRouter>
      <PokemonSets />
    </MemoryRouter>,
  )
}

describe('PokemonSets – grouping by series', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('renders one section per recent era', async () => {
    const sv = makeSet({ id: 'sv1', name: 'SV Base', series: 'Scarlet & Violet', release_date: '2023/03/31' })
    const swsh = makeSet({ id: 'swsh1', name: 'SWSH Base', series: 'Sword & Shield', release_date: '2020/02/07' })
    const sm = makeSet({ id: 'sm1', name: 'SM Base', series: 'Sun & Moon', release_date: '2017/02/03' })
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(setsResponse([sv, swsh, sm]))))

    renderPage()

    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'Scarlet & Violet' })).toBeInTheDocument()
    })
    expect(screen.getByRole('heading', { name: 'Sword & Shield' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Sun & Moon' })).toBeInTheDocument()
    // Tile titles
    expect(screen.getByText('SV Base')).toBeInTheDocument()
    expect(screen.getByText('SWSH Base')).toBeInTheDocument()
    expect(screen.getByText('SM Base')).toBeInTheDocument()
  })
})

describe('PokemonSets – expand-older toggle', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('hides legacy era sections behind a toggle by default', async () => {
    const sv = makeSet({ id: 'sv1', name: 'SV Base', series: 'Scarlet & Violet' })
    const xy = makeSet({ id: 'xy1', name: 'XY Base', series: 'XY', release_date: '2014/02/05' })
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(setsResponse([sv, xy]))))

    renderPage()
    await waitFor(() => expect(screen.getByText('SV Base')).toBeInTheDocument())

    // Older section is collapsed: XY heading and tile should not be visible.
    expect(screen.queryByRole('heading', { name: 'XY' })).not.toBeInTheDocument()
    expect(screen.queryByText('XY Base')).not.toBeInTheDocument()

    const toggle = screen.getByRole('button', { name: 'Show older sets' })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    fireEvent.click(toggle)

    expect(screen.getByRole('heading', { name: 'XY' })).toBeInTheDocument()
    expect(screen.getByText('XY Base')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Hide older sets' })).toHaveAttribute('aria-expanded', 'true')
  })

  it('omits the toggle when no older eras are present', async () => {
    const sv = makeSet({ id: 'sv1', name: 'SV Base', series: 'Scarlet & Violet' })
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(setsResponse([sv]))))

    renderPage()
    await waitFor(() => expect(screen.getByText('SV Base')).toBeInTheDocument())
    expect(screen.queryByRole('button', { name: 'Show older sets' })).not.toBeInTheDocument()
  })
})

describe('PokemonSets – ownership percentage', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('renders the owned/total/percent string on each tile', async () => {
    const sv = makeSet({ id: 'sv1', name: 'SV Base', total_cards: 195, owned_count: 12 })
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(setsResponse([sv]))))

    renderPage()
    await waitFor(() => expect(screen.getByText('SV Base')).toBeInTheDocument())
    // 12 / 195 = 6.15% → rounded to 6
    expect(screen.getByText('12 / 195 — 6%')).toBeInTheDocument()
  })

  it('shows 0% when the set has cards but the user owns none', async () => {
    const sv = makeSet({ id: 'sv1', name: 'SV Base', total_cards: 100, owned_count: 0 })
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(setsResponse([sv]))))

    renderPage()
    await waitFor(() => expect(screen.getByText('SV Base')).toBeInTheDocument())
    expect(screen.getByText('0 / 100 — 0%')).toBeInTheDocument()
  })

  it('handles divide-by-zero when total_cards is 0', async () => {
    const sv = makeSet({ id: 'sv1', name: 'Empty Set', total_cards: 0, owned_count: 0 })
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(setsResponse([sv]))))

    renderPage()
    await waitFor(() => expect(screen.getByText('Empty Set')).toBeInTheDocument())
    expect(screen.getByText('0 / 0 — 0%')).toBeInTheDocument()
  })
})

describe('PokemonSets – tile link', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('links each tile to /pokemon/sets/{id}', async () => {
    const sv = makeSet({ id: 'sv1', name: 'SV Base' })
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(setsResponse([sv]))))

    renderPage()
    await waitFor(() => expect(screen.getByText('SV Base')).toBeInTheDocument())

    const link = screen.getByRole('link', { name: 'Open SV Base' })
    expect(link).toHaveAttribute('href', '/pokemon/sets/sv1')
  })
})

describe('PokemonSets – error state', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('shows an inline alert with a retry button when the API fails', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: false })
      .mockResolvedValueOnce(setsResponse([makeSet({ id: 'sv1', name: 'SV Base' })]))
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('Failed to load Pokémon sets')
    })

    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))

    await waitFor(() => expect(screen.getByText('SV Base')).toBeInTheDocument())
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })
})
