// @vitest-environment happy-dom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent, cleanup, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router'
import WordfeudLeague from './WordfeudLeague'

function mockT(key: string, opts?: Record<string, string | number>): string {
  if (!opts) return key
  return `${key} ${Object.values(opts).join(' ')}`
}

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: mockT, i18n: { language: 'en' } }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}))

vi.mock('../i18n', () => ({
  default: { language: 'en' },
}))

const VIEW = {
  configured: true,
  username: 'luremus',
  can_report: true,
  can_invite: true,
  wordfeud_connected: true,
  fetched_at: '2026-10-11T08:00:00Z',
  group: {
    league: 'Wordfeud Norsk',
    season: 398,
    division: 5,
    group: 11,
    deadline: '2099-10-23T21:59:00Z',
    url: 'https://wordfeud.aasmul.net/serie-965530',
    standings: [
      { place: 1, name: 'tjobias', played: 2, won: 1, drawn: 0, lost: 1, score_for: 1046, score_against: 890, margin: 156, points: 2 },
      { place: 2, name: 'Luremus', played: 0, won: 0, drawn: 0, lost: 0, score_for: 0, score_against: 0, margin: 0, points: 0 },
    ],
  },
  matches: [
    { opponent: 'tjobias', status: 'playing', game_id: 77, my_score: 120, opp_score: 90, is_my_turn: true },
    { opponent: 'ave62', status: 'to_report', game_id: 78, my_score: 480, opp_score: 469 },
    { opponent: 'Illkl', status: 'not_started', my_score: 0, opp_score: 0 },
    { opponent: 'jenscb', status: 'not_started', my_score: 0, opp_score: 0 },
    { opponent: '1roxy1', status: 'reported', my_score: 500, opp_score: 400 },
  ],
}

interface Call {
  url: string
  method: string
  body?: Record<string, unknown>
}

let calls: Call[] = []
let view: Record<string, unknown> = VIEW
let failInvite: Record<string, unknown> | null = null

function mockFetch() {
  return vi.fn(async (url: string, opts?: RequestInit) => {
    const call: Call = { url: String(url), method: opts?.method ?? 'GET' }
    if (typeof opts?.body === 'string') call.body = JSON.parse(opts.body)
    calls.push(call)
    if (call.url.includes('/league/invite') && failInvite) {
      return { ok: false, status: 502, json: async () => failInvite }
    }
    if (call.method !== 'GET') {
      return { ok: true, status: 200, json: async () => ({ status: 'ok' }) }
    }
    return { ok: true, status: 200, json: async () => view }
  })
}

function renderLeague(onOpenGame = vi.fn()) {
  render(
    <MemoryRouter>
      <WordfeudLeague onOpenGame={onOpenGame} />
    </MemoryRouter>,
  )
  return onOpenGame
}

function rowFor(opponent: string): HTMLElement {
  // The name span, not the score label the report form adds for the opponent.
  const row = screen.getByText(opponent, { selector: 'li span.font-medium' }).closest('li')
  if (!row) throw new Error(`no row for ${opponent}`)
  return row
}

describe('WordfeudLeague', () => {
  beforeEach(() => {
    calls = []
    view = VIEW
    failInvite = null
    vi.stubGlobal('fetch', mockFetch())
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it('shows the group, each match status and the table', async () => {
    renderLeague()
    await screen.findByText('Wordfeud Norsk')
    expect(screen.getByText('league.header 398 5 11')).toBeTruthy()
    expect(within(rowFor('tjobias')).getByText('league.status.playingMyTurn')).toBeTruthy()
    expect(within(rowFor('tjobias')).getByText('120–90')).toBeTruthy()
    expect(within(rowFor('ave62')).getByText('league.status.to_report')).toBeTruthy()
    expect(within(rowFor('1roxy1')).getByText('league.status.reported')).toBeTruthy()
    // Only opponents without a game can be invited.
    expect(within(rowFor('Illkl')).getByText('league.invite')).toBeTruthy()
    expect(within(rowFor('tjobias')).queryByText('league.invite')).toBeNull()
    expect(screen.getByText('+156')).toBeTruthy()
  })

  it('opens a running game on the board', async () => {
    const onOpenGame = renderLeague()
    await screen.findByText('Wordfeud Norsk')
    fireEvent.click(within(rowFor('tjobias')).getByText('league.openGame'))
    expect(onOpenGame).toHaveBeenCalledWith(77)
  })

  it('invites one opponent and reloads', async () => {
    renderLeague()
    await screen.findByText('Wordfeud Norsk')
    fireEvent.click(within(rowFor('Illkl')).getByText('league.invite'))
    await waitFor(() => {
      const invites = calls.filter((c) => c.url.includes('/league/invite'))
      expect(invites).toHaveLength(1)
      expect(invites[0].body).toEqual({ opponent: 'Illkl' })
    })
    await waitFor(() => expect(calls.filter((c) => c.method === 'GET')).toHaveLength(2))
  })

  it('invites everyone without a game, and stops at the first refusal', async () => {
    renderLeague()
    await screen.findByText('Wordfeud Norsk')
    fireEvent.click(screen.getByText('league.inviteAll 2'))
    await waitFor(() => {
      expect(calls.filter((c) => c.url.includes('/league/invite')).map((c) => c.body?.opponent)).toEqual(['Illkl', 'jenscb'])
    })

    calls = []
    failInvite = { code: 'invite_refused', detail: 'user_not_found' }
    await waitFor(() => expect((screen.getByText('league.inviteAll 2').closest('button') as HTMLButtonElement).disabled).toBe(false))
    fireEvent.click(screen.getByText('league.inviteAll 2'))
    await screen.findByText('Illkl: league.errors.invite_refused (user_not_found)')
    expect(calls.filter((c) => c.url.includes('/league/invite'))).toHaveLength(1)
  })

  it('posts a result only after the scores are confirmed', async () => {
    renderLeague()
    await screen.findByText('Wordfeud Norsk')
    fireEvent.click(within(rowFor('ave62')).getByText('league.report'))
    expect(calls.some((c) => c.url.includes('/league/report'))).toBe(false)

    const row = rowFor('ave62')
    const inputs = row.querySelectorAll('input')
    expect((inputs[0] as HTMLInputElement).value).toBe('480')
    expect((inputs[1] as HTMLInputElement).value).toBe('469')
    fireEvent.change(inputs[1], { target: { value: '470' } })
    fireEvent.click(within(row).getByText('league.reportConfirm'))
    await waitFor(() => {
      const report = calls.find((c) => c.url.includes('/league/report'))
      expect(report?.body).toEqual({ opponent: 'ave62', my_score: 480, opp_score: 470 })
    })
  })

  it('asks for the league email before a result can be posted', async () => {
    view = { ...VIEW, can_report: false }
    renderLeague()
    await screen.findByText('Wordfeud Norsk')
    fireEvent.click(within(rowFor('ave62')).getByText('league.report'))
    expect(screen.getByText('league.needEmail')).toBeTruthy()
    expect(screen.queryByText('league.reportConfirm')).toBeNull()
  })

  it('shows the setup form when no league is configured and saves it', async () => {
    view = { configured: false, can_report: false, can_invite: false, wordfeud_connected: false, matches: [] }
    renderLeague()
    await screen.findByText('league.setup.title')
    const [username, email] = Array.from(document.querySelectorAll('input'))
    fireEvent.change(username, { target: { value: ' luremus ' } })
    fireEvent.change(email, { target: { value: 'me@example.com' } })
    fireEvent.click(screen.getByText('league.setup.save'))
    await waitFor(() => {
      const save = calls.find((c) => c.method === 'PUT')
      expect(save?.url).toContain('/league/config')
      expect(save?.body).toEqual({ username: 'luremus', email: 'me@example.com' })
    })
  })
})
