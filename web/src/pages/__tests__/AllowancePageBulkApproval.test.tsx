// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent, within } from '@testing-library/react'
import AllowancePage from '../AllowancePage'

// ── Translation mock ──────────────────────────────────────────────────────────
// mockT must be a stable reference: AllowancePage keeps `t` in effect
// dependency lists, so a fresh function per render would re-run them.

const TRANSLATIONS: Record<string, string> = {
  'title': 'Allowance',
  'loading': 'Loading...',
  'noPending': 'No pending approvals',
  'currency': 'kr',
  'teamMemberSeparator': ' + ',
  'tabs.label': 'Allowance sections',
  'tabs.today': 'Today',
  'tabs.chores': 'Chores',
  'tabs.payouts': 'Payouts',
  'tabs.extras': 'Extras',
  'tabs.bonuses': 'Bonuses',
  'actions.approve': 'Approve',
  'actions.reject': 'Reject',
  'actions.refresh': 'Refresh',
  'actions.viewPhoto': 'View photo',
  'actions.close': 'Close',
  'bulk.select': 'Select',
  'bulk.cancelSelection': 'Cancel',
  'bulk.selectAll': 'Select all',
  'bulk.selectItem': 'Select {{name}}',
  'bulk.selectedCount': '{{selected}} selected',
  'bulk.approveSelected': 'Approve selected',
  'bulk.rejectSelected': 'Reject selected',
  'bulk.toast.approved': '{{succeeded}} approved',
  'bulk.toast.rejected': '{{succeeded}} rejected',
  'bulk.toast.approvedPartial': '{{succeeded}} approved, {{failed}} failed',
  'bulk.toast.rejectedPartial': '{{succeeded}} rejected, {{failed}} failed',
  'errors.loadFailed': 'Failed to load data',
  'errors.actionFailed': 'Action failed',
  'reject.title': 'Reject completion',
  'reject.titleBatch': 'Reject {{count}} completions',
  'reject.reasonOptional': 'Reason (optional)',
  'reject.cancel': 'Cancel',
  'reject.confirm': 'Reject',
  'reject.chips.notFinished': 'Not finished',
  'reject.chips.redoProperly': 'Do it again properly',
  'reject.chips.wrongDay': 'Wrong day',
}

function mockT(key: string, opts?: Record<string, unknown>): string {
  const template = TRANSLATIONS[key] ?? key
  if (!opts) return template
  return template.replace(/\{\{(\w+)\}\}/g, (_, name: string) =>
    opts[name] === undefined ? '' : String(opts[name]),
  )
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: mockT,
    i18n: { language: 'en' },
  }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

// Keep the HttpBackend out of the test run — formatDate only needs a language.
vi.mock('../../i18n', () => ({
  default: { language: 'en' },
}))

// ── Fixtures ──────────────────────────────────────────────────────────────────

interface PendingFixture {
  id: number
  chore_name: string
}

const PENDING: PendingFixture[] = [
  { id: 1, chore_name: 'Clean room' },
  { id: 2, chore_name: 'Dishes' },
  { id: 3, chore_name: 'Trash' },
]

function makeCompletion({ id, chore_name }: PendingFixture) {
  return {
    id,
    chore_id: id,
    chore_name,
    chore_icon: '🧹',
    chore_amount: 20,
    child_id: 2,
    child_nickname: 'Ada',
    child_avatar: '⭐',
    date: '2026-03-28',
    status: 'pending',
    created_at: '2026-03-28T10:00:00Z',
  }
}

function jsonResponse(body: unknown, ok = true): Response {
  return { ok, json: async () => body } as unknown as Response
}

const fetchMock = vi.fn()

/** Records every batch call so tests can assert on the request count/payload. */
function batchCalls(action: 'approve' | 'reject') {
  return fetchMock.mock.calls.filter(([url]) => url === `/api/allowance/${action}/batch`)
}

function batchBody(action: 'approve' | 'reject', index = 0): Record<string, unknown> {
  const call = batchCalls(action)[index]
  return JSON.parse((call[1] as RequestInit).body as string)
}

beforeEach(() => {
  fetchMock.mockReset()
  fetchMock.mockImplementation((url: string) => {
    if (url.startsWith('/api/allowance/pending')) {
      return Promise.resolve(jsonResponse({ pending: PENDING.map(makeCompletion) }))
    }
    return Promise.reject(new Error(`unexpected url: ${url}`))
  })
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

// Chore names render next to their emoji, so match on a substring.
function choreRow(name: string) {
  return screen.queryByText(new RegExp(name))
}

async function renderTodayTab() {
  render(<AllowancePage />)
  await screen.findByText(/Clean room/)
}

function enterSelectionMode() {
  fireEvent.click(screen.getByRole('button', { name: 'Select' }))
}

function rowCheckbox(name: string): HTMLInputElement {
  return screen.getByRole('checkbox', { name: `Select ${name}` }) as HTMLInputElement
}

describe('AllowancePage bulk approval', () => {
  it('shows no checkboxes until selection mode is entered', async () => {
    await renderTodayTab()

    expect(screen.queryByRole('checkbox')).toBeNull()
    // Per-item actions are available outside selection mode.
    expect(screen.getAllByRole('button', { name: 'Approve' })).toHaveLength(3)

    enterSelectionMode()

    expect(rowCheckbox('Clean room')).toBeInTheDocument()
    expect(rowCheckbox('Dishes')).toBeInTheDocument()
    expect(rowCheckbox('Trash')).toBeInTheDocument()
  })

  it('disables the batch actions until at least one row is selected', async () => {
    await renderTodayTab()
    enterSelectionMode()

    const approve = screen.getByRole('button', { name: 'Approve selected' })
    const reject = screen.getByRole('button', { name: 'Reject selected' })
    expect(approve).toBeDisabled()
    expect(reject).toBeDisabled()

    fireEvent.click(rowCheckbox('Dishes'))

    expect(approve).toBeEnabled()
    expect(reject).toBeEnabled()
    expect(screen.getByText('1 selected')).toBeInTheDocument()
  })

  it('select-all toggles every listed pending row', async () => {
    await renderTodayTab()
    enterSelectionMode()

    const selectAll = screen.getByRole('checkbox', { name: /Select all/ })
    fireEvent.click(selectAll)

    expect(rowCheckbox('Clean room').checked).toBe(true)
    expect(rowCheckbox('Dishes').checked).toBe(true)
    expect(rowCheckbox('Trash').checked).toBe(true)
    expect(screen.getByText('3 selected')).toBeInTheDocument()

    fireEvent.click(selectAll)

    expect(rowCheckbox('Clean room').checked).toBe(false)
    expect(screen.getByText('0 selected')).toBeInTheDocument()
  })

  it('approves the selection with a single batch request', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (url.startsWith('/api/allowance/pending')) {
        return Promise.resolve(jsonResponse({ pending: PENDING.map(makeCompletion) }))
      }
      if (url === '/api/allowance/approve/batch') {
        return Promise.resolve(jsonResponse({
          succeeded: 3,
          failed: 0,
          results: PENDING.map(p => ({ id: p.id, status: 'approved' })),
        }))
      }
      return Promise.reject(new Error(`unexpected url: ${url}`))
    })

    await renderTodayTab()
    enterSelectionMode()
    fireEvent.click(screen.getByRole('checkbox', { name: /Select all/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Approve selected' }))

    await waitFor(() => expect(screen.getByText('3 approved')).toBeInTheDocument())

    // Approving never asks for a reason.
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(batchCalls('approve')).toHaveLength(1)
    expect(batchBody('approve')).toEqual({ ids: [1, 2, 3] })
    // Approved rows disappear, so the pending badge count drops with them.
    expect(choreRow('Clean room')).toBeNull()
    expect(screen.getByText('No pending approvals')).toBeInTheDocument()
  })

  it('reports partial failures and keeps the failed rows listed', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (url.startsWith('/api/allowance/pending')) {
        return Promise.resolve(jsonResponse({ pending: PENDING.map(makeCompletion) }))
      }
      if (url === '/api/allowance/approve/batch') {
        return Promise.resolve(jsonResponse({
          succeeded: 2,
          failed: 1,
          results: [
            { id: 1, status: 'approved' },
            { id: 2, status: 'approved' },
            { id: 3, status: 'skipped', error: 'completion is not pending' },
          ],
        }))
      }
      return Promise.reject(new Error(`unexpected url: ${url}`))
    })

    await renderTodayTab()
    enterSelectionMode()
    fireEvent.click(screen.getByRole('checkbox', { name: /Select all/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Approve selected' }))

    await waitFor(() => expect(screen.getByText('2 approved, 1 failed')).toBeInTheDocument())

    expect(choreRow('Clean room')).toBeNull()
    expect(choreRow('Dishes')).toBeNull()
    // The failed row stays listed and stays selected so it can be retried.
    expect(choreRow('Trash')).toBeInTheDocument()
    expect(rowCheckbox('Trash').checked).toBe(true)
  })

  it('rejects the selection with one shared reason from the dialog', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (url.startsWith('/api/allowance/pending')) {
        return Promise.resolve(jsonResponse({ pending: PENDING.map(makeCompletion) }))
      }
      if (url === '/api/allowance/reject/batch') {
        return Promise.resolve(jsonResponse({
          succeeded: 2,
          failed: 0,
          results: [
            { id: 1, status: 'rejected' },
            { id: 3, status: 'rejected' },
          ],
        }))
      }
      return Promise.reject(new Error(`unexpected url: ${url}`))
    })

    await renderTodayTab()
    enterSelectionMode()
    fireEvent.click(rowCheckbox('Clean room'))
    fireEvent.click(rowCheckbox('Trash'))
    fireEvent.click(screen.getByRole('button', { name: 'Reject selected' }))

    const dialog = await screen.findByRole('dialog', { name: 'Reject 2 completions' })
    expect(batchCalls('reject')).toHaveLength(0)
    fireEvent.change(within(dialog).getByLabelText('Reason (optional)'), { target: { value: '  Not finished  ' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Reject' }))

    await waitFor(() => expect(screen.getByText('2 rejected')).toBeInTheDocument())

    expect(batchCalls('reject')).toHaveLength(1)
    expect(batchBody('reject')).toEqual({ ids: [1, 3], reason: 'Not finished' })
    expect(choreRow('Clean room')).toBeNull()
    expect(choreRow('Dishes')).toBeInTheDocument()
  })

  it('sends no batch request when the reject dialog is cancelled', async () => {
    await renderTodayTab()
    enterSelectionMode()
    fireEvent.click(rowCheckbox('Dishes'))
    fireEvent.click(screen.getByRole('button', { name: 'Reject selected' }))

    const dialog = await screen.findByRole('dialog', { name: 'Reject completion' })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))

    expect(screen.queryByRole('dialog')).toBeNull()
    expect(batchCalls('reject')).toHaveLength(0)
    expect(choreRow('Dishes')).toBeInTheDocument()
  })

  it('surfaces an error and keeps the rows when the batch request fails', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (url.startsWith('/api/allowance/pending')) {
        return Promise.resolve(jsonResponse({ pending: PENDING.map(makeCompletion) }))
      }
      if (url === '/api/allowance/approve/batch') {
        return Promise.resolve(jsonResponse({ error: 'nope' }, false))
      }
      return Promise.reject(new Error(`unexpected url: ${url}`))
    })

    await renderTodayTab()
    enterSelectionMode()
    fireEvent.click(rowCheckbox('Dishes'))
    fireEvent.click(screen.getByRole('button', { name: 'Approve selected' }))

    await waitFor(() => expect(screen.getByText('Action failed')).toBeInTheDocument())
    expect(choreRow('Dishes')).toBeInTheDocument()
  })

  it('leaves selection mode when the parent cancels it', async () => {
    await renderTodayTab()
    enterSelectionMode()
    fireEvent.click(rowCheckbox('Dishes'))

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))

    expect(screen.queryByRole('checkbox')).toBeNull()
    expect(screen.getAllByRole('button', { name: 'Reject' })).toHaveLength(3)
  })
})

function singleRejectCalls() {
  return fetchMock.mock.calls.filter(([url]) => /^\/api\/allowance\/reject\/\d+$/.test(url))
}

describe('AllowancePage single reject', () => {
  it('opens the reason dialog and sends nothing when cancelled', async () => {
    await renderTodayTab()
    fireEvent.click(screen.getAllByRole('button', { name: 'Reject' })[1])

    const dialog = await screen.findByRole('dialog', { name: 'Reject completion' })
    expect(singleRejectCalls()).toHaveLength(0)
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }))

    expect(screen.queryByRole('dialog')).toBeNull()
    expect(singleRejectCalls()).toHaveLength(0)
    expect(choreRow('Dishes')).toBeInTheDocument()
  })

  it('sends the picked chip as the reason', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (url.startsWith('/api/allowance/pending')) {
        return Promise.resolve(jsonResponse({ pending: PENDING.map(makeCompletion) }))
      }
      if (url === '/api/allowance/reject/2') {
        return Promise.resolve(jsonResponse({ id: 2, status: 'rejected' }))
      }
      return Promise.reject(new Error(`unexpected url: ${url}`))
    })

    await renderTodayTab()
    fireEvent.click(screen.getAllByRole('button', { name: 'Reject' })[1])
    const dialog = await screen.findByRole('dialog', { name: 'Reject completion' })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Not finished' }))
    fireEvent.click(within(dialog).getByRole('button', { name: 'Reject' }))

    await waitFor(() => expect(choreRow('Dishes')).toBeNull())
    expect(screen.queryByRole('dialog')).toBeNull()
    const calls = singleRejectCalls()
    expect(calls).toHaveLength(1)
    expect(JSON.parse((calls[0][1] as RequestInit).body as string)).toEqual({ reason: 'Not finished' })
  })

  it('keeps the dialog and typed reason open when the request fails', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (url.startsWith('/api/allowance/pending')) {
        return Promise.resolve(jsonResponse({ pending: PENDING.map(makeCompletion) }))
      }
      if (url === '/api/allowance/reject/1') {
        return Promise.resolve(jsonResponse({ error: 'nope' }, false))
      }
      return Promise.reject(new Error(`unexpected url: ${url}`))
    })

    await renderTodayTab()
    fireEvent.click(screen.getAllByRole('button', { name: 'Reject' })[0])
    const dialog = await screen.findByRole('dialog', { name: 'Reject completion' })
    const field = within(dialog).getByLabelText('Reason (optional)') as HTMLTextAreaElement
    fireEvent.change(field, { target: { value: 'Bed not made' } })
    fireEvent.click(within(dialog).getByRole('button', { name: 'Reject' }))

    await waitFor(() => expect(within(dialog).getByRole('alert')).toHaveTextContent('Action failed'))
    expect(field.value).toBe('Bed not made')
    expect(choreRow('Clean room')).toBeInTheDocument()
  })
})
