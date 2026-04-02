import { useState, useCallback, useRef, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { Trash2 } from 'lucide-react'

// Letter values for the Norwegian Wordfeud tile set
const LETTER_VALUES: Record<string, number> = {
  A: 1, B: 4, C: 10, D: 1, E: 1, F: 4, G: 3, H: 3, I: 2, J: 8,
  K: 3, L: 2, M: 3, N: 1, O: 3, P: 4, Q: 10, R: 1, S: 1, T: 1,
  U: 4, V: 4, W: 8, X: 8, Y: 6, Z: 10, 'Æ': 8, 'Ø': 5, 'Å': 4,
}

// Norwegian Wordfeud tile bag (104 tiles total)
const TILE_BAG: { letter: string; count: number }[] = [
  { letter: 'A', count: 7 }, { letter: 'B', count: 3 }, { letter: 'C', count: 1 },
  { letter: 'D', count: 5 }, { letter: 'E', count: 9 }, { letter: 'F', count: 4 },
  { letter: 'G', count: 4 }, { letter: 'H', count: 3 }, { letter: 'I', count: 5 },
  { letter: 'J', count: 2 }, { letter: 'K', count: 4 }, { letter: 'L', count: 5 },
  { letter: 'M', count: 3 }, { letter: 'N', count: 7 }, { letter: 'O', count: 4 },
  { letter: 'P', count: 2 }, { letter: 'Q', count: 1 }, { letter: 'R', count: 6 },
  { letter: 'S', count: 6 }, { letter: 'T', count: 6 }, { letter: 'U', count: 3 },
  { letter: 'V', count: 3 }, { letter: 'W', count: 1 }, { letter: 'X', count: 1 },
  { letter: 'Y', count: 1 }, { letter: 'Z', count: 1 }, { letter: 'Æ', count: 1 },
  { letter: 'Ø', count: 2 }, { letter: 'Å', count: 2 }, { letter: '*', count: 2 },
]

const TOTAL_TILES = 104

// Board cell multiplier types
// 0=normal, 1=DL, 2=TL, 3=DW, 4=TW, 5=center
type BonusType = 0 | 1 | 2 | 3 | 4 | 5

const BONUS_LABELS = ['', 'DL', 'TL', 'DW', 'TW', '\u2605'] as const

// Standard Wordfeud board layout (15x15, symmetric)
// prettier-ignore
const BOARD_LAYOUT: BonusType[][] = [
  [4,0,0,1,0,0,0,4,0,0,0,1,0,0,4],
  [0,3,0,0,0,2,0,0,0,2,0,0,0,3,0],
  [0,0,3,0,0,0,1,0,1,0,0,0,3,0,0],
  [1,0,0,3,0,0,0,1,0,0,0,3,0,0,1],
  [0,0,0,0,3,0,0,0,0,0,3,0,0,0,0],
  [0,2,0,0,0,2,0,0,0,2,0,0,0,2,0],
  [0,0,1,0,0,0,1,0,1,0,0,0,1,0,0],
  [4,0,0,1,0,0,0,5,0,0,0,1,0,0,4],
  [0,0,1,0,0,0,1,0,1,0,0,0,1,0,0],
  [0,2,0,0,0,2,0,0,0,2,0,0,0,2,0],
  [0,0,0,0,3,0,0,0,0,0,3,0,0,0,0],
  [1,0,0,3,0,0,0,1,0,0,0,3,0,0,1],
  [0,0,3,0,0,0,1,0,1,0,0,0,3,0,0],
  [0,3,0,0,0,2,0,0,0,2,0,0,0,3,0],
  [4,0,0,1,0,0,0,4,0,0,0,1,0,0,4],
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

function createEmptyBoard(): (BoardCell | null)[][] {
  return Array.from({ length: 15 }, () => Array.from({ length: 15 }, () => null))
}

export default function WordfeudBoard() {
  const { t } = useTranslation('wordfeud')

  const [board, setBoard] = useState<(BoardCell | null)[][]>(createEmptyBoard)
  const [selectedCell, setSelectedCell] = useState<{ row: number; col: number } | null>(null)
  const [rackInput, setRackInput] = useState('')
  const cellRefs = useRef<(HTMLButtonElement | null)[][]>(
    Array.from({ length: 15 }, () => Array.from({ length: 15 }, () => null))
  )

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

  // Compute used tiles from board and rack
  const usedCounts = computeUsedTiles(board, rackInput)
  const remainingTiles = computeRemainingTiles(usedCounts)
  const totalRemaining = remainingTiles.reduce((sum, t) => sum + t.remaining, 0)

  // Board as JSON for API
  const boardState = board.map(row =>
    row.map(cell => cell ? { letter: cell.letter, is_blank: cell.isBlank } : null)
  )

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
                    } ${
                      cell
                        ? cell.isBlank
                          ? 'bg-purple-700 text-white'
                          : 'bg-amber-700 text-white'
                        : bonusClass(bonus)
                    }`}
                  >
                    {cell ? (
                      <>
                        <span>{cell.letter}</span>
                        {!cell.isBlank && LETTER_VALUES[cell.letter] != null && (
                          <span className="absolute bottom-0 right-0.5 text-[7px] leading-none opacity-70">
                            {LETTER_VALUES[cell.letter]}
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

      {/* Sidebar: rack + tile tracker */}
      <div className="flex-1 min-w-0 space-y-6">
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
        </div>

        {/* Tile tracker */}
        <div>
          <h3 className="text-sm font-medium text-gray-400 mb-2">
            {t('board.remainingTiles')}
          </h3>
          <p className="text-xs text-gray-500 mb-3">
            {t('board.tilesInBag', { count: totalRemaining, total: TOTAL_TILES })}
          </p>
          <div className="grid grid-cols-5 sm:grid-cols-6 md:grid-cols-8 gap-1.5">
            {remainingTiles.map(({ letter, remaining, total }) => (
              <div
                key={letter}
                className={`flex flex-col items-center px-1.5 py-1 rounded text-xs ${
                  remaining === 0
                    ? 'bg-gray-800/50 text-gray-600'
                    : 'bg-gray-800 text-gray-300'
                }`}
              >
                <span className="font-bold text-sm font-mono">
                  {letter === '*' ? '?' : letter}
                </span>
                <span className={`tabular-nums ${remaining === 0 ? 'text-gray-600' : 'text-gray-400'}`}>
                  {remaining}/{total}
                </span>
              </div>
            ))}
          </div>
        </div>

        {/* Board state serialized for future use by the solver API */}
        <input type="hidden" id="board-state" value={JSON.stringify(boardState)} />
      </div>
    </div>
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
