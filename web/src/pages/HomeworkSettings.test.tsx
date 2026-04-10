// @vitest-environment happy-dom
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import HomeworkSettings from './HomeworkSettings'

// ── Translation mock ──────────────────────────────────────────────────────────
// stableT must be a stable reference — HomeworkSettings' useEffect has `t` as a
// dependency, so a new function on every render would cause an infinite re-run
// loop that burns through fetch mocks out of order.

const TRANSLATIONS: Record<string, string> = {
  'backToList': 'Back to conversations',
  'settings.title': 'Profile Settings',
  'settings.age': 'Age',
  'settings.gradeLevel': 'Grade Level',
  'settings.selectGrade': 'Select grade...',
  'settings.preferredLanguage': 'Preferred Language',
  'settings.selectLanguage': 'Select language...',
  'settings.subjects': 'Current Subjects',
  'settings.schoolName': 'School Name',
  'settings.schoolNamePlaceholder': 'Optional',
  'settings.currentTopics': 'Current Topics',
  'settings.currentTopicsHint': 'One topic per line',
  'settings.currentTopicsPlaceholder': 'e.g. Fractions',
  'settings.save': 'Save',
  'settings.saved': 'Profile saved successfully',
  'settings.errors.failedToLoad': 'Failed to load profile',
  'settings.errors.failedToSave': 'Failed to save profile',
  'settings.errors.ageInvalid': 'Please enter an age between 1 and 25',
  'settings.errors.gradeRequired': 'Please select a grade level',
  'settings.errors.languageRequired': 'Please select a preferred language',
}

function stableT(key: string, opts?: Record<string, unknown>): string {
  let val = TRANSLATIONS[key] ?? key
  if (opts) {
    for (const [k, v] of Object.entries(opts)) {
      val = val.replace(`{{${k}}}`, String(v))
    }
  }
  return val
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: stableT,
    i18n: { language: 'en' },
  }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

// ── Helpers ───────────────────────────────────────────────────────────────────

function makeProfile(overrides: Partial<{
  age: number
  grade_level: string
  subjects: string[]
  preferred_language: string
  school_name: string
  current_topics: string[]
}> = {}) {
  return {
    id: 1,
    kid_id: 42,
    age: 10,
    grade_level: '4',
    subjects: ['math', 'science'],
    preferred_language: 'en',
    school_name: 'Testville School',
    current_topics: ['Fractions', 'Photosynthesis'],
    created_at: '2026-04-09T00:00:00Z',
    updated_at: '2026-04-09T00:00:00Z',
    ...overrides,
  }
}

function profileResponse(profile: ReturnType<typeof makeProfile> | null) {
  return { ok: true, json: () => Promise.resolve({ profile }) }
}

function renderPage() {
  return render(
    <MemoryRouter>
      <HomeworkSettings />
    </MemoryRouter>,
  )
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('HomeworkSettings – load success', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('shows loading spinner on initial render', () => {
    vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})))
    const { container } = renderPage()
    expect(container.querySelector('.animate-spin')).toBeInTheDocument()
  })

  it('populates form fields from loaded profile', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(profileResponse(makeProfile()))))
    renderPage()
    await waitFor(() => {
      expect(screen.getByLabelText('Age')).toHaveValue(10)
    })
    expect(screen.getByLabelText('Grade Level')).toHaveValue('4')
    expect(screen.getByLabelText('Preferred Language')).toHaveValue('en')
    expect(screen.getByLabelText('School Name')).toHaveValue('Testville School')
    expect(screen.getByLabelText('Current Topics')).toHaveValue('Fractions\nPhotosynthesis')
  })

  it('renders form with empty defaults when profile is null (no profile yet)', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(profileResponse(null))))
    renderPage()
    await waitFor(() => {
      expect(screen.getByLabelText('Age')).toBeInTheDocument()
    })
    expect(screen.getByLabelText('Age')).toHaveValue(null)
    expect(screen.getByLabelText('Grade Level')).toHaveValue('')
    expect(screen.getByLabelText('Preferred Language')).toHaveValue('')
  })
})

describe('HomeworkSettings – load failure', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('shows error when initial load fails', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({ ok: false })))
    renderPage()
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('Failed to load profile')
    })
  })
})

describe('HomeworkSettings – save success', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('shows success message after saving', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(profileResponse(makeProfile()))
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByLabelText('Age')).toHaveValue(10))

    fireEvent.click(screen.getByText('Save'))

    await waitFor(() => {
      expect(screen.getByRole('status')).toHaveTextContent('Profile saved successfully')
    })
  })

  it('sends PUT /api/homework/profile with form values', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(profileResponse(makeProfile()))
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({}) })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByLabelText('Age')).toHaveValue(10))

    fireEvent.click(screen.getByText('Save'))

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/homework/profile',
        expect.objectContaining({
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
        }),
      )
    })
  })
})

describe('HomeworkSettings – save failure', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('shows server error message when save fails', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(profileResponse(makeProfile()))
      .mockResolvedValueOnce({ ok: false, json: () => Promise.resolve({ error: 'database error' }) })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByLabelText('Age')).toHaveValue(10))

    fireEvent.click(screen.getByText('Save'))

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('database error')
    })
  })

  it('shows fallback error when save fails without error body', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(profileResponse(makeProfile()))
      .mockResolvedValueOnce({ ok: false, json: () => Promise.reject(new Error('no body')) })
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByLabelText('Age')).toHaveValue(10))

    fireEvent.click(screen.getByText('Save'))

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('Failed to save profile')
    })
  })
})

describe('HomeworkSettings – client-side validation', () => {
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('shows field errors for empty required fields without calling PUT', async () => {
    // null profile → form starts empty
    const fetchMock = vi.fn().mockResolvedValueOnce(profileResponse(null))
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByLabelText('Age')).toBeInTheDocument())

    fireEvent.click(screen.getByText('Save'))

    await waitFor(() => {
      expect(screen.getByText('Please enter an age between 1 and 25')).toBeInTheDocument()
      expect(screen.getByText('Please select a grade level')).toBeInTheDocument()
      expect(screen.getByText('Please select a preferred language')).toBeInTheDocument()
    })

    // PUT should NOT have been called
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('clears age field error when a valid age is entered', async () => {
    const fetchMock = vi.fn().mockResolvedValueOnce(profileResponse(null))
    vi.stubGlobal('fetch', fetchMock)

    renderPage()
    await waitFor(() => expect(screen.getByLabelText('Age')).toBeInTheDocument())

    fireEvent.click(screen.getByText('Save'))

    await waitFor(() => {
      expect(screen.getByText('Please enter an age between 1 and 25')).toBeInTheDocument()
    })

    fireEvent.change(screen.getByLabelText('Age'), { target: { value: '8' } })

    expect(screen.queryByText('Please enter an age between 1 and 25')).not.toBeInTheDocument()
  })
})
