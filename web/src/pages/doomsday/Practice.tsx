import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Lightbulb } from 'lucide-react'
import {
  centuryAnchor, diagnose, isLeap, isoDate, monthDoomsday, randomDate, solve, yearDoomsday,
  type Level, type SimpleDate, type Slip, type Weekday,
} from './doomsday'
import { centuryStart, dateText, dayMonthText, isPast, monthName, weekdayName } from './format'
import { MiniCalendar, SolutionView, WeekdayButtons } from './Parts'

export type Drill = 'anchor' | 'year' | 'month' | 'count'
const DRILLS: Drill[] = ['anchor', 'year', 'month', 'count']
export const MASTER_SECONDS = 30

/** All the dates that are ever a month's doomsday, for multiple choice. */
const DOOM_DATES = [3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 14, 28, 29]

type Kind = 'weekday' | 'date'

interface Question {
  kind: Kind
  date: SimpleDate
  prompt: string
  answer: number // weekday, or the doomsday date for 'date' questions
  options?: number[] // for 'date' questions
  hints: string[]
}

interface Outcome {
  picked: number | null // null = time ran out
  correct: boolean
  ms: number
  slip: Slip | 'timeout' | null
}

function pickOptions(correct: number, rand: () => number): number[] {
  const others = DOOM_DATES.filter(d => d !== correct).sort(() => rand() - 0.5).slice(0, 5)
  return [...others, correct].sort((a, b) => a - b)
}

async function record(body: Record<string, unknown>) {
  await fetch('/api/math/doomsday/attempts', {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
}

export default function Practice({ onSaved }: { onSaved?: () => void }) {
  const { t, i18n } = useTranslation('doomsday')
  const lang = i18n.language
  const [level, setLevel] = useState<Level | null>(null)
  const [drill, setDrill] = useState<Drill>('year')
  const [step, setStep] = useState(0) // level 1: 0–3
  const [q, setQ] = useState<Question | null>(null)
  const [outcome, setOutcome] = useState<Outcome | null>(null)
  const [hintsShown, setHintsShown] = useState(0)
  const [elapsed, setElapsed] = useState(0)
  const [session, setSession] = useState({ correct: 0, total: 0, streak: 0 })
  const started = useRef(0)
  const totalHints = useRef(0)

  const wd = useCallback((w: number) => weekdayName(w as Weekday, lang), [lang])

  // Builds the question for a level (and, on level 1, a step of a date).
  const build = useCallback((lvl: Level, stepIdx: number, date: SimpleDate | null, dr: Drill): Question => {
    const d = date ?? randomDate(lvl)
    const s = solve(d)
    const full = (): Question => ({
      kind: 'weekday', date: d, answer: s.answer,
      prompt: `${dateText(d, lang)} — ${isPast(d) ? t('practice.whatDay') : t('practice.whatDayFuture')}`,
      hints: [
        t('practice.hintAnchor', { day: wd(s.anchor) }),
        t('practice.hintYear', { year: d.y, day: wd(s.yearDay) }),
        t('practice.hintMonth', { date: dayMonthText(s.monthDay, d.m, lang) }),
        t('practice.hintCount', { d: d.d, md: s.monthDay, mod: s.diffMod }),
      ],
    })
    const anchorQ = (): Question => ({
      kind: 'weekday', date: d, answer: centuryAnchor(d.y), prompt: t('practice.qAnchor', { century: centuryStart(d.y) }),
      hints: [t('learn.step1Body')],
    })
    const yearQ = (): Question => ({
      kind: 'weekday', date: d, answer: yearDoomsday(d.y), prompt: t('practice.qYear', { year: d.y }),
      hints: [t('practice.hintAnchor', { day: wd(s.anchor) }), `${t('learn.oddRule1')} ${t('learn.oddRule2')} ${t('learn.oddRule3')} ${t('learn.oddRule4')} ${t('learn.oddRule5')}`],
    })
    const monthQ = (): Question => ({
      kind: 'date', date: d, answer: monthDoomsday(d.m, isLeap(d.y)), options: pickOptions(monthDoomsday(d.m, isLeap(d.y)), Math.random),
      prompt: t('practice.qMonth', { month: monthName(d.m, lang), year: d.y }),
      hints: [d.m <= 2 ? t('learn.janFeb') : d.m === 3 ? t('learn.march') : d.m % 2 === 0 ? t('learn.evenMonths') : t('learn.nineToFive')],
    })
    const countQ = (): Question => ({
      kind: 'weekday', date: d, answer: s.answer,
      prompt: t('practice.qCount', { day: wd(s.yearDay), date: dayMonthText(d.d, d.m, lang) }),
      hints: [t('practice.hintMonth', { date: dayMonthText(s.monthDay, d.m, lang) }), t('practice.hintCount', { d: d.d, md: s.monthDay, mod: s.diffMod })],
    })
    if (lvl === 1) return [anchorQ, yearQ, monthQ, full][stepIdx]()
    if (lvl === 2) return { anchor: anchorQ, year: yearQ, month: monthQ, count: countQ }[dr]()
    return full()
  }, [lang, t, wd])

  const start = useCallback((lvl: Level, dr: Drill) => {
    setLevel(lvl)
    setStep(0)
    setOutcome(null)
    setHintsShown(0)
    totalHints.current = 0
    setQ(build(lvl, 0, null, dr))
    started.current = performance.now()
    setElapsed(0)
  }, [build])

  const next = useCallback(() => {
    if (!level || !q) return
    setOutcome(null)
    setHintsShown(0)
    if (level === 1 && step < 3) {
      setStep(step + 1)
      setQ(build(1, step + 1, q.date, drill))
      return
    }
    totalHints.current = 0
    setStep(0)
    setQ(build(level, 0, null, drill))
    started.current = performance.now()
    setElapsed(0)
  }, [build, drill, level, q, step])

  const answer = useCallback((picked: number | null) => {
    if (!q || outcome || !level) return
    const ms = Math.round(performance.now() - started.current)
    const correct = picked === q.answer
    let slip: Outcome['slip'] = null
    const finalStep = level !== 1 || step === 3
    if (picked === null) slip = 'timeout'
    else if (!correct && q.kind === 'weekday' && (level >= 3 || (level === 1 && step === 3))) slip = diagnose(q.date, picked as Weekday)
    setOutcome({ picked, correct, ms, slip })
    totalHints.current += hintsShown
    if (!finalStep) return
    setSession(s => ({ correct: s.correct + (correct ? 1 : 0), total: s.total + 1, streak: correct ? s.streak + 1 : 0 }))
    const body = {
      level, drill: level === 2 ? drill : '', date: isoDate(q.date), response_ms: ms, hints: totalHints.current,
      answer: picked === null ? -1 : q.kind === 'weekday' ? picked : 0, correct, slip: slip ?? '',
    }
    record(body).then(() => onSaved?.()).catch(() => {})
  }, [drill, hintsShown, level, onSaved, outcome, q, step])

  // Stopwatch / countdown.
  useEffect(() => {
    if (!q || outcome || (level !== 4 && level !== 5)) return
    const id = window.setInterval(() => {
      const secs = (performance.now() - started.current) / 1000
      setElapsed(secs)
      if (level === 5 && secs >= MASTER_SECONDS) answer(null)
    }, 200)
    return () => window.clearInterval(id)
  }, [answer, level, outcome, q])

  // Keys 0–6 answer weekday questions (0 = Sunday, as the method counts); Enter goes on.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.target instanceof HTMLInputElement || e.target instanceof HTMLSelectElement) return
      if (e.key === 'Enter' && outcome) {
        e.preventDefault()
        next()
        return
      }
      const n = Number(e.key)
      if (q?.kind === 'weekday' && !outcome && /^[0-6]$/.test(e.key)) answer(n as Weekday)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [answer, next, outcome, q])

  if (!level || !q) {
    return <LevelPicker drill={drill} onDrill={setDrill} onPick={lvl => start(lvl, drill)} />
  }

  const hintsAllowed = level !== 5
  const showSolution = outcome && q.kind === 'weekday' && (level >= 3 || (level === 1 && step === 3) || (level === 2 && drill === 'count'))
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-gray-400">
        <button type="button" onClick={() => { setLevel(null); setQ(null) }} className="underline hover:text-white cursor-pointer">{t('practice.changeLevel')}</button>
        <span>
          {t('practice.session', { correct: session.correct, total: session.total })} · {t('practice.streak', { count: session.streak })}
        </span>
      </div>

      <div className="rounded-lg border border-gray-800 bg-gray-800/30 p-4 space-y-4">
        {level === 1 && <p className="text-xs text-gray-400">{t('practice.stepOf', { step: step + 1 })} · {dateText(q.date, lang)}</p>}
        <div className="flex items-start justify-between gap-3">
          <h2 className="text-lg sm:text-xl font-semibold">{q.prompt}</h2>
          {(level === 4 || level === 5) && (
            <span className={`shrink-0 tabular-nums text-lg ${level === 5 && MASTER_SECONDS - elapsed < 10 ? 'text-red-400' : 'text-gray-300'}`}>
              {level === 5 ? Math.max(0, Math.ceil(MASTER_SECONDS - elapsed)) : Math.floor(elapsed)}s
            </span>
          )}
        </div>

        {q.kind === 'weekday' ? (
          <WeekdayButtons
            onPick={w => answer(w)}
            disabled={!!outcome}
            picked={outcome && !outcome.correct ? (outcome.picked as Weekday | null) : null}
            correct={outcome ? (q.answer as Weekday) : null}
          />
        ) : (
          <div className="grid grid-cols-3 sm:grid-cols-6 gap-2">
            {q.options?.map(o => {
              let style = 'bg-gray-800 border-gray-700 hover:border-blue-500'
              if (outcome && o === q.answer) style = 'bg-emerald-700 border-emerald-500'
              else if (outcome && o === outcome.picked) style = 'bg-red-800 border-red-600'
              return (
                <button key={o} type="button" disabled={!!outcome} onClick={() => answer(o)} className={`rounded-lg border px-2 py-3 font-medium cursor-pointer disabled:cursor-default ${style}`}>
                  {dayMonthText(o, q.date.m, lang)}
                </button>
              )
            })}
          </div>
        )}
        {q.kind === 'weekday' && !outcome && <p className="text-[11px] text-gray-500">{t('practice.keys')}</p>}

        {!outcome && (hintsAllowed ? (
          <div className="space-y-1">
            {q.hints.slice(0, hintsShown).map((h, i) => (
              <p key={i} className="text-sm text-amber-200 flex gap-2"><Lightbulb size={14} className="mt-0.5 shrink-0" aria-hidden="true" />{h}</p>
            ))}
            {hintsShown < q.hints.length && (
              <button type="button" onClick={() => setHintsShown(h => h + 1)} className="inline-flex items-center gap-1 text-xs text-amber-300 hover:text-amber-200 cursor-pointer">
                <Lightbulb size={12} aria-hidden="true" /> {t('practice.hint')}
              </button>
            )}
          </div>
        ) : <p className="text-xs text-gray-500">{t('practice.noHints')}</p>)}

        {outcome && (
          <div className="space-y-3" role="status">
            <p className={`font-semibold ${outcome.correct ? 'text-emerald-300' : 'text-red-300'}`}>
              {outcome.correct
                ? `${t('practice.correct')} ${t('practice.time', { seconds: (outcome.ms / 1000).toFixed(1) })}`
                : outcome.slip === 'timeout'
                  ? t('practice.timeUp', { day: q.kind === 'weekday' ? wd(q.answer) : q.answer })
                  : t('practice.wrong', { day: q.kind === 'weekday' ? wd(q.answer) : dayMonthText(q.answer, q.date.m, lang) })}
            </p>
            {outcome.slip && outcome.slip !== 'timeout' && <p className="text-sm text-amber-200">{slipText(t, outcome.slip)}</p>}
            {showSolution && (
              <div className="flex flex-col sm:flex-row gap-4 rounded bg-gray-900 p-3">
                <div className="flex-1"><SolutionView date={q.date} /></div>
                <MiniCalendar date={q.date} />
              </div>
            )}
            <button type="button" onClick={next} autoFocus className="px-4 py-2 rounded bg-blue-600 hover:bg-blue-500 text-sm font-medium cursor-pointer">
              {t('practice.next')} <span className="text-xs text-blue-200">({t('practice.nextKey')})</span>
            </button>
          </div>
        )}
      </div>
    </div>
  )
}

function slipText(t: ReturnType<typeof useTranslation<'doomsday'>>['t'], s: Slip): string {
  switch (s) {
    case 'leap': return t('practice.slip.leap')
    case 'anchor': return t('practice.slip.anchor')
    case 'firstEleven': return t('practice.slip.firstEleven')
    case 'secondEleven': return t('practice.slip.secondEleven')
    case 'noSubtract': return t('practice.slip.noSubtract')
    case 'month': return t('practice.slip.month')
    case 'offByOne': return t('practice.slip.offByOne')
    default: return t('practice.slip.unknown')
  }
}

function LevelPicker({ drill, onDrill, onPick }: { drill: Drill; onDrill: (d: Drill) => void; onPick: (l: Level) => void }) {
  const { t } = useTranslation('doomsday')
  const levels: { level: Level; title: string; blurb: string }[] = [
    { level: 1, title: t('practice.level1'), blurb: t('practice.level1Blurb') },
    { level: 2, title: t('practice.level2'), blurb: t('practice.level2Blurb') },
    { level: 3, title: t('practice.level3'), blurb: t('practice.level3Blurb') },
    { level: 4, title: t('practice.level4'), blurb: t('practice.level4Blurb') },
    { level: 5, title: t('practice.level5'), blurb: t('practice.level5Blurb', { seconds: MASTER_SECONDS }) },
  ]
  const drillLabel = (d: Drill) => d === 'anchor' ? t('practice.drill.anchor') : d === 'year' ? t('practice.drill.year') : d === 'month' ? t('practice.drill.month') : t('practice.drill.count')
  return (
    <div className="space-y-3">
      <h2 className="font-semibold">{t('practice.pickLevel')}</h2>
      <ul className="grid gap-2 sm:grid-cols-2">
        {levels.map(l => (
          <li key={l.level} className="rounded-lg border border-gray-700 bg-gray-800 p-3 space-y-2">
            <button type="button" onClick={() => onPick(l.level)} className="w-full text-left cursor-pointer">
              <p className="font-semibold"><span className="text-blue-400">{l.level}</span> · {l.title}</p>
              <p className="text-sm text-gray-400">{l.blurb}</p>
            </button>
            {l.level === 2 && (
              <div role="radiogroup" aria-label={t('practice.drill.label')} className="flex flex-wrap gap-1">
                {DRILLS.map(d => (
                  <button
                    key={d}
                    type="button"
                    role="radio"
                    aria-checked={drill === d}
                    onClick={() => onDrill(d)}
                    className={`px-2 py-1 rounded-full text-xs border cursor-pointer ${drill === d ? 'bg-blue-700 border-blue-500' : 'bg-gray-900 border-gray-700 text-gray-300'}`}
                  >
                    {drillLabel(d)}
                  </button>
                ))}
              </div>
            )}
          </li>
        ))}
      </ul>
    </div>
  )
}
