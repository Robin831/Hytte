// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router'
import RecipeDetail from './RecipeDetail'
import type { Recipe } from '../types/recipes'

// ── Translation mock ──────────────────────────────────────────────────────────
// `mockT` must be a stable reference: the recipe hook has `t` in its effect
// dependencies, so a fresh function per render would re-fetch forever.

const TRANSLATIONS: Record<string, string> = {
  'detail.back': 'Back to recipes',
  'detail.ingredients': 'Ingredients',
  'detail.steps': 'Method',
  'detail.notes': 'Notes',
  'detail.tags': 'Tags',
  'detail.portions': 'Portions',
  'detail.portionIncrease': 'One more portion',
  'detail.portionDecrease': 'One fewer portion',
  'detail.portionReset': 'Reset to {{count}}',
  'detail.scaledFrom': 'Scaled from {{count}} servings',
  'detail.selectIngredient': 'Select ingredient',
  'detail.selectAll': 'Select all',
  'detail.selectNone': 'Select none',
  'detail.addMissingToGroceryList': 'Add missing to grocery list',
  'detail.addMissingEmpty': 'Select the ingredients you are missing first',
  'detail.addMissingError': 'Could not add the ingredients to the grocery list',
  'detail.addMissingPending': 'Adding…',
  'detail.startCooking': 'Start cooking',
  'detail.markCooked': 'Mark as cooked',
  'detail.markedCooked': 'Logged as cooked',
  'detail.edit': 'Edit recipe',
  'detail.delete': 'Delete recipe',
  'detail.totalTime': 'Total time {{minutes}} min',
  'detail.stepDuration': '{{minutes}} min',
  'detail.noIngredients': 'This recipe has no ingredients yet',
  'detail.noSteps': 'This recipe has no steps yet',
  'detail.loading': 'Loading recipe…',
  'edit.titleField': 'Title',
  'edit.servings': 'Servings the quantities describe',
  'edit.baseQuantitiesHint': 'Quantities are stored for this yield.',
  'edit.notes': 'Notes',
  'edit.tags': 'Tags',
  'edit.addTag': 'Add tag',
  'edit.addTagPlaceholder': 'e.g. italian',
  'edit.removeTag': 'Remove tag {{tag}}',
  'edit.ingredients': 'Ingredients',
  'edit.ingredientText': 'Ingredient line {{index}}',
  'edit.ingredientQuantity': 'Quantity for ingredient {{index}}',
  'edit.ingredientUnit': 'Unit for ingredient {{index}}',
  'edit.ingredientName': 'Name for ingredient {{index}}',
  'edit.addIngredient': 'Add ingredient',
  'edit.removeIngredient': 'Remove ingredient {{index}}',
  'edit.steps': 'Method',
  'edit.stepText': 'Step {{index}}',
  'edit.stepMinutes': 'Minutes for step {{index}}',
  'edit.addStep': 'Add step',
  'edit.removeStep': 'Remove step {{index}}',
  'edit.moveStepUp': 'Move step {{index}} up',
  'edit.moveStepDown': 'Move step {{index}} down',
  'edit.save': 'Save recipe',
  'edit.saving': 'Saving…',
  'edit.cancel': 'Cancel',
  'edit.titleRequired': 'Give the recipe a title',
  'create.title': 'New recipe',
  'rating.label': 'Rating',
  'rating.none': 'Not rated',
  'rating.star': 'Rate {{value}} out of 5',
  'rating.clear': 'Clear rating',
  'rating.value': '{{value}} of 5',
  'list.lastCooked': 'Last cooked {{date}}',
  'list.neverCooked': 'Never cooked',
  'errors.notFound': 'That recipe does not exist',
  'errors.failedToLoadRecipe': 'Could not load this recipe',
  'errors.failedToSave': 'Could not save the recipe',
  'errors.retry': 'Try again',
}

/** The plural keys the page reaches for, resolved the way English pluralises. */
const PLURALS: Record<string, (count: number) => string> = {
  'detail.addMissingSuccess': count =>
    `Added ${count} item${count === 1 ? '' : 's'} to the grocery list`,
  'detail.addMissingSkipped': count =>
    `${count} item${count === 1 ? '' : 's'} ${count === 1 ? 'was' : 'were'} already on the list`,
}

function mockT(key: string, opts?: Record<string, string | number>): string {
  const plural = PLURALS[key]
  if (plural) return plural(Number(opts?.count ?? 0))
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
// header renders for a known locale.
vi.mock('../i18n', () => ({
  default: { language: 'en' },
}))

// ── Router mock ───────────────────────────────────────────────────────────────
// Everything but `useNavigate` stays real, so `useParams` resolves from the
// route below and <Link> still renders anchors.

const navigateMock = vi.fn()

vi.mock('react-router', async () => {
  const actual = await vi.importActual<typeof import('react-router')>('react-router')
  return { ...actual, useNavigate: () => navigateMock }
})

// ── Fixtures ──────────────────────────────────────────────────────────────────

/**
 * Four servings, so halving and doubling both land on whole numbers while
 * thirds and quarters exercise the fraction formatting. "salt" carries no
 * amount at all and must survive scaling untouched.
 */
const RECIPE: Recipe = {
  id: 1,
  title: 'Fish gratin',
  notes: 'Best with day-old bread.',
  servings: 4,
  rating: 4,
  rated_at: '2026-03-01T12:00:00Z',
  last_cooked_at: '2026-03-05T12:00:00Z',
  created_at: '2026-01-01T12:00:00Z',
  updated_at: '2026-01-01T12:00:00Z',
  ingredients: [
    { id: 11, position: 1, text: '400 g cod, cubed', quantity: 400, unit: 'g', name: 'cod' },
    { id: 12, position: 2, text: '2 dl cream', quantity: 2, unit: 'dl', name: 'cream' },
    { id: 13, position: 3, text: 'salt', quantity: 0, unit: '', name: 'salt' },
  ],
  steps: [
    { id: 21, position: 1, text: 'Cube the cod.', duration_seconds: 0 },
    { id: 22, position: 2, text: 'Bake until golden.', duration_seconds: 1800 },
  ],
  tags: ['norwegian', 'weeknight'],
}

// ── Fetch mock ────────────────────────────────────────────────────────────────

function jsonResponse(body: unknown, status = 200) {
  return { ok: status < 400, status, json: () => Promise.resolve(body) }
}

interface FetchOptions {
  /** Body the grocery push resolves with, or a status to fail on. */
  grocery?: { added: number; skipped: number } | { status: number }
  /** Set to hold the grocery push open so the pending state can be asserted. */
  groceryPending?: boolean
}

/**
 * Routes the endpoints the detail page touches. GET returns the fixture
 * wrapped the way the Go handlers wrap it (`{"recipe": ...}`); PUT echoes the
 * recipe back so a save settles.
 */
function mockFetch(options: FetchOptions = {}) {
  const { grocery = { added: 2, skipped: 0 }, groceryPending = false } = options

  const fetchMock = vi.fn((input: string, init?: RequestInit) => {
    const url = String(input)
    if (url === '/api/recipes/1/grocery') {
      if (groceryPending) return new Promise(() => {})
      if ('status' in grocery) {
        return Promise.resolve(jsonResponse({ error: 'grocery list unavailable' }, grocery.status))
      }
      return Promise.resolve(jsonResponse({ ...grocery, items: [] }))
    }
    if (url === '/api/recipes/1' && init?.method === 'PUT') {
      return Promise.resolve(jsonResponse({ recipe: RECIPE }))
    }
    if (url === '/api/recipes/1') {
      return Promise.resolve(jsonResponse({ recipe: RECIPE }))
    }
    return Promise.resolve(jsonResponse({ error: 'unexpected request' }, 404))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

// ── Helpers ───────────────────────────────────────────────────────────────────

async function renderAndWait(options?: FetchOptions) {
  const fetchMock = mockFetch(options)
  render(
    <MemoryRouter initialEntries={['/recipes/1']}>
      <Routes>
        <Route path="/recipes/:id" element={<RecipeDetail />} />
      </Routes>
    </MemoryRouter>,
  )
  await screen.findByRole('heading', { name: 'Fish gratin' })
  return fetchMock
}

/** Every ingredient line currently rendered, in recipe order. */
function ingredientLines(): string[] {
  const list = screen.getByRole('region', { name: 'Ingredients' })
  return Array.from(list.querySelectorAll('li span')).map(el => el.textContent ?? '')
}

function setPortions(portions: number) {
  fireEvent.change(screen.getByLabelText('Portions'), { target: { value: String(portions) } })
}

/** The parsed JSON body of the last request matching `predicate`. */
function lastBody(fetchMock: ReturnType<typeof mockFetch>, url: string, method: string): unknown {
  const call = [...fetchMock.mock.calls]
    .reverse()
    .find(([input, init]) => String(input) === url && (init as RequestInit)?.method === method)
  if (!call) throw new Error(`no ${method} ${url} request was made`)
  return JSON.parse(String((call[1] as RequestInit).body))
}

beforeEach(() => {
  navigateMock.mockReset()
})

afterEach(() => {
  vi.unstubAllGlobals()
})

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('RecipeDetail', () => {
  it('renders the stored quantities at the recipe’s own yield', async () => {
    await renderAndWait()
    expect(ingredientLines()).toEqual(['400 g cod, cubed', '2 dl cream', 'salt'])
    expect(screen.queryByText('Scaled from 4 servings')).toBeNull()
  })

  it('scales displayed quantities with the portion control', async () => {
    await renderAndWait()

    setPortions(8)
    expect(ingredientLines()).toEqual(['800 g cod, cubed', '4 dl cream', 'salt'])

    setPortions(6)
    expect(ingredientLines()).toEqual(['600 g cod, cubed', '3 dl cream', 'salt'])

    setPortions(2)
    expect(ingredientLines()).toEqual(['200 g cod, cubed', '1 dl cream', 'salt'])

    // 4 -> 3 portions leaves cream on 1.5 dl, which reads as a mixed number.
    setPortions(3)
    expect(ingredientLines()).toEqual(['300 g cod, cubed', '1 1/2 dl cream', 'salt'])

    setPortions(1)
    expect(ingredientLines()).toEqual(['100 g cod, cubed', '1/2 dl cream', 'salt'])

    expect(screen.getByText('Scaled from 4 servings')).toBeTruthy()
  })

  it('steps portions with the plus and minus buttons and resets to the base yield', async () => {
    await renderAndWait()

    fireEvent.click(screen.getByRole('button', { name: 'One more portion' }))
    expect(ingredientLines()[0]).toBe('500 g cod, cubed')

    fireEvent.click(screen.getByRole('button', { name: 'One fewer portion' }))
    expect(ingredientLines()[0]).toBe('400 g cod, cubed')

    setPortions(8)
    fireEvent.click(screen.getByRole('button', { name: 'Reset to 4' }))
    expect(ingredientLines()[0]).toBe('400 g cod, cubed')
  })

  it('saves base quantities even while a scaled view is on screen', async () => {
    const fetchMock = await renderAndWait()

    setPortions(8)
    expect(ingredientLines()).toEqual(['800 g cod, cubed', '4 dl cream', 'salt'])

    fireEvent.click(screen.getByRole('button', { name: 'Edit recipe' }))
    fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Fish gratin, revised' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save recipe' }))

    await waitFor(() => {
      expect(lastBody(fetchMock, '/api/recipes/1', 'PUT')).toEqual({
        title: 'Fish gratin, revised',
        notes: 'Best with day-old bread.',
        servings: 4,
        ingredients: [
          { text: '400 g cod, cubed', quantity: 400, unit: 'g', name: 'cod' },
          { text: '2 dl cream', quantity: 2, unit: 'dl', name: 'cream' },
          { text: 'salt', quantity: 0, unit: '', name: 'salt' },
        ],
        steps: [
          { text: 'Cube the cod.', duration_seconds: 0 },
          { text: 'Bake until golden.', duration_seconds: 1800 },
        ],
        tags: ['norwegian', 'weeknight'],
      })
    })

    // The portion count is a view concern, so it survives the save untouched.
    await screen.findByRole('heading', { name: 'Fish gratin' })
    expect(ingredientLines()).toEqual(['800 g cod, cubed', '4 dl cream', 'salt'])
  })

  it('edits base quantities rather than the scaled ones', async () => {
    const fetchMock = await renderAndWait()

    setPortions(8)
    fireEvent.click(screen.getByRole('button', { name: 'Edit recipe' }))

    // The form is bound to the stored amount, not to the doubled one on screen.
    const quantity = screen.getByLabelText('Quantity for ingredient 1') as HTMLInputElement
    expect(quantity.value).toBe('400')

    fireEvent.change(quantity, { target: { value: '500' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save recipe' }))

    await waitFor(() => {
      const body = lastBody(fetchMock, '/api/recipes/1', 'PUT') as { ingredients: unknown[] }
      expect(body.ingredients[0]).toEqual({
        text: '400 g cod, cubed',
        quantity: 500,
        unit: 'g',
        name: 'cod',
      })
    })
  })

  it('refuses to save a recipe without a title', async () => {
    const fetchMock = await renderAndWait()

    fireEvent.click(screen.getByRole('button', { name: 'Edit recipe' }))
    fireEvent.change(screen.getByLabelText('Title'), { target: { value: '  ' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save recipe' }))

    expect(await screen.findByText('Give the recipe a title')).toBeTruthy()
    expect(
      fetchMock.mock.calls.some(([, init]) => (init as RequestInit)?.method === 'PUT'),
    ).toBe(false)
  })

  it('pushes exactly the selected ingredients to the grocery list', async () => {
    const fetchMock = await renderAndWait({ grocery: { added: 2, skipped: 0 } })

    fireEvent.click(screen.getByLabelText('Select ingredient: 400 g cod, cubed'))
    fireEvent.click(screen.getByLabelText('Select ingredient: salt'))
    fireEvent.click(screen.getByRole('button', { name: 'Add missing to grocery list' }))

    await waitFor(() => {
      expect(lastBody(fetchMock, '/api/recipes/1/grocery', 'POST')).toEqual({
        ingredient_ids: [11, 13],
      })
    })
    expect(await screen.findByText(/Added 2 items to the grocery list/)).toBeTruthy()
  })

  it('sends the selection in recipe order regardless of click order', async () => {
    const fetchMock = await renderAndWait()

    fireEvent.click(screen.getByLabelText('Select ingredient: 2 dl cream'))
    fireEvent.click(screen.getByLabelText('Select ingredient: 400 g cod, cubed'))
    fireEvent.click(screen.getByRole('button', { name: 'Add missing to grocery list' }))

    await waitFor(() => {
      expect(lastBody(fetchMock, '/api/recipes/1/grocery', 'POST')).toEqual({
        ingredient_ids: [11, 12],
      })
    })
  })

  it('selects and clears every ingredient at once', async () => {
    const fetchMock = await renderAndWait()

    fireEvent.click(screen.getByRole('button', { name: 'Select all' }))
    fireEvent.click(screen.getByRole('button', { name: 'Add missing to grocery list' }))

    await waitFor(() => {
      expect(lastBody(fetchMock, '/api/recipes/1/grocery', 'POST')).toEqual({
        ingredient_ids: [11, 12, 13],
      })
    })

    fireEvent.click(screen.getByRole('button', { name: 'Select all' }))
    fireEvent.click(screen.getByRole('button', { name: 'Select none' }))
    expect(screen.getByRole('button', { name: 'Add missing to grocery list' })).toBeDisabled()
  })

  it('scales the selection labels too, so a checkbox names what will be bought', async () => {
    await renderAndWait()
    setPortions(8)
    expect(screen.getByLabelText('Select ingredient: 800 g cod, cubed')).toBeTruthy()
  })

  it('keeps the grocery push disabled until something is selected', async () => {
    await renderAndWait()

    expect(screen.getByRole('button', { name: 'Add missing to grocery list' })).toBeDisabled()
    expect(screen.getByText('Select the ingredients you are missing first')).toBeTruthy()

    fireEvent.click(screen.getByLabelText('Select ingredient: salt'))
    expect(screen.getByRole('button', { name: 'Add missing to grocery list' })).toBeEnabled()
  })

  it('shows a pending state while the grocery push is in flight', async () => {
    await renderAndWait({ groceryPending: true })

    fireEvent.click(screen.getByLabelText('Select ingredient: salt'))
    fireEvent.click(screen.getByRole('button', { name: 'Add missing to grocery list' }))

    const pending = await screen.findByRole('button', { name: 'Adding…' })
    expect(pending).toBeDisabled()
  })

  it('reports how many items were skipped as already on the list', async () => {
    await renderAndWait({ grocery: { added: 1, skipped: 1 } })

    fireEvent.click(screen.getByRole('button', { name: 'Select all' }))
    fireEvent.click(screen.getByRole('button', { name: 'Add missing to grocery list' }))

    expect(await screen.findByText(/1 item was already on the list/)).toBeTruthy()
  })

  it('surfaces a failed grocery push and keeps the selection', async () => {
    await renderAndWait({ grocery: { status: 500 } })

    fireEvent.click(screen.getByLabelText('Select ingredient: salt'))
    fireEvent.click(screen.getByRole('button', { name: 'Add missing to grocery list' }))

    expect(await screen.findByText('grocery list unavailable')).toBeTruthy()
    expect(screen.getByLabelText('Select ingredient: salt')).toBeChecked()
  })

  it('reports a missing recipe without an error banner', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(jsonResponse({ error: 'recipe not found' }, 404))),
    )
    render(
      <MemoryRouter initialEntries={['/recipes/9']}>
        <Routes>
          <Route path="/recipes/:id" element={<RecipeDetail />} />
        </Routes>
      </MemoryRouter>,
    )

    expect(await screen.findByText('That recipe does not exist')).toBeTruthy()
  })

  it('opens straight into the editor in create mode', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(jsonResponse({ error: 'nope' }, 404))))
    render(
      <MemoryRouter initialEntries={['/recipes/new']}>
        <Routes>
          <Route path="/recipes/:id" element={<RecipeDetail />} />
        </Routes>
      </MemoryRouter>,
    )

    expect(await screen.findByRole('heading', { name: 'New recipe' })).toBeTruthy()
    expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe('')
  })
})
