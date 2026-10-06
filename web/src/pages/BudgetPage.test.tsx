// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import BudgetPage from './BudgetPage'
import enBudget from '../../public/locales/en/budget.json'

// ── Translation helpers ───────────────────────────────────────────────────────

type JsonValue = string | number | boolean | null | JsonObject | JsonValue[]
interface JsonObject { [key: string]: JsonValue }

function resolveKey(obj: JsonObject, parts: string[]): JsonValue | undefined {
  const [head, ...rest] = parts
  const val = obj[head]
  if (rest.length === 0) return val
  if (val && typeof val === 'object' && !Array.isArray(val)) {
    return resolveKey(val as JsonObject, rest)
  }
  return undefined
}

function makeT(translations: JsonObject) {
  return function t(key: string, opts?: Record<string, unknown>): string {
    if (opts?.defaultValue && typeof opts.defaultValue === 'string') return opts.defaultValue

    // Handle pluralization (i18next stores plural as key_one / key_other)
    if (opts?.count !== undefined) {
      const suffix = Number(opts.count) === 1 ? '_one' : '_other'
      const pluralVal = resolveKey(translations, (key + suffix).split('.'))
      if (typeof pluralVal === 'string') {
        return pluralVal.replace(/\{\{(\w+)\}\}/g, (_, k) => String(opts[k] ?? `{{${k}}}`))
      }
    }

    const val = resolveKey(translations, key.split('.'))
    if (typeof val === 'string') {
      if (opts) {
        return val.replace(/\{\{(\w+)\}\}/g, (_, k) => String(opts[k] ?? `{{${k}}}`))
      }
      return val
    }
    return key
  }
}

// Mock react-i18next using actual translation data so that missing or
// object-valued keys surface in tests (guards against React error #310).
//
// IMPORTANT: the `t` function returned by useTranslation must be a stable
// reference (same object across re-renders). If a new function is created on
// every call, the component's useEffect([t, ...]) fires on every render,
// causing an infinite re-render loop → OOM in the test worker.
vi.mock('react-i18next', () => {
  // Cache stable t functions keyed by namespace so they are created once.
  const cache = new Map<string, ReturnType<typeof makeT>>()
  function getT(ns: string, translations: JsonObject) {
    if (!cache.has(ns)) cache.set(ns, makeT(translations))
    return cache.get(ns)!
  }
  return {
    useTranslation: (ns?: string) => ({
      t: ns === 'budget'
        ? getT('budget', enBudget as unknown as JsonObject)
        : getT('__empty__', {}),
      i18n: { language: 'en' },
    }),
    Trans: ({ i18nKey }: { i18nKey: string }) => i18nKey,
    initReactI18next: { type: '3rdParty', init: () => {} },
  }
})

// Mock lucide-react to avoid loading the full icon library (~30 MB) in tests.
// See src/test/lucideStub.tsx.
vi.mock('lucide-react', async () => (await import('../test/lucideStub')).lucideStub)

// Mock formatDate/formatNumber to avoid loading the i18n HTTP backend in tests.
vi.mock('../utils/formatDate', () => ({
  formatDate: (date: Date | string, options?: Intl.DateTimeFormatOptions) => {
    const d = typeof date === 'string' ? new Date(date) : date
    return d.toLocaleDateString('en', options)
  },
  formatNumber: (n: number, options?: Intl.NumberFormatOptions) =>
    n.toLocaleString('en', options),
  formatTime: (date: Date | string) => {
    const d = typeof date === 'string' ? new Date(date) : date
    return d.toLocaleTimeString('en')
  },
  formatDateTime: (date: Date | string) => {
    const d = typeof date === 'string' ? new Date(date) : date
    return d.toLocaleString('en')
  },
  toLocalDateString: () => '2026-10-06',
}))

// ── Test data ─────────────────────────────────────────────────────────────────

const ACCOUNTS = [
  { id: 1, name: 'Checking', type: 'checking', currency: 'NOK', balance: 0, icon: '' },
  { id: 2, name: 'Savings', type: 'savings', currency: 'NOK', balance: 0, icon: '' },
]

const CATEGORIES = [
  { id: 10, name: 'Groceries', group_name: '', icon: '', color: '#22c55e', is_income: false },
  { id: 11, name: 'Rent', group_name: '', icon: '', color: '#ef4444', is_income: false },
]

interface Txn {
  id: number
  account_id: number
  category_id: number | null
  amount: number
  description: string
  date: string
  tags: string[]
  is_transfer: boolean
  transfer_to: number | null
}

function initialTransactions(): Txn[] {
  return [
    {
      id: 1, account_id: 1, category_id: 10, amount: -250.5, description: 'Rema 1000',
      date: '2026-10-03', tags: ['food'], is_transfer: false, transfer_to: null,
    },
    {
      id: 2, account_id: 1, category_id: null, amount: -99, description: 'CSV import row',
      date: '2026-10-02', tags: [], is_transfer: false, transfer_to: null,
    },
    {
      id: 3, account_id: 1, category_id: null, amount: -1000, description: 'To savings',
      date: '2026-10-01', tags: [], is_transfer: true, transfer_to: 4,
    },
  ]
}

// ── Fetch mock ────────────────────────────────────────────────────────────────

let serverTxns: Txn[] = []
let putHandler: ((id: number, body: Txn) => Promise<Response>) | null = null

function jsonResponse(data: unknown, ok = true): Response {
  return { ok, status: ok ? 200 : 500, json: () => Promise.resolve(data) } as Response
}

function makeFetchMock() {
  return vi.fn((url: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET'
    const putMatch = url.match(/^\/api\/budget\/transactions\/(\d+)$/)
    if (method === 'PUT' && putMatch) {
      const id = Number(putMatch[1])
      const body = JSON.parse(String(init?.body)) as Txn
      if (putHandler) return putHandler(id, body)
      serverTxns = serverTxns.map(t => (t.id === id ? { ...body, id } : t))
      return Promise.resolve(jsonResponse({ transaction: body }))
    }
    if (url.startsWith('/api/budget/accounts')) return Promise.resolve(jsonResponse({ accounts: ACCOUNTS }))
    if (url.startsWith('/api/budget/categories')) return Promise.resolve(jsonResponse({ categories: CATEGORIES }))
    if (url.startsWith('/api/budget/transactions?')) {
      return Promise.resolve(jsonResponse({ transactions: serverTxns }))
    }
    if (url.startsWith('/api/budget/summary')) {
      return Promise.resolve(jsonResponse({
        month: '2026-10', income_total: 0, expense_total: 0, net: 0, income_split: 100, by_category: [],
      }))
    }
    if (url.startsWith('/api/budget/upcoming')) return Promise.resolve(jsonResponse({ upcoming: [] }))
    return Promise.resolve(jsonResponse({}))
  })
}

let fetchMock: ReturnType<typeof makeFetchMock>

function putCalls() {
  return fetchMock.mock.calls.filter(([, init]) => (init as RequestInit | undefined)?.method === 'PUT')
}

async function renderPage() {
  render(
    <MemoryRouter>
      <BudgetPage />
    </MemoryRouter>,
  )
  await screen.findByText('Rema 1000')
}

function rowFor(description: string): HTMLElement {
  return screen.getByText(description).closest('li') as HTMLElement
}

function openEditor(description: string) {
  fireEvent.click(within(rowFor(description)).getByRole('button', { name: enBudget.edit.editTransaction }))
}

function getEditForm(): HTMLFormElement {
  return screen.getByRole('button', { name: enBudget.edit.save }).closest('form') as HTMLFormElement
}

beforeEach(() => {
  serverTxns = initialTransactions()
  putHandler = null
  fetchMock = makeFetchMock()
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('BudgetPage inline transaction editing', () => {
  it('renders an edit button with an accessible label on every row', async () => {
    await renderPage()
    const editButtons = screen.getAllByRole('button', { name: enBudget.edit.editTransaction })
    expect(editButtons).toHaveLength(3)
  })

  it('pre-fills the edit form from the transaction', async () => {
    await renderPage()
    openEditor('Rema 1000')
    const form = within(getEditForm())
    expect((form.getByRole('textbox', { name: enBudget.quickAdd.description }) as HTMLInputElement).value).toBe('Rema 1000')
    expect((form.getByRole('textbox', { name: enBudget.quickAdd.amount }) as HTMLInputElement).value).toBe('-250.5')
    expect((form.getByLabelText(enBudget.quickAdd.date) as HTMLInputElement).value).toBe('2026-10-03')
    expect((form.getByRole('combobox', { name: enBudget.quickAdd.category }) as HTMLSelectElement).value).toBe('10')
    expect((form.getByRole('combobox', { name: enBudget.quickAdd.account }) as HTMLSelectElement).value).toBe('1')
  })

  it('assigns a category to an uncategorised row and PUTs the full transaction', async () => {
    await renderPage()
    openEditor('CSV import row')
    const form = within(getEditForm())
    expect((form.getByRole('combobox', { name: enBudget.quickAdd.category }) as HTMLSelectElement).value).toBe('')
    fireEvent.change(form.getByRole('combobox', { name: enBudget.quickAdd.category }), { target: { value: '11' } })
    fireEvent.change(form.getByRole('textbox', { name: enBudget.quickAdd.amount }), { target: { value: '-120,75' } })
    fireEvent.click(form.getByRole('button', { name: enBudget.edit.save }))

    await waitFor(() => expect(putCalls()).toHaveLength(1))
    const [url, init] = putCalls()[0] as [string, RequestInit]
    expect(url).toBe('/api/budget/transactions/2')
    expect(init.credentials).toBe('include')
    expect(JSON.parse(String(init.body))).toEqual({
      id: 2, account_id: 1, category_id: 11, amount: -120.75, description: 'CSV import row',
      date: '2026-10-02', tags: [], is_transfer: false, transfer_to: null,
    })

    // The list reloads and the row leaves edit mode showing the new category.
    await waitFor(() => expect(screen.queryByRole('button', { name: enBudget.edit.save })).toBeNull())
    expect(within(rowFor('CSV import row')).getByText('Rent')).toBeTruthy()
  })

  it('preserves is_transfer and transfer_to when editing a transfer row', async () => {
    await renderPage()
    openEditor('To savings')
    const form = within(getEditForm())
    fireEvent.change(form.getByRole('textbox', { name: enBudget.quickAdd.description }), { target: { value: 'Monthly savings' } })
    fireEvent.click(form.getByRole('button', { name: enBudget.edit.save }))

    await waitFor(() => expect(putCalls()).toHaveLength(1))
    const body = JSON.parse(String((putCalls()[0][1] as RequestInit).body))
    expect(body.is_transfer).toBe(true)
    expect(body.transfer_to).toBe(4)
    expect(body.description).toBe('Monthly savings')
  })

  it('shows an inline error and stays in edit mode when the PUT fails', async () => {
    putHandler = () => Promise.resolve(jsonResponse({ error: 'boom' }, false))
    await renderPage()
    openEditor('Rema 1000')
    const form = within(getEditForm())
    fireEvent.change(form.getByRole('textbox', { name: enBudget.quickAdd.description }), { target: { value: 'Rema 1000 Torget' } })
    fireEvent.click(form.getByRole('button', { name: enBudget.edit.save }))

    expect(await screen.findByText(enBudget.edit.errors.updateFailed)).toBeTruthy()
    const stillOpen = within(getEditForm())
    expect((stillOpen.getByRole('textbox', { name: enBudget.quickAdd.description }) as HTMLInputElement).value).toBe('Rema 1000 Torget')
  })

  it('cancel restores the row unchanged', async () => {
    await renderPage()
    openEditor('Rema 1000')
    const form = within(getEditForm())
    fireEvent.change(form.getByRole('textbox', { name: enBudget.quickAdd.description }), { target: { value: 'Changed' } })
    fireEvent.click(form.getByRole('button', { name: enBudget.edit.cancel }))

    expect(screen.queryByRole('button', { name: enBudget.edit.save })).toBeNull()
    expect(screen.getByText('Rema 1000')).toBeTruthy()
    expect(screen.queryByText('Changed')).toBeNull()
    expect(putCalls()).toHaveLength(0)
  })

  it('only one row is editable at a time', async () => {
    await renderPage()
    openEditor('Rema 1000')
    openEditor('CSV import row')

    expect(screen.getAllByRole('button', { name: enBudget.edit.save })).toHaveLength(1)
    // The first row is back in display mode.
    expect(screen.getByText('Rema 1000')).toBeTruthy()
    const form = within(getEditForm())
    expect((form.getByRole('textbox', { name: enBudget.quickAdd.description }) as HTMLInputElement).value).toBe('CSV import row')
  })

  it('disables Save while the request is in flight', async () => {
    let resolvePut: (r: Response) => void = () => {}
    putHandler = () => new Promise<Response>(resolve => { resolvePut = resolve })
    await renderPage()
    openEditor('Rema 1000')
    fireEvent.click(within(getEditForm()).getByRole('button', { name: enBudget.edit.save }))

    const savingButton = await screen.findByRole('button', { name: enBudget.edit.saving })
    expect((savingButton as HTMLButtonElement).disabled).toBe(true)

    resolvePut(jsonResponse({ error: 'boom' }, false))
    const saveButton = await screen.findByRole('button', { name: enBudget.edit.save })
    expect((saveButton as HTMLButtonElement).disabled).toBe(false)
  })
})
