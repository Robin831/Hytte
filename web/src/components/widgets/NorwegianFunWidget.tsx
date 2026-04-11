import { useTranslation } from 'react-i18next'
import Widget from '../Widget'

interface NorwegianEntry {
  word: string
  pronunciation?: string
  translation: string
  description: string
}

const ENTRIES: NorwegianEntry[] = [
  {
    word: 'Koselig',
    pronunciation: 'KOO-seh-lee',
    translation: 'Cozy / snug',
    description: "The Norwegian concept of warmth and togetherness — candles, blankets, and good company. Norway's answer to hygge.",
  },
  {
    word: 'Friluftsliv',
    pronunciation: 'FREE-loofts-leev',
    translation: 'Open-air life',
    description: 'A cultural philosophy of spending time outdoors in all seasons. Rain, snow, or shine — Norwegians just put on another layer.',
  },
  {
    word: 'Utepils',
    pronunciation: 'OO-teh-pills',
    translation: 'Outdoor beer',
    description: "The first beer of the year enjoyed outside when spring arrives. One of Norway's most anticipated annual events.",
  },
  {
    word: 'Dugnad',
    pronunciation: 'DOOG-nahd',
    translation: 'Communal work',
    description: 'Voluntary collective work — cleaning the neighbourhood, painting the fence, tidying the cabin. Norway runs on dugnad.',
  },
  {
    word: 'Hytteliv',
    pronunciation: 'HYTT-eh-leev',
    translation: 'Cabin life',
    description: 'The sacred Norwegian tradition of retreating to a mountain or fjord cabin. About 500,000 Norwegians own a hytte.',
  },
  {
    word: 'Pålegg',
    pronunciation: 'POH-leg',
    translation: 'Topping / topping on bread',
    description: "Anything you put on a slice of bread. Brown cheese, salami, mackerel in tomato — if it sits on bread, it's pålegg.",
  },
  {
    word: 'Forelsket',
    pronunciation: 'for-EL-sket',
    translation: 'Euphoria of falling in love',
    description: "That overwhelming, dizzy feeling at the very beginning of love. English doesn't have a single word for it — Norwegian does.",
  },
  {
    word: 'Vinduspose',
    pronunciation: 'VIN-doos-POO-seh',
    translation: 'Window bag',
    description: 'A bag hung outside the window to keep butter, cheese, or milk at the perfect cool temperature. Low-tech Norwegian fridge.',
  },
  {
    word: 'Janteloven',
    pronunciation: 'YAN-teh-loh-ven',
    translation: 'The Jante Law',
    description: "The unwritten rule: don't think you're better than anyone else. A cultural norm that prizes equality and humility.",
  },
  {
    word: 'Karsk',
    translation: 'Moonshine coffee',
    description: 'A Northern Norwegian drink: pour moonshine (or strong spirits) into black coffee until a coin at the bottom of the cup disappears. Then drink.',
  },
  {
    word: 'Fjordhest',
    pronunciation: 'FYORD-hest',
    translation: 'Fjord horse',
    description: "One of the world's oldest and purest horse breeds, found in Norway for over 4,000 years. Known for their distinctive dun coat and carved mane.",
  },
  {
    word: 'Solvind',
    pronunciation: 'SOL-vinn',
    translation: 'Sun wind',
    description: 'A light, warm summer breeze under sunshine. Not really a dictionary word, but Norwegians understand it immediately — and they live for it.',
  },
  {
    word: 'Mørketid',
    pronunciation: 'MUR-keh-teed',
    translation: 'Dark time',
    description: "The polar night period in Northern Norway when the sun doesn't rise for weeks. Locals counter it with candles, skiing, and extraordinary resilience.",
  },
  {
    word: 'Rusledag',
    pronunciation: 'ROOS-leh-dahg',
    translation: 'Wandering day',
    description: 'A day dedicated to aimless walking — no destination, no hurry. A thoroughly Norwegian idea that other cultures pay therapists to discover.',
  },
  {
    word: 'Epleslang',
    translation: 'Apple snake',
    description: 'Norwegian word for the peel of an apple — literally "apple snake". Because when you peel an apple in one long spiral, it looks like a snake.',
  },
  {
    word: 'Matpakke',
    pronunciation: 'MAHT-pahk-keh',
    translation: 'Packed lunch',
    description: 'The humble Norwegian packed lunch — typically two slices of bread in wax paper. CEOs and schoolchildren bring the same matpakke.',
  },
  {
    word: 'Glede',
    pronunciation: 'GLEH-deh',
    translation: 'Joy / gladness',
    description: 'The Norwegian word for joy, pure and simple. Often heard in "det er en glede" — it\'s a pleasure. Sounds as cheerful as it feels.',
  },
  {
    word: 'Snøballkrig',
    pronunciation: 'SNUR-ball-krig',
    translation: 'Snowball war',
    description: 'A proper snowball fight. Norway has codified this into near-military terminology. Losing is not an option in January.',
  },
  {
    word: 'Tyttebær',
    pronunciation: 'TIT-teh-baer',
    translation: 'Lingonberry',
    description: 'The small red berry that grows wild across Norwegian forests and heaths. Goes on everything: meatballs, porridge, lefse, and straight from the bush.',
  },
  {
    word: 'Kaffe kopp',
    pronunciation: 'KAF-feh kop',
    translation: 'Coffee cup',
    description: 'Norway is the second highest coffee-consuming nation per capita. A kaffe kopp is refilled approximately every 20 minutes in any Norwegian workplace.',
  },
]

/** Pick a deterministic entry based on the UTC day of the year. */
function getTodayEntry(): NorwegianEntry {
  const now = new Date()
  const year = now.getUTCFullYear()
  // Use UTC midnight for both dates so DST transitions don't shift the day count.
  const startOfYear = Date.UTC(year, 0, 1)
  const today = Date.UTC(year, now.getUTCMonth(), now.getUTCDate())
  const dayOfYear = Math.floor((today - startOfYear) / 86400000)
  return ENTRIES[dayOfYear % ENTRIES.length]
}

export default function NorwegianFunWidget() {
  const { t } = useTranslation('dashboard')
  const entry = getTodayEntry()

  return (
    <Widget title={t('widgets.norwegianWord.title')}>
      <div className="space-y-3">
        <div>
          <p className="text-2xl font-bold text-white">{entry.word}</p>
          {entry.pronunciation && (
            <p className="text-xs text-gray-500 mt-0.5 font-mono">[{entry.pronunciation}]</p>
          )}
        </div>
        <p className="text-sm text-blue-400 font-medium">{entry.translation}</p>
        <p className="text-sm text-gray-300 leading-relaxed">{entry.description}</p>
        <p className="text-xs text-gray-600 pt-1">{t('widgets.norwegianWord.footer', { count: ENTRIES.length })}</p>
      </div>
    </Widget>
  )
}
