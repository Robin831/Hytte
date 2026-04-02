import { useState, useEffect, useCallback, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../auth'
import { Settings, Search, Gamepad2, Grid3X3, Trophy, ChevronDown, ChevronRight } from 'lucide-react'
import { useNavigate } from 'react-router-dom'
import WordfeudBoard from './WordfeudBoard'

interface Tile {
  letter: string
  value: number
  is_wild?: boolean
}

interface GameSummary {
  id: number
  opponent: string
  scores: [number, number]
  is_my_turn: boolean
  last_move: {
    user_id: number
    move_type: string
    points: number
  }
}

interface Player {
  username: string
  id: number
  score: number
  is_my_turn?: boolean
}

interface GameState {
  id: number
  board: (Tile | null)[][]
  rack: Tile[]
  players: [Player, Player]
  is_my_turn: boolean
  is_running: boolean
}

interface FoundWord {
  word: string
  score: number
  blank_positions?: number[]
}

// Reference mapping for Wordfeud bonus square types.
// Currently unused because getBonusType() always returns 0 (normal)
// and the API does not expose the randomized board layouts.
// 0=normal, 1=DL, 2=TL, 3=DW, 4=TW, 5=center star
const BONUS_TYPES = ['', 'DL', 'TL', 'DW', 'TW', '*'] as const

function bonusClass(type: number): string {
  switch (type) {
    case 1: return 'bg-sky-800 text-sky-300'       // DL
    case 2: return 'bg-emerald-800 text-emerald-300' // TL
    case 3: return 'bg-rose-900 text-rose-300'      // DW
    case 4: return 'bg-amber-800 text-amber-300'    // TW
    default: return 'bg-gray-800'
  }
}

type SearchMode = 'anagram' | 'starts_with' | 'ends_with' | 'contains'

const TAB_KEY = 'wordfeud-tab'

export default function WordfeudPage() {
  const { t } = useTranslation('wordfeud')

  const [activeTab, setActiveTab] = useState<'finder' | 'board' | 'games'>(() => {
    const stored = localStorage.getItem(TAB_KEY)
    // Migrate legacy 'mygames' value to 'games'
    if (stored === 'mygames') {
      localStorage.setItem(TAB_KEY, 'games')
      return 'games'
    }
    const valid = ['finder', 'board', 'games'] as const
    return (valid as readonly string[]).includes(stored ?? '') ? (stored as 'finder' | 'board' | 'games') : 'finder'
  })

  const handleTabChange = (tab: 'finder' | 'board' | 'games') => {
    setActiveTab(tab)
    localStorage.setItem(TAB_KEY, tab)
  }

  return (
    <div className="max-w-6xl mx-auto p-4 md:p-8">
      <h1 className="text-2xl font-bold mb-4">{t('title')}</h1>

      {/* Tabs */}
      <div role="tablist" className="flex gap-1 mb-6 border-b border-gray-700">
        <button
          role="tab"
          id="tab-finder"
          aria-selected={activeTab === 'finder'}
          aria-controls="tabpanel-finder"
          onClick={() => handleTabChange('finder')}
          className={`flex items-center gap-2 px-4 py-2.5 text-sm font-medium border-b-2 transition-colors cursor-pointer ${
            activeTab === 'finder'
              ? 'border-blue-500 text-white'
              : 'border-transparent text-gray-400 hover:text-gray-200'
          }`}
        >
          <Search size={16} />
          {t('finder.tab')}
        </button>
        <button
          role="tab"
          id="tab-board"
          aria-selected={activeTab === 'board'}
          aria-controls="tabpanel-board"
          onClick={() => handleTabChange('board')}
          className={`flex items-center gap-2 px-4 py-2.5 text-sm font-medium border-b-2 transition-colors cursor-pointer ${
            activeTab === 'board'
              ? 'border-blue-500 text-white'
              : 'border-transparent text-gray-400 hover:text-gray-200'
          }`}
        >
          <Grid3X3 size={16} />
          {t('board.tab')}
        </button>
        <button
          role="tab"
          id="tab-games"
          aria-selected={activeTab === 'games'}
          aria-controls="tabpanel-games"
          onClick={() => handleTabChange('games')}
          className={`flex items-center gap-2 px-4 py-2.5 text-sm font-medium border-b-2 transition-colors cursor-pointer ${
            activeTab === 'games'
              ? 'border-blue-500 text-white'
              : 'border-transparent text-gray-400 hover:text-gray-200'
          }`}
        >
          <Gamepad2 size={16} />
          {t('games.tab')}
        </button>
      </div>

      <div role="tabpanel" id="tabpanel-finder" aria-labelledby="tab-finder" hidden={activeTab !== 'finder'}>
        {activeTab === 'finder' && <WordFinder />}
      </div>
      <div role="tabpanel" id="tabpanel-board" aria-labelledby="tab-board" hidden={activeTab !== 'board'}>
        {activeTab === 'board' && <WordfeudBoard />}
      </div>
      <div role="tabpanel" id="tabpanel-games" aria-labelledby="tab-games" hidden={activeTab !== 'games'}>
        {activeTab === 'games' && <GamesTab />}
      </div>
    </div>
  )
}

function WordFinder() {
  const { t } = useTranslation('wordfeud')

  const [letters, setLetters] = useState('')
  const [pattern, setPattern] = useState('')
  const [mode, setMode] = useState<SearchMode>('anagram')
  const [results, setResults] = useState<FoundWord[]>([])
  const [totalMatches, setTotalMatches] = useState(0)
  const [searching, setSearching] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [hasSearched, setHasSearched] = useState(false)
  const controllerRef = useRef<AbortController | null>(null)

  const handleSearch = useCallback(async () => {
    const trimmedLetters = letters.trim().toUpperCase()
    const trimmedPattern = pattern.trim().toUpperCase()

    if (mode === 'anagram') {
      if (!trimmedLetters || trimmedLetters.length > 7) return
    } else {
      if (!trimmedPattern || trimmedPattern.length > 15) return
    }

    controllerRef.current?.abort()
    const controller = new AbortController()
    controllerRef.current = controller

    setSearching(true)
    setError(null)
    setHasSearched(true)

    try {
      let url: string
      let body: string

      if (mode === 'anagram') {
        url = '/api/wordfeud/find'
        body = JSON.stringify({ letters: trimmedLetters })
      } else {
        url = '/api/wordfeud/search'
        body = JSON.stringify({
          pattern: trimmedPattern,
          mode,
          letters: trimmedLetters || undefined,
        })
      }

      const res = await fetch(url, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body,
        signal: controller.signal,
      })

      if (!res.ok) {
        const data = await res.json().catch(() => ({ error: 'unknown' }))
        throw new Error(data.error || t('finder.errors.searchFailed'))
      }

      const data = await res.json()
      if (!controller.signal.aborted) {
        setResults(data.words ?? [])
        setTotalMatches(data.total ?? 0)
      }
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') return
      if (!controller.signal.aborted) {
        setError(err instanceof Error ? err.message : t('finder.errors.searchFailed'))
        setResults([])
        setTotalMatches(0)
      }
    } finally {
      if (!controller.signal.aborted) {
        setSearching(false)
      }
    }
  }, [letters, pattern, mode, t])

  useEffect(() => {
    return () => { controllerRef.current?.abort() }
  }, [])

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      handleSearch()
    }
  }

  const handleLetterInput = (value: string) => {
    // Allow letters, Norwegian characters, and * for blanks
    const filtered = value.toUpperCase().replace(/[^A-ZÆØÅ*]/g, '')
    if (filtered.length <= 7) {
      setLetters(filtered)
    }
  }

  const handlePatternInput = (value: string) => {
    // Pattern: letters only, no blanks
    const filtered = value.toUpperCase().replace(/[^A-ZÆØÅ]/g, '')
    if (filtered.length <= 15) {
      setPattern(filtered)
    }
  }

  const modes: { value: SearchMode; label: string }[] = [
    { value: 'anagram', label: t('finder.modeAnagram') },
    { value: 'contains', label: t('finder.modeContains') },
    { value: 'starts_with', label: t('finder.modeStartsWith') },
    { value: 'ends_with', label: t('finder.modeEndsWith') },
  ]

  return (
    <div>
      {/* Search mode selector */}
      <div className="flex flex-wrap gap-2 mb-4">
        {modes.map(m => (
          <button
            key={m.value}
            onClick={() => {
              controllerRef.current?.abort()
              controllerRef.current = null
              setSearching(false)
              setMode(m.value)
              setResults([])
              setHasSearched(false)
              setError(null)
            }}
            className={`px-3 py-1.5 text-sm rounded-lg transition-colors cursor-pointer ${
              mode === m.value
                ? 'bg-blue-600 text-white'
                : 'bg-gray-800 text-gray-300 hover:bg-gray-700'
            }`}
          >
            {m.label}
          </button>
        ))}
      </div>

      {/* Input fields */}
      <div className="flex flex-col gap-2 mb-4">
        {/* Rack letters input — always visible */}
        <div className="flex gap-2">
          <div className="flex-1 relative">
            <input
              type="text"
              value={letters}
              onChange={e => handleLetterInput(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder={t('finder.placeholderAnagram')}
              maxLength={7}
              aria-label={t('finder.rackLabel')}
              className="w-full bg-gray-800 border border-gray-700 rounded-lg px-3 py-2.5 text-sm text-white placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500 uppercase tracking-wider font-mono"
            />
            <span className="absolute right-3 top-1/2 -translate-y-1/2 text-xs text-gray-500">
              {letters.length}/7
            </span>
          </div>
          {mode === 'anagram' && (
            <button
              onClick={handleSearch}
              disabled={searching || !letters.trim()}
              className="px-4 py-2.5 bg-blue-600 hover:bg-blue-500 disabled:bg-gray-700 disabled:text-gray-500 rounded-lg text-sm font-medium transition-colors cursor-pointer flex items-center gap-2"
            >
              <Search size={16} />
              <span className="hidden sm:inline">{t('finder.search')}</span>
            </button>
          )}
        </div>

        {/* Pattern input — only for non-anagram modes */}
        {mode !== 'anagram' && (
          <div className="flex gap-2">
            <div className="flex-1 relative">
              <input
                type="text"
                value={pattern}
                onChange={e => handlePatternInput(e.target.value)}
                onKeyDown={handleKeyDown}
                placeholder={t('finder.placeholderPattern')}
                maxLength={15}
                aria-label={t('finder.patternLabel')}
                className="w-full bg-gray-800 border border-gray-700 rounded-lg px-3 py-2.5 text-sm text-white placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500 uppercase tracking-wider font-mono"
              />
              <span className="absolute right-3 top-1/2 -translate-y-1/2 text-xs text-gray-500">
                {pattern.length}/15
              </span>
            </div>
            <button
              onClick={handleSearch}
              disabled={searching || !pattern.trim()}
              className="px-4 py-2.5 bg-blue-600 hover:bg-blue-500 disabled:bg-gray-700 disabled:text-gray-500 rounded-lg text-sm font-medium transition-colors cursor-pointer flex items-center gap-2"
            >
              <Search size={16} />
              <span className="hidden sm:inline">{t('finder.search')}</span>
            </button>
          </div>
        )}
      </div>

      {/* Help text */}
      <p className="text-xs text-gray-500 mb-4">
        {mode === 'anagram' ? t('finder.blankHint') : t('finder.patternHint')}
      </p>

      {/* Error */}
      {error && (
        <div className="bg-red-900/50 border border-red-700 text-red-200 rounded-lg p-3 mb-4 text-sm">
          {error}
        </div>
      )}

      {/* Loading */}
      {searching && (
        <div className="flex items-center justify-center h-24">
          <div className="animate-spin rounded-full h-8 w-8 border-2 border-gray-600 border-t-blue-500" />
        </div>
      )}

      {/* Results */}
      {!searching && hasSearched && (
        <div>
          <div className="flex items-center justify-between mb-3">
            <p className="text-sm text-gray-400">
              {t('finder.resultsCount', { count: totalMatches })}
            </p>
          </div>

          {results.length > 0 ? (
            <div className="bg-gray-800/50 rounded-lg border border-gray-700 overflow-hidden">
              {/* Header */}
              <div className="grid grid-cols-[1fr_auto_auto] gap-4 px-4 py-2.5 bg-gray-800 border-b border-gray-700 text-xs font-medium text-gray-400 uppercase tracking-wide">
                <span>{t('finder.colWord')}</span>
                <span className="text-right w-12">{t('finder.colLength')}</span>
                <span className="text-right w-14">{t('finder.colPoints')}</span>
              </div>

              {/* Rows */}
              <div className="max-h-[60vh] overflow-y-auto">
                {results.map((result, i) => (
                  <div
                    key={`${result.word}-${i}`}
                    className={`grid grid-cols-[1fr_auto_auto] gap-4 px-4 py-2 text-sm ${
                      i % 2 === 0 ? 'bg-gray-800/30' : ''
                    } hover:bg-gray-700/50 transition-colors`}
                  >
                    <span className="font-mono tracking-wider text-white">
                      {renderWord(result)}
                    </span>
                    <span className="text-right w-12 text-gray-400 tabular-nums">
                      {[...result.word].length}
                    </span>
                    <span className="text-right w-14 font-medium text-amber-400 tabular-nums">
                      {result.score}
                    </span>
                  </div>
                ))}
              </div>
            </div>
          ) : (
            <p className="text-gray-500 text-sm">{t('finder.noResults')}</p>
          )}
        </div>
      )}

      {/* Initial state */}
      {!searching && !hasSearched && (
        <p className="text-gray-500 text-sm">{t('finder.hint')}</p>
      )}
    </div>
  )
}

function renderWord(result: FoundWord): React.ReactNode {
  if (!result.blank_positions || result.blank_positions.length === 0) {
    return result.word
  }
  const blanks = new Set(result.blank_positions)
  return (
    <>
      {[...result.word].map((ch, i) => (
        <span key={i} className={blanks.has(i) ? 'text-purple-400' : ''}>
          {ch}
        </span>
      ))}
    </>
  )
}

function GamesTab() {
  const { t } = useTranslation('wordfeud')
  const { user } = useAuth()
  const navigate = useNavigate()

  const [connected, setConnected] = useState<boolean | null>(null)
  const [games, setGames] = useState<GameSummary[]>([])
  const [finishedGames, setFinishedGames] = useState<GameSummary[]>([])
  const [finishedExpanded, setFinishedExpanded] = useState(false)
  const [selectedGameId, setSelectedGameId] = useState<number | null>(null)
  const [gameState, setGameState] = useState<GameState | null>(null)
  const [loadingGames, setLoadingGames] = useState(false)
  const [loadingGame, setLoadingGame] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Fetch games list
  const gamesControllerRef = useRef<AbortController | null>(null)
  const fetchGames = useCallback(async () => {
    gamesControllerRef.current?.abort()
    const controller = new AbortController()
    gamesControllerRef.current = controller
    if (controller.signal.aborted) return
    setLoadingGames(true)
    setError(null)
    try {
      const res = await fetch('/api/wordfeud/games', { credentials: 'include', signal: controller.signal })
      if (!res.ok) {
        const data = await res.json().catch(() => ({ error: 'unknown' }))
        if (res.status === 400) {
          setConnected(false)
          setGames([])
          setFinishedGames([])
          setSelectedGameId(null)
          setGameState(null)
          return
        }
        if (res.status === 401) {
          setConnected(true)
          setError(data.error || t('errors.failedToLoadGames'))
          return
        }
        throw new Error(data.error || t('errors.failedToLoadGames'))
      }
      const data = await res.json()
      setGames(data.games ?? [])
      setFinishedGames(data.finished_games ?? [])
      setConnected(true)
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') return
      setConnected(true)
      setError(err instanceof Error ? err.message : t('errors.failedToLoadGames'))
    } finally {
      if (!controller.signal.aborted) {
        setLoadingGames(false)
      }
    }
  }, [t])

  useEffect(() => {
    if (!user) return
    ;(async () => { await fetchGames() })()
    return () => {
      gamesControllerRef.current?.abort()
    }
  }, [user, fetchGames])

  // Fetch full game state when a game is selected
  useEffect(() => {
    if (selectedGameId == null) return
    let cancelled = false
    const controller = new AbortController()
    ;(async () => {
      setGameState(null)
      setLoadingGame(true)
      setError(null)
      try {
        const res = await fetch(`/api/wordfeud/games/${selectedGameId}`, { credentials: 'include', signal: controller.signal })
        if (!res.ok) {
          let message = t('errors.failedToLoadGame')
          try {
            const d = await res.json()
            if (d && typeof d === 'object' && 'error' in d && typeof (d as { error: unknown }).error === 'string') {
              message = (d as { error: string }).error
            }
          } catch {
            // Non-JSON response body — use fallback message
          }
          if (cancelled) return
          setGameState(null)
          if (res.status === 400) {
            setConnected(false)
            setSelectedGameId(null)
            return
          }
          if (res.status === 401) {
            setError(message)
            return
          }
          throw new Error(message)
        }
        const data = await res.json()
        if (!cancelled) setGameState(data.game)
      } catch (err) {
        if (err instanceof DOMException && err.name === 'AbortError') return
        if (!cancelled) {
          setGameState(null)
          setError(err instanceof Error ? err.message : t('errors.failedToLoadGame'))
        }
      } finally {
        if (!cancelled) setLoadingGame(false)
      }
    })()
    return () => {
      cancelled = true
      controller.abort()
    }
  }, [selectedGameId, t])

  // Not connected — show prompt to configure
  if (connected === false) {
    return (
      <div className="bg-gray-800 rounded-lg p-6 text-center">
        <p className="text-gray-300 mb-4">{user?.is_admin ? t('notConnected') : t('notConnectedNonAdmin')}</p>
        {user?.is_admin && (
          <button
            onClick={() => navigate('/settings')}
            className="inline-flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-500 rounded-lg text-sm font-medium transition-colors cursor-pointer"
          >
            <Settings size={16} />
            {t('goToSettings')}
          </button>
        )}
      </div>
    )
  }

  // Still checking connection
  if (connected === null) {
    return (
      <div className="flex items-center justify-center h-32">
        <div className="animate-spin rounded-full h-8 w-8 border-2 border-gray-600 border-t-blue-500" />
      </div>
    )
  }

  const sortedGames = [...games].sort((a, b) => {
    if (a.is_my_turn !== b.is_my_turn) return a.is_my_turn ? -1 : 1
    return a.opponent.localeCompare(b.opponent)
  })

  return (
    <div>
      {error && (
        <div className="bg-red-900/50 border border-red-700 text-red-200 rounded-lg p-3 mb-4 text-sm">
          {error}
        </div>
      )}

      {/* Game selector */}
      <div className="mb-6">
        <label htmlFor="game-select" className="block text-sm font-medium text-gray-300 mb-2">
          {t('selectGame')}
        </label>
        <select
          id="game-select"
          value={selectedGameId ?? ''}
          onChange={e => setSelectedGameId(e.target.value ? Number(e.target.value) : null)}
          disabled={loadingGames}
          className="w-full max-w-md bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-blue-500 disabled:opacity-50"
        >
          <option value="">{loadingGames ? t('loading') : t('selectGamePlaceholder')}</option>
          {sortedGames.map(game => (
            <option key={game.id} value={game.id}>
              {game.is_my_turn ? '\u25B6 ' : ''}{game.opponent} ({game.scores[0]}{'\u2013'}{game.scores[1]}){game.is_my_turn ? ` \u2014 ${t('yourTurn')}` : ''}
            </option>
          ))}
        </select>
      </div>

      {/* Finished games — rendered directly under the selector so it stays visible when a game is selected */}
      {finishedGames.length > 0 && (
        <div className="mt-2 mb-6">
          <button
            onClick={() => setFinishedExpanded(prev => !prev)}
            aria-expanded={finishedExpanded}
            aria-controls="finished-games-list"
            className="flex items-center gap-1.5 text-sm font-medium text-gray-400 hover:text-gray-200 transition-colors cursor-pointer"
          >
            {finishedExpanded ? <ChevronDown size={16} /> : <ChevronRight size={16} />}
            {t('finishedGames.title')} ({finishedGames.length})
          </button>
          {finishedExpanded && (
            <div id="finished-games-list" className="mt-3 space-y-2">
              {finishedGames.map(game => {
                const myScore = game.scores[0]
                const opponentScore = game.scores[1]
                const iWon = myScore > opponentScore
                const isDraw = myScore === opponentScore
                const resultLabel = isDraw ? t('finishedGames.draw') : iWon ? t('finishedGames.won') : t('finishedGames.lost')
                return (
                  <div
                    key={game.id}
                    className="flex items-center justify-between bg-gray-800 rounded-lg px-4 py-2.5 text-sm"
                  >
                    <span className="text-gray-300">{game.opponent}</span>
                    <div className="flex items-center gap-2">
                      <span className={iWon ? 'font-bold text-green-400' : 'text-gray-400'}>
                        {myScore}
                      </span>
                      <span className="text-gray-600">&ndash;</span>
                      <span className={!iWon && !isDraw ? 'font-bold text-green-400' : 'text-gray-400'}>
                        {opponentScore}
                      </span>
                      {!isDraw && (
                        <Trophy size={14} className={iWon ? 'text-green-400' : 'text-gray-600'} aria-hidden="true" />
                      )}
                      <span className={`text-xs ${iWon ? 'text-green-400' : isDraw ? 'text-gray-500' : 'text-gray-500'}`}>
                        {resultLabel}
                      </span>
                    </div>
                  </div>
                )
              })}
            </div>
          )}
        </div>
      )}

      {/* No game selected yet but games loaded */}
      {selectedGameId == null && !loadingGames && games.length > 0 && (
        <p className="text-gray-500 text-sm">{t('selectGameHint')}</p>
      )}

      {/* No games available */}
      {!loadingGames && !error && games.length === 0 && finishedGames.length === 0 && connected && (
        <p className="text-gray-500 text-sm">{t('noGames')}</p>
      )}

      {/* Loading game state */}
      {selectedGameId != null && loadingGame && (
        <div className="flex items-center justify-center h-32">
          <div className="animate-spin rounded-full h-8 w-8 border-2 border-gray-600 border-t-blue-500" />
        </div>
      )}

      {/* Game board + rack */}
      {selectedGameId != null && gameState && !loadingGame && (
        <div>
          {/* Players and scores */}
          <div className="flex items-center gap-4 mb-4 text-sm">
            {gameState.players[0] && (
              <div
                key={gameState.players[0].id}
                className={`flex items-center gap-2 px-3 py-1.5 rounded-lg ${
                  gameState.is_my_turn
                    ? 'bg-blue-900/50 border border-blue-700 text-blue-200'
                    : 'bg-gray-800 text-gray-400'
                }`}
              >
                <span className="font-medium">{gameState.players[0].username}</span>
                <span className="text-lg font-bold">{gameState.players[0].score}</span>
              </div>
            )}
            {gameState.players.length > 1 && (
              <span className="text-gray-600 mx-1">&ndash;</span>
            )}
            {gameState.players[1] && (
              <div
                key={gameState.players[1].id}
                className={`flex items-center gap-2 px-3 py-1.5 rounded-lg ${
                  !gameState.is_my_turn
                    ? 'bg-blue-900/50 border border-blue-700 text-blue-200'
                    : 'bg-gray-800 text-gray-400'
                }`}
              >
                <span className="font-medium">{gameState.players[1].username}</span>
                <span className="text-lg font-bold">{gameState.players[1].score}</span>
              </div>
            )}
          </div>

          {/* Board */}
          <div className="inline-block border border-gray-700 rounded-lg overflow-hidden mb-6">
            <div
              className="grid"
              style={{
                gridTemplateColumns: 'repeat(15, minmax(0, 1fr))',
                gap: '1px',
                backgroundColor: '#374151',
              }}
            >
              {Array.from({ length: 15 }, (_, row) =>
                Array.from({ length: 15 }, (_, col) => {
                  const tile = gameState.board[row]?.[col]
                  const bonus = tile ? 0 : getBonusType(row, col)
                  return (
                    <div
                      key={`${row}-${col}`}
                      className={`w-7 h-7 sm:w-8 sm:h-8 flex items-center justify-center text-xs font-bold relative ${
                        tile
                          ? tile.is_wild
                            ? 'bg-purple-700 text-white'
                            : 'bg-amber-700 text-white'
                          : bonusClass(bonus)
                      }`}
                      title={tile ? `${tile.letter} (${tile.value})` : BONUS_TYPES[bonus] || undefined}
                    >
                      {tile ? (
                        <>
                          <span>{tile.letter}</span>
                          {tile.value > 0 && (
                            <span className="absolute bottom-0 right-0.5 text-[7px] leading-none opacity-70">
                              {tile.value}
                            </span>
                          )}
                        </>
                      ) : (
                        BONUS_TYPES[bonus] && (
                          <span className="text-[8px] opacity-60">{BONUS_TYPES[bonus]}</span>
                        )
                      )}
                    </div>
                  )
                })
              )}
            </div>
          </div>

          {/* Rack */}
          <div className="mb-4">
            <h3 className="text-sm font-medium text-gray-400 mb-2">{t('yourRack')}</h3>
            <div className="flex gap-1">
              {(gameState.rack ?? []).map((tile, i) => (
                <div
                  key={`${i}-${tile.letter}-${tile.value}`}
                  className={`w-10 h-10 sm:w-12 sm:h-12 flex items-center justify-center text-lg font-bold rounded relative ${
                    tile.is_wild
                      ? 'bg-purple-700 text-white'
                      : 'bg-amber-700 text-white'
                  }`}
                >
                  <span>{tile.letter || '?'}</span>
                  {tile.value > 0 && (
                    <span className="absolute bottom-0.5 right-1 text-[9px] opacity-70">
                      {tile.value}
                    </span>
                  )}
                </div>
              ))}
            </div>
          </div>
        </div>
      )}

    </div>
  )
}

/**
 * Returns the bonus type for a given board position.
 * Wordfeud uses randomized board layouts, but since the API doesn't expose
 * the bonus square positions, we don't show bonus squares for now.
 * This function returns 0 (normal) for all positions.
 * A future enhancement could fetch the board layout from the API.
 */
function getBonusType(_row: number, _col: number): number {
  return 0
}
