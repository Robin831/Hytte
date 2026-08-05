// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, waitFor, within, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import MealPlanner from './MealPlanner'
import {
  addPlanDays,
  isoWeekStart,
  parsePlanDate,
  type PlanEntry,
  type PlanWeek,
  type Recipe,
} from '../types/recipes'

// ── Translation mock ──────────────────────────────────────────────────────────
// `mockT` must be a stable reference: the plan and recipe-list hooks both keep
// `t` in their effect dependencies, so a fresh function per render would
// re-fetch forever.

const TRANSLATIONS: Record<string, string> = {
  'detail.back': 'Back to recipes',
  'list.loading': 'Loading recipes…',
  'planner.title': 'Meal plan',
  'planner.subtitle': 'Plan the week, then tick meals off as you cook them',
  'planner.loading': 'Loading the meal plan…',
  'planner.previousWeek': 'Previous week',
  'planner.nextWeek': 'Next week',
  'planner.thisWeek': 'This week',
  'planner.weekRange': '{{start}} – {{end}}',
  'planner.today': 'Today',
  'planner.emptyDay': 'Nothing planned',
  'planner.addMeal': 'Add a meal on {{day}}',
  'planner.remove': 'Remove {{title}} from the plan',
  'planner.markCooked': 'Mark as cooked',
  'planner.cooked': 'Cooked',
  'planner.slot': 'Meal',
  'planner.slots.breakfast': 'Breakfast',
  'planner.slots.lunch': 'Lunch',
  'planner.slots.dinner': 'Dinner',
  'planner.slots.snack': 'Snack',
  'planner.pickerTitle': 'Choose a recipe',
  'planner.pickerFor': '{{slot}} on {{day}}',
  'planner.pickerSearch': 'Search recipes',
  'planner.pickerSearchPlaceholder': 'Search by title, ingredient or tag',
  'planner.pickerEmpty': 'No recipes match that search',
  'planner.pickerNoRecipes': 'Create a recipe first, then plan it in',
  'planner.close': 'Close',
  'planner.ratePrompt': 'How was {{title}}?',
  'planner.rateHint': 'Your rating decides what comes back as a cook-again suggestion',
  'planner.rateSkip': 'Skip for now',
  'rating.label': 'Rating',
  'rating.star': 'Rate {{value}} out of 5',
  'errors.retry': 'Try again',
  'errors.failedToLoadPlan': 'Could not load the meal plan',
  'errors.failedToSavePlan': 'Could not save the meal plan',
  'errors.failedToClearPlan': 'Could not clear that meal',
  'errors.failedToLogCook': 'Could not log this cook',
  'errors.failedToRate': 'Could not save the rating',
  'errors.failedToLoad': 'Could not load recipes',
}

function mockT(key: string, opts?: Record<string, string | number>): string {
  if (key === 'planner.mealCount') {
    const count = Number(opts?.count ?? 0)
    return `${count} meal${count === 1 ? '' : 's'} planned`
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
// day headings below are formatted for a known locale.
vi.mock('../i18n', () => ({
  default: { language: 'en' },
}))

// ── Fixtures ──────────────────────────────────────────────────────────────────
// The page opens on the current week, so the fixture is built around today
// rather than around a frozen date — no fake timers needed, and the assertions
// stay correct whichever day the suite runs on.

const WEEK_START = isoWeekStart(new Date())
const MONDAY = WEEK_START
const TUESDAY = addPlanDays(WEEK_START, 1)

/** The heading `DayCard` renders for a plan date, in the pinned locale. */
function dayHeading(date: string): string {
  const day = parsePlanDate(date)
  const weekday = day.toLocaleDateString('en', { weekday: 'long' })
  const short = day.toLocaleDateString('en', { day: 'numeric', month: 'short' })
  return `${weekday} ${short}`
}

function weekdayName(date: string): string {
  return parsePlanDate(date).toLocaleDateString('en', { weekday: 'long' })
}

function makeRecipe(over: Partial<Recipe> = {}): Recipe {
  return {
    id: 1,
    title: 'Carbonara',
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

const CARBONARA = makeRecipe({ id: 1, title: 'Carbonara' })
const GREEN_CURRY = makeRecipe({ id: 2, title: 'Green curry' })

const MONDAY_DINNER: PlanEntry = {
  id: 11,
  date: MONDAY,
  slot: 'dinner',
  recipe_id: CARBONARA.id,
  recipe_title: CARBONARA.title,
}

/** A week with all seven days present, as the endpoint always returns it. */
function makeWeek(entries: PlanEntry[] = [MONDAY_DINNER]): PlanWeek {
  const days: Record<string, PlanEntry[]> = {}
  for (let i = 0; i < 7; i++) days[addPlanDays(WEEK_START, i)] = []
  for (const entry of entries) days[entry.date] = [...(days[entry.date] ?? []), entry]
  return { week_start: WEEK_START, week_end: addPlanDays(WEEK_START, 6), days }
}

// ── Fetch mock ────────────────────────────────────────────────────────────────

function jsonResponse(body: unknown, status = 200) {
  return { ok: status < 400, status, json: () => Promise.resolve(body) }
}

interface FetchOptions {
  week?: PlanWeek
  recipes?: Recipe[]
  /** Status for GET /api/recipes/plan. */
  planStatus?: number
  /** Status for PUT /api/recipes/plan. */
  assignStatus?: number
  /** Status for DELETE /api/recipes/plan. */
  clearStatus?: number
  /** The entry PUT /api/recipes/plan echoes back. */
  saved?: PlanEntry
}

function mockFetch(options: FetchOptions = {}) {
  const {
    week = makeWeek(),
    recipes = [CARBONARA, GREEN_CURRY],
    planStatus = 200,
    assignStatus = 200,
    clearStatus = 200,
    saved,
  } = options

  const fetchMock = vi.fn((input: string, init?: RequestInit) => {
    const url = String(input)
    const method = init?.method ?? 'GET'

    if (url.startsWith('/api/recipes/plan')) {
      if (method === 'PUT') {
        if (assignStatus !== 200) {
          return Promise.resolve(jsonResponse({ error: 'slot is taken' }, assignStatus))
        }
        const body = JSON.parse(String(init?.body)) as {
          entries: Array<{ date: string; slot: PlanEntry['slot']; recipe_id: number }>
        }
        const requested = body.entries[0]
        const recipe = recipes.find(r => r.id === requested.recipe_id)
        return Promise.resolve(
          jsonResponse({
            entries: [
              saved ?? {
                id: 99,
                date: requested.date,
                slot: requested.slot,
                recipe_id: requested.recipe_id,
                recipe_title: recipe?.title ?? '',
              },
            ],
          }),
        )
      }
      if (method === 'DELETE') {
        return Promise.resolve(
          clearStatus === 200
            ? jsonResponse({ status: 'ok' })
            : jsonResponse({ error: 'meal plan entry not found' }, clearStatus),
        )
      }
      return Promise.resolve(
        planStatus === 200
          ? jsonResponse(week)
          : jsonResponse({ error: 'could not load the plan' }, planStatus),
      )
    }

    if (url.endsWith('/cooked')) {
      return Promise.resolve(jsonResponse({ recipe: CARBONARA }))
    }
    if (url.endsWith('/rating')) {
      return Promise.resolve(jsonResponse({ recipe: { ...CARBONARA, rating: 3 } }))
    }
    if (url.startsWith('/api/recipes/cook-again')) {
      return Promise.resolve(jsonResponse({ recipes: [] }))
    }
    return Promise.resolve(jsonResponse({ recipes }))
  })

  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

// ── Helpers ───────────────────────────────────────────────────────────────────

async function renderAndWait(options?: FetchOptions) {
  const fetchMock = mockFetch(options)
  render(
    <MemoryRouter>
      <MealPlanner />
    </MemoryRouter>,
  )
  await screen.findByRole('region', { name: dayHeading(MONDAY) })
  return fetchMock
}

function daySection(date: string): HTMLElement {
  return screen.getByRole('region', { name: dayHeading(date) })
}

/** Opens the picker from a day's "add a meal" button. */
async function openPicker(date: string): Promise<HTMLElement> {
  fireEvent.click(
    within(daySection(date)).getByRole('button', {
      name: `Add a meal on ${weekdayName(date)}`,
    }),
  )
  return screen.findByRole('dialog', { name: 'Choose a recipe' })
}

function callsTo(
  fetchMock: ReturnType<typeof mockFetch>,
  predicate: (url: string, init?: RequestInit) => boolean,
) {
  return fetchMock.mock.calls.filter(call => predicate(String(call[0]), call[1] as RequestInit))
}

afterEach(() => {
  vi.unstubAllGlobals()
})

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('MealPlanner', () => {
  it('renders the fetched week with a card per day', async () => {
    const fetchMock = await renderAndWait()

    // The plan is asked for by the Monday of the current week.
    const planCall = callsTo(fetchMock, url => url.startsWith('/api/recipes/plan'))[0]
    expect(String(planCall[0])).toBe(`/api/recipes/plan?week=${WEEK_START}`)

    expect(screen.getAllByRole('region')).toHaveLength(7)

    const monday = daySection(MONDAY)
    expect(within(monday).getByText('Dinner')).toBeTruthy()
    expect(within(monday).getByRole('link', { name: 'Carbonara' })).toBeTruthy()

    // Every other day is empty, and the header counts the one planned meal.
    expect(within(daySection(TUESDAY)).getByText('Nothing planned')).toBeTruthy()
    expect(screen.getByText('1 meal planned')).toBeTruthy()
  })

  it('assigns a recipe by tapping a day and then a recipe', async () => {
    const fetchMock = await renderAndWait()

    const picker = await openPicker(TUESDAY)
    fireEvent.click(within(picker).getByRole('button', { name: 'Green curry' }))

    await waitFor(() => {
      expect(screen.queryByRole('dialog', { name: 'Choose a recipe' })).toBeNull()
    })

    const put = callsTo(fetchMock, (url, init) =>
      url.startsWith('/api/recipes/plan') && init?.method === 'PUT',
    )
    expect(put).toHaveLength(1)
    expect(JSON.parse(String((put[0][1] as RequestInit).body))).toEqual({
      entries: [{ date: TUESDAY, slot: 'dinner', recipe_id: GREEN_CURRY.id }],
    })

    // The assignment shows on the day it was made for, not just in the request.
    await waitFor(() => {
      expect(within(daySection(TUESDAY)).getByRole('link', { name: 'Green curry' })).toBeTruthy()
    })
    expect(screen.getByText('2 meals planned')).toBeTruthy()
  })

  it('files the assignment under the slot picked in the sheet', async () => {
    const fetchMock = await renderAndWait()

    const picker = await openPicker(TUESDAY)
    fireEvent.click(within(picker).getByRole('button', { name: 'Lunch' }))
    fireEvent.click(within(picker).getByRole('button', { name: 'Green curry' }))

    await waitFor(() => {
      const put = callsTo(fetchMock, (url, init) =>
        url.startsWith('/api/recipes/plan') && init?.method === 'PUT',
      )
      expect(JSON.parse(String((put[0][1] as RequestInit).body)).entries[0].slot).toBe('lunch')
    })
    expect(within(daySection(TUESDAY)).getByText('Lunch')).toBeTruthy()
  })

  it('narrows the picker as the user types', async () => {
    await renderAndWait()

    const picker = await openPicker(TUESDAY)
    fireEvent.change(within(picker).getByLabelText('Search recipes'), {
      target: { value: 'curry' },
    })

    expect(within(picker).queryByRole('button', { name: 'Carbonara' })).toBeNull()
    expect(within(picker).getByRole('button', { name: 'Green curry' })).toBeTruthy()

    fireEvent.change(within(picker).getByLabelText('Search recipes'), {
      target: { value: 'nothing matches this' },
    })
    expect(within(picker).getByText('No recipes match that search')).toBeTruthy()
  })

  it('rolls the assignment back when the plan endpoint rejects it', async () => {
    await renderAndWait({ assignStatus: 404 })

    const picker = await openPicker(TUESDAY)
    fireEvent.click(within(picker).getByRole('button', { name: 'Green curry' }))

    await screen.findByText('slot is taken')
    expect(within(daySection(TUESDAY)).getByText('Nothing planned')).toBeTruthy()
  })

  it('clears a slot through the delete endpoint', async () => {
    const fetchMock = await renderAndWait()

    fireEvent.click(
      within(daySection(MONDAY)).getByRole('button', { name: 'Remove Carbonara from the plan' }),
    )

    await waitFor(() => {
      expect(within(daySection(MONDAY)).getByText('Nothing planned')).toBeTruthy()
    })
    const deletes = callsTo(
      fetchMock,
      (url, init) => url.startsWith('/api/recipes/plan') && init?.method === 'DELETE',
    )
    expect(deletes).toHaveLength(1)
    expect(String(deletes[0][0])).toBe(`/api/recipes/plan?date=${MONDAY}&slot=dinner`)
  })

  it('restores a cleared meal when the delete fails', async () => {
    await renderAndWait({ clearStatus: 500 })

    fireEvent.click(
      within(daySection(MONDAY)).getByRole('button', { name: 'Remove Carbonara from the plan' }),
    )

    await screen.findByText('meal plan entry not found')
    expect(within(daySection(MONDAY)).getByRole('link', { name: 'Carbonara' })).toBeTruthy()
  })

  it('logs a cook and then asks for a rating', async () => {
    const fetchMock = await renderAndWait()

    fireEvent.click(within(daySection(MONDAY)).getByRole('button', { name: 'Mark as cooked' }))

    await screen.findByRole('dialog', { name: 'How was Carbonara?' })
    const cookCalls = callsTo(fetchMock, url => url === '/api/recipes/1/cooked')
    expect(cookCalls).toHaveLength(1)
    expect((cookCalls[0][1] as RequestInit).method).toBe('POST')

    // The entry itself now reads as cooked.
    expect(within(daySection(MONDAY)).getByRole('button', { name: 'Cooked' })).toBeTruthy()
  })

  it('sends the rating chosen in the prompt', async () => {
    const fetchMock = await renderAndWait()

    fireEvent.click(within(daySection(MONDAY)).getByRole('button', { name: 'Mark as cooked' }))
    const prompt = await screen.findByRole('dialog', { name: 'How was Carbonara?' })
    fireEvent.click(within(prompt).getByRole('button', { name: 'Rate 3 out of 5' }))

    await waitFor(() => {
      expect(screen.queryByRole('dialog', { name: 'How was Carbonara?' })).toBeNull()
    })
    const rateCalls = callsTo(fetchMock, url => url === '/api/recipes/1/rating')
    expect(rateCalls).toHaveLength(1)
    expect(JSON.parse(String((rateCalls[0][1] as RequestInit).body))).toEqual({ rating: 3 })
  })

  it('lets the rating prompt be skipped without sending one', async () => {
    const fetchMock = await renderAndWait()

    fireEvent.click(within(daySection(MONDAY)).getByRole('button', { name: 'Mark as cooked' }))
    const prompt = await screen.findByRole('dialog', { name: 'How was Carbonara?' })
    fireEvent.click(within(prompt).getByRole('button', { name: 'Skip for now' }))

    await waitFor(() => {
      expect(screen.queryByRole('dialog', { name: 'How was Carbonara?' })).toBeNull()
    })
    expect(callsTo(fetchMock, url => url.endsWith('/rating'))).toHaveLength(0)
  })

  it('re-requests the plan when the week changes', async () => {
    const fetchMock = await renderAndWait()

    fireEvent.click(screen.getByRole('button', { name: 'Next week' }))

    const nextWeek = addPlanDays(WEEK_START, 7)
    await waitFor(() => {
      expect(
        callsTo(fetchMock, url => url === `/api/recipes/plan?week=${nextWeek}`),
      ).toHaveLength(1)
    })
    // The cards follow the week, and "this week" appears to get back.
    expect(screen.getByRole('region', { name: dayHeading(nextWeek) })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'This week' })).toBeTruthy()
  })

  it('surfaces a localized error when the plan cannot be loaded', async () => {
    mockFetch({ planStatus: 500 })
    render(
      <MemoryRouter>
        <MealPlanner />
      </MemoryRouter>,
    )

    await screen.findByText('could not load the plan')
    expect(screen.getByRole('button', { name: 'Try again' })).toBeTruthy()
  })

  it('tells the user to create a recipe when there are none to plan', async () => {
    await renderAndWait({ recipes: [] })

    const picker = await openPicker(TUESDAY)
    expect(within(picker).getByText('Create a recipe first, then plan it in')).toBeTruthy()
  })
})
