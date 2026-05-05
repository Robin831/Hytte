// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { NewSuggestionForm } from './NewSuggestionForm'
import enCommon from '../../../public/locales/en/common.json'
import enSuggestions from '../../../public/locales/en/suggestions.json'

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

function format(template: string, vars?: Record<string, unknown>): string {
  if (!vars) return template
  return template.replace(/\{\{(\w+)\}\}/g, (_, k) => String(vars[k] ?? ''))
}

function makeT(translations: JsonObject) {
  return function t(key: string, vars?: Record<string, unknown>): string {
    const val = resolveKey(translations, key.split('.'))
    return typeof val === 'string' ? format(val, vars) : key
  }
}

const namespaceMap: Record<string, JsonObject> = {
  common: enCommon as unknown as JsonObject,
  suggestions: enSuggestions as unknown as JsonObject,
}

vi.mock('react-i18next', () => ({
  useTranslation: (ns: string = 'common') => ({
    t: makeT(namespaceMap[ns] ?? (enCommon as unknown as JsonObject)),
    i18n: { language: 'en' },
  }),
  Trans: ({ i18nKey }: { i18nKey: string }) => i18nKey,
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

const PAGES = [
  { slug: 'weather', title: 'Weather' },
  { slug: 'budget', title: 'Budget' },
  { slug: '__new_page__', title: 'New page' },
]

function pagesFetch() {
  return vi.fn((url: string) => {
    if (url === '/api/suggestions/pages') {
      return Promise.resolve({ ok: true, json: () => Promise.resolve(PAGES) })
    }
    return Promise.reject(new Error(`Unexpected fetch: ${url}`))
  })
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

describe('NewSuggestionForm', () => {
  it('does not render when closed', () => {
    vi.stubGlobal('fetch', pagesFetch())
    render(<NewSuggestionForm open={false} onClose={() => {}} onCreated={() => {}} />)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('loads pages on open and populates the page dropdown', async () => {
    vi.stubGlobal('fetch', pagesFetch())
    render(<NewSuggestionForm open={true} onClose={() => {}} onCreated={() => {}} />)

    const pageSelect = await screen.findByLabelText('Page') as HTMLSelectElement
    await waitFor(() => {
      const slugs = Array.from(pageSelect.options).map(o => o.value)
      expect(slugs).toContain('weather')
      expect(slugs).toContain('budget')
    })
  })

  it('shows inline validation errors when fields are missing', async () => {
    vi.stubGlobal('fetch', pagesFetch())
    render(<NewSuggestionForm open={true} onClose={() => {}} onCreated={() => {}} />)

    await screen.findByLabelText('Page')

    fireEvent.click(screen.getByRole('button', { name: /Create suggestion/ }))

    await waitFor(() => {
      expect(screen.getByText('Type is required')).toBeInTheDocument()
    })
    expect(screen.getByText('Size is required')).toBeInTheDocument()
    expect(screen.getByText('Page is required')).toBeInTheDocument()
    expect(screen.getByText('Title is required')).toBeInTheDocument()
    expect(screen.getByText('Body is required')).toBeInTheDocument()
  })

  it('rejects body that exceeds the 4096-byte cap', async () => {
    vi.stubGlobal('fetch', pagesFetch())
    render(<NewSuggestionForm open={true} onClose={() => {}} onCreated={() => {}} />)

    const pageSelect = (await screen.findByLabelText('Page')) as HTMLSelectElement
    await waitFor(() => {
      expect(Array.from(pageSelect.options).map(o => o.value)).toContain('weather')
    })

    fireEvent.change(screen.getByLabelText('Type'), { target: { value: 'addition' } })
    fireEvent.change(screen.getByLabelText('Size'), { target: { value: 's' } })
    fireEvent.change(pageSelect, { target: { value: 'weather' } })
    fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'Hi' } })
    fireEvent.change(screen.getByLabelText('Body'), { target: { value: 'x'.repeat(5000) } })

    fireEvent.click(screen.getByRole('button', { name: /Create suggestion/ }))

    await waitFor(() => {
      expect(screen.getByText('Body must be at most 4096 bytes')).toBeInTheDocument()
    })
  })

  it('forces page to __new_page__ when type=new_page is selected', async () => {
    vi.stubGlobal('fetch', pagesFetch())
    render(<NewSuggestionForm open={true} onClose={() => {}} onCreated={() => {}} />)

    const pageSelect = (await screen.findByLabelText('Page')) as HTMLSelectElement
    await waitFor(() => {
      expect(Array.from(pageSelect.options).map(o => o.value)).toContain('__new_page__')
    })

    fireEvent.change(screen.getByLabelText('Type'), { target: { value: 'new_page' } })

    await waitFor(() => {
      expect(pageSelect.value).toBe('__new_page__')
    })
    expect(pageSelect).toBeDisabled()
  })

  it('submits valid form, posts to /api/suggestions and calls onCreated', async () => {
    const created = {
      id: 99,
      user_id: 1,
      generated_at: '2026-05-01T00:00:00Z',
      page_slug: 'weather',
      source: 'user',
      type: 'addition',
      size: 's',
      title: 'My idea',
      body: 'A body',
      status: 'pending',
    }
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (url === '/api/suggestions/pages') {
        return Promise.resolve({ ok: true, json: () => Promise.resolve(PAGES) })
      }
      if (url === '/api/suggestions' && init?.method === 'POST') {
        return Promise.resolve({ ok: true, json: () => Promise.resolve(created) })
      }
      return Promise.reject(new Error(`Unexpected fetch: ${url}`))
    })
    vi.stubGlobal('fetch', fetchMock)

    const onCreated = vi.fn()
    render(<NewSuggestionForm open={true} onClose={() => {}} onCreated={onCreated} />)

    const pageSelect = (await screen.findByLabelText('Page')) as HTMLSelectElement
    await waitFor(() => {
      expect(Array.from(pageSelect.options).map(o => o.value)).toContain('weather')
    })

    fireEvent.change(screen.getByLabelText('Type'), { target: { value: 'addition' } })
    fireEvent.change(screen.getByLabelText('Size'), { target: { value: 's' } })
    fireEvent.change(pageSelect, { target: { value: 'weather' } })
    fireEvent.change(screen.getByLabelText('Title'), { target: { value: '  My idea  ' } })
    fireEvent.change(screen.getByLabelText('Body'), { target: { value: '  A body  ' } })

    fireEvent.click(screen.getByRole('button', { name: /Create suggestion/ }))

    await waitFor(() => {
      expect(onCreated).toHaveBeenCalledWith(created)
    })

    const postCall = fetchMock.mock.calls.find(([url, init]) =>
      url === '/api/suggestions' && (init as RequestInit | undefined)?.method === 'POST',
    )
    expect(postCall).toBeDefined()
    const body = JSON.parse((postCall![1] as RequestInit).body as string)
    expect(body).toEqual({
      type: 'addition',
      size: 's',
      page_slug: 'weather',
      title: 'My idea',
      body: 'A body',
    })
  })

  it('surfaces the backend error message on submit failure', async () => {
    const fetchMock = vi.fn((url: string, init?: RequestInit) => {
      if (url === '/api/suggestions/pages') {
        return Promise.resolve({ ok: true, json: () => Promise.resolve(PAGES) })
      }
      if (url === '/api/suggestions' && init?.method === 'POST') {
        return Promise.resolve({
          ok: false,
          status: 400,
          json: () => Promise.resolve({ error: 'invalid type' }),
        })
      }
      return Promise.reject(new Error(`Unexpected fetch: ${url}`))
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<NewSuggestionForm open={true} onClose={() => {}} onCreated={() => {}} />)

    const pageSelect = (await screen.findByLabelText('Page')) as HTMLSelectElement
    await waitFor(() => {
      expect(Array.from(pageSelect.options).map(o => o.value)).toContain('weather')
    })

    fireEvent.change(screen.getByLabelText('Type'), { target: { value: 'addition' } })
    fireEvent.change(screen.getByLabelText('Size'), { target: { value: 's' } })
    fireEvent.change(pageSelect, { target: { value: 'weather' } })
    fireEvent.change(screen.getByLabelText('Title'), { target: { value: 'A title' } })
    fireEvent.change(screen.getByLabelText('Body'), { target: { value: 'A body' } })

    fireEvent.click(screen.getByRole('button', { name: /Create suggestion/ }))

    await waitFor(() => {
      expect(screen.getByTestId('new-suggestion-submit-error')).toHaveTextContent('invalid type')
    })
  })

  it('shows a load error if pages fetch fails', async () => {
    const fetchMock = vi.fn((url: string) => {
      if (url === '/api/suggestions/pages') {
        return Promise.resolve({ ok: false, status: 500, json: () => Promise.resolve({}) })
      }
      return Promise.reject(new Error(`Unexpected fetch: ${url}`))
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<NewSuggestionForm open={true} onClose={() => {}} onCreated={() => {}} />)

    await waitFor(() => {
      expect(screen.getByText('Failed to load pages')).toBeInTheDocument()
    })
  })

  it('cancel button calls onClose', async () => {
    vi.stubGlobal('fetch', pagesFetch())
    const onClose = vi.fn()
    render(<NewSuggestionForm open={true} onClose={onClose} onCreated={() => {}} />)

    await screen.findByLabelText('Page')

    fireEvent.click(screen.getByRole('button', { name: /^Cancel$/ }))
    expect(onClose).toHaveBeenCalled()
  })
})
