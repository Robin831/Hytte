import { Gamepad2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import Widget from '../Widget'
import WidgetLink from './WidgetLink'
import { getJSON, useWidgetData } from './useWidgetData'

interface Game {
  id: number
  opponent: string
  scores: [number, number]
  is_my_turn: boolean
}

const loadGames = (signal: AbortSignal) =>
  getJSON<{ games: Game[] | null }>('/api/wordfeud/games', signal).then((d) => d.games ?? [])

/**
 * Games waiting on you. The endpoint asks Wordfeud's servers live, so a slow
 * or failed call (or no linked account) simply leaves the widget out.
 */
export default function WordfeudWidget() {
  const { t } = useTranslation('dashboard')
  const { data: games, loaded } = useWidgetData(loadGames)
  if (!loaded || !games || games.length === 0) return null

  const mine = games.filter((g) => g.is_my_turn)
  const shown = (mine.length > 0 ? mine : games).slice(0, 4)

  return (
    <Widget title={t('widgets.wordfeud.title')}>
      <p className="flex items-center gap-2 text-base font-semibold">
        <Gamepad2 size={16} className={`shrink-0 ${mine.length > 0 ? 'text-green-400' : 'text-gray-500'}`} aria-hidden="true" />
        {mine.length > 0
          ? t('widgets.wordfeud.yourTurn', { count: mine.length })
          : t('widgets.wordfeud.waiting', { count: games.length })}
      </p>
      <ul className="mt-3 space-y-1">
        {shown.map((g) => (
          <li key={g.id} className="flex items-baseline justify-between gap-3 text-sm">
            <span className="truncate text-gray-200">{g.opponent}</span>
            <span className="shrink-0 text-xs tabular-nums text-gray-500">
              {g.scores[0]}–{g.scores[1]}
            </span>
          </li>
        ))}
      </ul>
      <WidgetLink to="/wordfeud">{t('widgets.wordfeud.open')}</WidgetLink>
    </Widget>
  )
}
