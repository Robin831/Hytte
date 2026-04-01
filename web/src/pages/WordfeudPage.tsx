import { useState, useEffect, useCallback, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../auth'
import { Settings } from 'lucide-react'
import { useNavigate } from 'react-router-dom'

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

// Reference mapping for Wordfeud bonus square types.
// Currently unused because getBonusType() always returns 0 (normal)
// and the API does not expose the randomized board layouts.
// 0=normal, 1=DL, 2=TL, 3=DW, 4=TW, 5=center star
const BONUS_TYPES = ['', 'DL', 'TL', 'DW', 'TW', ''] as const

function bonusClass(type: number): string {
  switch (type) {
    case 1: return 'bg-sky-800 text-sky-300'       // DL
    case 2: return 'bg-emerald-800 text-emerald-300' // TL
    case 3: return 'bg-rose-900 text-rose-300'      // DW
    case 4: return 'bg-amber-800 text-amber-300'    // TW
    default: return 'bg-gray-800'
  }
}

export default function WordfeudPage() {
  const { t } = useTranslation('wordfeud')
  const { user } = useAuth()
  const navigate = useNavigate()

  const [connected, setConnected] = useState<boolean | null>(null)
  const [games, setGames] = useState<GameSummary[]>([])
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
    setLoadingGames(true)
    setError(null)
    try {
      const res = await fetch('/api/wordfeud/games', { credentials: 'include', signal: controller.signal })
      if (!res.ok) {
        const data = await res.json().catch(() => ({ error: 'unknown' }))
        if (res.status === 400 && data.error?.includes('no Wordfeud session')) {
          setConnected(false)
          setGames([])
          setSelectedGameId(null)
          setGameState(null)
          return
        }
        throw new Error(data.error || t('errors.failedToLoadGames'))
      }
      const data = await res.json()
      setGames(data.games ?? [])
      setConnected(true)
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') return
      setError(err instanceof Error ? err.message : t('errors.failedToLoadGames'))
    } finally {
      setLoadingGames(false)
    }
  }, [t])

  // Fetch games on mount; `connected` is driven by the games response.
  // Deferred via Promise.resolve() to avoid synchronous setState in effect.
  useEffect(() => {
    if (!user) return
    Promise.resolve().then(() => fetchGames())
    return () => {
      gamesControllerRef.current?.abort()
    }
  }, [user, fetchGames])

  // Fetch full game state when a game is selected
  useEffect(() => {
    if (selectedGameId == null) return
    const controller = new AbortController()
    Promise.resolve().then(() => {
      setGameState(null)
      setLoadingGame(true)
      setError(null)
    })
    fetch(`/api/wordfeud/games/${selectedGameId}`, { credentials: 'include', signal: controller.signal })
      .then(async res => {
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
          throw new Error(message)
        }
        return res.json()
      })
      .then(data => {
        setGameState(data.game)
      })
      .catch(err => {
        if (err instanceof DOMException && err.name === 'AbortError') return
        setError(err instanceof Error ? err.message : t('errors.failedToLoadGame'))
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoadingGame(false)
      })
    return () => { controller.abort() }
  }, [selectedGameId, t])

  // Not connected — show prompt to configure
  if (connected === false) {
    return (
      <div className="max-w-4xl mx-auto p-4 md:p-8">
        <h1 className="text-2xl font-bold mb-6">{t('title')}</h1>
        <div className="bg-gray-800 rounded-lg p-6 text-center">
          <p className="text-gray-300 mb-4">{t('notConnected')}</p>
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
      </div>
    )
  }

  // Still checking connection
  if (connected === null) {
    return (
      <div className="max-w-4xl mx-auto p-4 md:p-8">
        <h1 className="text-2xl font-bold mb-6">{t('title')}</h1>
        <div className="flex items-center justify-center h-32">
          <div className="animate-spin rounded-full h-8 w-8 border-2 border-gray-600 border-t-blue-500" />
        </div>
      </div>
    )
  }

  const sortedGames = [...games].sort((a, b) => {
    // My turn first, then by opponent name
    if (a.is_my_turn !== b.is_my_turn) return a.is_my_turn ? -1 : 1
    return a.opponent.localeCompare(b.opponent)
  })

  return (
    <div className="max-w-5xl mx-auto p-4 md:p-8">
      <h1 className="text-2xl font-bold mb-6">{t('title')}</h1>

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
              {game.is_my_turn ? '\u25B6 ' : ''}{game.opponent} ({game.scores[0]}\u2013{game.scores[1]}){game.is_my_turn ? ` \u2014 ${t('yourTurn')}` : ''}
            </option>
          ))}
        </select>
      </div>

      {/* Loading game state */}
      {loadingGame && (
        <div className="flex items-center justify-center h-32">
          <div className="animate-spin rounded-full h-8 w-8 border-2 border-gray-600 border-t-blue-500" />
        </div>
      )}

      {/* Game board + rack */}
      {selectedGameId && gameState && !loadingGame && (
        <div>
          {/* Players and scores */}
          <div className="flex items-center gap-4 mb-4 text-sm">
            {gameState.players.map((player, i) => (
              <div
                key={player.id}
                className={`flex items-center gap-2 px-3 py-1.5 rounded-lg ${
                  player.is_my_turn
                    ? 'bg-blue-900/50 border border-blue-700 text-blue-200'
                    : 'bg-gray-800 text-gray-400'
                }`}
              >
                <span className="font-medium">{player.username}</span>
                <span className="text-lg font-bold">{player.score}</span>
                {i === 0 && <span className="text-gray-600 mx-1">&ndash;</span>}
              </div>
            ))}
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
              {gameState.rack.map((tile, i) => (
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

      {/* No game selected yet but games loaded */}
      {!selectedGameId && !loadingGames && games.length > 0 && (
        <p className="text-gray-500 text-sm">{t('selectGameHint')}</p>
      )}

      {/* No games available */}
      {!loadingGames && games.length === 0 && connected && (
        <p className="text-gray-500 text-sm">{t('noGames')}</p>
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
