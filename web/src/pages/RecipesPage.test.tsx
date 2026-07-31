// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, within, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import RecipesPage from './RecipesPage'
import type { Recipe } from '../types/recipes'

// ── Translation mock ──────────────────────────────────────────────────────────
// `mockT` must be a stable reference: the list and cook-again hooks both have
// `t` in their effect dependencies, so a fresh function per render would
// re-fetch forever.

const TRANSLATIONS: Record<string, string> = {
  'list.title': 'Recipes',
  'list.subtitle': 'Everything you cook, in one place',
  'list.search': 'Search recipes',
  'list.searchPlaceholder': 'Search by title, ingredient or tag',
  'list.create': 'New recipe',
  'list.import': 'Import from URL',
  'list.importPlaceholder': 'https://example.com/recipe',
  'list.importSubmit': 'Import',
  'list.importCancel': 'Cancel',
  'list.importing': 'Reading the page…',
  'list.cookAgain': 'Cook again',
  'list.cookAgainSubtitle': 'Seasonal picks first, then whatever you have gone longest without making',
  'list.lastCooked': 'Last cooked {{date}}',
  'list.neverCooked': 'Never cooked',
  'list.loading': 'Loading recipes…',
  'list.open': 'Open recipe',
  'planner.open': 'Meal plan',
  'filters.label': 'Filters',
  'filters.cuisine': 'Cuisine',
  'filters.season': 'Season',
  'filters.occasion': 'Occasion',
  'filters.clear': 'Clear filters',
  'filters.cuisineValues.italian': 'Italian',
  'filters.cuisineValues.thai': 'Thai',
  'filters.cuisineValues.norwegian': 'Norwegian',
  'filters.seasonValues.summer': 'Summer',
  'filters.seasonValues.winter': 'Winter',
  'filters.occasionValues.weeknight': 'Weeknight',
  'filters.occasionValues.guests': 'Guests',
  'rating.none': 'Not rated',
  'rating.value': '{{value}} of 5',
  'empty.title': 'No recipes yet',
  'empty.description': 'Create your first recipe, or import one from a web page.',
  'empty.action': 'New recipe',
  'empty.filtered': 'No recipes match these filters',
  'empty.filteredAction': 'Clear filters',
  'errors.failedToLoad': 'Could not load recipes',
  'errors.failedToImport': 'Could not import a recipe from that page',
}

function mockT(key: string, opts?: Record<string, string | number>): string {
  if (key === 'list.servings') {
    const count = Number(opts?.count ?? 0)
    return `${count} serving${count === 1 ? '' : 's'}`
  }
  if (key === 'list.resultCount') {
    const count = Number(opts?.count ?? 0)
    return `${count} recipe${count === 1 ? '' : 's'}`
  }
  const raw = TRANSLATIONS[key]
  if (raw === undefined) return key
  return raw.replace(/\{\{(\w+)\}\}/g, (_, name: string) => String(opts?.[name] ?? ''))
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: mockT,
    i18n: { language: 'en' },
  }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

// `formatDate` reads the active language off the i18n singleton; pin it so the
// assertions below exercise the real Intl formatting for a known locale.
vi.mock('../i18n', () => ({
  default: { language: 'en' },
}))

// ── Router mock ───────────────────────────────────────────────────────────────
// Everything but `useNavigate` stays real so <Link> still renders anchors.

const navigateMock = vi.fn()

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom')
  return { ...actual, useNavigate: () => navigateMock }
})

// ── Fixtures ──────────────────────────────────────────────────────────────────

function makeRecipe(over: Partial<Recipe> = {}): Recipe {
  return {
    id: 1,
    title: 'Pasta',
    notes: '',
    servings: 4,
    rating: null,
    rated_at: null,
    last_cooked_at: null,
    created_at: '2026-01-01T12:00:00Z',
    updated_at: '2026-01-01T12:00:00Z',
    ingredients: [],
    steps: [],
    tags: [],
    ...over,
  }
}

const CARBONARA = makeRecipe({
  id: 1,
  title: 'Carbonara',
  rating: 4,
  last_cooked_at: '2026-03-05T12:00:00Z',
  tags: ['italian', 'winter', 'weeknight'],
})

const GREEN_CURRY = makeRecipe({
  id: 2,
  title: 'Green curry',
  rating: 5,
  last_cooked_at: '2026-06-20T12:00:00Z',
  tags: ['thai', 'summer', 'weeknight'],
})

const RAKFISK = makeRecipe({
  id: 3,
  title: 'Rakfisk',
  rating: null,
  last_cooked_at: null,
  tags: ['norwegian', 'winter', 'guests'],
})

const ALL_RECIPES = [CARBONARA, GREEN_CURRY, RAKFISK]

const IMPORTED_RECIPE = makeRecipe({ id: 0, title: 'Imported cake', tags: ['baking'] })

// ── Fetch mock ────────────────────────────────────────────────────────────────

function jsonResponse(body: unknown, status = 200) {
  return { ok: status < 400, status, json: () => Promise.resolve(body) }
}

/**
 * Routes the three endpoints the page touches. `cookAgain` defaults to empty so
 * the suggestions section stays hidden and recipe titles appear exactly once —
 * the cook-again test opts in.
 */
function mockFetch(options: { list?: Recipe[]; cookAgain?: Recipe[]; importStatus?: number } = {}) {
  const { list = ALL_RECIPES, cookAgain = [], importStatus = 200 } = options
  const fetchMock = vi.fn((input: string, _init?: RequestInit) => {
    const url = String(input)
    if (url.startsWith('/api/recipes/cook-again')) {
      return Promise.resolve(jsonResponse({ recipes: cookAgain }))
    }
    if (url.startsWith('/api/recipes/import')) {
      return Promise.resolve(
        importStatus === 200
          ? jsonResponse({ recipe: IMPORTED_RECIPE })
          : jsonResponse({ error: 'could not find a recipe on that page' }, importStatus),
      )
    }
    return Promise.resolve(jsonResponse({ recipes: list }))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function renderPage() {
  return render(
    <MemoryRouter>
      <RecipesPage />
    </MemoryRouter>,
  )
}

/** Titles currently rendered in the main recipe list, in order. */
function listedTitles(): string[] {
  const list = screen.getByRole('region', { name: 'Recipes' })
  return within(list)
    .getAllByRole('heading', { level: 3 })
    .map(h => h.textContent ?? '')
}

async function renderAndWait(options?: Parameters<typeof mockFetch>[0]) {
  const fetchMock = mockFetch(options)
  renderPage()
  await screen.findByRole('region', { name: 'Recipes' })
  return fetchMock
}

/** How many times the list endpoint has been hit. */
function listRequestCount(fetchMock: ReturnType<typeof mockFetch>): number {
  return fetchMock.mock.calls.filter(call => {
    const url = String(call[0])
    return url.startsWith('/api/recipes') && !url.startsWith('/api/recipes/')
  }).length
}

function clickChip(name: string) {
  fireEvent.click(screen.getByRole('button', { name }))
}

beforeEach(() => {
  navigateMock.mockReset()
})

afterEach(() => {
  vi.unstubAllGlobals()
})

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('RecipesPage', () => {
  it('lists every recipe once loaded', async () => {
    await renderAndWait()
    expect(listedTitles()).toEqual(['Carbonara', 'Green curry', 'Rakfisk'])
    expect(screen.getByText('3 recipes')).toBeTruthy()
  })

  it('renders the rating and the last-cooked date formatted for the locale', async () => {
    await renderAndWait()

    const card = screen.getByRole('link', { name: 'Open recipe: Carbonara' })
    expect(within(card).getByLabelText('4 of 5')).toBeTruthy()

    const expectedDate = new Date('2026-03-05T12:00:00Z').toLocaleDateString('en', {
      year: 'numeric',
      month: 'short',
      day: 'numeric',
    })
    expect(within(card).getByText(`Last cooked ${expectedDate}`)).toBeTruthy()

    // A recipe that has never been cooked says so instead of showing a date.
    const never = screen.getByRole('link', { name: 'Open recipe: Rakfisk' })
    expect(within(never).getByText('Never cooked')).toBeTruthy()
    expect(within(never).getByLabelText('Not rated')).toBeTruthy()
  })

  it('filters by cuisine', async () => {
    await renderAndWait()
    clickChip('Italian')
    expect(listedTitles()).toEqual(['Carbonara'])
  })

  it('filters by season', async () => {
    await renderAndWait()
    clickChip('Winter')
    expect(listedTitles()).toEqual(['Carbonara', 'Rakfisk'])
  })

  it('filters by occasion', async () => {
    await renderAndWait()
    clickChip('Guests')
    expect(listedTitles()).toEqual(['Rakfisk'])
  })

  it('ORs selections within one dimension', async () => {
    await renderAndWait()
    clickChip('Italian')
    clickChip('Thai')
    expect(listedTitles()).toEqual(['Carbonara', 'Green curry'])
  })

  it('ANDs selections across dimensions', async () => {
    await renderAndWait()
    // Winter OR summer, AND a weeknight occasion: Rakfisk is winter but is a
    // guests recipe, so it drops out even though it matches one dimension.
    clickChip('Winter')
    clickChip('Summer')
    clickChip('Weeknight')
    expect(listedTitles()).toEqual(['Carbonara', 'Green curry'])
  })

  it('shows the filtered empty state when no recipe matches every dimension', async () => {
    await renderAndWait()
    clickChip('Italian')
    clickChip('Guests')

    expect(screen.queryByRole('region', { name: 'Recipes' })).toBeNull()
    expect(screen.getByText('No recipes match these filters')).toBeTruthy()

    // The filter row and the empty state both offer "Clear filters"; either
    // one restores the list.
    fireEvent.click(screen.getAllByRole('button', { name: 'Clear filters' })[0])
    expect(listedTitles()).toEqual(['Carbonara', 'Green curry', 'Rakfisk'])
  })

  it('restores the full list when a chip is deselected', async () => {
    await renderAndWait()

    clickChip('Italian')
    expect(listedTitles()).toEqual(['Carbonara'])

    clickChip('Italian')
    expect(listedTitles()).toEqual(['Carbonara', 'Green curry', 'Rakfisk'])
  })

  it('narrows the list as the user types without re-requesting it', async () => {
    const fetchMock = await renderAndWait()
    expect(listRequestCount(fetchMock)).toBe(1)

    const box = screen.getByLabelText('Search recipes')
    // One change event per keystroke: if search were a query parameter, each of
    // these would cost a round trip.
    for (const value of ['c', 'cu', 'cur', 'curr', 'curry']) {
      fireEvent.change(box, { target: { value } })
    }

    expect(listedTitles()).toEqual(['Green curry'])
    expect(listRequestCount(fetchMock)).toBe(1)

    fireEvent.change(box, { target: { value: '' } })
    expect(listedTitles()).toEqual(['Carbonara', 'Green curry', 'Rakfisk'])
    expect(listRequestCount(fetchMock)).toBe(1)
  })

  it('combines the search box with the tag chips', async () => {
    await renderAndWait()

    clickChip('Weeknight')
    fireEvent.change(screen.getByLabelText('Search recipes'), { target: { value: 'carbo' } })
    expect(listedTitles()).toEqual(['Carbonara'])

    // Rakfisk matches the text but not the occasion chip, so nothing is left.
    fireEvent.change(screen.getByLabelText('Search recipes'), { target: { value: 'rakfisk' } })
    expect(screen.queryByRole('region', { name: 'Recipes' })).toBeNull()
    expect(screen.getByText('No recipes match these filters')).toBeTruthy()
  })

  it('offers only the tag values the fetched recipes actually carry', async () => {
    await renderAndWait({ list: [CARBONARA] })
    expect(screen.getByRole('button', { name: 'Italian' })).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Thai' })).toBeNull()
  })

  it('shows the cook-again suggestions in the order the endpoint returns them', async () => {
    await renderAndWait({ cookAgain: [RAKFISK, CARBONARA] })

    const section = screen.getByRole('region', { name: 'Cook again' })
    const suggested = within(section)
      .getAllByRole('heading', { level: 3 })
      .map(h => h.textContent)
    expect(suggested).toEqual(['Rakfisk', 'Carbonara'])
    expect(within(section).getByText('Never cooked')).toBeTruthy()
  })

  it('hides the cook-again suggestions while a filter narrows the list', async () => {
    await renderAndWait({ cookAgain: [RAKFISK, CARBONARA] })
    expect(screen.getByRole('region', { name: 'Cook again' })).toBeTruthy()

    clickChip('Italian')
    expect(screen.queryByRole('region', { name: 'Cook again' })).toBeNull()
  })

  it('routes the create button to the detail page in create mode', async () => {
    await renderAndWait()
    fireEvent.click(screen.getByRole('button', { name: 'New recipe' }))
    expect(navigateMock).toHaveBeenCalledWith('/recipes/new')
  })

  it('imports a URL and hands the parsed recipe to the detail page in create mode', async () => {
    const fetchMock = mockFetch()
    renderPage()
    await screen.findByRole('region', { name: 'Recipes' })

    fireEvent.click(screen.getByRole('button', { name: 'Import from URL' }))
    fireEvent.change(screen.getByLabelText('Import from URL'), {
      target: { value: 'https://example.com/cake' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Import' }))

    await waitFor(() => {
      expect(navigateMock).toHaveBeenCalledWith('/recipes/new', {
        state: { importedRecipe: IMPORTED_RECIPE, importUrl: 'https://example.com/cake' },
      })
    })

    const importCall = fetchMock.mock.calls.find(call => String(call[0]).includes('/import'))
    expect(importCall).toBeTruthy()
    expect(JSON.parse((importCall![1] as RequestInit).body as string)).toEqual({
      url: 'https://example.com/cake',
    })
  })

  it('reports a failed import without navigating away', async () => {
    mockFetch({ importStatus: 422 })
    renderPage()
    await screen.findByRole('region', { name: 'Recipes' })

    fireEvent.click(screen.getByRole('button', { name: 'Import from URL' }))
    fireEvent.change(screen.getByLabelText('Import from URL'), {
      target: { value: 'https://example.com/not-a-recipe' },
    })
    fireEvent.click(screen.getByRole('button', { name: 'Import' }))

    await screen.findByText('could not find a recipe on that page')
    expect(navigateMock).not.toHaveBeenCalled()
  })

  it('shows the empty state when the user has no recipes', async () => {
    mockFetch({ list: [] })
    renderPage()
    await screen.findByText('No recipes yet')

    // Header button plus the empty-state call to action, both create-mode.
    const createButtons = screen.getAllByRole('button', { name: 'New recipe' })
    expect(createButtons).toHaveLength(2)
    fireEvent.click(createButtons[1])
    expect(navigateMock).toHaveBeenCalledWith('/recipes/new')
  })

  it('surfaces a list error', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(jsonResponse({ error: 'nope' }, 500))),
    )
    renderPage()
    await screen.findByText('nope')
  })
})
