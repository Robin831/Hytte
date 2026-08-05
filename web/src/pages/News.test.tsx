// @vitest-environment happy-dom
// Card titles are target="_blank" links; without this happy-dom tries to
// actually open example.test when a test clicks one.
// @vitest-environment-options { "settings": { "navigation": { "disableChildPageNavigation": true, "disableMainFrameNavigation": true } } }
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent, within } from '@testing-library/react'
import News from './News'

const TRANSLATIONS: Record<string, string> = {
  'title': 'News',
  'tabs.feed': 'Feed',
  'tabs.saved': 'Saved',
  'emptySaved': 'No saved articles yet.',
  'card.more': 'More like this',
  'card.less': 'Less like this',
  'card.save': 'Save',
  'card.saved': 'Saved',
}

function mockT(key: string, opts?: Record<string, unknown>): string {
  if (key === 'card.match') return `${opts?.score}% match`
  return TRANSLATIONS[key] ?? key
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: mockT, i18n: { language: 'en' } }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

interface FeedArticle {
  id: string
  source: string
  source_name: string
  source_color: string
  title: string
  url: string
  summary: string
  image_url: string
  published_at: string
  categories: string[]
  read: boolean
  saved: boolean
  feedback: number
  score: number
  score_reason: string
  also_in: null
}

function article(id: string, title: string, over: Partial<FeedArticle> = {}): FeedArticle {
  return {
    id,
    source: 'nrk',
    source_name: 'NRK',
    source_color: '#111111',
    title,
    url: `https://example.test/${id}`,
    summary: `${title} summary`,
    image_url: '',
    published_at: '2026-08-05T06:00:00Z',
    categories: [],
    read: false,
    saved: false,
    feedback: 0,
    score: 82,
    score_reason: 'matches your interests',
    also_in: null,
    ...over,
  }
}

interface Call { url: string; method: string; body: unknown }

let calls: Call[] = []

function noContent() {
  return Promise.resolve({ ok: true, status: 204, json: () => Promise.resolve({}) })
}

function jsonRes(data: unknown) {
  return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(data) })
}

interface FetchOpts {
  rankingEnabled?: boolean
  feed?: FeedArticle[]
  saved?: FeedArticle[]
}

function stubFetch({ rankingEnabled = true, feed, saved }: FetchOpts = {}) {
  const feedArticles = feed ?? [article('alpha', 'Alpha'), article('gamma', 'Gamma')]
  // The saved endpoint stores only the bookmark: no score, read or vote state.
  const savedArticles = saved ?? [
    article('alpha', 'Alpha', { saved: true, score: -1, score_reason: '', source_color: '' }),
    article('beta', 'Beta', { saved: true, score: -1, score_reason: '', source_color: '' }),
  ]

  const fetchMock = vi.fn((url: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET'
    calls.push({ url, method, body: init?.body ? JSON.parse(init.body as string) : undefined })
    if (url === '/api/news/articles') {
      return jsonRes({
        articles: feedArticles,
        hidden: [],
        ranking_enabled: rankingEnabled,
        scoring_pending: false,
        score_threshold: 25,
        layout: 'timeline',
        generated_at: '2026-08-05T06:00:00Z',
      })
    }
    if (url === '/api/news/saved' && method === 'GET') return jsonRes({ articles: savedArticles })
    if (url === '/api/news/saved') return noContent()
    if (url === '/api/news/read' || url === '/api/news/feedback') return noContent()
    return jsonRes({})
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

// Tabs are plain buttons whose only content is their label; the card action
// buttons contain an icon and no text, so matching on textContent is safe.
function tab(label: string): HTMLElement {
  const el = screen.getAllByRole('button').find(b => b.textContent === label)
  if (!el) throw new Error(`tab "${label}" not found`)
  return el
}

function card(title: string): HTMLElement {
  const el = screen.getByText(title).closest('article')
  if (!el) throw new Error(`card "${title}" not found`)
  return el
}

async function openSavedTab() {
  render(<News />)
  await waitFor(() => expect(screen.getByText('Alpha')).toBeInTheDocument())
  fireEvent.click(tab('Saved'))
  await waitFor(() => expect(screen.getByText('Beta')).toBeInTheDocument())
}

const postsTo = (url: string) => calls.filter(c => c.url === url && c.method === 'POST')

describe('News saved tab', () => {
  beforeEach(() => { calls = []; localStorage.clear() })
  afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks() })

  it('highlights the thumbs-up immediately and clears it on a second click', async () => {
    stubFetch()
    await openSavedTab()

    const up = within(card('Alpha')).getByTitle('More like this')
    expect(up).toHaveAttribute('aria-pressed', 'false')

    fireEvent.click(up)
    await waitFor(() => {
      expect(within(card('Alpha')).getByTitle('More like this')).toHaveAttribute('aria-pressed', 'true')
    })
    expect(postsTo('/api/news/feedback')).toHaveLength(1)
    expect(postsTo('/api/news/feedback')[0].body).toEqual({
      article_id: 'alpha',
      signal: 1,
      title: 'Alpha',
      summary: 'Alpha summary',
      source: 'nrk',
    })

    fireEvent.click(within(card('Alpha')).getByTitle('More like this'))
    await waitFor(() => {
      expect(within(card('Alpha')).getByTitle('More like this')).toHaveAttribute('aria-pressed', 'false')
    })
    expect(postsTo('/api/news/feedback')).toHaveLength(2)
    expect(postsTo('/api/news/feedback')[1].body).toMatchObject({ article_id: 'alpha', signal: 0 })
  })

  it('switches the highlight from thumbs-up to thumbs-down', async () => {
    stubFetch()
    await openSavedTab()

    fireEvent.click(within(card('Alpha')).getByTitle('More like this'))
    await waitFor(() => {
      expect(within(card('Alpha')).getByTitle('More like this')).toHaveAttribute('aria-pressed', 'true')
    })

    fireEvent.click(within(card('Alpha')).getByTitle('Less like this'))
    await waitFor(() => {
      expect(within(card('Alpha')).getByTitle('Less like this')).toHaveAttribute('aria-pressed', 'true')
    })
    expect(within(card('Alpha')).getByTitle('More like this')).toHaveAttribute('aria-pressed', 'false')
    expect(postsTo('/api/news/feedback')[1].body).toMatchObject({ article_id: 'alpha', signal: -1 })
  })

  it('votes on a saved article that is not in the current feed', async () => {
    stubFetch()
    await openSavedTab()

    fireEvent.click(within(card('Beta')).getByTitle('Less like this'))
    await waitFor(() => {
      expect(within(card('Beta')).getByTitle('Less like this')).toHaveAttribute('aria-pressed', 'true')
    })
    expect(postsTo('/api/news/feedback')).toHaveLength(1)
    expect(postsTo('/api/news/feedback')[0].body).toMatchObject({ article_id: 'beta', signal: -1 })
  })

  it('dims a saved article as soon as it is opened', async () => {
    stubFetch()
    await openSavedTab()

    expect(card('Alpha')).not.toHaveClass('opacity-60')
    fireEvent.click(screen.getByText('Alpha'))
    await waitFor(() => expect(card('Alpha')).toHaveClass('opacity-60'))

    expect(postsTo('/api/news/read')).toHaveLength(1)
    expect(postsTo('/api/news/read')[0].body).toEqual({ article_id: 'alpha' })
  })

  it('carries vote and read state over to the feed tab without refetching', async () => {
    stubFetch()
    await openSavedTab()

    fireEvent.click(within(card('Alpha')).getByTitle('More like this'))
    fireEvent.click(screen.getByText('Alpha'))
    await waitFor(() => expect(card('Alpha')).toHaveClass('opacity-60'))

    const feedLoads = calls.filter(c => c.url === '/api/news/articles').length
    fireEvent.click(tab('Feed'))
    await waitFor(() => expect(screen.getByText('Gamma')).toBeInTheDocument())

    expect(within(card('Alpha')).getByTitle('More like this')).toHaveAttribute('aria-pressed', 'true')
    expect(card('Alpha')).toHaveClass('opacity-60')
    expect(calls.filter(c => c.url === '/api/news/articles')).toHaveLength(feedLoads)
  })

  it('sends the same feedback body from the feed tab as from the saved tab', async () => {
    stubFetch()
    render(<News />)
    await waitFor(() => expect(screen.getByText('Alpha')).toBeInTheDocument())

    fireEvent.click(within(card('Alpha')).getByTitle('More like this'))
    await waitFor(() => expect(postsTo('/api/news/feedback')).toHaveLength(1))
    expect(postsTo('/api/news/feedback')[0].body).toEqual({
      article_id: 'alpha',
      signal: 1,
      title: 'Alpha',
      summary: 'Alpha summary',
      source: 'nrk',
    })
  })

  // /api/news/saved returns score -1 (unknown), so a saved card only has a
  // score to show once a vote pins it — 100 for 👍, matching the feed.
  it('shows the match score on saved cards when ranking is enabled', async () => {
    stubFetch({ rankingEnabled: true })
    await openSavedTab()

    expect(within(card('Alpha')).queryByText('100% match')).not.toBeInTheDocument()
    fireEvent.click(within(card('Alpha')).getByTitle('More like this'))
    await waitFor(() => expect(within(card('Alpha')).getByText('100% match')).toBeInTheDocument())
  })

  it('hides the match score on saved cards when ranking is disabled', async () => {
    stubFetch({ rankingEnabled: false })
    await openSavedTab()

    fireEvent.click(within(card('Alpha')).getByTitle('More like this'))
    await waitFor(() => {
      expect(within(card('Alpha')).getByTitle('More like this')).toHaveAttribute('aria-pressed', 'true')
    })
    expect(within(card('Alpha')).queryByText('100% match')).not.toBeInTheDocument()
  })

  it('removes a row immediately when it is unsaved', async () => {
    stubFetch()
    await openSavedTab()

    fireEvent.click(within(card('Alpha')).getByTitle('Saved'))
    await waitFor(() => expect(screen.queryByText('Alpha')).not.toBeInTheDocument())
    expect(screen.getByText('Beta')).toBeInTheDocument()
    expect(postsTo('/api/news/saved')).toHaveLength(1)
    expect(postsTo('/api/news/saved')[0].body).toMatchObject({ saved: false })
  })
})
