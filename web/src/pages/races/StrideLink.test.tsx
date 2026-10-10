// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, fireEvent, cleanup, act } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import { StrideLink } from './StrideLink'
import type { Watch } from './racesApi'

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (k: string) => k, i18n: { language: 'en' } }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

const watch = (over: Partial<Watch> = {}): Watch => ({
  event_id: 7, state: 'registered', notes: '', stride_race_id: null, created_at: '', updated_at: '', ...over,
})

describe('StrideLink', () => {
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('adds the race with priority and target time', async () => {
    let body: unknown = null
    vi.stubGlobal('fetch', vi.fn(async (_url: string, init?: RequestInit) => {
      body = JSON.parse(String(init?.body))
      return { ok: true, status: 201, json: async () => ({ watch: watch({ stride_race_id: 99 }), stride_race: { id: 99 } }) } as Response
    }))
    const onChange = vi.fn()
    render(<MemoryRouter><StrideLink eventId={7} watch={watch()} onChange={onChange} /></MemoryRouter>)
    expect(screen.getByText('stride.suggest')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /stride.add/ }))
    fireEvent.click(screen.getByRole('button', { name: 'A' }))
    fireEvent.change(screen.getByLabelText('stride.target'), { target: { value: '3:29' } })
    // 3:29 reads as minutes:seconds — fine for the parser; a typo is flagged instead.
    fireEvent.change(screen.getByLabelText('stride.target'), { target: { value: '3:29:x' } })
    expect(screen.getByLabelText('stride.target')).toHaveAttribute('aria-invalid', 'true')
    fireEvent.change(screen.getByLabelText('stride.target'), { target: { value: '3:29:00' } })
    await act(async () => { fireEvent.click(screen.getAllByRole('button', { name: 'stride.add' }).at(-1)!) })
    expect(body).toEqual({ priority: 'A', target_time: 3 * 3600 + 29 * 60 })
    expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ stride_race_id: 99 }))
  })

  it('shows a linked race and removes the link, keeping the Stride race when not confirmed', async () => {
    const urls: string[] = []
    vi.stubGlobal('fetch', vi.fn(async (url: string) => {
      urls.push(url)
      return { ok: true, status: 200, json: async () => ({ watch: watch() }) } as Response
    }))
    window.confirm = vi.fn(() => false)
    render(<MemoryRouter><StrideLink eventId={7} watch={watch({ stride_race_id: 99 })} onChange={() => {}} /></MemoryRouter>)
    expect(screen.getByText('stride.linked')).toBeInTheDocument()
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'stride.remove' })) })
    expect(urls).toEqual(['/api/races/7/stride'])
  })
})
