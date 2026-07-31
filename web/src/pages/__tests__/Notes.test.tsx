// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, screen, waitFor, fireEvent } from '@testing-library/react'
import Notes from '../Notes'
import { renderWithDataRouter } from '../../test/renderWithDataRouter'
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

// Notes uses `useBlocker`, which only works under a data router. `/links`
// stands in for any other route the user might navigate to.
function renderNotes() {
  return renderWithDataRouter(<Notes />, {
    path: '/notes',
    extraRoutes: [{ path: '/links', element: <div>links page</div> }],
  })
}

/** Dispatches a cancelable `beforeunload` and reports whether it was blocked. */
function dispatchBeforeUnload(): boolean {
  const event = new Event('beforeunload', { cancelable: true })
  window.dispatchEvent(event)
  return event.defaultPrevented
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
    renderNotes()
    expect(await screen.findByText('First note')).toBeInTheDocument()
  })

  it('filters by tag by issuing a query with the tag param', async () => {
    renderNotes()
    await screen.findByText('First note')

    // Tag filter chips render once tags load; wait for the button before clicking.
    const workTag = await screen.findByRole('button', { name: 'work' })
    fireEvent.click(workTag)

    await waitFor(() => {
      expect(
        fetchMock.mock.calls.some(([url]) => typeof url === 'string' && url.includes('tag=work'))
      ).toBe(true)
    })
  })

  it('keeps the save button disabled until an opened note is edited', async () => {
    renderNotes()
    fireEvent.click(await screen.findByText('First note'))

    const saveButton = screen.getByRole('button', { name: /editor.save/ })
    expect(saveButton).toBeDisabled()

    fireEvent.change(screen.getByLabelText('fields.titleLabel'), { target: { value: 'First note edited' } })
    expect(saveButton).not.toBeDisabled()
  })

  it('creates a note via POST when saving a new draft', async () => {
    renderNotes()
    await screen.findByText('First note')

    fireEvent.click(screen.getAllByRole('button', { name: 'newNote' })[0])
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
    renderNotes()
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

describe('Notes unsaved-changes guard', () => {
  /** Renders, opens the seeded note and edits its title so the draft is dirty. */
  async function renderDirty() {
    const result = renderNotes()
    fireEvent.click(await screen.findByText('First note'))
    fireEvent.change(screen.getByLabelText('fields.titleLabel'), { target: { value: 'Edited title' } })
    return result
  }

  it('does not block unload while the editor is closed', async () => {
    renderNotes()
    await screen.findByText('First note')
    expect(dispatchBeforeUnload()).toBe(false)
  })

  it('does not block unload for an opened but unedited note', async () => {
    renderNotes()
    fireEvent.click(await screen.findByText('First note'))
    expect(dispatchBeforeUnload()).toBe(false)
  })

  it('blocks unload while the draft has unsaved changes', async () => {
    await renderDirty()
    expect(dispatchBeforeUnload()).toBe(true)
  })

  it('stops blocking unload once the draft is saved', async () => {
    await renderDirty()

    fetchMock.mockImplementationOnce(() =>
      Promise.resolve(jsonResponse({ note: makeNote({ title: 'Edited title' }) }))
    )
    fireEvent.click(screen.getByRole('button', { name: /editor.save/ }))

    await waitFor(() => expect(dispatchBeforeUnload()).toBe(false))
  })

  it('removes the beforeunload listener when the page unmounts', async () => {
    const { unmount } = await renderDirty()
    expect(dispatchBeforeUnload()).toBe(true)

    unmount()
    expect(dispatchBeforeUnload()).toBe(false)
  })

  it('blocks in-app navigation and shows the discard dialog', async () => {
    const { router } = await renderDirty()

    // Wait for dirty state to propagate
    await waitFor(() => expect(screen.getByRole('button', { name: /editor.save/ })).not.toBeDisabled())

    // Navigate without awaiting — the returned promise hangs when blocked
    act(() => { router.navigate('/links') })

    expect(await screen.findByText('discardConfirm.title')).toBeInTheDocument()

    expect(router.state.location.pathname).toBe('/notes')
    expect(screen.queryByText('links page')).not.toBeInTheDocument()
  })

  it('completes the blocked navigation exactly once when confirmed', async () => {
    const { router } = await renderDirty()
    await waitFor(() => expect(screen.getByRole('button', { name: /editor.save/ })).not.toBeDisabled())

    act(() => { router.navigate('/links') })
    fireEvent.click(await screen.findByRole('button', { name: 'discardConfirm.confirm' }))

    await waitFor(() => expect(router.state.location.pathname).toBe('/links'))
    expect(await screen.findByText('links page')).toBeInTheDocument()

    act(() => { router.navigate(-1) })
    await waitFor(() => expect(router.state.location.pathname).toBe('/notes'))
  })

  it('stays on the page with the draft intact when the dialog is cancelled', async () => {
    const { router } = await renderDirty()
    await waitFor(() => expect(screen.getByRole('button', { name: /editor.save/ })).not.toBeDisabled())

    act(() => { router.navigate('/links') })
    fireEvent.click(await screen.findByRole('button', { name: 'discardConfirm.cancel' }))

    await waitFor(() => expect(screen.queryByText('discardConfirm.title')).not.toBeInTheDocument())
    expect(router.state.location.pathname).toBe('/notes')
    expect(screen.getByLabelText('fields.titleLabel')).toHaveValue('Edited title')
    expect(screen.getByRole('button', { name: /editor.save/ })).not.toBeDisabled()
    expect(dispatchBeforeUnload()).toBe(true)
  })

  it('allows navigation again after the draft is saved', async () => {
    const { router } = await renderDirty()

    fetchMock.mockImplementationOnce(() =>
      Promise.resolve(jsonResponse({ note: makeNote({ title: 'Edited title' }) }))
    )
    fireEvent.click(screen.getByRole('button', { name: /editor.save/ }))
    await waitFor(() => expect(screen.getByRole('button', { name: /editor.save/ })).toBeDisabled())

    act(() => { router.navigate('/links') })

    await waitFor(() => expect(router.state.location.pathname).toBe('/links'))
    expect(screen.queryByText('discardConfirm.title')).not.toBeInTheDocument()
  })

  it('still guards opening another note in the same page', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (url.startsWith('/api/notes/tags')) {
        return Promise.resolve(jsonResponse({ tags: [] }))
      }
      if (url.startsWith('/api/notes')) {
        return Promise.resolve(
          jsonResponse({ notes: [makeNote(), makeNote({ id: 2, title: 'Second note' })] })
        )
      }
      return Promise.reject(new Error(`unexpected url: ${url}`))
    })

    renderNotes()
    fireEvent.click(await screen.findByText('First note'))
    fireEvent.change(screen.getByLabelText('fields.titleLabel'), { target: { value: 'Edited title' } })

    // Wait for dirty state to propagate before clicking the second note
    await waitFor(() => expect(screen.getByRole('button', { name: /editor.save/ })).not.toBeDisabled())

    fireEvent.click(await screen.findByText('Second note'))
    expect(await screen.findByText('discardConfirm.title')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'discardConfirm.confirm' }))
    await waitFor(() => expect(screen.getByLabelText('fields.titleLabel')).toHaveValue('Second note'))
  })
})
