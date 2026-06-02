// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import Notes from '../Notes'
import type { Note } from '../../hooks/useNotes'

// t returns interpolated keys so assertions can target stable strings.
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, opts?: Record<string, unknown>) =>
      opts && 'title' in opts ? `${key}:${opts.title}` : key,
    i18n: { language: 'en', changeLanguage: () => {} },
  }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

// ReactMarkdown + syntax highlighter are heavy and irrelevant to wiring tests.
vi.mock('react-markdown', () => ({ default: ({ children }: { children: string }) => <div>{children}</div> }))
vi.mock('remark-gfm', () => ({ default: () => {} }))
vi.mock('react-syntax-highlighter', () => ({ Prism: ({ children }: { children: string }) => <pre>{children}</pre> }))
vi.mock('react-syntax-highlighter/dist/esm/styles/prism', () => ({ vscDarkPlus: {} }))

function makeNote(overrides: Partial<Note> = {}): Note {
  return {
    id: 1,
    user_id: 1,
    title: 'First note',
    content: 'Hello world',
    tags: ['work'],
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

function jsonResponse(body: unknown, ok = true): Response {
  return {
    ok,
    json: async () => body,
  } as unknown as Response
}

const fetchMock = vi.fn()

beforeEach(() => {
  fetchMock.mockReset()
  // Default: notes list + tags endpoints.
  fetchMock.mockImplementation((url: string) => {
    if (url.startsWith('/api/notes/tags')) {
      return Promise.resolve(jsonResponse({ tags: ['work', 'personal'] }))
    }
    if (url.startsWith('/api/notes')) {
      return Promise.resolve(jsonResponse({ notes: [makeNote()] }))
    }
    return Promise.reject(new Error(`unexpected url: ${url}`))
  })
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('Notes', () => {
  it('loads and renders the note list', async () => {
    render(<Notes />)
    expect(await screen.findByText('First note')).toBeInTheDocument()
  })

  it('filters by tag by issuing a query with the tag param', async () => {
    render(<Notes />)
    await screen.findByText('First note')

    // Tag filter chips render once tags load; click "work".
    fireEvent.click(screen.getByRole('button', { name: 'work' }))

    await waitFor(() => {
      expect(
        fetchMock.mock.calls.some(([url]) => typeof url === 'string' && url.includes('tag=work'))
      ).toBe(true)
    })
  })

  it('keeps the save button disabled until an opened note is edited', async () => {
    render(<Notes />)
    fireEvent.click(await screen.findByText('First note'))

    const saveButton = screen.getByRole('button', { name: /editor.save/ })
    expect(saveButton).toBeDisabled()

    fireEvent.change(screen.getByLabelText('fields.titleLabel'), { target: { value: 'First note edited' } })
    expect(saveButton).not.toBeDisabled()
  })

  it('creates a note via POST when saving a new draft', async () => {
    render(<Notes />)
    await screen.findByText('First note')

    fireEvent.click(screen.getByRole('button', { name: 'newNote' }))
    fireEvent.change(screen.getByLabelText('fields.titleLabel'), { target: { value: 'Created' } })

    fetchMock.mockImplementationOnce(() =>
      Promise.resolve(jsonResponse({ note: makeNote({ id: 2, title: 'Created' }) }))
    )

    fireEvent.click(screen.getByRole('button', { name: /editor.save/ }))

    await waitFor(() => {
      const post = fetchMock.mock.calls.find(([, opts]) => opts?.method === 'POST')
      expect(post).toBeTruthy()
      expect(post![0]).toBe('/api/notes')
    })
  })

  it('opens the delete confirmation dialog and issues DELETE on confirm', async () => {
    render(<Notes />)
    fireEvent.click(await screen.findByText('First note'))

    fireEvent.click(screen.getByRole('button', { name: 'editor.deleteNote' }))

    // ConfirmDialog renders with the interpolated title message.
    expect(await screen.findByText('confirmDelete:First note')).toBeInTheDocument()

    fetchMock.mockImplementationOnce(() => Promise.resolve(jsonResponse({})))
    fireEvent.click(screen.getByRole('button', { name: 'confirm.delete' }))

    await waitFor(() => {
      const del = fetchMock.mock.calls.find(([, opts]) => opts?.method === 'DELETE')
      expect(del).toBeTruthy()
      expect(del![0]).toBe('/api/notes/1')
    })
  })
})
