// Playing a move in a live Wordfeud game: the tiles going on the board, the
// server's check of them, and the calls that post to Wordfeud.

export interface PlayTile {
  row: number
  col: number
  letter: string
  blank: boolean
}

// A tile laid by hand, remembering which rack slot it came from.
export interface PendingTile extends PlayTile {
  rackIdx: number
}

export interface MovePreview {
  problem: string
  main_word: string
  words: { word: string; valid: boolean }[]
  score: number
  all_valid: boolean
}

export type PlayResult =
  | { ok: true; points?: number; mainWord?: string }
  | { ok: false; code: string }

export function tilesKey(tiles: PlayTile[]): string {
  return tiles
    .map(t => `${t.row},${t.col},${t.letter},${t.blank ? 1 : 0}`)
    .sort()
    .join(';')
}

async function post(path: string, body?: unknown, signal?: AbortSignal): Promise<Response> {
  return fetch(path, {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal,
  })
}

export async function previewMove(gameId: number, tiles: PlayTile[], signal?: AbortSignal): Promise<MovePreview | null> {
  const res = await post(`/api/wordfeud/games/${gameId}/preview`, { tiles }, signal)
  if (!res.ok) return null
  return res.json()
}

async function result(res: Response): Promise<PlayResult> {
  const data = await res.json().catch(() => ({}))
  if (!res.ok) return { ok: false, code: typeof data.error === 'string' ? data.error : 'wordfeud_unreachable' }
  return { ok: true, points: data.points, mainWord: data.main_word }
}

async function send(path: string, body?: unknown): Promise<PlayResult> {
  try {
    return await result(await post(path, body))
  } catch {
    return { ok: false, code: 'wordfeud_unreachable' }
  }
}

export const playMove = (gameId: number, tiles: PlayTile[]) =>
  send(`/api/wordfeud/games/${gameId}/move`, { tiles })
export const passTurn = (gameId: number) => send(`/api/wordfeud/games/${gameId}/pass`)
export const swapTiles = (gameId: number, letters: string[]) =>
  send(`/api/wordfeud/games/${gameId}/swap`, { letters })
export const resignGame = (gameId: number) => send(`/api/wordfeud/games/${gameId}/resign`)
