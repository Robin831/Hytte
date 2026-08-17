// @vitest-environment happy-dom
import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent, act } from '@testing-library/react'
import enSalary from '../../../public/locales/en/salary.json'
import ConfigEditor from './ConfigEditor'
import MonthView from './MonthView'
import TrekktabellEditor from './TrekktabellEditor'
import {
  collectDecimalErrors,
  parseOptionalDecimal,
  parseOptionalInteger,
  parseRequiredDecimal,
} from './decimalDraft'
import type { SalaryData } from './useSalaryData'

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
    if (opts?.count !== undefined) {
      const suffix = Number(opts.count) === 1 ? '_one' : '_other'
      const pluralVal = resolveKey(translations, (key + suffix).split('.'))
      if (typeof pluralVal === 'string') {
        return pluralVal.replace(/\{\{(\w+)\}\}/g, (_, k) => String(opts[k] ?? `{{${k}}}`))
      }
    }
    const val = resolveKey(translations, key.split('.'))
    if (typeof val === 'string') {
      if (opts) return val.replace(/\{\{(\w+)\}\}/g, (_, k) => String(opts[k] ?? `{{${k}}}`))
      return val
    }
    return key
  }
}

// Mock react-i18next with the real translation data so missing keys surface.
// The `t` reference must be stable across renders or effects depending on it
// would loop forever.
vi.mock('react-i18next', () => {
  const cache = new Map<string, ReturnType<typeof makeT>>()
  function getT(ns: string, translations: JsonObject) {
    if (!cache.has(ns)) cache.set(ns, makeT(translations))
    return cache.get(ns)!
  }
  return {
    useTranslation: (ns?: string) => ({
      t: ns === 'salary'
        ? getT('salary', enSalary as unknown as JsonObject)
        : getT('__empty__', {}),
      i18n: { language: 'en' },
    }),
    Trans: ({ i18nKey }: { i18nKey: string }) => i18nKey,
    initReactI18next: { type: '3rdParty', init: () => {} },
  }
})

// Mock lucide-react to avoid loading the full icon library (~30 MB) in tests.
// See src/test/lucideStub.tsx.
vi.mock('lucide-react', async () => (await import('../../test/lucideStub')).lucideStub)

vi.mock('../../auth', () => ({
  useAuth: () => ({ user: { id: 1, is_admin: false } }),
}))

const INVALID_MESSAGE = enSalary.validation.invalidNumber
const REQUIRED_MESSAGE = enSalary.validation.required

// ── Test data ─────────────────────────────────────────────────────────────────

const CONFIG = {
  id: 1,
  user_id: 1,
  base_salary: 50000,
  hourly_rate: 1200,
  internal_hourly_rate: 600,
  standard_hours: 7.5,
  currency: 'NOK',
  taxable_benefits: 0,
  effective_from: '2024-01-01',
}

const ESTIMATE = {
  month: '2024-01',
  config: CONFIG,
  commission_tiers: [],
  adjusted_commission_tiers: [],
  estimate: {
    id: 1,
    user_id: 1,
    month: '2024-01',
    working_days: 21,
    hours_worked: 0,
    billable_hours: 0,
    internal_hours: 0,
    base_amount: 50000,
    commission: 0,
    gross: 50000,
    tax: 15000,
    net: 35000,
    vacation_days: 0,
    sick_days: 0,
    is_estimate: true,
  },
  working_days: 21,
  working_days_done: 21,
  working_days_remaining: 0,
  hours_worked: 0,
  internal_hours_worked: 0,
  standard_hours_total: 157.5,
  billable_revenue: 0,
  internal_revenue: 0,
  absence_cost_per_day: 0,
  sick_day_cost: 0,
  vacation_day_cost: 0,
  extra_hour_net: 0,
}

const TREKKTABELL = {
  id: 1,
  user_id: 1,
  year: 2024,
  minstefradrag_rate: 0.46,
  minstefradrag_min: 4000,
  minstefradrag_max: 104450,
  personfradrag: 88250,
  alminnelig_skatt_rate: 0.22,
  trygdeavgift: 0.078,
  trinnskatt_tiers: [{ income_from: 208050, rate: 0.017 }],
}

function stubSalary(overrides: Record<string, unknown>): SalaryData {
  return {
    estimate: null,
    vacation: null,
    trekktabell: null,
    assignments: [],
    assignmentsLoading: false,
    assignmentsError: null,
    setAssignmentsError: vi.fn(),
    formatCurrency: (amount: number) => String(amount),
    saveConfig: vi.fn().mockResolvedValue(undefined),
    saveOverride: vi.fn().mockResolvedValue(undefined),
    saveTrekktabell: vi.fn().mockResolvedValue(TREKKTABELL),
    resetTrekktabellDefaults: vi.fn().mockResolvedValue(TREKKTABELL),
    saveAssignment: vi.fn().mockResolvedValue(true),
    deleteAssignment: vi.fn().mockResolvedValue(undefined),
    importTrekktabellData: vi.fn(),
    ...overrides,
  } as unknown as SalaryData
}

const typeInto = (label: string, value: string) =>
  fireEvent.change(screen.getByLabelText(label), { target: { value } })

const click = async (name: string) => {
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name }))
  })
}

// ── decimalDraft helpers ──────────────────────────────────────────────────────

describe('decimalDraft', () => {
  it('flags blank required fields without inventing a value', () => {
    expect(parseRequiredDecimal('   ')).toEqual({ value: 0, error: 'required' })
  })

  it('flags unparseable required fields', () => {
    expect(parseRequiredDecimal('abc')).toEqual({ value: 0, error: 'invalid' })
  })

  it('parses comma decimals in required fields', () => {
    expect(parseRequiredDecimal('1250,50')).toEqual({ value: 1250.5, error: null })
  })

  it('falls back for blank optional fields but not for garbage', () => {
    expect(parseOptionalDecimal('')).toEqual({ value: 0, error: null })
    expect(parseOptionalDecimal('', 7.5)).toEqual({ value: 7.5, error: null })
    expect(parseOptionalDecimal('x')).toEqual({ value: 0, error: 'invalid' })
  })

  it('rejects fractional day counts instead of rounding them', () => {
    expect(parseOptionalInteger('3')).toEqual({ value: 3, error: null })
    expect(parseOptionalInteger('')).toEqual({ value: 0, error: null })
    expect(parseOptionalInteger('3,5')).toEqual({ value: 0, error: 'invalid' })
  })

  it('collects one message per failing field', () => {
    const errors = collectDecimalErrors(
      { a: parseRequiredDecimal(''), b: parseRequiredDecimal('x'), c: parseRequiredDecimal('1,5') },
      { required: 'req', invalid: 'inv' },
    )
    expect(errors).toEqual({ a: 'req', b: 'inv' })
  })
})

// ── ConfigEditor ──────────────────────────────────────────────────────────────

describe('ConfigEditor decimal input', () => {
  const renderEditor = () => {
    const saveConfig = vi.fn().mockResolvedValue(undefined)
    const salary = stubSalary({ estimate: ESTIMATE, saveConfig })
    render(
      <ConfigEditor salary={salary} noConfig={false} noConfigPastMonth={false} onClose={vi.fn()} />,
    )
    return saveConfig
  }

  it('uses text inputs with a decimal keypad, not number inputs', () => {
    renderEditor()
    const input = screen.getByLabelText(enSalary.config.standardHours)
    expect(input).toHaveAttribute('type', 'text')
    expect(input).toHaveAttribute('inputmode', 'decimal')
  })

  it('saves a comma decimal typed into standard hours', async () => {
    const saveConfig = renderEditor()
    typeInto(enSalary.config.standardHours, '7,5')
    await click(enSalary.config.save)
    expect(saveConfig).toHaveBeenCalledWith(expect.objectContaining({ standard_hours: 7.5 }))
  })

  it('saves a comma decimal typed into an amount field', async () => {
    const saveConfig = renderEditor()
    typeInto(enSalary.config.baseSalary, '1250,50')
    await click(enSalary.config.save)
    expect(saveConfig).toHaveBeenCalledWith(expect.objectContaining({ base_salary: 1250.5 }))
  })

  it('still accepts dot decimals', async () => {
    const saveConfig = renderEditor()
    typeInto(enSalary.config.baseSalary, '1250.50')
    typeInto(enSalary.config.hourlyRate, '1100.25')
    await click(enSalary.config.save)
    expect(saveConfig).toHaveBeenCalledWith(
      expect.objectContaining({ base_salary: 1250.5, hourly_rate: 1100.25 }),
    )
  })

  it('renders saved values on load', () => {
    renderEditor()
    expect(screen.getByLabelText(enSalary.config.baseSalary)).toHaveValue('50000')
    expect(screen.getByLabelText(enSalary.config.standardHours)).toHaveValue('7.5')
  })

  it('blocks the save and shows an inline error for non-numeric input', async () => {
    const saveConfig = renderEditor()
    typeInto(enSalary.config.hourlyRate, 'abc')
    await click(enSalary.config.save)
    expect(saveConfig).not.toHaveBeenCalled()
    expect(screen.getByText(INVALID_MESSAGE)).toBeInTheDocument()
  })

  it('blocks the save when a required field is cleared', async () => {
    const saveConfig = renderEditor()
    typeInto(enSalary.config.standardHours, '')
    await click(enSalary.config.save)
    expect(saveConfig).not.toHaveBeenCalled()
    expect(screen.getByText(REQUIRED_MESSAGE)).toBeInTheDocument()
  })
})

// ── MonthView override form ───────────────────────────────────────────────────

describe('MonthView override decimal input', () => {
  const renderOverrideForm = async () => {
    const saveOverride = vi.fn().mockResolvedValue(undefined)
    const salary = stubSalary({ estimate: ESTIMATE, saveOverride })
    render(
      <MonthView
        salary={salary}
        selectedMonth="2024-01"
        currentMonthStr="2024-05"
        locale="en"
        onChangeMonth={vi.fn()}
      />,
    )
    await click(enSalary.override.enter)
    return saveOverride
  }

  it('uses text inputs with a decimal keypad', async () => {
    await renderOverrideForm()
    const input = screen.getByLabelText(enSalary.override.billableHours)
    expect(input).toHaveAttribute('type', 'text')
    expect(input).toHaveAttribute('inputmode', 'decimal')
  })

  it('saves comma decimals from the override fields', async () => {
    const saveOverride = await renderOverrideForm()
    typeInto(enSalary.override.billableHours, '7,5')
    typeInto(enSalary.override.internalHours, '2,5')
    typeInto(enSalary.override.actualGross, '1250,50')
    typeInto(enSalary.override.actualNet, '1000,25')
    await click(enSalary.override.save)
    expect(saveOverride).toHaveBeenCalledWith(expect.objectContaining({
      billable_hours: 7.5,
      internal_hours: 2.5,
      hours_worked: 10,
      gross: 1250.5,
      net: 1000.25,
      tax: 250.25,
    }))
  })

  it('blocks the save and shows an error for non-numeric input', async () => {
    const saveOverride = await renderOverrideForm()
    typeInto(enSalary.override.billableHours, '7,5')
    typeInto(enSalary.override.actualGross, 'abc')
    typeInto(enSalary.override.actualNet, '1000')
    await click(enSalary.override.save)
    expect(saveOverride).not.toHaveBeenCalled()
    expect(screen.getByText(INVALID_MESSAGE)).toBeInTheDocument()
    expect(screen.getByLabelText(enSalary.override.actualGross)).toHaveAttribute('aria-invalid', 'true')
  })

  it('blocks the save when a required field is empty', async () => {
    const saveOverride = await renderOverrideForm()
    typeInto(enSalary.override.billableHours, '7,5')
    typeInto(enSalary.override.actualGross, '1250')
    await click(enSalary.override.save)
    expect(saveOverride).not.toHaveBeenCalled()
    expect(screen.getByText(REQUIRED_MESSAGE)).toBeInTheDocument()
  })
})

// ── TrekktabellEditor ─────────────────────────────────────────────────────────

describe('TrekktabellEditor decimal input', () => {
  const renderEditor = async () => {
    const saveTrekktabell = vi.fn().mockResolvedValue(TREKKTABELL)
    const salary = stubSalary({ trekktabell: TREKKTABELL, saveTrekktabell })
    render(<TrekktabellEditor salary={salary} />)
    await click(enSalary.trekktabell.edit)
    return saveTrekktabell
  }

  it('keeps a partially typed tier value verbatim', async () => {
    await renderEditor()
    const rate = screen.getByLabelText('Rate, tier 1')
    expect(rate).toHaveAttribute('type', 'text')
    fireEvent.change(rate, { target: { value: '0,' } })
    expect(rate).toHaveValue('0,')
    fireEvent.change(rate, { target: { value: '0,05' } })
    expect(rate).toHaveValue('0,05')
  })

  it('saves comma decimals from tier rows and scalar fields', async () => {
    const saveTrekktabell = await renderEditor()
    fireEvent.change(screen.getByLabelText('Rate, tier 1'), { target: { value: '0,' } })
    fireEvent.change(screen.getByLabelText('Rate, tier 1'), { target: { value: '0,05' } })
    typeInto(enSalary.trekktabell.personfradragLabel, '1250,50')
    await click(enSalary.trekktabell.save)
    expect(saveTrekktabell).toHaveBeenCalledWith(expect.objectContaining({
      personfradrag: 1250.5,
      trinnskatt_tiers: [{ income_from: 208050, rate: 0.05 }],
    }))
  })

  it('blocks the save and shows an inline error for non-numeric input', async () => {
    const saveTrekktabell = await renderEditor()
    typeInto(enSalary.trekktabell.minstefradragRate, 'abc')
    await click(enSalary.trekktabell.save)
    expect(saveTrekktabell).not.toHaveBeenCalled()
    expect(screen.getByText(INVALID_MESSAGE)).toBeInTheDocument()
  })

  it('blocks the save when a tier field is cleared', async () => {
    const saveTrekktabell = await renderEditor()
    fireEvent.change(screen.getByLabelText('Income from, tier 1'), { target: { value: '' } })
    await click(enSalary.trekktabell.save)
    expect(saveTrekktabell).not.toHaveBeenCalled()
    expect(screen.getByText(REQUIRED_MESSAGE)).toBeInTheDocument()
  })
})
