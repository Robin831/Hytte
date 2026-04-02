import { useState, useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { Plus, Trash2, Undo2, ChevronRight, Trophy, Users } from 'lucide-react'

interface LocalGameSummary {
  id: number
  player1: string
  player2: string
  score1: number
  score2: number
  current_turn: number
  status: string
  move_count: number
  created_at: string
  updated_at: string
}

interface LocalGame {
  id: number
  player1: string
  player2: string
  score1: number
  score2: number
  current_turn: number
  board: string
  rack1: string
  rack2: string
  status: string
  created_at: string
  updated_at: string
}

interface LocalMove {
  id: number
  game_id: number
  move_number: number
  player_turn: number
  word: string
  position: string
  direction: string
  score: number
  move_type: string
  created_at: string
}

type View = 'list' | 'game'

export default function WordfeudLocalGames() {
  const { t, i18n } = useTranslation('wordfeud')

  const [view, setView] = useState<View>('list')
  const [games, setGames] = useState<LocalGameSummary[]>([])
  const [selectedGame, setSelectedGame] = useState<LocalGame | null>(null)
  const [moves, setMoves] = useState<LocalMove[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // New game form
  const [showNewForm, setShowNewForm] = useState(false)
  const [newPlayer1, setNewPlayer1] = useState('')
  const [newPlayer2, setNewPlayer2] = useState('')
  const [creating, setCreating] = useState(false)

  const controllerRef = useRef<AbortController | null>(null)
  const [refreshKey, setRefreshKey] = useState(0)

  useEffect(() => {
    const controller = new AbortController()
    controllerRef.current = controller

    const load = async () => {
      setLoading(true)
      setError(null)
      try {
        const res = await fetch('/api/wordfeud/local-games', {
          credentials: 'include',
          signal: controller.signal,
        })
        if (!res.ok) {
          const data = await res.json().catch(() => ({ error: 'unknown' }))
          throw new Error(data.error || t('localGames.errors.failedToLoad'))
        }
        const data = await res.json()
        if (!controller.signal.aborted) {
          setGames(data.games ?? [])
        }
      } catch (err) {
        if (err instanceof DOMException && err.name === 'AbortError') return
        if (!controller.signal.aborted) {
          setError(err instanceof Error ? err.message : t('localGames.errors.failedToLoad'))
        }
      } finally {
        if (!controller.signal.aborted) {
          setLoading(false)
        }
      }
    }

    load()
    return () => { controller.abort() }
  }, [t, refreshKey])

  const handleCreateGame = async () => {
    if (!newPlayer1.trim() || !newPlayer2.trim()) return
    setCreating(true)
    setError(null)
    try {
      const res = await fetch('/api/wordfeud/local-games', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ player1: newPlayer1.trim(), player2: newPlayer2.trim() }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({ error: 'unknown' }))
        throw new Error(data.error || t('localGames.errors.failedToCreate'))
      }
      setShowNewForm(false)
      setNewPlayer1('')
      setNewPlayer2('')
      setRefreshKey(k => k + 1)
    } catch (err) {
      setError(err instanceof Error ? err.message : t('localGames.errors.failedToCreate'))
    } finally {
      setCreating(false)
    }
  }

  const handleDeleteGame = async (gameId: number, e: React.MouseEvent) => {
    e.stopPropagation()
    try {
      const res = await fetch(`/api/wordfeud/local-games/${gameId}`, {
        method: 'DELETE',
        credentials: 'include',
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({ error: 'unknown' }))
        throw new Error(data.error || t('localGames.errors.failedToDelete'))
      }
      setRefreshKey(k => k + 1)
    } catch (err) {
      setError(err instanceof Error ? err.message : t('localGames.errors.failedToDelete'))
    }
  }

  const handleSelectGame = async (gameId: number) => {
    setLoading(true)
    setError(null)
    try {
      const res = await fetch(`/api/wordfeud/local-games/${gameId}`, {
        credentials: 'include',
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({ error: 'unknown' }))
        throw new Error(data.error || t('localGames.errors.failedToLoad'))
      }
      const data = await res.json()
      setSelectedGame(data.game)
      setMoves(data.moves ?? [])
      setView('game')
    } catch (err) {
      setError(err instanceof Error ? err.message : t('localGames.errors.failedToLoad'))
    } finally {
      setLoading(false)
    }
  }

  const handleRecordMove = async (move: {
    word: string
    position: string
    direction: string
    score: number
    move_type: string
  }) => {
    if (!selectedGame) return
    setError(null)
    try {
      const moveNumber = moves.length + 1

      // Record the move with a snapshot of the current state.
      const res = await fetch(`/api/wordfeud/local-games/${selectedGame.id}/moves`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({
          move_number: moveNumber,
          player_turn: selectedGame.current_turn,
          word: move.word,
          position: move.position,
          direction: move.direction,
          score: move.score,
          move_type: move.move_type,
          board_before: selectedGame.board,
          score1_before: selectedGame.score1,
          score2_before: selectedGame.score2,
          rack1_before: selectedGame.rack1,
          rack2_before: selectedGame.rack2,
        }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({ error: 'unknown' }))
        throw new Error(data.error || t('localGames.errors.failedToRecordMove'))
      }

      // Update game state: add score to current player, switch turns.
      const newScore1 = selectedGame.current_turn === 1
        ? selectedGame.score1 + move.score
        : selectedGame.score1
      const newScore2 = selectedGame.current_turn === 2
        ? selectedGame.score2 + move.score
        : selectedGame.score2
      const newTurn = selectedGame.current_turn === 1 ? 2 : 1

      const updateRes = await fetch(`/api/wordfeud/local-games/${selectedGame.id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({
          score1: newScore1,
          score2: newScore2,
          current_turn: newTurn,
        }),
      })
      if (!updateRes.ok) {
        const data = await updateRes.json().catch(() => ({ error: 'unknown' }))
        throw new Error(data.error || t('localGames.errors.failedToUpdateGame'))
      }

      // Refresh game state.
      await handleSelectGame(selectedGame.id)
    } catch (err) {
      setError(err instanceof Error ? err.message : t('localGames.errors.failedToRecordMove'))
    }
  }

  const handleUndo = async () => {
    if (!selectedGame) return
    setError(null)
    try {
      const res = await fetch(`/api/wordfeud/local-games/${selectedGame.id}/undo`, {
        method: 'POST',
        credentials: 'include',
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({ error: 'unknown' }))
        throw new Error(data.error || t('localGames.errors.failedToUndo'))
      }
      // Refresh game state.
      await handleSelectGame(selectedGame.id)
    } catch (err) {
      setError(err instanceof Error ? err.message : t('localGames.errors.failedToUndo'))
    }
  }

  const handleFinishGame = async () => {
    if (!selectedGame) return
    setError(null)
    try {
      const res = await fetch(`/api/wordfeud/local-games/${selectedGame.id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ status: 'finished' }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({ error: 'unknown' }))
        throw new Error(data.error || t('localGames.errors.failedToUpdateGame'))
      }
      await handleSelectGame(selectedGame.id)
    } catch (err) {
      setError(err instanceof Error ? err.message : t('localGames.errors.failedToUpdateGame'))
    }
  }

  const handleBackToList = () => {
    setView('list')
    setSelectedGame(null)
    setMoves([])
    setRefreshKey(k => k + 1)
  }

  if (view === 'game' && selectedGame) {
    return (
      <GameView
        game={selectedGame}
        moves={moves}
        error={error}
        onRecordMove={handleRecordMove}
        onUndo={handleUndo}
        onFinish={handleFinishGame}
        onBack={handleBackToList}
      />
    )
  }

  return (
    <div>
      {error && (
        <div className="bg-red-900/50 border border-red-700 text-red-200 rounded-lg p-3 mb-4 text-sm">
          {error}
        </div>
      )}

      <div className="flex items-center justify-between mb-4">
        <h2 className="text-lg font-semibold text-gray-200">{t('localGames.title')}</h2>
        <button
          onClick={() => setShowNewForm(!showNewForm)}
          className="flex items-center gap-2 px-3 py-2 bg-blue-600 hover:bg-blue-500 rounded-lg text-sm font-medium transition-colors cursor-pointer"
        >
          <Plus size={16} />
          {t('localGames.newGame')}
        </button>
      </div>

      {/* New game form */}
      {showNewForm && (
        <div className="bg-gray-800 rounded-lg p-4 mb-4 border border-gray-700">
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 mb-3">
            <div>
              <label htmlFor="new-player1" className="block text-sm text-gray-400 mb-1">
                {t('localGames.player1')}
              </label>
              <input
                id="new-player1"
                type="text"
                value={newPlayer1}
                onChange={e => setNewPlayer1(e.target.value)}
                maxLength={50}
                placeholder={t('localGames.playerNamePlaceholder')}
                className="w-full bg-gray-900 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500"
              />
            </div>
            <div>
              <label htmlFor="new-player2" className="block text-sm text-gray-400 mb-1">
                {t('localGames.player2')}
              </label>
              <input
                id="new-player2"
                type="text"
                value={newPlayer2}
                onChange={e => setNewPlayer2(e.target.value)}
                maxLength={50}
                placeholder={t('localGames.playerNamePlaceholder')}
                className="w-full bg-gray-900 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500"
              />
            </div>
          </div>
          <div className="flex gap-2">
            <button
              onClick={handleCreateGame}
              disabled={creating || !newPlayer1.trim() || !newPlayer2.trim()}
              className="px-4 py-2 bg-green-600 hover:bg-green-500 disabled:bg-gray-700 disabled:text-gray-500 rounded-lg text-sm font-medium transition-colors cursor-pointer"
            >
              {creating ? t('localGames.creating') : t('localGames.create')}
            </button>
            <button
              onClick={() => { setShowNewForm(false); setNewPlayer1(''); setNewPlayer2('') }}
              className="px-4 py-2 bg-gray-700 hover:bg-gray-600 rounded-lg text-sm font-medium transition-colors cursor-pointer"
            >
              {t('localGames.cancel')}
            </button>
          </div>
        </div>
      )}

      {/* Loading */}
      {loading && (
        <div className="flex items-center justify-center h-24">
          <div className="animate-spin rounded-full h-8 w-8 border-2 border-gray-600 border-t-blue-500" />
        </div>
      )}

      {/* Games list */}
      {!loading && games.length === 0 && (
        <div className="bg-gray-800/50 rounded-lg p-8 text-center">
          <Users size={32} className="mx-auto mb-3 text-gray-600" />
          <p className="text-gray-400 text-sm">{t('localGames.noGames')}</p>
        </div>
      )}

      {!loading && games.length > 0 && (
        <div className="space-y-2">
          {games.map(game => (
            <div
              key={game.id}
              onClick={() => handleSelectGame(game.id)}
              className="bg-gray-800 hover:bg-gray-750 border border-gray-700 rounded-lg p-4 cursor-pointer transition-colors group"
            >
              <div className="flex items-center justify-between">
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-3 mb-1">
                    <span className="font-medium text-white">{game.player1}</span>
                    <span className="text-lg font-bold text-blue-400 tabular-nums">{game.score1}</span>
                    <span className="text-gray-600">-</span>
                    <span className="text-lg font-bold text-blue-400 tabular-nums">{game.score2}</span>
                    <span className="font-medium text-white">{game.player2}</span>
                  </div>
                  <div className="flex items-center gap-3 text-xs text-gray-500">
                    {game.status === 'active' ? (
                      <span className="text-green-400">
                        {game.current_turn === 1 ? game.player1 : game.player2}{' '}
                        {t('localGames.turn')}
                      </span>
                    ) : (
                      <span className="flex items-center gap-1 text-amber-400">
                        <Trophy size={12} />
                        {t('localGames.finished')}
                      </span>
                    )}
                    <span>{t('localGames.moveCount', { count: game.move_count })}</span>
                    <span>{new Intl.DateTimeFormat(i18n.language, { dateStyle: 'medium' }).format(new Date(game.updated_at))}</span>
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  <button
                    onClick={(e) => handleDeleteGame(game.id, e)}
                    className="p-2 text-gray-500 hover:text-red-400 transition-colors opacity-0 group-hover:opacity-100 cursor-pointer"
                    aria-label={t('localGames.delete')}
                  >
                    <Trash2 size={16} />
                  </button>
                  <ChevronRight size={16} className="text-gray-600" />
                </div>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

// Move recording form state
interface MoveFormData {
  word: string
  score: string
  move_type: string
}

function GameView({
  game,
  moves,
  error,
  onRecordMove,
  onUndo,
  onFinish,
  onBack,
}: {
  game: LocalGame
  moves: LocalMove[]
  error: string | null
  onRecordMove: (move: { word: string; position: string; direction: string; score: number; move_type: string }) => Promise<void>
  onUndo: () => Promise<void>
  onFinish: () => Promise<void>
  onBack: () => void
}) {
  const { t } = useTranslation('wordfeud')
  const [moveForm, setMoveForm] = useState<MoveFormData>({ word: '', score: '', move_type: 'move' })
  const [submitting, setSubmitting] = useState(false)

  const currentPlayer = game.current_turn === 1 ? game.player1 : game.player2

  const handleSubmitMove = async () => {
    const scoreNum = parseInt(moveForm.score, 10)
    if (moveForm.move_type === 'move' && (!moveForm.word.trim() || isNaN(scoreNum))) return
    if (moveForm.move_type !== 'move' && isNaN(scoreNum)) {
      // Pass/swap scores 0
    }

    setSubmitting(true)
    try {
      await onRecordMove({
        word: moveForm.word.trim().toUpperCase(),
        position: '',
        direction: '',
        score: isNaN(scoreNum) ? 0 : scoreNum,
        move_type: moveForm.move_type,
      })
      setMoveForm({ word: '', score: '', move_type: 'move' })
    } finally {
      setSubmitting(false)
    }
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      handleSubmitMove()
    }
  }

  return (
    <div>
      {/* Back button */}
      <button
        onClick={onBack}
        className="flex items-center gap-1 text-sm text-gray-400 hover:text-white mb-4 transition-colors cursor-pointer"
      >
        <ChevronRight size={16} className="rotate-180" />
        {t('localGames.backToList')}
      </button>

      {error && (
        <div className="bg-red-900/50 border border-red-700 text-red-200 rounded-lg p-3 mb-4 text-sm">
          {error}
        </div>
      )}

      {/* Scoreboard */}
      <div className="bg-gray-800 rounded-lg p-4 mb-4 border border-gray-700">
        <div className="flex items-center justify-center gap-6">
          <div className={`text-center px-4 py-2 rounded-lg ${
            game.current_turn === 1 && game.status === 'active'
              ? 'bg-blue-900/50 border border-blue-700'
              : 'bg-gray-900'
          }`}>
            <div className="text-sm text-gray-400 mb-1">{game.player1}</div>
            <div className="text-3xl font-bold text-white tabular-nums">{game.score1}</div>
            {game.current_turn === 1 && game.status === 'active' && (
              <div className="text-xs text-blue-400 mt-1">{t('localGames.currentTurn')}</div>
            )}
          </div>
          <span className="text-2xl text-gray-600 font-light">-</span>
          <div className={`text-center px-4 py-2 rounded-lg ${
            game.current_turn === 2 && game.status === 'active'
              ? 'bg-blue-900/50 border border-blue-700'
              : 'bg-gray-900'
          }`}>
            <div className="text-sm text-gray-400 mb-1">{game.player2}</div>
            <div className="text-3xl font-bold text-white tabular-nums">{game.score2}</div>
            {game.current_turn === 2 && game.status === 'active' && (
              <div className="text-xs text-blue-400 mt-1">{t('localGames.currentTurn')}</div>
            )}
          </div>
        </div>
        {game.status === 'finished' && (
          <div className="text-center mt-3 text-amber-400 text-sm font-medium flex items-center justify-center gap-1">
            <Trophy size={14} />
            {t('localGames.gameFinished')}
          </div>
        )}
      </div>

      {/* Record move form (only for active games) */}
      {game.status === 'active' && (
        <div className="bg-gray-800 rounded-lg p-4 mb-4 border border-gray-700">
          <h3 className="text-sm font-medium text-gray-300 mb-3">
            {t('localGames.recordMove', { player: currentPlayer })}
          </h3>

          {/* Move type selector */}
          <div className="flex gap-2 mb-3">
            {(['move', 'pass', 'swap'] as const).map(mt => (
              <button
                key={mt}
                onClick={() => setMoveForm(f => ({ ...f, move_type: mt }))}
                className={`px-3 py-1.5 text-xs rounded-lg transition-colors cursor-pointer ${
                  moveForm.move_type === mt
                    ? 'bg-blue-600 text-white'
                    : 'bg-gray-700 text-gray-300 hover:bg-gray-600'
                }`}
              >
                {t(`localGames.moveTypes.${mt}`)}
              </button>
            ))}
          </div>

          <div className="grid grid-cols-1 sm:grid-cols-[1fr_auto_auto] gap-2">
            {moveForm.move_type === 'move' && (
              <input
                type="text"
                value={moveForm.word}
                onChange={e => setMoveForm(f => ({ ...f, word: e.target.value }))}
                onKeyDown={handleKeyDown}
                placeholder={t('localGames.wordPlaceholder')}
                aria-label={t('localGames.wordLabel')}
                className="bg-gray-900 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500 uppercase tracking-wider font-mono"
              />
            )}
            {moveForm.move_type !== 'pass' && (
              <input
                type="number"
                value={moveForm.score}
                onChange={e => setMoveForm(f => ({ ...f, score: e.target.value }))}
                onKeyDown={handleKeyDown}
                placeholder={t('localGames.scorePlaceholder')}
                aria-label={t('localGames.scoreLabel')}
                min={0}
                className="bg-gray-900 border border-gray-600 rounded-lg px-3 py-2 text-sm text-white placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500 w-24 tabular-nums"
              />
            )}
            <button
              onClick={handleSubmitMove}
              disabled={submitting || (moveForm.move_type === 'move' && (!moveForm.word.trim() || !moveForm.score))}
              className="px-4 py-2 bg-green-600 hover:bg-green-500 disabled:bg-gray-700 disabled:text-gray-500 rounded-lg text-sm font-medium transition-colors cursor-pointer whitespace-nowrap"
            >
              {submitting ? '...' : t('localGames.submitMove')}
            </button>
          </div>
        </div>
      )}

      {/* Actions */}
      <div className="flex gap-2 mb-4">
        {moves.length > 0 && game.status === 'active' && (
          <button
            onClick={onUndo}
            className="flex items-center gap-1.5 px-3 py-2 bg-gray-700 hover:bg-gray-600 rounded-lg text-sm transition-colors cursor-pointer"
          >
            <Undo2 size={14} />
            {t('localGames.undo')}
          </button>
        )}
        {game.status === 'active' && (
          <button
            onClick={onFinish}
            className="flex items-center gap-1.5 px-3 py-2 bg-amber-700 hover:bg-amber-600 rounded-lg text-sm transition-colors cursor-pointer"
          >
            <Trophy size={14} />
            {t('localGames.finishGame')}
          </button>
        )}
      </div>

      {/* Move history */}
      <div>
        <h3 className="text-sm font-medium text-gray-300 mb-2">
          {t('localGames.moveHistory')} ({moves.length})
        </h3>
        {moves.length === 0 ? (
          <p className="text-gray-500 text-sm">{t('localGames.noMoves')}</p>
        ) : (
          <div className="bg-gray-800/50 rounded-lg border border-gray-700 overflow-hidden">
            <div className="grid grid-cols-[auto_1fr_auto_auto] gap-3 px-4 py-2 bg-gray-800 border-b border-gray-700 text-xs font-medium text-gray-400 uppercase tracking-wide">
              <span>#</span>
              <span>{t('localGames.colPlayer')}</span>
              <span>{t('localGames.colWord')}</span>
              <span className="text-right">{t('localGames.colScore')}</span>
            </div>
            <div className="max-h-[40vh] overflow-y-auto">
              {moves.map((mv, i) => {
                const playerName = mv.player_turn === 1 ? game.player1 : game.player2
                return (
                  <div
                    key={mv.id}
                    className={`grid grid-cols-[auto_1fr_auto_auto] gap-3 px-4 py-2 text-sm ${
                      i % 2 === 0 ? 'bg-gray-800/30' : ''
                    }`}
                  >
                    <span className="text-gray-500 tabular-nums w-6">{mv.move_number}</span>
                    <span className="text-gray-300">{playerName}</span>
                    <span className="font-mono tracking-wider text-white">
                      {mv.move_type === 'move' ? mv.word : (
                        <span className="text-gray-500 italic">{t(`localGames.moveTypes.${mv.move_type}`)}</span>
                      )}
                    </span>
                    <span className="text-right text-amber-400 font-medium tabular-nums w-10">
                      {mv.move_type !== 'pass' ? `+${mv.score}` : '-'}
                    </span>
                  </div>
                )
              })}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
