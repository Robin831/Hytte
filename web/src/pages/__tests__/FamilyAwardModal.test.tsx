// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import Family from '../Family'

// ── Translation mock ──────────────────────────────────────────────────────────
// stableT must be a stable reference: Family keeps `t` in the loadData
// dependency list, so a fresh function per render would re-run the effect.

const TRANSLATIONS: Record<string, string> = {
  'family.title': 'Family',
  'family.overview': 'Overview',
  'family.children': 'Children',
  'family.addChild': 'Add child',
  'family.editChild': 'Edit child',
  'family.removeChild': 'Remove child',
  'family.viewDetails': 'View details',
  'family.manageRewards': 'Manage rewards',
  'family.awardStars.button': 'Award stars',
  'family.awardStars.title': 'Award stars',
  'family.awardStars.amount': 'Amount',
  'family.awardStars.amountPlaceholder': 'e.g. 10',
  'family.awardStars.reason': 'Reason',
  'family.awardStars.reasonPlaceholder': 'e.g. Helped out',
  'family.awardStars.description': 'Description',
  'family.awardStars.descriptionPlaceholder': 'Optional detail',
  'family.awardStars.submit': 'Award',
  'family.awardStars.success': 'Stars awarded',
  'family.errors.failedToAward': 'Failed to award stars',
  'family.errors.failedToLoad': 'Failed to load family',
  'actions.close': 'Close',
  'actions.cancel': 'Cancel',
  'skeleton.loading': 'Loading...',
}

function stableT(key: string, opts?: Record<string, unknown>): string {
  const template = TRANSLATIONS[key] ?? key
  if (!opts) return template
  return template.replace(/\{\{(\w+)\}\}/g, (_, name: string) =>
    opts[name] === undefined ? '' : String(opts[name]),
  )
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: stableT, i18n: { language: 'en' } }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

// Keep the HttpBackend out of the test run — formatDate only needs a language.
vi.mock('../../i18n', () => ({
  default: { language: 'en' },
}))

// ── Auth mock ─────────────────────────────────────────────────────────────────
// The award button only renders for admins.

const authState = {
  user: { id: 1, name: 'Alice', email: 'alice@example.com', is_admin: true },
  refreshFamilyStatus: vi.fn(),
}

vi.mock('../../auth', () => ({
  useAuth: () => authState,
}))

// ── Fixtures ──────────────────────────────────────────────────────────────────

const CHILDREN = [
  { id: 1, parent_id: 1, child_id: 2, nickname: 'Ada', avatar_emoji: '⭐', created_at: '2026-01-01T00:00:00Z' },
  { id: 2, parent_id: 1, child_id: 3, nickname: 'Bo', avatar_emoji: '🚀', created_at: '2026-01-01T00:00:00Z' },
]

const STATS = {
  current_balance: 40,
  total_earned: 60,
  total_spent: 20,
  level: 2,
  xp: 120,
  title: 'Explorer',
  current_streak: 3,
  longest_streak: 5,
  this_week_stars: 10,
  this_week_starred_workouts: 2,
  last_week_stars: 8,
  last_week_starred_workouts: 1,
}

function jsonResponse(body: unknown, ok = true): Response {
  return { ok, json: async () => body } as unknown as Response
}

const fetchMock = vi.fn()

/** Award responses are per-test; everything else uses the same happy path. */
function routeFetch(award: () => Promise<Response>) {
  fetchMock.mockImplementation((url: string) => {
    if (url === '/api/family/status') {
      return Promise.resolve(jsonResponse({ is_parent: true, is_child: false }))
    }
    if (url === '/api/family/children') {
      return Promise.resolve(jsonResponse({ children: CHILDREN }))
    }
    if (/^\/api\/family\/children\/\d+\/stats$/.test(url)) {
      return Promise.resolve(jsonResponse(STATS))
    }
    if (url === '/api/admin/stars/award') {
      return award()
    }
    return Promise.reject(new Error(`unexpected url: ${url}`))
  })
}

beforeEach(() => {
  fetchMock.mockReset()
  routeFetch(() => Promise.resolve(jsonResponse({ ok: true })))
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

async function renderFamily() {
  render(
    <MemoryRouter>
      <Family />
    </MemoryRouter>,
  )
  await screen.findByText('Ada')
}

/** Opens the award modal for the nth child card (0-indexed). */
function openAwardModal(index = 0) {
  fireEvent.click(screen.getAllByRole('button', { name: 'Award stars' })[index])
}

function fillAndSubmit() {
  fireEvent.change(screen.getByLabelText('Amount'), { target: { value: '5' } })
  fireEvent.change(screen.getByLabelText('Reason'), { target: { value: 'Tidied up' } })
  fireEvent.click(screen.getByRole('button', { name: 'Award' }))
}

describe('Family award stars modal', () => {
  it('shows the failure inside the dialog and keeps it open', async () => {
    routeFetch(() => Promise.resolve(jsonResponse({ error: 'nope' }, false)))
    await renderFamily()
    openAwardModal()
    fillAndSubmit()

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('Failed to award stars')
    // The dialog stays open with the entered values, so the award can be retried.
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(screen.getByLabelText('Reason')).toHaveValue('Tidied up')
    // The message renders inside the dialog, not in the page-level banner
    // that sits behind the overlay.
    expect(within(screen.getByRole('dialog')).getByRole('alert')).toBe(alert)
  })

  it('reports network failures the same way', async () => {
    routeFetch(() => Promise.reject(new Error('offline')))
    await renderFamily()
    openAwardModal()
    fillAndSubmit()

    expect(await screen.findByRole('alert')).toHaveTextContent('Failed to award stars')
  })

  it('clears a stale error when the modal is reopened for another child', async () => {
    routeFetch(() => Promise.resolve(jsonResponse({ error: 'nope' }, false)))
    await renderFamily()
    openAwardModal()
    fillAndSubmit()
    await screen.findByRole('alert')

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    openAwardModal(1)

    expect(screen.queryByRole('alert')).toBeNull()
    expect(screen.queryByText('Failed to award stars')).toBeNull()
  })

  it('clears the previous error before the next submit', async () => {
    let fail = true
    routeFetch(() =>
      Promise.resolve(fail ? jsonResponse({ error: 'nope' }, false) : jsonResponse({ ok: true })),
    )
    await renderFamily()
    openAwardModal()
    fillAndSubmit()
    await screen.findByRole('alert')

    fail = false
    fireEvent.click(screen.getByRole('button', { name: 'Award' }))

    await screen.findByText('Stars awarded')
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('shows the success state with no error text', async () => {
    await renderFamily()
    openAwardModal()
    fillAndSubmit()

    await screen.findByText('Stars awarded')
    expect(screen.queryByRole('alert')).toBeNull()
    expect(screen.queryByText('Failed to award stars')).toBeNull()
  })

  it('focuses the amount input on open so Escape closes the dialog immediately', async () => {
    await renderFamily()
    openAwardModal()

    const amount = screen.getByLabelText('Amount')
    await waitFor(() => expect(document.activeElement).toBe(amount))

    fireEvent.keyDown(document.activeElement as HTMLElement, { key: 'Escape' })

    expect(screen.queryByRole('dialog')).toBeNull()
  })
})
