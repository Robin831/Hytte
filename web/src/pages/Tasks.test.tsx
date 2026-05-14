// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import Tasks from './Tasks'

const TRANSLATIONS: Record<string, string> = {
  'pageTitle': 'Tasks',
  'inputPlaceholder': 'What needs doing?',
  'addTask': 'Add task',
  'tags.label': 'Tags',
  'tags.work': 'work',
  'tags.personal': 'personal',
  'tags.addCustom': 'Add tag',
  'tags.customPlaceholder': 'Add a tag…',
  'filter.label': 'Filter by tag',
  'filter.clearLabel': 'Clear tag filter',
  'showArchived': 'Show archived',
  'archivedLabel': 'Archived',
  'archive': 'Archive',
  'unarchive': 'Unarchive',
  'delete': 'Delete',
  'expand': 'Expand task',
  'collapse': 'Collapse task',
  'body.label': 'Details',
  'body.placeholder': 'Add more detail…',
  'notes.heading': 'Notes',
  'notes.composerPlaceholder': 'Add a note…',
  'notes.add': 'Add note',
  'notes.delete': 'Delete note',
  'notes.empty': 'No notes yet.',
  'empty.active': 'No tasks yet. Add one above to get started.',
  'empty.archived': 'Nothing archived yet.',
  'empty.filtered': 'No tasks match the selected tag.',
  'time.justNow': 'just now',
  'errors.failedToLoad': 'Failed to load tasks',
  'errors.failedToCreate': 'Failed to create task',
  'errors.failedToUpdate': 'Failed to update task',
  'errors.failedToAddNote': 'Failed to add note',
  'errors.failedToLoadNotes': 'Failed to load notes',
}

function mockT(key: string, opts?: Record<string, string | number>): string {
  if (key === 'notes.badge') return `+${opts?.count ?? 0} notes`
  if (key === 'time.created') return `Created ${opts?.relative ?? ''}`
  if (key === 'time.updated') return `Updated ${opts?.relative ?? ''}`
  return TRANSLATIONS[key] ?? key
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: mockT,
    i18n: { language: 'en' },
  }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

function makeTask(overrides: Partial<{
  id: number
  title: string
  body: string
  tags: string[]
  archived: boolean
  note_count: number
  created_at: string
  updated_at: string
}> = {}) {
  const now = '2026-05-14T10:00:00Z'
  return {
    id: 1,
    user_id: 1,
    title: 'Test task',
    body: '',
    archived: false,
    created_at: now,
    updated_at: now,
    tags: [] as string[],
    note_count: 0,
    ...overrides,
  }
}

type TaskShape = ReturnType<typeof makeTask>

function tasksResponse(tasks: TaskShape[]) {
  return { ok: true, json: () => Promise.resolve({ tasks }) }
}

function notesResponse(notes: Array<{ id: number; task_id: number; content: string; created_at: string }>) {
  return { ok: true, json: () => Promise.resolve({ notes }) }
}

function renderPage() {
  return render(
    <MemoryRouter>
      <Tasks />
    </MemoryRouter>,
  )
}

describe('Tasks – initial load', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('shows empty state when no tasks', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(tasksResponse([]))))
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('No tasks yet. Add one above to get started.')).toBeInTheDocument()
    })
  })

  it('renders tasks returned by the API', async () => {
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve(tasksResponse([makeTask({ title: 'Buy bread' })])),
    ))
    renderPage()
    await waitFor(() => {
      expect(screen.getByText('Buy bread')).toBeInTheDocument()
    })
  })
})

describe('Tasks – create flow', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('creates a task and prepends it to the list', async () => {
    const created = makeTask({ id: 2, title: 'New task', tags: ['work'] })
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(tasksResponse([]))
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ task: created }) })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByText('No tasks yet. Add one above to get started.')).toBeInTheDocument())

    const input = screen.getByPlaceholderText('What needs doing?')
    fireEvent.change(input, { target: { value: 'New task' } })

    // Tap the built-in "work" chip so the POST body contains it.
    fireEvent.click(screen.getByRole('button', { name: 'work' }))

    fireEvent.click(screen.getByLabelText('Add task'))

    await waitFor(() => {
      expect(screen.getByText('New task')).toBeInTheDocument()
    })

    const postCall = fetchMock.mock.calls.find(call => call[0] === '/api/tasks' && call[1]?.method === 'POST')
    expect(postCall).toBeDefined()
    expect(JSON.parse(postCall![1].body as string)).toEqual({ title: 'New task', tags: ['work'] })
  })

  it('attaches a custom tag entered with Enter', async () => {
    const created = makeTask({ id: 3, title: 'Plant trees', tags: ['garden'] })
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(tasksResponse([]))
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ task: created }) })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByPlaceholderText('What needs doing?')).toBeInTheDocument())

    fireEvent.change(screen.getByPlaceholderText('What needs doing?'), { target: { value: 'Plant trees' } })

    const customTagInput = screen.getByPlaceholderText('Add a tag…')
    fireEvent.change(customTagInput, { target: { value: 'garden' } })
    fireEvent.keyDown(customTagInput, { key: 'Enter', code: 'Enter' })

    fireEvent.click(screen.getByLabelText('Add task'))

    await waitFor(() => {
      const postCall = fetchMock.mock.calls.find(call => call[0] === '/api/tasks' && call[1]?.method === 'POST')
      expect(postCall).toBeDefined()
      expect(JSON.parse(postCall![1].body as string).tags).toEqual(['garden'])
    })
  })
})

describe('Tasks – tag filter', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('filters the visible list when a tag chip is clicked', async () => {
    const taskA = makeTask({ id: 1, title: 'Email Alice', tags: ['work'] })
    const taskB = makeTask({ id: 2, title: 'Walk the dog', tags: ['personal'] })
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(tasksResponse([taskA, taskB]))))

    renderPage()
    await waitFor(() => expect(screen.getByText('Email Alice')).toBeInTheDocument())
    expect(screen.getByText('Walk the dog')).toBeInTheDocument()

    // Click the filter chip group's "work" — there are multiple "work" buttons
    // (composer toggle + filter chip). Find the filter group specifically.
    const filterGroup = screen.getByRole('group', { name: 'Filter by tag' })
    fireEvent.click(within(filterGroup).getByRole('button', { name: 'work' }))

    await waitFor(() => {
      expect(screen.queryByText('Walk the dog')).not.toBeInTheDocument()
    })
    expect(screen.getByText('Email Alice')).toBeInTheDocument()
  })
})

describe('Tasks – archive toggle', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('archives a task via PATCH and dims/removes the card', async () => {
    const task = makeTask({ id: 7, title: 'Renew passport' })
    const archived = { ...task, archived: true, archived_at: '2026-05-14T11:00:00Z' }
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(tasksResponse([task]))   // initial load (archived=false)
      .mockResolvedValueOnce(notesResponse([]))       // notes lookup on expand
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ task: archived }) }) // PATCH
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByText('Renew passport')).toBeInTheDocument())

    // Expand
    fireEvent.click(screen.getByRole('button', { name: 'Expand task' }))
    await waitFor(() => expect(screen.getByText('No notes yet.')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: /Archive/ }))

    await waitFor(() => {
      expect(screen.queryByText('Renew passport')).not.toBeInTheDocument()
    })

    const patchCall = fetchMock.mock.calls.find(call =>
      typeof call[0] === 'string' && call[0].startsWith('/api/tasks/7') && call[1]?.method === 'PATCH',
    )
    expect(patchCall).toBeDefined()
    expect(JSON.parse(patchCall![1].body as string)).toEqual({ archived: true })
  })

  it('renders an archived task with dim style when Show archived is on', async () => {
    const archived = makeTask({ id: 9, title: 'Old task', archived: true })
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(tasksResponse([]))           // initial active list
      .mockResolvedValueOnce(tasksResponse([archived]))   // after toggle
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByText('No tasks yet. Add one above to get started.')).toBeInTheDocument())

    fireEvent.click(screen.getByLabelText('Show archived'))

    await waitFor(() => expect(screen.getByText('Old task')).toBeInTheDocument())
    const card = screen.getByTestId('task-card-9')
    expect(card.className).toMatch(/opacity-60/)
  })
})

describe('Tasks – add note', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('adds a note to an expanded task', async () => {
    const task = makeTask({ id: 5, title: 'Pack for trip' })
    const newNote = { id: 100, task_id: 5, content: 'Charger', created_at: '2026-05-14T12:00:00Z' }
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(tasksResponse([task]))   // initial load
      .mockResolvedValueOnce(notesResponse([]))       // notes list (empty)
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ note: newNote }) }) // POST note
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByText('Pack for trip')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: 'Expand task' }))
    await waitFor(() => expect(screen.getByText('No notes yet.')).toBeInTheDocument())

    const composer = screen.getByPlaceholderText('Add a note…')
    fireEvent.change(composer, { target: { value: 'Charger' } })
    fireEvent.click(screen.getByRole('button', { name: 'Add note' }))

    await waitFor(() => {
      expect(screen.getByText('Charger')).toBeInTheDocument()
    })

    const postCall = fetchMock.mock.calls.find(call =>
      typeof call[0] === 'string' && call[0] === '/api/tasks/5/notes' && call[1]?.method === 'POST',
    )
    expect(postCall).toBeDefined()
    expect(JSON.parse(postCall![1].body as string)).toEqual({ content: 'Charger' })
  })
})

describe('Tasks – failure paths', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('shows an alert when the initial load fails', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({ ok: false })))
    renderPage()
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('Failed to load tasks')
    })
  })
})
