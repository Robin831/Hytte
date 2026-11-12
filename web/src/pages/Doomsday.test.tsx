// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import Doomsday from './Doomsday'

function mockT(key: string, opts?: Record<string, unknown>): string {
  if (!opts) return key
  return `${key}:${Object.entries(opts).map(([k, v]) => `${k}=${v}`).join(',')}`
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: mockT, i18n: { language: 'en' } }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))
vi.mock('../i18n', () => ({ default: { language: 'en' } }))

function mockFetch() {
  const calls: { url: string; init?: RequestInit }[] = []
  vi.stubGlobal('fetch', vi.fn(async (url: string, init?: RequestInit) => {
    calls.push({ url, init })
    return new Response(JSON.stringify({}), { status: 201, headers: { 'Content-Type': 'application/json' } })
  }))
  return calls
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('Doomsday practice', () => {
  it('asks a date, takes a keyboard answer and records it', async () => {
    const calls = mockFetch()
    // Level 3 with Math.random() = 0.5 draws 16 July 2015 — a Thursday.
    vi.spyOn(Math, 'random').mockReturnValue(0.5)
    render(<MemoryRouter><Doomsday /></MemoryRouter>)
    fireEvent.click(screen.getByRole('button', { name: /^3 ·/ }))
    expect(screen.getByRole('heading', { level: 2 }).textContent).toContain('16 July 2015')
    fireEvent.keyDown(window, { key: '4' }) // 4 = Thursday
    expect(await screen.findByText(/practice.correct/)).toBeTruthy()
    await waitFor(() => expect(calls.some(c => c.url === '/api/math/doomsday/attempts')).toBe(true))
    const body = JSON.parse(String(calls[0].init?.body))
    expect(body).toMatchObject({ level: 3, date: '2015-07-16', answer: 4, correct: true, hints: 0, slip: '' })
  })

  it('shows the worked solution and a likely slip after a wrong answer', async () => {
    mockFetch()
    vi.spyOn(Math, 'random').mockReturnValue(0.5)
    render(<MemoryRouter><Doomsday /></MemoryRouter>)
    fireEvent.click(screen.getByRole('button', { name: /^3 ·/ }))
    fireEvent.click(screen.getByRole('button', { name: /practice.hint/ }))
    expect(screen.getByText(/practice.hintAnchor/)).toBeTruthy()
    fireEvent.keyDown(window, { key: '5' }) // 5 = Friday: one day off
    expect(await screen.findByText(/practice.wrong/)).toBeTruthy()
    expect(screen.getByText(/solution.answer/)).toBeTruthy()
    expect(screen.getByText(/practice.slip/)).toBeTruthy()
  })

  it('orders the buttons Sunday (0) to Saturday (6) and takes key 0 as Sunday', async () => {
    const calls = mockFetch()
    vi.spyOn(Math, 'random').mockReturnValue(0.5)
    render(<MemoryRouter><Doomsday /></MemoryRouter>)
    fireEvent.click(screen.getByRole('button', { name: /^3 ·/ }))
    const days = screen.getAllByRole('button').filter(b => /day$/.test(b.textContent ?? ''))
    expect(days.map(b => b.textContent)).toEqual(['0Sunday', '1Monday', '2Tuesday', '3Wednesday', '4Thursday', '5Friday', '6Saturday'])
    fireEvent.keyDown(window, { key: ' ' })
    expect(screen.queryByText(/practice.wrong|practice.correct/)).toBeNull()
    fireEvent.keyDown(window, { key: '0' })
    await waitFor(() => expect(calls.length).toBe(1))
    expect(JSON.parse(String(calls[0].init?.body)).answer).toBe(0)
  })

  it('walks the four steps on level 1', async () => {
    mockFetch()
    vi.spyOn(Math, 'random').mockReturnValue(0.5)
    render(<MemoryRouter><Doomsday /></MemoryRouter>)
    fireEvent.click(screen.getByRole('button', { name: /^1 ·/ }))
    expect(screen.getByText(/practice.qAnchor/)).toBeTruthy()
    fireEvent.keyDown(window, { key: '2' }) // year 2000 → the 2000s anchor, Tuesday
    expect(await screen.findByText(/practice.correct/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: /practice.next/ }))
    expect(screen.getByText(/practice.qYear/)).toBeTruthy()
  })
})
