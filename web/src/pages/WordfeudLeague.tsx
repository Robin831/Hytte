import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import { RefreshCw, Loader2, ExternalLink, Send, Settings2, Check, Trophy } from 'lucide-react'
import { formatDateTime } from '../utils/formatDate'

type MatchStatus = 'not_started' | 'invite_sent' | 'invite_received' | 'playing' | 'to_report' | 'reported'

interface LeagueMatch {
  opponent: string
  status: MatchStatus
  game_id?: number
  my_score: number
  opp_score: number
  is_my_turn?: boolean
  resigned?: 'me' | 'opponent'
}

interface Standing {
  place: number
  name: string
  played: number
  won: number
  drawn: number
  lost: number
  score_for: number
  score_against: number
  margin: number
  points: number
}

interface LeagueGroup {
  league: string
  season: number
  division: number
  group: number
  deadline: string
  url: string
  standings: Standing[]
}

interface LeagueView {
  configured: boolean
  username?: string
  can_report: boolean
  can_invite: boolean
  wordfeud_connected: boolean
  group?: LeagueGroup
  matches: LeagueMatch[]
  fetched_at?: string
  stale?: boolean
}

interface ApiError {
  code?: string
  detail?: string
}

const ERROR_CODES = [
  'player_not_found',
  'league_login_failed',
  'league_unavailable',
  'not_connected',
  'invite_unsupported',
  'already_started',
  'invite_refused',
  'invite_failed',
  'already_reported',
  'not_registered',
] as const

type ErrorCode = (typeof ERROR_CODES)[number]

function isErrorCode(code: string | undefined): code is ErrorCode {
  return (ERROR_CODES as readonly string[]).includes(code ?? '')
}

const STATUS_STYLE: Record<MatchStatus, string> = {
  not_started: 'bg-gray-700 text-gray-300',
  invite_sent: 'bg-blue-900/60 text-blue-200',
  invite_received: 'bg-purple-900/60 text-purple-200',
  playing: 'bg-emerald-900/60 text-emerald-200',
  to_report: 'bg-amber-900/60 text-amber-200',
  reported: 'bg-gray-700 text-gray-300',
}

export default function WordfeudLeague({ onOpenGame }: { onOpenGame: (gameId: number) => void }) {
  const { t } = useTranslation('wordfeud')

  const [view, setView] = useState<LeagueView | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)
  const [editing, setEditing] = useState(false)
  const [inviting, setInviting] = useState<string | null>(null)
  const [invitingAll, setInvitingAll] = useState(false)
  const [reportFor, setReportFor] = useState<string | null>(null)
  // The time the countdown is measured from; refreshed with every load.
  const [now, setNow] = useState(() => Date.now())

  const errorText = useCallback((err: ApiError, fallback: string) => {
    const base = isErrorCode(err.code) ? t(`league.errors.${err.code}`) : fallback
    return err.detail ? `${base} (${err.detail})` : base
  }, [t])

  const load = useCallback(async (refresh: boolean) => {
    setLoading(true)
    try {
      const res = await fetch(`/api/wordfeud/league${refresh ? '?refresh=1' : ''}`, { credentials: 'include' })
      const data = await res.json().catch(() => ({}))
      if (!res.ok) {
        setError(errorText(data, t('league.errors.loadFailed')))
        return
      }
      setView(data)
      setNow(Date.now())
      setError(null)
    } catch {
      setError(t('league.errors.loadFailed'))
    } finally {
      setLoading(false)
    }
  }, [errorText, t])

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- initial fetch on mount
    void load(false)
  }, [load])

  const sendInvite = async (opponent: string): Promise<boolean> => {
    const res = await fetch('/api/wordfeud/league/invite', {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ opponent }),
    })
    if (!res.ok) {
      const data = await res.json().catch(() => ({}))
      setActionError(`${opponent}: ${errorText(data, t('league.errors.invite_failed'))}`)
      return false
    }
    return true
  }

  const invite = async (opponent: string) => {
    setInviting(opponent)
    setActionError(null)
    try {
      await sendInvite(opponent)
    } catch {
      setActionError(`${opponent}: ${t('league.errors.invite_failed')}`)
    } finally {
      setInviting(null)
      await load(false)
    }
  }

  const inviteAll = async (opponents: string[]) => {
    setInvitingAll(true)
    setActionError(null)
    try {
      for (const opponent of opponents) {
        setInviting(opponent)
        // Stop at the first refusal so one problem isn't repeated for everyone.
        if (!(await sendInvite(opponent))) break
      }
    } catch {
      setActionError(t('league.errors.invite_failed'))
    } finally {
      setInviting(null)
      setInvitingAll(false)
      await load(false)
    }
  }

  const statusLabel = (m: LeagueMatch) => {
    switch (m.status) {
      case 'not_started': return t('league.status.not_started')
      case 'invite_sent': return t('league.status.invite_sent')
      case 'invite_received': return t('league.status.invite_received')
      case 'playing': return m.is_my_turn ? t('league.status.playingMyTurn') : t('league.status.playingTheirTurn')
      case 'to_report': return t('league.status.to_report')
      case 'reported': return t('league.status.reported')
    }
  }

  if (editing || (view && !view.configured)) {
    return (
      <LeagueSetup
        username={view?.username ?? ''}
        configured={view?.configured ?? true}
        errorText={errorText}
        onCancel={view?.configured && view.group ? () => setEditing(false) : undefined}
        onSaved={() => {
          setEditing(false)
          void load(true)
        }}
      />
    )
  }

  if (loading && !view) {
    return (
      <div className="flex items-center gap-2 text-gray-400 text-sm">
        <Loader2 size={16} className="animate-spin" />
        {t('league.loading')}
      </div>
    )
  }

  if (!view || !view.group) {
    return (
      <div className="p-3 bg-gray-800/70 rounded-lg border border-gray-700 text-sm text-gray-300">
        {error ?? t('league.errors.loadFailed')}
        <button onClick={() => void load(false)} className="ml-2 text-blue-400 hover:text-blue-300 underline cursor-pointer">
          {t('league.retry')}
        </button>
        <button onClick={() => setEditing(true)} className="ml-2 text-blue-400 hover:text-blue-300 underline cursor-pointer">
          {t('league.settings')}
        </button>
      </div>
    )
  }

  const group = view.group
  const deadline = new Date(group.deadline)
  const msLeft = deadline.getTime() - now
  const daysLeft = Math.floor(msLeft / 86_400_000)
  const hoursLeft = Math.floor(msLeft / 3_600_000)
  const toInvite = view.matches.filter((m) => m.status === 'not_started').map((m) => m.opponent)
  const me = view.username?.toLowerCase()

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h2 className="text-lg font-semibold">{group.league}</h2>
          <p className="text-sm text-gray-400">
            {t('league.header', { season: group.season, division: group.division, group: group.group })}
          </p>
          <p className={`text-sm mt-1 ${msLeft > 0 && daysLeft < 2 ? 'text-amber-300' : 'text-gray-300'}`}>
            {msLeft <= 0
              ? t('league.seasonOver')
              : daysLeft >= 1
                ? t('league.daysLeft', { count: daysLeft })
                : t('league.hoursLeft', { count: Math.max(hoursLeft, 1) })}
            <span className="text-gray-500"> · {t('league.deadline', { date: formatDateTime(deadline) })}</span>
          </p>
        </div>
        <div className="flex items-center gap-2">
          <a
            href={group.url}
            target="_blank"
            rel="noreferrer"
            className="flex items-center gap-1.5 px-3 py-1.5 text-sm rounded-lg bg-gray-800 hover:bg-gray-700 text-gray-200"
          >
            <ExternalLink size={14} />
            <span className="hidden sm:inline">{t('league.openSite')}</span>
          </a>
          <button
            onClick={() => setEditing(true)}
            aria-label={t('league.settings')}
            title={t('league.settings')}
            className="p-2 rounded-lg bg-gray-800 hover:bg-gray-700 text-gray-200 cursor-pointer"
          >
            <Settings2 size={14} />
          </button>
          <button
            onClick={() => void load(true)}
            disabled={loading}
            aria-label={t('league.refresh')}
            title={t('league.refresh')}
            className="p-2 rounded-lg bg-gray-800 hover:bg-gray-700 text-gray-200 cursor-pointer disabled:opacity-50"
          >
            <RefreshCw size={14} className={loading ? 'animate-spin' : ''} />
          </button>
        </div>
      </div>

      {view.stale && (
        <div className="p-3 bg-amber-950/40 rounded-lg border border-amber-900 text-sm text-amber-200">
          {t('league.stale', { date: view.fetched_at ? formatDateTime(view.fetched_at) : '' })}
        </div>
      )}
      {!view.wordfeud_connected && (
        <div className="p-3 bg-gray-800/70 rounded-lg border border-gray-700 text-sm text-gray-300">
          {t('league.wordfeudNotConnected')}
          <Link to="/settings" className="ml-2 text-blue-400 hover:text-blue-300 underline">
            {t('goToSettings')}
          </Link>
        </div>
      )}
      {(error || actionError) && (
        <div role="alert" className="p-3 bg-red-950/40 rounded-lg border border-red-900 text-sm text-red-200">
          {actionError ?? error}
        </div>
      )}

      <section>
        <div className="flex items-center justify-between gap-3 mb-2">
          <h3 className="text-sm font-medium text-gray-400">{t('league.myMatches')}</h3>
          {view.can_invite && toInvite.length > 1 && (
            <button
              onClick={() => void inviteAll(toInvite)}
              disabled={invitingAll || inviting !== null}
              className="flex items-center gap-1.5 px-3 py-1.5 text-sm rounded-lg bg-blue-600 hover:bg-blue-500 text-white cursor-pointer disabled:opacity-50"
            >
              {invitingAll ? <Loader2 size={14} className="animate-spin" /> : <Send size={14} />}
              {t('league.inviteAll', { count: toInvite.length })}
            </button>
          )}
        </div>
        <ul className="divide-y divide-gray-800 rounded-lg border border-gray-800 bg-gray-900">
          {view.matches.map((m) => (
            <li key={m.opponent} className="p-3">
              <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
                <span className="font-medium min-w-0 truncate" translate="no">{m.opponent}</span>
                <span className={`px-2 py-0.5 rounded-full text-xs whitespace-nowrap ${STATUS_STYLE[m.status]}`}>
                  {statusLabel(m)}
                </span>
                {(m.status === 'playing' || m.status === 'to_report' || m.status === 'reported') && (
                  <span className="text-sm tabular-nums text-gray-300">
                    {m.my_score}–{m.opp_score}
                  </span>
                )}
                <span className="ml-auto flex items-center gap-2">
                  {m.status === 'not_started' && view.can_invite && (
                    <button
                      onClick={() => void invite(m.opponent)}
                      disabled={inviting !== null}
                      className="flex items-center gap-1.5 px-3 py-1.5 text-sm rounded-lg bg-gray-800 hover:bg-gray-700 text-gray-100 cursor-pointer disabled:opacity-50"
                    >
                      {inviting === m.opponent ? <Loader2 size={14} className="animate-spin" /> : <Send size={14} />}
                      {t('league.invite')}
                    </button>
                  )}
                  {m.status === 'playing' && m.game_id !== undefined && (
                    <button
                      onClick={() => onOpenGame(m.game_id as number)}
                      className="px-3 py-1.5 text-sm rounded-lg bg-gray-800 hover:bg-gray-700 text-gray-100 cursor-pointer"
                    >
                      {t('league.openGame')}
                    </button>
                  )}
                  {m.status === 'to_report' && reportFor !== m.opponent && (
                    <button
                      onClick={() => {
                        setActionError(null)
                        setReportFor(m.opponent)
                      }}
                      className="flex items-center gap-1.5 px-3 py-1.5 text-sm rounded-lg bg-amber-600 hover:bg-amber-500 text-white cursor-pointer"
                    >
                      <Trophy size={14} />
                      {t('league.report')}
                    </button>
                  )}
                  {m.status === 'reported' && <Check size={16} className="text-emerald-400" aria-hidden="true" />}
                </span>
              </div>
              {m.status === 'invite_received' && (
                <p className="mt-1 text-xs text-gray-400">{t('league.acceptInApp')}</p>
              )}
              {m.status === 'to_report' && reportFor === m.opponent && (
                <ReportForm
                  match={m}
                  canReport={view.can_report}
                  errorText={errorText}
                  onAddEmail={() => {
                    setReportFor(null)
                    setEditing(true)
                  }}
                  onCancel={() => setReportFor(null)}
                  onDone={() => {
                    setReportFor(null)
                    void load(false)
                  }}
                />
              )}
            </li>
          ))}
        </ul>
      </section>

      <section>
        <h3 className="text-sm font-medium text-gray-400 mb-2">{t('league.standings')}</h3>
        <div className="overflow-x-auto rounded-lg border border-gray-800 bg-gray-900">
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left text-xs text-gray-400 border-b border-gray-800">
                <th className="px-3 py-2 font-medium w-8">#</th>
                <th className="px-3 py-2 font-medium">{t('league.col.player')}</th>
                <th className="px-2 py-2 font-medium text-right">{t('league.col.played')}</th>
                <th className="px-2 py-2 font-medium text-right hidden sm:table-cell">{t('league.col.record')}</th>
                <th className="px-2 py-2 font-medium text-right hidden sm:table-cell">{t('league.col.score')}</th>
                <th className="px-2 py-2 font-medium text-right">{t('league.col.margin')}</th>
                <th className="px-3 py-2 font-medium text-right">{t('league.col.points')}</th>
              </tr>
            </thead>
            <tbody>
              {group.standings.map((s) => (
                <tr
                  key={s.name}
                  className={`border-b border-gray-800 last:border-0 ${s.name.toLowerCase() === me ? 'bg-blue-950/40 font-medium' : ''}`}
                >
                  <td className="px-3 py-2 text-gray-400 tabular-nums">{s.place}</td>
                  <td className="px-3 py-2 max-w-[9rem] truncate" translate="no">{s.name}</td>
                  <td className="px-2 py-2 text-right tabular-nums">{s.played}</td>
                  <td className="px-2 py-2 text-right tabular-nums hidden sm:table-cell">{s.won}-{s.drawn}-{s.lost}</td>
                  <td className="px-2 py-2 text-right tabular-nums hidden sm:table-cell">{s.score_for}–{s.score_against}</td>
                  <td className="px-2 py-2 text-right tabular-nums">{s.margin > 0 ? `+${s.margin}` : s.margin}</td>
                  <td className="px-3 py-2 text-right tabular-nums">{s.points}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  )
}

function ReportForm({ match, canReport, errorText, onAddEmail, onCancel, onDone }: {
  match: LeagueMatch
  canReport: boolean
  errorText: (err: ApiError, fallback: string) => string
  onAddEmail: () => void
  onCancel: () => void
  onDone: () => void
}) {
  const { t } = useTranslation('wordfeud')
  const [myScore, setMyScore] = useState(String(match.my_score))
  const [oppScore, setOppScore] = useState(String(match.opp_score))
  const [sending, setSending] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const my = Number(myScore)
  const opp = Number(oppScore)
  const valid = myScore !== '' && oppScore !== '' && Number.isInteger(my) && Number.isInteger(opp) && my >= 0 && opp >= 0

  if (!canReport) {
    return (
      <div className="mt-3 p-3 rounded-lg bg-gray-800/70 text-sm text-gray-300">
        {t('league.needEmail')}
        <button onClick={onAddEmail} className="ml-2 text-blue-400 hover:text-blue-300 underline cursor-pointer">
          {t('league.addEmail')}
        </button>
      </div>
    )
  }

  const submit = async () => {
    setSending(true)
    setError(null)
    try {
      const res = await fetch('/api/wordfeud/league/report', {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ opponent: match.opponent, my_score: my, opp_score: opp }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        setError(errorText(data, t('league.errors.reportFailed')))
        return
      }
      onDone()
    } catch {
      setError(t('league.errors.reportFailed'))
    } finally {
      setSending(false)
    }
  }

  const inputClass = 'w-20 px-2 py-1.5 rounded-lg bg-gray-950 border border-gray-700 text-sm text-right tabular-nums'

  return (
    <div className="mt-3 p-3 rounded-lg bg-gray-800/70 space-y-3">
      <p className="text-sm text-gray-300">{t('league.reportIntro', { opponent: match.opponent })}</p>
      {match.resigned && (
        <p className="text-xs text-amber-200">
          {match.resigned === 'opponent' ? t('league.resignedOpponent') : t('league.resignedMe')}
        </p>
      )}
      <div className="flex flex-wrap items-end gap-3">
        <label className="text-xs text-gray-400">
          <span className="block mb-1">{t('league.myScore')}</span>
          <input type="number" inputMode="numeric" min={0} value={myScore} onChange={(e) => setMyScore(e.target.value)} className={inputClass} />
        </label>
        <label className="text-xs text-gray-400">
          <span className="block mb-1" translate="no">{match.opponent}</span>
          <input type="number" inputMode="numeric" min={0} value={oppScore} onChange={(e) => setOppScore(e.target.value)} className={inputClass} />
        </label>
        <button
          onClick={() => void submit()}
          disabled={!valid || sending}
          className="flex items-center gap-1.5 px-3 py-1.5 text-sm rounded-lg bg-amber-600 hover:bg-amber-500 text-white cursor-pointer disabled:opacity-50"
        >
          {sending && <Loader2 size={14} className="animate-spin" />}
          {t('league.reportConfirm')}
        </button>
        <button onClick={onCancel} disabled={sending} className="px-3 py-1.5 text-sm rounded-lg bg-gray-700 hover:bg-gray-600 text-gray-100 cursor-pointer">
          {t('league.cancel')}
        </button>
      </div>
      {error && <p role="alert" className="text-sm text-red-300">{error}</p>}
    </div>
  )
}

function LeagueSetup({ username, configured, errorText, onCancel, onSaved }: {
  username: string
  configured: boolean
  errorText: (err: ApiError, fallback: string) => string
  onCancel?: () => void
  onSaved: () => void
}) {
  const { t } = useTranslation('wordfeud')
  const [name, setName] = useState(username)
  const [email, setEmail] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const save = async (body: { username: string; email: string }) => {
    setSaving(true)
    setError(null)
    try {
      const res = await fetch('/api/wordfeud/league/config', {
        method: 'PUT',
        credentials: 'include',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        setError(errorText(data, t('league.errors.saveFailed')))
        return
      }
      onSaved()
    } catch {
      setError(t('league.errors.saveFailed'))
    } finally {
      setSaving(false)
    }
  }

  const inputClass = 'w-full px-3 py-2 rounded-lg bg-gray-950 border border-gray-700 text-sm'

  return (
    <form
      className="max-w-md space-y-4"
      onSubmit={(e) => {
        e.preventDefault()
        void save({ username: name.trim(), email: email.trim() })
      }}
    >
      <div>
        <h2 className="text-lg font-semibold">{t('league.setup.title')}</h2>
        <p className="text-sm text-gray-400 mt-1">{t('league.setup.intro')}</p>
      </div>
      <label className="block text-sm text-gray-300">
        <span className="block mb-1">{t('league.setup.username')}</span>
        <input value={name} onChange={(e) => setName(e.target.value)} required maxLength={60} autoComplete="off" className={inputClass} />
      </label>
      <label className="block text-sm text-gray-300">
        <span className="block mb-1">{t('league.setup.email')}</span>
        <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} maxLength={200} autoComplete="email" className={inputClass} />
        <span className="block mt-1 text-xs text-gray-500">
          {configured ? t('league.setup.emailHintEdit') : t('league.setup.emailHint')}
        </span>
      </label>
      {error && <p role="alert" className="text-sm text-red-300">{error}</p>}
      <div className="flex flex-wrap items-center gap-2">
        <button
          type="submit"
          disabled={saving || name.trim() === ''}
          className="flex items-center gap-1.5 px-4 py-2 text-sm rounded-lg bg-blue-600 hover:bg-blue-500 text-white cursor-pointer disabled:opacity-50"
        >
          {saving && <Loader2 size={14} className="animate-spin" />}
          {t('league.setup.save')}
        </button>
        {onCancel && (
          <button type="button" onClick={onCancel} disabled={saving} className="px-4 py-2 text-sm rounded-lg bg-gray-800 hover:bg-gray-700 text-gray-100 cursor-pointer">
            {t('league.cancel')}
          </button>
        )}
        {configured && (
          <button
            type="button"
            onClick={() => void save({ username: '', email: '' })}
            disabled={saving}
            className="ml-auto px-3 py-2 text-sm text-red-300 hover:text-red-200 cursor-pointer"
          >
            {t('league.setup.remove')}
          </button>
        )}
      </div>
    </form>
  )
}
