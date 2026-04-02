import { useState, useCallback, useRef, useEffect, useLayoutEffect, useMemo, startTransition } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { Trash2, Search, Loader2, Trophy, ChevronDown, ChevronRight, RefreshCw } from 'lucide-react'
import { formatDate } from '../utils/formatDate'
import { useAuth } from '../auth'

// Scoring table: official Norwegian Wordfeud tile point values from the API
// (POST /tile_points/1/). Q, X, Z are included here for completeness but have
// 0 tiles in the Norwegian bag and will never appear in actual play.
const LETTER_VALUES: Record<string, number> = {
  A: 1, B: 4, C: 10, D: 1, E: 1, F: 2, G: 4, H: 3, I: 2, J: 4,
  K: 3, L: 2, M: 2, N: 1, O: 3, P: 4, Q: 10, R: 1, S: 1, T: 1,
  U: 4, V: 5, W: 10, X: 10, Y: 8, Z: 10, 'Æ': 8, 'Ø': 5, 'Å': 4,
}

// Norwegian Wordfeud tile bag distribution — Q, X, Z have 0 tiles and are absent
const TILE_BAG: { letter: string; count: number }[] = [
  { letter: 'A', count: 7 }, { letter: 'B', count: 3 }, { letter: 'C', count: 1 },
  { letter: 'D', count: 5 }, { letter: 'E', count: 9 }, { letter: 'F', count: 4 },
  { letter: 'G', count: 4 }, { letter: 'H', count: 3 }, { letter: 'I', count: 6 },
  { letter: 'J', count: 2 }, { letter: 'K', count: 4 }, { letter: 'L', count: 5 },
  { letter: 'M', count: 3 }, { letter: 'N', count: 6 }, { letter: 'O', count: 4 },
  { letter: 'P', count: 2 }, { letter: 'R', count: 7 },
  { letter: 'S', count: 7 }, { letter: 'T', count: 7 }, { letter: 'U', count: 3 },
  { letter: 'V', count: 3 }, { letter: 'W', count: 1 },
  { letter: 'Y', count: 1 }, { letter: 'Æ', count: 1 },
  { letter: 'Ø', count: 2 }, { letter: 'Å', count: 2 }, { letter: '*', count: 2 },
]

const TOTAL_TILES = 104

// Board cell multiplier types
// 0=normal, 1=DL, 2=TL, 3=DW, 4=TW, 5=center
type BonusType = 0 | 1 | 2 | 3 | 4 | 5

const BONUS_LABELS = ['', 'DL', 'TL', 'DW', 'TW', '\u2605'] as const

// Standard Wordfeud board layout (15x15, symmetric)
// prettier-ignore
// Standard Wordfeud board layout (board ID 0), fetched from POST /board/0/.
// 0=normal, 1=DL, 2=TL, 3=DW, 4=TW, 5=center star
const BOARD_LAYOUT: BonusType[][] = [
  [2,0,0,0,4,0,0,1,0,0,4,0,0,0,2],
  [0,1,0,0,0,2,0,0,0,2,0,0,0,1,0],
  [0,0,3,0,0,0,1,0,1,0,0,0,3,0,0],
  [0,0,0,2,0,0,0,3,0,0,0,2,0,0,0],
  [4,0,0,0,3,0,1,0,1,0,3,0,0,0,4],
  [0,2,0,0,0,2,0,0,0,2,0,0,0,2,0],
  [0,0,1,0,1,0,0,0,0,0,1,0,1,0,0],
  [1,0,0,3,0,0,0,5,0,0,0,3,0,0,1],
  [0,0,1,0,1,0,0,0,0,0,1,0,1,0,0],
  [0,2,0,0,0,2,0,0,0,2,0,0,0,2,0],
  [4,0,0,0,3,0,1,0,1,0,3,0,0,0,4],
  [0,0,0,2,0,0,0,3,0,0,0,2,0,0,0],
  [0,0,3,0,0,0,1,0,1,0,0,0,3,0,0],
  [0,1,0,0,0,2,0,0,0,2,0,0,0,1,0],
  [2,0,0,0,4,0,0,1,0,0,4,0,0,0,2],
]

function bonusClass(type: BonusType): string {
  switch (type) {
    case 1: return 'bg-sky-900/70 text-sky-400'       // DL - light blue
    case 2: return 'bg-blue-900/70 text-blue-400'      // TL - blue
    case 3: return 'bg-pink-900/70 text-pink-400'      // DW - pink
    case 4: return 'bg-red-900/70 text-red-400'        // TW - red
    case 5: return 'bg-amber-900/70 text-amber-400'    // center star
    default: return 'bg-gray-800'
  }
}

// Valid Wordfeud letters (Norwegian)
const VALID_LETTERS = new Set('ABCDEFGHIJKLMNOPQRSTUVWXYZÆØÅ')

export interface BoardCell {
  letter: string
  isBlank: boolean
}

interface SolverMove {
  word: string
  row: number
  col: number
  direction: 'horizontal' | 'vertical'
  score: number
  tiles_used: number
  blank_tiles?: number[]
}

interface SolveResponse {
  moves: SolverMove[]
  elapsed_ms: number
}

interface GameSummary {
  id: number
  my_username: string
  opponent: string
  scores: [number, number]
  is_my_turn: boolean
  ended_at?: number
}

interface GameTile {
  letter: string
  value: number
  is_wild?: boolean
}

interface GameState {
  id: number
  board: (GameTile | null)[][]
  rack: GameTile[]
  players: [{ username: string; id: number; score: number }, { username: string; id: number; score: number }]
  is_my_turn: boolean
  is_running: boolean
  bag_count?: number
}

// Vowels for tile classification (derived from Norwegian alphabet used in the tile bag)
const VOWELS = new Set(['A', 'E', 'I', 'O', 'U', 'Y', 'Æ', 'Ø', 'Å'])
// Consonants: all non-blank, non-vowel letters in the tile bag
const CONSONANTS = new Set(
  TILE_BAG.map(t => t.letter).filter(l => l !== '*' && !VOWELS.has(l))
)

function createEmptyBoard(): (BoardCell | null)[][] {
  return Array.from({ length: 15 }, () => Array.from({ length: 15 }, () => null))
}

function formatPosition(row: number, col: number): string {
  return `${String.fromCharCode(65 + col)}${row + 1}`
}

export default function WordfeudBoard() {
  const { t } = useTranslation('wordfeud')
  const { user } = useAuth()

  const [board, setBoard] = useState<(BoardCell | null)[][]>(createEmptyBoard)
  const [selectedCell, setSelectedCell] = useState<{ row: number; col: number } | null>(null)
  const [rackInput, setRackInput] = useState('')
  const cellRefs = useRef<(HTMLButtonElement | null)[][]>(
    Array.from({ length: 15 }, () => Array.from({ length: 15 }, () => null))
  )

  // Solver state
  const [solving, setSolving] = useState(false)
  const [solverMoves, setSolverMoves] = useState<SolverMove[]>([])
  const [solverElapsed, setSolverElapsed] = useState(0)
  const [solverError, setSolverError] = useState<string | null>(null)
  const [hasSolved, setHasSolved] = useState(false)
  const [selectedMoveIdx, setSelectedMoveIdx] = useState<number | null>(null)
  const [hoveredMoveIdx, setHoveredMoveIdx] = useState<number | null>(null)
  const solveControllerRef = useRef<AbortController | null>(null)

  // Game loading state
  const [games, setGames] = useState<GameSummary[]>([])
  const [finishedGames, setFinishedGames] = useState<GameSummary[]>([])
  const [yourTurnExpanded, setYourTurnExpanded] = useState(true)
  const [theirTurnExpanded, setTheirTurnExpanded] = useState(false)
  const [finishedExpanded, setFinishedExpanded] = useState(false)
  const [selectedGameId, setSelectedGameId] = useState<number | null>(null)
  const [loadingGames, setLoadingGames] = useState(false)
  const [loadingGame, setLoadingGame] = useState(false)
  const [gamesAvailable, setGamesAvailable] = useState<boolean | null>(null)
  const [gamesError, setGamesError] = useState<'not_connected' | 'auth_expired' | 'unknown' | null>(null)
  const [bagCount, setBagCount] = useState<number | null>(null)
  const [activeGame, setActiveGame] = useState<GameState | null>(null)
  const [autoSolvePending, setAutoSolvePending] = useState(false)
  const gamesControllerRef = useRef<AbortController | null>(null)

  // Fetch games list
  const fetchGames = useCallback(async (signal?: AbortSignal) => {
    setLoadingGames(true)
    try {
      const res = await fetch('/api/wordfeud/games', { credentials: 'include', signal })
      if (!res.ok) {
        if (res.status === 400) setGamesError('not_connected')
        else if (res.status === 401) setGamesError('auth_expired')
        else setGamesError('unknown')
        setGamesAvailable(false)
        return
      }
      const data = await res.json()
      setGamesError(null)
      setGames(data.games ?? [])
      setFinishedGames(data.finished_games ?? [])
      setGamesAvailable(true)
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') return
      setGamesError('unknown')
      setGamesAvailable(false)
    } finally {
      if (!signal?.aborted) setLoadingGames(false)
    }
  }, [])

  // Fetch games list on mount
  useEffect(() => {
    const controller = new AbortController()
    gamesControllerRef.current = controller
    startTransition(() => { fetchGames(controller.signal) })
    return () => { controller.abort() }
  }, [fetchGames])

  const handleRefreshGames = useCallback(() => {
    gamesControllerRef.current?.abort()
    const controller = new AbortController()
    gamesControllerRef.current = controller
    fetchGames(controller.signal)
  }, [fetchGames])

  // Load game state when a game is selected
  useEffect(() => {
    let cancelled = false
    const controller = new AbortController()
    ;(async () => {
      if (selectedGameId == null) {
        setActiveGame(null)
        return
      }
      setLoadingGame(true)
      setActiveGame(null)
      setBoard(createEmptyBoard())
      setRackInput('')
      setBagCount(null)
      setSolverMoves([])
      setHasSolved(false)
      setSolverError(null)
      setSelectedMoveIdx(null)
      setSelectedCell(null)
      try {
        const res = await fetch(`/api/wordfeud/games/${selectedGameId}`, { credentials: 'include', signal: controller.signal })
        if (!res.ok) {
          if (cancelled) return
          setBoard(createEmptyBoard())
          setRackInput('')
          setBagCount(null)
          setActiveGame(null)
          setLoadingGame(false)
          return
        }
        const data = await res.json()
        if (cancelled) return
        const gs: GameState = data.game
        setActiveGame(gs)

        // Convert game board to solver board format
        const newBoard = createEmptyBoard()
        for (let row = 0; row < 15; row++) {
          for (let col = 0; col < 15; col++) {
            // Wordfeud API uses [col,row] order — swap for rendering
            const tile = gs.board[col]?.[row]
            if (tile) {
              newBoard[row][col] = { letter: tile.letter, isBlank: !!tile.is_wild }
            }
          }
        }
        setBoard(newBoard)

        // Convert rack tiles to rack input string
        const rackStr = (gs.rack ?? []).map(t => t.is_wild ? '*' : t.letter).join('')
        setRackInput(rackStr)

        // Store bag count from API
        setBagCount(gs.bag_count ?? null)

        // Clear solver state since board changed — auto-solve will run
        setSolverMoves([])
        setHasSolved(false)
        setSolverError(null)
        setSelectedMoveIdx(null)
        setSelectedCell(null)
        setAutoSolvePending(true)
      } catch (err) {
        if (err instanceof DOMException && err.name === 'AbortError') return
        if (!cancelled) setActiveGame(null)
      } finally {
        if (!cancelled) setLoadingGame(false)
      }
    })()
    return () => {
      cancelled = true
      controller.abort()
    }
  }, [selectedGameId])

  // Focus the selected cell
  useEffect(() => {
    if (selectedCell) {
      cellRefs.current[selectedCell.row]?.[selectedCell.col]?.focus()
    }
  }, [selectedCell])

  const setCellRef = useCallback((row: number, col: number, el: HTMLButtonElement | null) => {
    cellRefs.current[row][col] = el
  }, [])

  const placeCell = useCallback((row: number, col: number, letter: string, isBlank: boolean) => {
    setBoard(prev => {
      const next = prev.map(r => [...r])
      next[row][col] = { letter, isBlank }
      return next
    })
  }, [])

  const clearCell = useCallback((row: number, col: number) => {
    setBoard(prev => {
      const next = prev.map(r => [...r])
      next[row][col] = null
      return next
    })
  }, [])

  const clearBoard = useCallback(() => {
    setBoard(createEmptyBoard())
    setSelectedCell(null)
    setSolverMoves([])
    setHasSolved(false)
    setSolverError(null)
    setSelectedMoveIdx(null)
  }, [])

  const moveSelection = useCallback((dRow: number, dCol: number, fromRow?: number, fromCol?: number) => {
    setSelectedCell(prev => {
      const baseRow = prev?.row ?? fromRow ?? 0
      const baseCol = prev?.col ?? fromCol ?? 0
      const row = Math.max(0, Math.min(14, baseRow + dRow))
      const col = Math.max(0, Math.min(14, baseCol + dCol))
      return { row, col }
    })
  }, [])

  const handleCellKeyDown = useCallback((e: React.KeyboardEvent, row: number, col: number) => {
    switch (e.key) {
      case 'ArrowUp':
        e.preventDefault()
        moveSelection(-1, 0, row, col)
        break
      case 'ArrowDown':
        e.preventDefault()
        moveSelection(1, 0, row, col)
        break
      case 'ArrowLeft':
        e.preventDefault()
        moveSelection(0, -1, row, col)
        break
      case 'ArrowRight':
        e.preventDefault()
        moveSelection(0, 1, row, col)
        break
      case 'Delete':
      case 'Backspace':
        e.preventDefault()
        clearCell(row, col)
        break
      case ' ':
        // Toggle blank flag on existing tile
        e.preventDefault()
        if (board[row][col]) {
          placeCell(row, col, board[row][col]!.letter, !board[row][col]!.isBlank)
        }
        break
      default: {
        const upper = e.key.toUpperCase()
        if (upper.length === 1 && VALID_LETTERS.has(upper)) {
          e.preventDefault()
          placeCell(row, col, upper, false)
          // Auto-advance right after placing a letter
          moveSelection(0, 1, row, col)
        }
        break
      }
    }
  }, [board, clearCell, moveSelection, placeCell])

  // Solver: find moves
  const handleSolve = useCallback(async () => {
    const rack = rackInput.trim().toUpperCase()
    if (!rack) return

    solveControllerRef.current?.abort()
    const controller = new AbortController()
    solveControllerRef.current = controller

    setSolving(true)
    setSolverError(null)
    setSolverElapsed(0)
    setHasSolved(true)
    setSelectedMoveIdx(null)
    setHoveredMoveIdx(null)

    const boardPayload = board.map(row =>
      row.map(cell => cell ? { letter: cell.letter, is_blank: cell.isBlank } : null)
    )

    try {
      const res = await fetch('/api/wordfeud/solve', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({ board: boardPayload, rack }),
        signal: controller.signal,
      })

      if (!res.ok) {
        const data = await res.json().catch(() => ({ error: 'unknown' }))
        throw new Error(data.error || t('solver.error'))
      }

      const data: SolveResponse = await res.json()
      if (!controller.signal.aborted) {
        setSolverMoves(data.moves ?? [])
        setSolverElapsed(data.elapsed_ms)
      }
    } catch (err) {
      if (err instanceof DOMException && err.name === 'AbortError') return
      if (!controller.signal.aborted) {
        setSolverError(err instanceof Error ? err.message : t('solver.error'))
        setSolverMoves([])
        setSolverElapsed(0)
      }
    } finally {
      if (!controller.signal.aborted) {
        setSolving(false)
      }
    }
  }, [board, rackInput, t])

  useEffect(() => {
    return () => { solveControllerRef.current?.abort() }
  }, [])

  // Auto-solve after loading a game — use a ref to avoid stale closure over handleSolve
  const handleSolveRef = useRef(handleSolve)
  useLayoutEffect(() => {
    handleSolveRef.current = handleSolve
  }, [handleSolve])

  useEffect(() => {
    if (!autoSolvePending || loadingGame) return

    // Game has finished loading; clear the pending flag regardless of rack contents
    startTransition(() => { setAutoSolvePending(false) })

    const rack = rackInput.trim()
    if (rack) {
      handleSolveRef.current()
    }
  }, [autoSolvePending, loadingGame, rackInput])

  // Expand a move into a per-cell map
  const cellsForMove = useCallback((moveIdx: number | null) => {
    if (moveIdx == null || !solverMoves[moveIdx]) return new Map<string, { letter: string; isNew: boolean; isBlank: boolean }>()
    const move = solverMoves[moveIdx]
    const chars = [...move.word]
    const dr = move.direction === 'vertical' ? 1 : 0
    const dc = move.direction === 'horizontal' ? 1 : 0
    const blanks = new Set(move.blank_tiles ?? [])
    const cells = new Map<string, { letter: string; isNew: boolean; isBlank: boolean }>()
    for (let i = 0; i < chars.length; i++) {
      const r = move.row + i * dr
      const c = move.col + i * dc
      const isNew = !board[r]?.[c]
      cells.set(`${r}-${c}`, { letter: chars[i], isNew, isBlank: blanks.has(i) })
    }
    return cells
  }, [solverMoves, board])

  // Compute highlighted cells for the selected move
  const highlightCells = useMemo(() => cellsForMove(selectedMoveIdx), [cellsForMove, selectedMoveIdx])

  // Compute preview cells for hovered move (shown when no move is selected, or different from selected)
  const previewCells = useMemo(() => {
    if (hoveredMoveIdx === selectedMoveIdx) return new Map<string, { letter: string; isNew: boolean; isBlank: boolean }>()
    return cellsForMove(hoveredMoveIdx)
  }, [cellsForMove, hoveredMoveIdx, selectedMoveIdx])

  // Compute used tiles from board and rack
  const usedCounts = computeUsedTiles(board, rackInput)
  const remainingTiles = computeRemainingTiles(usedCounts)
  const totalRemaining = remainingTiles.reduce((sum, t) => sum + t.remaining, 0)

  // Tile breakdown: vowels, consonants, blanks
  const remainingVowels = remainingTiles
    .filter(t => VOWELS.has(t.letter))
    .reduce((sum, t) => sum + t.remaining, 0)
  const remainingConsonants = remainingTiles
    .filter(t => CONSONANTS.has(t.letter))
    .reduce((sum, t) => sum + t.remaining, 0)
  const remainingBlanks = remainingTiles
    .filter(t => t.letter === '*')
    .reduce((sum, t) => sum + t.remaining, 0)

  // Vowel trade percentage (chance of getting a vowel when trading full rack)
  const vowelTradePercent = totalRemaining > 0
    ? Math.round((remainingVowels / totalRemaining) * 100)
    : 0

  // Opponent rack deduction: when the bag is empty, remaining tiles minus yours
  // are known to be in the opponent's rack
  const showOpponentRack = bagCount === 0

  // Rack tiles parsed
  const rackLetters = rackInput.toUpperCase().split('').filter(ch => VALID_LETTERS.has(ch) || ch === '*')

  return (
    <div className="flex flex-col lg:flex-row gap-6">
      {/* Board */}
      <div className="shrink-0">
        <div className="flex items-center justify-between mb-3">
          <h3 className="text-sm font-medium text-gray-400">{t('board.title')}</h3>
          <button
            type="button"
            onClick={clearBoard}
            className="flex items-center gap-1.5 px-2.5 py-1 text-xs text-gray-400 hover:text-red-400 bg-gray-800 hover:bg-gray-700 rounded transition-colors cursor-pointer"
            title={t('board.clear')}
          >
            <Trash2 size={14} />
            {t('board.clear')}
          </button>
        </div>
        {/* Active game scores and turn indicator — above the board on all breakpoints */}
        {activeGame && !loadingGame && (
          <div className="flex items-center gap-3 mb-3 p-3 bg-gray-800/70 rounded-lg border border-gray-700">
            <div className={`flex items-center gap-2 px-3 py-1.5 rounded-lg ${
              activeGame.is_my_turn
                ? 'bg-blue-900/50 border border-blue-700 text-blue-200'
                : 'bg-gray-800 text-gray-400'
            }`}>
              <span className="font-medium text-sm">{activeGame.players[0].username}</span>
              <span className="text-lg font-bold">{activeGame.players[0].score}</span>
            </div>
            <span className="text-gray-500">&ndash;</span>
            <div className={`flex items-center gap-2 px-3 py-1.5 rounded-lg ${
              !activeGame.is_my_turn
                ? 'bg-blue-900/50 border border-blue-700 text-blue-200'
                : 'bg-gray-800 text-gray-400'
            }`}>
              <span className="font-medium text-sm">{activeGame.players[1].username}</span>
              <span className="text-lg font-bold">{activeGame.players[1].score}</span>
            </div>
            <span className={`ml-auto text-sm font-medium ${activeGame.is_my_turn ? 'text-green-400' : 'text-gray-400'}`}>
              {activeGame.is_my_turn ? t('yourTurn') : t('theirTurn')}
            </span>
          </div>
        )}
        <div
          className="inline-block border border-gray-700 rounded-lg overflow-hidden"
          role="grid"
          aria-label={t('board.title')}
        >
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
                const cell = board[row][col]
                const bonus = BOARD_LAYOUT[row][col]
                const isSelected = selectedCell?.row === row && selectedCell?.col === col
                const highlight = highlightCells.get(`${row}-${col}`)
                const preview = previewCells.get(`${row}-${col}`)

                let cellClass: string
                if (highlight && highlight.isNew) {
                  cellClass = highlight.isBlank
                    ? 'bg-emerald-800 text-emerald-100'
                    : 'bg-emerald-700 text-white'
                } else if (preview && preview.isNew) {
                  cellClass = preview.isBlank
                    ? 'bg-emerald-900/60 text-emerald-200/80'
                    : 'bg-emerald-800/50 text-emerald-100/80'
                } else if (cell) {
                  cellClass = cell.isBlank
                    ? 'bg-purple-700 text-white'
                    : 'bg-amber-700 text-white'
                } else {
                  cellClass = bonusClass(bonus)
                }

                const activeSource: 'highlight' | 'preview' | 'cell' | undefined =
                  highlight?.isNew ? 'highlight' : preview?.isNew ? 'preview' : cell ? 'cell' : undefined

                const displayLetter =
                  activeSource === 'highlight'
                    ? highlight!.letter
                    : activeSource === 'preview'
                      ? preview!.letter
                      : activeSource === 'cell'
                        ? cell!.letter
                        : undefined

                const isBlank =
                  activeSource === 'highlight'
                    ? !!highlight?.isBlank
                    : activeSource === 'preview'
                      ? !!preview?.isBlank
                      : activeSource === 'cell'
                        ? !!cell?.isBlank
                        : false

                const displayValue = displayLetter && !isBlank
                  ? LETTER_VALUES[displayLetter]
                  : undefined

                return (
                  <button
                    key={`${row}-${col}`}
                    type="button"
                    ref={(el) => setCellRef(row, col, el)}
                    role="gridcell"
                    aria-label={cellAriaLabel(row, col, cell, bonus, t as (key: string) => string)}
                    onClick={() => setSelectedCell({ row, col })}
                    onKeyDown={(e) => handleCellKeyDown(e, row, col)}
                    tabIndex={isSelected || (!selectedCell && row === 0 && col === 0) ? 0 : -1}
                    className={`w-7 h-7 sm:w-8 sm:h-8 flex items-center justify-center text-xs font-bold relative cursor-pointer outline-none focus-visible:ring-2 focus-visible:ring-blue-400 focus-visible:ring-inset transition-shadow ${
                      isSelected ? 'ring-2 ring-blue-400 ring-inset z-10' : ''
                    } ${cellClass}`}
                  >
                    {displayLetter ? (
                      <>
                        <span>{displayLetter}</span>
                        {displayValue != null && (
                          <span className="absolute bottom-0 right-0.5 text-[7px] leading-none opacity-70">
                            {displayValue}
                          </span>
                        )}
                      </>
                    ) : (
                      bonus > 0 && (
                        <span className="text-[8px] opacity-60">{BONUS_LABELS[bonus]}</span>
                      )
                    )}
                  </button>
                )
              })
            )}
          </div>
        </div>
        <p className="text-xs text-gray-500 mt-2">{t('board.hint')}</p>
      </div>

      {/* Sidebar: game loader + rack + solver + tile tracker */}
      <div className="flex-1 min-w-0 space-y-6">
        {/* Game list */}
        {gamesAvailable && (games.length > 0 || finishedGames.length > 0) && (() => {
          const yourTurnGames = [...games].filter(g => g.is_my_turn).sort((a, b) => a.opponent.localeCompare(b.opponent))
          const theirTurnGames = [...games].filter(g => !g.is_my_turn).sort((a, b) => a.opponent.localeCompare(b.opponent))
          return (
            <div className="space-y-1">
              {/* Header with refresh */}
              <div className="flex items-center justify-between mb-2">
                <span className="text-sm font-medium text-gray-400">{t('board.loadFromGame')}</span>
                <button
                  type="button"
                  onClick={handleRefreshGames}
                  disabled={loadingGames}
                  className="flex items-center gap-1.5 px-2 py-1 text-xs text-gray-400 hover:text-gray-200 bg-gray-800 hover:bg-gray-700 rounded transition-colors cursor-pointer disabled:opacity-50"
                  title={t('gameList.refresh')}
                >
                  <RefreshCw size={14} className={loadingGames ? 'animate-spin' : ''} />
                  {t('gameList.refresh')}
                </button>
              </div>

              {loadingGame && (
                <div className="flex items-center gap-2 text-sm text-gray-400 py-1">
                  <Loader2 size={16} className="animate-spin" />
                  {t('loading')}
                </div>
              )}

              {/* Your Turn section */}
              {yourTurnGames.length > 0 && (
                <div>
                  <button
                    onClick={() => setYourTurnExpanded(prev => !prev)}
                    aria-expanded={yourTurnExpanded}
                    aria-controls="wf-your-turn-games"
                    className="flex items-center gap-1.5 w-full text-sm font-medium text-green-400 hover:text-green-300 transition-colors cursor-pointer py-1"
                  >
                    {yourTurnExpanded ? <ChevronDown size={16} /> : <ChevronRight size={16} />}
                    {t('gameList.yourTurnSection')} ({yourTurnGames.length})
                  </button>
                  {yourTurnExpanded && (
                    <div id="wf-your-turn-games" className="space-y-1 mt-1">
                      {yourTurnGames.map(game => (
                        <button
                          key={game.id}
                          type="button"
                          onClick={() => setSelectedGameId(game.id)}
                          disabled={loadingGame}
                          aria-label={t('vsOpponent', { opponent: game.opponent })}
                          className={`w-full flex items-center justify-between rounded-lg px-3 py-2 text-sm transition-colors cursor-pointer disabled:opacity-50 ${
                            selectedGameId === game.id
                              ? 'bg-blue-900/50 border border-blue-700 text-blue-200'
                              : 'bg-gray-800 hover:bg-gray-700 text-gray-300 border border-transparent'
                          }`}
                        >
                          <span className="truncate">{game.opponent}</span>
                          <span className="tabular-nums text-gray-400 ml-2 shrink-0">{game.scores[0]}&ndash;{game.scores[1]}</span>
                        </button>
                      ))}
                    </div>
                  )}
                </div>
              )}

              {/* Their Turn section */}
              {theirTurnGames.length > 0 && (
                <div>
                  <button
                    onClick={() => setTheirTurnExpanded(prev => !prev)}
                    aria-expanded={theirTurnExpanded}
                    aria-controls="wf-their-turn-games"
                    className="flex items-center gap-1.5 w-full text-sm font-medium text-gray-400 hover:text-gray-300 transition-colors cursor-pointer py-1"
                  >
                    {theirTurnExpanded ? <ChevronDown size={16} /> : <ChevronRight size={16} />}
                    {t('gameList.theirTurnSection')} ({theirTurnGames.length})
                  </button>
                  {theirTurnExpanded && (
                    <div id="wf-their-turn-games" className="space-y-1 mt-1">
                      {theirTurnGames.map(game => (
                        <button
                          key={game.id}
                          type="button"
                          onClick={() => setSelectedGameId(game.id)}
                          disabled={loadingGame}
                          aria-label={t('vsOpponent', { opponent: game.opponent })}
                          className={`w-full flex items-center justify-between rounded-lg px-3 py-2 text-sm transition-colors cursor-pointer disabled:opacity-50 ${
                            selectedGameId === game.id
                              ? 'bg-blue-900/50 border border-blue-700 text-blue-200'
                              : 'bg-gray-800 hover:bg-gray-700 text-gray-300 border border-transparent'
                          }`}
                        >
                          <span className="truncate">{game.opponent}</span>
                          <span className="tabular-nums text-gray-400 ml-2 shrink-0">{game.scores[0]}&ndash;{game.scores[1]}</span>
                        </button>
                      ))}
                    </div>
                  )}
                </div>
              )}

              {/* Finished Games section */}
              {finishedGames.length > 0 && (
                <div>
                  <button
                    onClick={() => setFinishedExpanded(prev => !prev)}
                    aria-expanded={finishedExpanded}
                    aria-controls="wf-finished-games-list"
                    className="flex items-center gap-1.5 w-full text-sm font-medium text-gray-400 hover:text-gray-300 transition-colors cursor-pointer py-1"
                  >
                    {finishedExpanded ? <ChevronDown size={16} /> : <ChevronRight size={16} />}
                    {t('gameList.finishedGamesSection')} ({finishedGames.length})
                  </button>
                  {finishedExpanded && (
                    <div id="wf-finished-games-list" className="space-y-1 mt-1">
                      {finishedGames.map(game => {
                        const myScore = game.scores[0]
                        const opponentScore = game.scores[1]
                        const iWon = myScore > opponentScore
                        const isDraw = myScore === opponentScore
                        const resultLabel = isDraw ? t('finishedGames.draw') : iWon ? t('finishedGames.won') : t('finishedGames.lost')
                        return (
                          <button
                            key={game.id}
                            type="button"
                            onClick={() => setSelectedGameId(game.id)}
                            disabled={loadingGame}
                            aria-label={t('vsOpponent', { opponent: game.opponent })}
                            className={`w-full flex items-center justify-between rounded-lg px-3 py-2 text-sm transition-colors cursor-pointer disabled:opacity-50 ${
                              selectedGameId === game.id
                                ? 'bg-blue-900/50 border border-blue-700 text-blue-200'
                                : 'bg-gray-800 hover:bg-gray-700 text-gray-300 border border-transparent'
                            }`}
                          >
                            <div className="flex flex-col items-start min-w-0">
                              <span className="truncate">{game.opponent}</span>
                              {game.ended_at != null && game.ended_at > 0 && (
                                <span className="text-xs text-gray-500">
                                  {t('finishedGames.completed', { date: formatDate(new Date(game.ended_at * 1000), { dateStyle: 'medium' }) })}
                                </span>
                              )}
                            </div>
                            <div className="flex items-center gap-2 ml-2 shrink-0">
                              <span className={`tabular-nums ${iWon ? 'font-bold text-green-400' : 'text-gray-400'}`}>
                                {myScore}
                              </span>
                              <span className="text-gray-600">&ndash;</span>
                              <span className={`tabular-nums ${!iWon && !isDraw ? 'font-bold text-green-400' : 'text-gray-400'}`}>
                                {opponentScore}
                              </span>
                              {!isDraw && (
                                <Trophy size={14} className={iWon ? 'text-green-400' : 'text-gray-600'} aria-hidden="true" />
                              )}
                              <span className={`text-xs ${iWon ? 'text-green-400' : 'text-gray-500'}`}>
                                {resultLabel}
                              </span>
                            </div>
                          </button>
                        )
                      })}
                    </div>
                  )}
                </div>
              )}
            </div>
          )
        })()}

        {/* Loading games indicator */}
        {loadingGames && !gamesAvailable && (
          <div className="flex items-center gap-2 text-sm text-gray-400">
            <Loader2 size={16} className="animate-spin" />
            {t('loading')}
          </div>
        )}

        {/* Error state when games are not available */}
        {gamesAvailable === false && gamesError && (
          <div className="p-3 bg-gray-800/70 rounded-lg border border-gray-700 text-sm text-gray-300">
            {gamesError === 'not_connected' || gamesError === 'auth_expired'
              ? (user?.is_admin ? t('notConnected') : t('notConnectedNonAdmin'))
              : t('errors.failedToLoadGames')}
            {user?.is_admin && (
              <Link to="/settings" className="ml-2 text-blue-400 hover:text-blue-300 underline">
                {t('goToSettings')}
              </Link>
            )}
          </div>
        )}

        {/* Rack input */}
        <div>
          <label htmlFor="rack-input" className="block text-sm font-medium text-gray-400 mb-2">
            {t('board.rack')}
          </label>
          <input
            id="rack-input"
            type="text"
            value={rackInput}
            onChange={e => {
              const filtered = e.target.value.toUpperCase().replace(/[^A-ZÆØÅ*]/g, '')
              if (filtered.length <= 7) setRackInput(filtered)
            }}
            onKeyDown={e => { if (e.key === 'Enter') handleSolve() }}
            placeholder={t('board.rackPlaceholder')}
            maxLength={7}
            className="w-full max-w-xs bg-gray-800 border border-gray-700 rounded-lg px-3 py-2.5 text-sm text-white placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500 uppercase tracking-wider font-mono"
          />
          {/* Visual rack tiles */}
          {rackLetters.length > 0 && (
            <div className="flex gap-1 mt-2">
              {rackLetters.map((ch, i) => (
                <div
                  key={i}
                  className={`w-10 h-10 flex items-center justify-center text-lg font-bold rounded relative ${
                    ch === '*'
                      ? 'bg-purple-700 text-white'
                      : 'bg-amber-700 text-white'
                  }`}
                >
                  <span>{ch === '*' ? '?' : ch}</span>
                  {ch !== '*' && LETTER_VALUES[ch] != null && (
                    <span className="absolute bottom-0.5 right-1 text-[9px] opacity-70">
                      {LETTER_VALUES[ch]}
                    </span>
                  )}
                </div>
              ))}
            </div>
          )}

          {/* Solve button */}
          <button
            type="button"
            onClick={handleSolve}
            disabled={solving || !rackInput.trim()}
            className="mt-3 flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-500 disabled:bg-gray-700 disabled:text-gray-500 rounded-lg text-sm font-medium transition-colors cursor-pointer"
          >
            {solving ? <Loader2 size={16} className="animate-spin" /> : <Search size={16} />}
            {t('solver.solve')}
          </button>
        </div>

        {/* Solver results */}
        {solverError && (
          <div className="bg-red-900/50 border border-red-700 text-red-200 rounded-lg p-3 text-sm">
            {solverError}
          </div>
        )}

        {!solving && hasSolved && (
          <div>
            <div className="flex items-center justify-between mb-2">
              <h3 className="text-sm font-medium text-gray-400">{t('solver.topMoves')}</h3>
              <span className="text-xs text-gray-500">
                {t('solver.elapsed', { ms: solverElapsed })}
              </span>
            </div>

            {solverMoves.length > 0 ? (
              <div className="bg-gray-800/50 rounded-lg border border-gray-700 overflow-hidden">
                {/* Header */}
                <div className="grid grid-cols-[1fr_auto_auto_auto] gap-2 px-3 py-2 bg-gray-800 border-b border-gray-700 text-xs font-medium text-gray-400 uppercase tracking-wide">
                  <span>{t('finder.colWord')}</span>
                  <span className="w-12 text-center">{t('solver.position')}</span>
                  <span className="w-6 text-center">{t('solver.dir')}</span>
                  <span className="w-12 text-right">{t('finder.colPoints')}</span>
                </div>

                {/* Rows */}
                <div className="max-h-[40vh] overflow-y-auto">
                  {solverMoves.map((move, i) => (
                    <button
                      key={`${move.word}-${move.row}-${move.col}-${move.direction}-${i}`}
                      type="button"
                      onClick={() => setSelectedMoveIdx(selectedMoveIdx === i ? null : i)}
                      onMouseEnter={() => setHoveredMoveIdx(i)}
                      onMouseLeave={() => setHoveredMoveIdx(null)}
                      onPointerEnter={() => setHoveredMoveIdx(i)}
                      onPointerLeave={() => setHoveredMoveIdx(null)}
                      onFocus={() => setHoveredMoveIdx(i)}
                      onBlur={() => setHoveredMoveIdx(null)}
                      className={`w-full grid grid-cols-[1fr_auto_auto_auto] gap-2 px-3 py-1.5 text-sm text-left transition-colors cursor-pointer ${
                        selectedMoveIdx === i
                          ? 'bg-emerald-900/40 text-emerald-200'
                          : hoveredMoveIdx === i
                            ? 'bg-emerald-900/20 text-emerald-100'
                            : i % 2 === 0
                              ? 'bg-gray-800/30 hover:bg-gray-700/50'
                              : 'hover:bg-gray-700/50'
                      }`}
                    >
                      <span className="font-mono tracking-wider text-white truncate">
                        {renderSolverWord(move)}
                      </span>
                      <span className="w-12 text-center text-gray-400 tabular-nums text-xs leading-5">
                        {formatPosition(move.row, move.col)}
                      </span>
                      <span className="w-6 text-center text-gray-500 text-xs leading-5">
                        {move.direction === 'horizontal' ? '\u2192' : '\u2193'}
                      </span>
                      <span className="w-12 text-right font-medium text-amber-400 tabular-nums">
                        {move.score}
                      </span>
                    </button>
                  ))}
                </div>
              </div>
            ) : (
              <p className="text-gray-500 text-sm">{t('solver.noResults')}</p>
            )}
          </div>
        )}

        {/* Tile tracker */}
        <div>
          <h3 className="text-sm font-medium text-gray-400 mb-2">
            {showOpponentRack ? t('board.opponentRack') : t('board.remainingTiles')}
          </h3>
          <p className="text-xs text-gray-500 mb-3">
            {t('board.tilesInBag', { count: totalRemaining, total: TOTAL_TILES })}
          </p>
          {showOpponentRack && (
            <p className="text-xs text-amber-400/80 mb-3">
              {t('board.opponentRackHint')}
            </p>
          )}
          <div className="grid grid-cols-5 sm:grid-cols-6 md:grid-cols-8 gap-1.5">
            {remainingTiles.filter(t => t.remaining > 0).map(({ letter, remaining }) => (
              <div
                key={letter}
                className={`flex flex-col items-center px-1.5 py-1 rounded text-xs ${
                  showOpponentRack
                    ? 'bg-amber-900/40 text-amber-200'
                    : 'bg-gray-800 text-gray-300'
                }`}
              >
                <span className="font-bold text-sm font-mono">
                  {letter === '*' ? '?' : letter}
                </span>
                <div className="flex items-center gap-1">
                  <span className={`tabular-nums ${showOpponentRack ? 'text-amber-300/70' : 'text-gray-400'}`}>
                    {remaining}
                  </span>
                  {letter !== '*' && LETTER_VALUES[letter] != null && (
                    <span className="text-gray-500 text-[10px]">
                      ({LETTER_VALUES[letter]})
                    </span>
                  )}
                </div>
              </div>
            ))}
            {/* Show depleted tiles in a muted style */}
            {remainingTiles.filter(t => t.remaining === 0).map(({ letter, total }) => (
              <div
                key={letter}
                className="flex flex-col items-center px-1.5 py-1 rounded text-xs bg-gray-800/50 text-gray-600"
              >
                <span className="font-bold text-sm font-mono">
                  {letter === '*' ? '?' : letter}
                </span>
                <div className="flex items-center gap-1">
                  <span className="tabular-nums text-gray-600">
                    0/{total}
                  </span>
                </div>
              </div>
            ))}
          </div>

          {/* Breakdown: consonants, vowels, blanks */}
          <div className="flex flex-wrap gap-3 mt-3 text-xs text-gray-400">
            <span>{t('board.consonants', { count: remainingConsonants })}</span>
            <span>{t('board.vowels', { count: remainingVowels })}</span>
            <span>{t('board.blanks', { count: remainingBlanks })}</span>
          </div>

          {/* Full letter list with values */}
          {totalRemaining > 0 && (
            <div className="mt-2">
              <p className="text-xs text-gray-500 mb-1">
                {t(showOpponentRack ? 'board.letters' : 'board.letterList')}
              </p>
              <p className="text-xs text-gray-400 font-mono leading-relaxed break-words">
                {remainingTiles
                  .filter(t => t.remaining > 0)
                  .flatMap(({ letter, remaining }) => {
                    const display = letter === '*' ? '?' : letter
                    const value = letter === '*' ? 0 : LETTER_VALUES[letter]
                    const tileString =
                      value === undefined ? display : `${display}(${value})`
                    return Array.from({ length: remaining }, () => tileString)
                  })
                  .join(' ')}
              </p>
            </div>
          )}

          {/* Vowel trade percentage */}
          {totalRemaining > 0 && (
            <p className="text-xs text-gray-500 mt-2">
              {t('board.vowelTradeChance', { percent: vowelTradePercent })}
            </p>
          )}
        </div>
      </div>
    </div>
  )
}

function renderSolverWord(move: SolverMove): React.ReactNode {
  if (!move.blank_tiles || move.blank_tiles.length === 0) {
    return move.word
  }
  const blanks = new Set(move.blank_tiles)
  return (
    <>
      {[...move.word].map((ch, i) => (
        <span key={i} className={blanks.has(i) ? 'text-purple-400' : ''}>
          {ch}
        </span>
      ))}
    </>
  )
}

function cellAriaLabel(
  row: number,
  col: number,
  cell: BoardCell | null,
  bonus: BonusType,
  t: (key: string) => string
): string {
  const pos = `${String.fromCharCode(65 + col)}${row + 1}`
  if (cell) {
    return `${pos}: ${cell.letter}${cell.isBlank ? ` (${t('board.blank')})` : ''}`
  }
  if (bonus > 0) {
    return `${pos}: ${BONUS_LABELS[bonus]}`
  }
  return pos
}

function computeUsedTiles(
  board: (BoardCell | null)[][],
  rackInput: string
): Map<string, number> {
  const counts = new Map<string, number>()

  // Count board tiles
  for (const row of board) {
    for (const cell of row) {
      if (!cell) continue
      if (cell.isBlank) {
        // Blank tiles count as '*' in the bag
        counts.set('*', (counts.get('*') ?? 0) + 1)
      } else {
        counts.set(cell.letter, (counts.get(cell.letter) ?? 0) + 1)
      }
    }
  }

  // Count rack tiles
  for (const ch of rackInput.toUpperCase()) {
    if (VALID_LETTERS.has(ch) || ch === '*') {
      counts.set(ch, (counts.get(ch) ?? 0) + 1)
    }
  }

  return counts
}

function computeRemainingTiles(
  usedCounts: Map<string, number>
): { letter: string; remaining: number; total: number }[] {
  return TILE_BAG.map(({ letter, count }) => {
    const used = usedCounts.get(letter) ?? 0
    return { letter, remaining: Math.max(0, count - used), total: count }
  })
}
