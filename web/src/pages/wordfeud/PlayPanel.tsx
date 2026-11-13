import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { TFunction } from 'i18next'
import { ChevronDown, ChevronRight, Loader2, Send, Undo2, AlertTriangle, CheckCircle2 } from 'lucide-react'
import { ConfirmDialog, Dialog, DialogBody, DialogHeader } from '../../components/ui/dialog'
import { LETTER_VALUES } from '../wordfeudConstants'
import {
  passTurn,
  playMove,
  previewMove,
  resignGame,
  swapTiles,
  tilesKey,
  type MovePreview,
  type PlayResult,
  type PlayTile,
} from './play'

export interface RackTile {
  letter: string
  is_wild?: boolean
}

interface Props {
  gameId: number
  opponent: string
  isMyTurn: boolean
  isRunning: boolean
  bagCount: number | null
  rack: RackTile[]
  // The move on offer: the selected solver suggestion or tiles laid by hand.
  candidate: PlayTile[]
  ownTiles: boolean
  usedRackIdx: ReadonlySet<number>
  armedIdx: number | null
  onArm: (idx: number | null) => void
  onRecall: () => void
  // A blank tile waiting for its letter before it goes on the board.
  blankPending: boolean
  onBlankLetter: (letter: string | null) => void
  // The board was edited by hand and no longer matches the game.
  boardEdited: boolean
  message: { ok: boolean; text: string } | null
  onDone: (message: { ok: boolean; text: string }) => void
}

const BLANK_LETTERS = Object.keys(LETTER_VALUES).filter(l => l !== '*').sort((a, b) => a.localeCompare(b, 'nb'))

type Confirm = 'play' | 'pass' | 'swap' | 'resign' | null

export default function PlayPanel(props: Props) {
  const { t } = useTranslation('wordfeud')
  const { gameId, candidate, rack } = props
  const [preview, setPreview] = useState<{ key: string; data: MovePreview | null } | null>(null)
  const [moreOpen, setMoreOpen] = useState(false)
  const [swapPick, setSwapPick] = useState<ReadonlySet<number>>(new Set())
  const [confirm, setConfirm] = useState<Confirm>(null)

  const key = tilesKey(candidate)
  useEffect(() => {
    if (candidate.length === 0) return
    const controller = new AbortController()
    previewMove(gameId, candidate, controller.signal)
      .then(data => setPreview({ key, data }))
      .catch(() => { /* aborted or offline: the play button stays disabled */ })
    return () => controller.abort()
    // candidate is identified by key; gameId changes remount the panel.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, gameId])

  const current = preview?.key === key && candidate.length > 0 ? preview.data : null
  const checking = candidate.length > 0 && preview?.key !== key

  if (!props.isRunning || !props.isMyTurn) {
    return (
      <section className="mt-4 p-3 rounded-lg border border-gray-700 bg-gray-800/50 text-sm text-gray-400" aria-label={t('play.title')}>
        {props.message && <Status message={props.message} />}
        <p>{props.isRunning ? t('play.waiting', { opponent: props.opponent }) : t('play.gameOver')}</p>
      </section>
    )
  }

  const canSwap = (props.bagCount ?? 0) >= 7
  const swapping = moreOpen && swapPick.size > 0
  const invalidWords = current?.words.filter(w => !w.valid).map(w => w.word) ?? []

  const finish = (res: PlayResult, okText: string) => {
    if (res.ok) {
      setSwapPick(new Set())
      setMoreOpen(false)
      props.onDone({ ok: true, text: okText })
    } else {
      props.onDone({ ok: false, text: errorText(t, res.code) })
    }
  }

  const onConfirm = async () => {
    switch (confirm) {
      case 'play': {
        const res = await playMove(gameId, candidate)
        finish(res, res.ok ? t('play.played', { word: res.mainWord ?? current?.main_word ?? '', points: res.points ?? current?.score ?? 0 }) : '')
        break
      }
      case 'pass':
        finish(await passTurn(gameId), t('play.passed'))
        break
      case 'swap':
        finish(await swapTiles(gameId, [...swapPick].map(i => (rack[i].is_wild ? '*' : rack[i].letter))), t('play.swapped'))
        break
      case 'resign':
        finish(await resignGame(gameId), t('play.resigned'))
        break
    }
  }

  const tileButton = (tile: RackTile, i: number) => {
    const used = props.usedRackIdx.has(i)
    const armed = props.armedIdx === i
    const picked = swapPick.has(i)
    const onClick = () => {
      if (moreOpen) {
        const next = new Set(swapPick)
        if (next.has(i)) next.delete(i)
        else next.add(i)
        setSwapPick(next)
      } else if (!used) {
        props.onArm(armed ? null : i)
      }
    }
    const letter = tile.is_wild ? '' : tile.letter.toUpperCase()
    return (
      <button
        key={i}
        type="button"
        onClick={onClick}
        disabled={used && !moreOpen}
        aria-pressed={moreOpen ? picked : armed}
        aria-label={letter || t('board.blank')}
        className={`relative w-10 h-10 sm:w-11 sm:h-11 rounded-md font-bold text-lg flex items-center justify-center transition-all cursor-pointer disabled:cursor-default ${
          used && !moreOpen
            ? 'bg-gray-800 text-gray-600 border border-dashed border-gray-700'
            : moreOpen
              ? picked
                ? 'bg-red-700 text-white ring-2 ring-red-400'
                : 'bg-amber-700/70 text-white'
              : armed
                ? 'bg-emerald-600 text-white ring-2 ring-emerald-300 -translate-y-1'
                : tile.is_wild
                  ? 'bg-purple-700 text-white'
                  : 'bg-amber-700 text-white'
        }`}
      >
        {used && !moreOpen ? '' : letter}
        {!used && letter && LETTER_VALUES[letter] != null && (
          <span className="absolute bottom-0.5 right-1 text-[9px] leading-none opacity-70">{LETTER_VALUES[letter]}</span>
        )}
      </button>
    )
  }

  const confirmTitle =
    confirm === 'play' ? t('play.confirmPlayTitle', { word: current?.main_word ?? '' })
      : confirm === 'pass' ? t('play.confirmPassTitle')
        : confirm === 'swap' ? t('play.confirmSwapTitle', { tiles: [...swapPick].map(i => rack[i].is_wild ? '?' : rack[i].letter.toUpperCase()).join(' ') })
          : t('play.confirmResignTitle', { opponent: props.opponent })
  const confirmBody =
    confirm === 'play'
      ? [
          t('play.confirmPlayBody', { opponent: props.opponent, points: t('play.points', { count: current?.score ?? 0 }) }),
          invalidWords.length > 0 ? t('play.confirmWordsWarning', { words: invalidWords.join(', ') }) : '',
        ].filter(Boolean).join(' ')
      : confirm === 'pass' ? t('play.confirmPassBody')
        : confirm === 'swap' ? t('play.confirmSwapBody')
          : t('play.confirmResignBody')

  return (
    <section className="mt-4 p-3 sm:p-4 rounded-lg border border-emerald-800/60 bg-gray-800/60 space-y-3" aria-label={t('play.title')}>
      <h3 className="text-sm font-medium text-emerald-300">{t('play.title')}</h3>
      {props.message && <Status message={props.message} />}

      {/* Rack: tap a tile, then a square on the board. */}
      <div>
        <p className="text-xs text-gray-500 mb-1.5">{moreOpen ? t('play.swapPick') : t('play.rackLabel')}</p>
        <div className="flex flex-wrap gap-1.5">{rack.map(tileButton)}</div>
      </div>

      {!moreOpen && (
        <>
          {candidate.length === 0 ? (
            <p className="text-sm text-gray-400">{t('play.intro')}</p>
          ) : (
            <div className="space-y-1.5" aria-live="polite">
              {checking && (
                <p className="text-sm text-gray-400 flex items-center gap-1.5">
                  <Loader2 size={14} className="animate-spin" /> {t('play.checking')}
                </p>
              )}
              {current && current.problem && (
                <p className="text-sm text-amber-300 flex items-start gap-1.5">
                  <AlertTriangle size={16} className="shrink-0 mt-0.5" /> {problemText(t, current.problem)}
                </p>
              )}
              {current && !current.problem && (
                <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
                  <span className="font-mono text-xl font-bold tracking-wider text-white">{current.main_word}</span>
                  <span className="text-emerald-300 font-semibold">{t('play.points', { count: current.score })}</span>
                  {current.words.slice(1).map(w => (
                    <span key={w.word} className={`font-mono text-sm ${w.valid ? 'text-gray-300' : 'text-red-400 line-through'}`}>
                      +{w.word}
                    </span>
                  ))}
                </div>
              )}
              {current && !current.problem && invalidWords.length > 0 && (
                <p className="text-xs text-red-400">
                  {invalidWords.join(', ')}: {t('play.notInDictionary')}
                </p>
              )}
            </div>
          )}
          {props.boardEdited && <p className="text-xs text-amber-300">{t('play.boardEdited')}</p>}

          <div className="flex flex-wrap gap-2">
            <button
              type="button"
              onClick={() => setConfirm('play')}
              disabled={!current || !!current.problem || props.boardEdited}
              className="flex-1 sm:flex-none inline-flex items-center justify-center gap-2 px-4 py-2.5 rounded-lg bg-emerald-600 hover:bg-emerald-500 text-white font-medium disabled:opacity-40 disabled:cursor-not-allowed cursor-pointer transition-colors"
            >
              <Send size={16} /> {t('play.play')}
            </button>
            {props.ownTiles && (
              <button
                type="button"
                onClick={props.onRecall}
                className="inline-flex items-center gap-1.5 px-3 py-2.5 rounded-lg bg-gray-700 hover:bg-gray-600 text-gray-200 text-sm cursor-pointer transition-colors"
              >
                <Undo2 size={16} /> {t('play.recall')}
              </button>
            )}
          </div>
        </>
      )}

      {/* Pass / swap / resign, folded away so they're never hit by accident. */}
      <div className="border-t border-gray-700 pt-2">
        <button
          type="button"
          onClick={() => { setMoreOpen(o => !o); setSwapPick(new Set()); props.onArm(null) }}
          aria-expanded={moreOpen}
          className="inline-flex items-center gap-1 text-sm text-gray-400 hover:text-gray-200 cursor-pointer"
        >
          {moreOpen ? <ChevronDown size={16} /> : <ChevronRight size={16} />} {t('play.more')}
        </button>
        {moreOpen && (
          <div className="mt-2 space-y-2">
            {!canSwap && <p className="text-xs text-gray-500">{t('play.swapBagTooSmall')}</p>}
            <div className="flex flex-wrap gap-2">
              <button
                type="button"
                onClick={() => setConfirm('swap')}
                disabled={!swapping || !canSwap}
                className="px-3 py-2 rounded-lg bg-gray-700 hover:bg-gray-600 text-sm text-gray-100 disabled:opacity-40 disabled:cursor-not-allowed cursor-pointer"
              >
                {swapPick.size > 0 ? t('play.swapGo', { count: swapPick.size }) : t('play.swap')}
              </button>
              <button
                type="button"
                onClick={() => setConfirm('pass')}
                className="px-3 py-2 rounded-lg bg-gray-700 hover:bg-gray-600 text-sm text-gray-100 cursor-pointer"
              >
                {t('play.pass')}
              </button>
              <button
                type="button"
                onClick={() => setConfirm('resign')}
                className="px-3 py-2 rounded-lg bg-red-900/60 hover:bg-red-800 text-sm text-red-100 cursor-pointer sm:ml-auto"
              >
                {t('play.resign')}
              </button>
            </div>
          </div>
        )}
      </div>

      <ConfirmDialog
        open={confirm !== null}
        onClose={() => setConfirm(null)}
        onConfirm={onConfirm}
        title={confirmTitle}
        message={confirmBody}
        confirmLabel={confirm === 'play' ? t('play.confirmPlay') : t('play.confirmAction')}
        cancelLabel={t('play.cancel')}
        variant={confirm === 'resign' ? 'destructive' : 'default'}
      />

      <Dialog open={props.blankPending} onClose={() => props.onBlankLetter(null)} aria-label={t('play.chooseBlank')}>
        <DialogHeader title={t('play.chooseBlank')} onClose={() => props.onBlankLetter(null)} />
        <DialogBody>
          <div className="grid grid-cols-6 gap-1.5">
            {BLANK_LETTERS.map(l => (
              <button
                key={l}
                type="button"
                onClick={() => props.onBlankLetter(l)}
                className="h-11 rounded-md bg-purple-700 hover:bg-purple-600 text-white font-bold text-lg cursor-pointer"
              >
                {l}
              </button>
            ))}
          </div>
        </DialogBody>
      </Dialog>
    </section>
  )
}

function Status({ message }: { message: { ok: boolean; text: string } }) {
  return (
    <p role="status" className={`text-sm flex items-start gap-1.5 ${message.ok ? 'text-emerald-300' : 'text-red-300'}`}>
      {message.ok ? <CheckCircle2 size={16} className="shrink-0 mt-0.5" /> : <AlertTriangle size={16} className="shrink-0 mt-0.5" />}
      {message.text}
    </p>
  )
}

function problemText(t: TFunction<'wordfeud'>, code: string): string {
  switch (code) {
    case 'no_tiles': return t('play.problem.no_tiles')
    case 'too_many_tiles': return t('play.problem.too_many_tiles')
    case 'off_board': return t('play.problem.off_board')
    case 'occupied': return t('play.problem.occupied')
    case 'duplicate': return t('play.problem.duplicate')
    case 'not_in_line': return t('play.problem.not_in_line')
    case 'gap': return t('play.problem.gap')
    case 'not_connected': return t('play.problem.not_connected')
    case 'first_move_center': return t('play.problem.first_move_center')
    case 'too_short': return t('play.problem.too_short')
    case 'bad_letter': return t('play.problem.bad_letter')
    case 'not_in_rack': return t('play.problem.not_in_rack')
    default: return t('play.error.unknown', { code })
  }
}

function errorText(t: TFunction<'wordfeud'>, code: string): string {
  switch (code) {
    case 'illegal_word': return t('play.error.illegal_word')
    case 'illegal_tiles': return t('play.error.illegal_tiles')
    case 'not_your_turn': return t('play.error.not_your_turn')
    case 'game_over': return t('play.error.game_over')
    case 'bag_too_small': return t('play.error.bag_too_small')
    case 'wordfeud_unreachable': return t('play.error.wordfeud_unreachable')
    default: return problemText(t, code)
  }
}
