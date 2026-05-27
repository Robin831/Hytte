// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import BudgetCreditCards from './BudgetCreditCards'
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
vi.mock('lucide-react', () => ({
  ChevronLeft: () => null,
  ChevronRight: () => null,
  Upload: () => null,
  X: () => null,
  Link2: () => null,
  CreditCard: () => null,
  Plus: () => null,
  Trash2: () => null,
  Settings: () => null,
  History: () => null,
}))

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
}))

// ── Test data ─────────────────────────────────────────────────────────────────

const ACCOUNT = {
  id: 1,
  name: 'Visa Gold',
  type: 'credit',
  currency: 'NOK',
  balance: -5000,
  icon: '💳',
  credit_limit: 50000,
}

const GROUPS = [
  { id: 1, name: 'Diverse', sort_order: 0 },
  { id: 2, name: 'Groceries', sort_order: 1 },
  { id: 3, name: 'Transport', sort_order: 2 },
]

const TRANSACTIONS = {
  transactions: [
    {
      id: 1,
      transaksjonsdato: '2024-01-15',
      beskrivelse: 'Rema 1000',
      belop: -250,
      belop_i_valuta: 0,
      is_pending: false,
      is_innbetaling: false,
      group_id: 2,
      group_name: 'Groceries',
    },
    {
      id: 2,
      transaksjonsdato: '2024-01-16',
      beskrivelse: 'Pending Charge',
      belop: -100,
      belop_i_valuta: 0,
      is_pending: true,
      is_innbetaling: false,
      group_id: null,
      group_name: '',
    },
    {
      id: 3,
      transaksjonsdato: '2024-01-17',
      beskrivelse: 'Card Payment',
      belop: 5000,
      belop_i_valuta: 0,
      is_pending: false,
      is_innbetaling: true,
      group_id: null,
      group_name: '',
    },
  ],
  variable_bill_name: null,
  variable_bill_amount: 0,
}

const SUMMARY = {
  account: ACCOUNT,
  credit_limit: 50000,
  used_amount: 5000,
  remaining: 45000,
  month: '2024-01',
  expense_total: 5000,
  by_category: [],
}

const IMPORT_PREVIEW = {
  new_count: 2,
  skipped_count: 1,
  rows: [
    {
      line: 1,
      transaksjonsdato: '2024-01-20',
      beskrivelse: 'New Store',
      belop: -399,
      belop_i_valuta: 0,
      is_pending: false,
      is_innbetaling: false,
    },
    {
      line: 2,
      transaksjonsdato: '2024-01-21',
      beskrivelse: 'Another Store',
      belop: -199,
      belop_i_valuta: 0,
      is_pending: false,
      is_innbetaling: false,
    },
  ],
}

// ── Fetch mock factory ────────────────────────────────────────────────────────

type FetchOverrides = {
  accounts?: unknown
  groups?: unknown
  transactions?: unknown
  summary?: unknown
  importPreview?: unknown
}

function makeFetchMock(overrides: FetchOverrides = {}) {
  return vi.fn((url: string) => {
    const makeResponse = (data: unknown) =>
      Promise.resolve({ ok: true, json: () => Promise.resolve(data) } as Response)

    if (url.includes('/api/budget/accounts')) {
      return makeResponse(overrides.accounts ?? { accounts: [ACCOUNT] })
    }
    if (url.includes('/api/credit-card/groups')) {
      return makeResponse(overrides.groups ?? GROUPS)
    }
    if (url.includes('/api/budget/credit/summary')) {
      return makeResponse(overrides.summary ?? SUMMARY)
    }
    if (url.includes('/api/credit-card/transactions?')) {
      return makeResponse(overrides.transactions ?? TRANSACTIONS)
    }
    if (url.includes('/api/credit-card/import/preview')) {
      return makeResponse(overrides.importPreview ?? IMPORT_PREVIEW)
    }
    return makeResponse({})
  })
}

function renderComponent() {
  return render(
    <MemoryRouter>
      <BudgetCreditCards />
    </MemoryRouter>,
  )
}

// ── Translation key resolution tests ─────────────────────────────────────────
// These verify that all keys referenced by the credit card page exist in the
// English translation file and resolve to string values, not objects.
// An object-valued key causes React error #310 at runtime.

describe('Translation key resolution', () => {
  const budget = enBudget as unknown as JsonObject
  const t = makeT(budget)

  function expectString(key: string) {
    const result = t(key)
    expect(typeof result, `key "${key}" should resolve to a string, got ${typeof resolveKey(budget, key.split('.'))}`).toBe('string')
    // The resolved value must not equal the key itself (which would indicate a missing key)
    const directValue = resolveKey(budget, key.split('.'))
    expect(directValue, `key "${key}" is missing from translation file`).not.toBeUndefined()
  }

  it('has top-level credit card keys', () => {
    const keys = [
      'creditCards.title',
      'creditCards.noCards',
      'creditCards.limit',
      'creditCards.used',
      'creditCards.available',
      'creditCards.remaining',
      'creditCards.import',
      'creditCards.importing',
      'creditCards.pending',
      'creditCards.payment',
      'creditCards.diverse',
      'creditCards.noGroup',
      'creditCards.monthlyTotal',
      'creditCards.moveToGroup',
      'creditCards.noTransactions',
      'creditCards.manageGroups',
      'creditCards.newGroupName',
    ]
    keys.forEach(expectString)
  })

  it('has history footer keys', () => {
    const keys = [
      'creditCards.history.expenses',
      'creditCards.history.payments',
      'creditCards.history.netOutstanding',
    ]
    keys.forEach(expectString)
  })

  it('has importPreview keys', () => {
    const keys = [
      'creditCards.importPreview.title',
      'creditCards.importPreview.cancel',
      'creditCards.importPreview.empty',
    ]
    keys.forEach(expectString)
    // Plural keys
    expect(resolveKey(budget, 'creditCards.importPreview.new_one'.split('.'))).toBeDefined()
    expect(resolveKey(budget, 'creditCards.importPreview.new_other'.split('.'))).toBeDefined()
    expect(resolveKey(budget, 'creditCards.importPreview.confirm_one'.split('.'))).toBeDefined()
    expect(resolveKey(budget, 'creditCards.importPreview.confirm_other'.split('.'))).toBeDefined()
    expect(resolveKey(budget, 'creditCards.importPreview.skipped_one'.split('.'))).toBeDefined()
    expect(resolveKey(budget, 'creditCards.importPreview.skipped_other'.split('.'))).toBeDefined()
  })

  it('has error keys', () => {
    const keys = [
      'creditCards.errors.loadFailed',
      'creditCards.errors.loadTransactionsFailed',
      'creditCards.errors.importPreviewFailed',
      'creditCards.errors.importConfirmFailed',
      'creditCards.errors.assignFailed',
      'creditCards.errors.groupSaveFailed',
      'creditCards.errors.groupDeleteFailed',
    ]
    keys.forEach(expectString)
  })

  it('has importDone plural keys', () => {
    expect(resolveKey(budget, 'creditCards.importDone_one'.split('.'))).toBeDefined()
    expect(resolveKey(budget, 'creditCards.importDone_other'.split('.'))).toBeDefined()
    expect(typeof resolveKey(budget, 'creditCards.importDone_one'.split('.'))).toBe('string')
    expect(typeof resolveKey(budget, 'creditCards.importDone_other'.split('.'))).toBe('string')
  })

  it('all resolved credit card values are strings, not objects', () => {
    const cc = resolveKey(budget, ['creditCards'])
    expect(typeof cc).toBe('object')
    // Flatten one level of creditCards keys and check they resolve to strings or objects (nested)
    const ccObj = cc as JsonObject
    for (const key of Object.keys(ccObj)) {
      const val = ccObj[key]
      // Direct string values must be strings
      if (typeof val !== 'object') {
        expect(typeof val, `creditCards.${key} must be a string`).toBe('string')
      }
    }
  })
})

// ── Component rendering tests ─────────────────────────────────────────────────

describe('BudgetCreditCards', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', makeFetchMock())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
  })

  it('shows loading state while accounts are loading', () => {
    vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})))
    renderComponent()
    expect(screen.getByText('Loading budget…')).toBeInTheDocument()
  })

  it('shows no-cards message when there are no credit card accounts', async () => {
    vi.stubGlobal(
      'fetch',
      makeFetchMock({ accounts: { accounts: [] } }),
    )
    renderComponent()
    await waitFor(() => {
      expect(screen.getByText('No credit card accounts found. Add a credit card account to get started.')).toBeInTheDocument()
    })
  })

  it('renders the page title', async () => {
    renderComponent()
    await waitFor(() => {
      expect(screen.getByText('Credit Cards')).toBeInTheDocument()
    })
  })

  it('renders the account name', async () => {
    renderComponent()
    await waitFor(() => {
      expect(screen.getByText('Visa Gold')).toBeInTheDocument()
    })
  })
})

describe('BudgetCreditCards – transaction list', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', makeFetchMock())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
  })

  it('renders transaction descriptions', async () => {
    renderComponent()
    await waitFor(() => {
      expect(screen.getByText('Rema 1000')).toBeInTheDocument()
    })
    expect(screen.getByText('Pending Charge')).toBeInTheDocument()
    expect(screen.getByText('Card Payment')).toBeInTheDocument()
  })

  it('renders pending badge for pending transactions', async () => {
    renderComponent()
    await waitFor(() => {
      expect(screen.getAllByText('Pending').length).toBeGreaterThan(0)
    })
  })

  it('renders payment badge for card payment transactions', async () => {
    renderComponent()
    await waitFor(() => {
      expect(screen.getAllByText('Payment').length).toBeGreaterThan(0)
    })
  })

  it('renders group section headers', async () => {
    renderComponent()
    await waitFor(() => {
      // 'Groceries' appears both as a group header span and as select option values;
      // use getAllByText to handle multiple matching elements.
      expect(screen.getAllByText('Groceries').length).toBeGreaterThan(0)
    })
  })

  it('renders the monthly total label', async () => {
    renderComponent()
    await waitFor(() => {
      expect(screen.getByText('Monthly total')).toBeInTheDocument()
    })
  })

  it('shows no-transactions message when transaction list is empty', async () => {
    vi.stubGlobal(
      'fetch',
      makeFetchMock({
        transactions: { transactions: [], variable_bill_name: null, variable_bill_amount: 0 },
      }),
    )
    renderComponent()
    await waitFor(() => {
      expect(screen.getByText('No transactions this month.')).toBeInTheDocument()
    })
  })

  it('renders group select dropdowns for each transaction', async () => {
    renderComponent()
    await waitFor(() => {
      const selects = screen.getAllByRole('combobox')
      expect(selects.length).toBeGreaterThan(0)
    })
  })
})

describe('BudgetCreditCards – group management', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', makeFetchMock())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
  })

  it('renders manage groups button', async () => {
    renderComponent()
    await waitFor(() => {
      expect(screen.getByText('Manage groups')).toBeInTheDocument()
    })
  })

  it('shows group list when manage groups is expanded', async () => {
    renderComponent()
    await waitFor(() => {
      expect(screen.getByText('Manage groups')).toBeInTheDocument()
    })

    fireEvent.click(screen.getByText('Manage groups'))

    await waitFor(() => {
      // Group names appear as management panel items and also as select option values;
      // use getAllByText to handle multiple matching elements.
      expect(screen.getAllByText('Diverse').length).toBeGreaterThan(0)
      expect(screen.getAllByText('Transport').length).toBeGreaterThan(0)
    })
  })

  it('renders new group name input when management panel is open', async () => {
    renderComponent()
    await waitFor(() => {
      expect(screen.getByText('Manage groups')).toBeInTheDocument()
    })

    fireEvent.click(screen.getByText('Manage groups'))

    await waitFor(() => {
      expect(screen.getByPlaceholderText('New group name')).toBeInTheDocument()
    })
  })
})

describe('BudgetCreditCards – import preview modal', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.clearAllMocks()
  })

  it('shows import preview modal after file selection', async () => {
    vi.stubGlobal('fetch', makeFetchMock())
    renderComponent()

    // Wait for component to load
    await waitFor(() => {
      expect(screen.getByText('Credit Cards')).toBeInTheDocument()
    })

    // Find the hidden file input and simulate a file upload
    const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement
    expect(fileInput).toBeTruthy()

    const file = new File(['date,desc,amount\n2024-01-20,New Store,-399'], 'export.csv', {
      type: 'text/csv',
    })

    await act(async () => {
      fireEvent.change(fileInput, { target: { files: [file] } })
    })

    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })
  })

  it('import preview modal shows new transaction count', async () => {
    vi.stubGlobal('fetch', makeFetchMock())
    renderComponent()

    await waitFor(() => {
      expect(screen.getByText('Credit Cards')).toBeInTheDocument()
    })

    const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement
    const file = new File(['date,desc,amount\n2024-01-20,New Store,-399'], 'export.csv', {
      type: 'text/csv',
    })

    await act(async () => {
      fireEvent.change(fileInput, { target: { files: [file] } })
    })

    await waitFor(() => {
      // 2 new transactions from IMPORT_PREVIEW mock
      expect(screen.getByText('2 new')).toBeInTheDocument()
    })
  })

  it('import preview modal shows transaction descriptions', async () => {
    vi.stubGlobal('fetch', makeFetchMock())
    renderComponent()

    await waitFor(() => {
      expect(screen.getByText('Credit Cards')).toBeInTheDocument()
    })

    const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement
    const file = new File(['date,desc,amount\n2024-01-20,New Store,-399'], 'export.csv', {
      type: 'text/csv',
    })

    await act(async () => {
      fireEvent.change(fileInput, { target: { files: [file] } })
    })

    await waitFor(() => {
      expect(screen.getByText('New Store')).toBeInTheDocument()
      expect(screen.getByText('Another Store')).toBeInTheDocument()
    })
  })

  it('import preview modal can be dismissed with cancel', async () => {
    vi.stubGlobal('fetch', makeFetchMock())
    renderComponent()

    await waitFor(() => {
      expect(screen.getByText('Credit Cards')).toBeInTheDocument()
    })

    const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement
    const file = new File(['date,desc,amount'], 'export.csv', { type: 'text/csv' })

    await act(async () => {
      fireEvent.change(fileInput, { target: { files: [file] } })
    })

    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })

    // Click cancel button
    const cancelButtons = screen.getAllByText('Cancel')
    fireEvent.click(cancelButtons[0])

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
  })

  it('shows empty preview message when no new transactions', async () => {
    vi.stubGlobal(
      'fetch',
      makeFetchMock({ importPreview: { new_count: 0, skipped_count: 3, rows: [] } }),
    )
    renderComponent()

    await waitFor(() => {
      expect(screen.getByText('Credit Cards')).toBeInTheDocument()
    })

    const fileInput = document.querySelector('input[type="file"]') as HTMLInputElement
    const file = new File(['date,desc,amount'], 'export.csv', { type: 'text/csv' })

    await act(async () => {
      fireEvent.change(fileInput, { target: { files: [file] } })
    })

    await waitFor(() => {
      expect(screen.getByText('No new transactions in this file.')).toBeInTheDocument()
    })
  })
})
