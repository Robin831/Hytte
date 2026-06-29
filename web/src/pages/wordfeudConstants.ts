// Shared Wordfeud tile constants — single source of truth for scoring, bag
// distribution, and board layout. These were previously duplicated/drifting
// across components, which caused recurring mismatch bugs. Keep all tile/letter
// value corrections in this one module.

// Scoring table: official Norwegian Wordfeud tile point values from the API
// (POST /tile_points/1/). Q, X, Z are included here for completeness but have
// 0 tiles in the Norwegian bag and will never appear in actual play.
export const LETTER_VALUES: Record<string, number> = {
  A: 1, B: 4, C: 10, D: 1, E: 1, F: 2, G: 4, H: 3, I: 2, J: 4,
  K: 3, L: 2, M: 2, N: 1, O: 3, P: 4, Q: 10, R: 1, S: 1, T: 1,
  U: 4, V: 5, W: 10, X: 10, Y: 8, Z: 10, 'Æ': 8, 'Ø': 5, 'Å': 4,
}

// Norwegian Wordfeud tile bag distribution — Q, X, Z have 0 tiles and are absent
export const TILE_BAG: { letter: string; count: number }[] = [
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

// Board cell multiplier types
// 0=normal, 1=DL, 2=TL, 3=DW, 4=TW, 5=center
export type BonusType = 0 | 1 | 2 | 3 | 4 | 5

// Standard Wordfeud board layout (board ID 0, 15x15 symmetric),
// fetched from POST /board/0/.
// 0=normal, 1=DL, 2=TL, 3=DW, 4=TW, 5=center star
// prettier-ignore
export const BOARD_LAYOUT: BonusType[][] = [
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
