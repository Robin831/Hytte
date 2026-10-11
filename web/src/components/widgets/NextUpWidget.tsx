import { type ReactNode } from 'react'
import { Link } from 'react-router'
import { CalendarDays, ClipboardCheck, Clock, Flag, Gamepad2, ListChecks, MessageCircle, Plane, Timer, Zap } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useAuth } from '../../auth'
import Widget from '../Widget'
import { daysBetween, useWidgetData } from './useWidgetData'
import {
  conversationsSource, isOpenChore, myChoresSource, pendingApprovalsSource, racesSource, strideSource, todaysEventsSource,
  todaysSession, tripsSource, upcomingTrips, watchedRaces, wordfeudSource, workHoursSource,
} from './sources'
import { useCurrentTime } from '../../hooks/useCurrentTime'
import { dateIn, toLang, tripState, txt } from '../../pages/trips/tripsApi'
import { daysUntil } from '../../pages/races/racesApi'
import { useRaceFormat } from '../../pages/races/useRaceFormat'

interface Item {
  id: string
  /** Lower comes first: things for right now, then dated things by days left. */
  rank: number
  icon: ReactNode
  text: string
  to: string
}

const MAX_ITEMS = 6
/** Dated things start ranking here, plus the days left, so "now" always wins. */
const LATER = 100
const TRIP_HORIZON_DAYS = 7
const RACE_HORIZON_DAYS = 14
const DEADLINE_HORIZON_DAYS = 7

/**
 * The handful of things that matter most right now, across every feature the
 * user has: what to do today first, then what is coming up within days.
 */
export default function NextUpWidget() {
  const { t, i18n } = useTranslation('dashboard')
  const { t: tRaces, daysLeft } = useRaceFormat()
  const { hasFeature, familyStatus } = useAuth()
  const now = useCurrentTime()
  const isChild = familyStatus?.is_child ?? false
  const isParent = familyStatus?.is_parent ?? false

  const workHours = useWidgetData(workHoursSource, hasFeature('work_hours')).data
  const events = useWidgetData(todaysEventsSource, hasFeature('calendar')).data
  const chores = useWidgetData(myChoresSource, hasFeature('kids_allowance') && isChild).data
  const approvals = useWidgetData(pendingApprovalsSource, hasFeature('kids_allowance') && isParent).data
  const plan = useWidgetData(strideSource, hasFeature('stride')).data
  const trips = useWidgetData(tripsSource, hasFeature('trips')).data
  const games = useWidgetData(wordfeudSource, hasFeature('wordfeud')).data
  const conversations = useWidgetData(conversationsSource, hasFeature('family_chat')).data
  const races = useWidgetData(racesSource, hasFeature('races')).data

  const items: Item[] = []
  const icon = 'shrink-0'

  // A weekday morning with nothing on the clock yet.
  const weekday = now.getDay() >= 1 && now.getDay() <= 5
  if (workHours && weekday && now.getHours() >= 6 && now.getHours() < 12 && !workHours.session && !workHours.summary?.net_minutes) {
    items.push({ id: 'punch', rank: 10, icon: <Timer size={16} className={`${icon} text-amber-300`} />, text: t('widgets.nextUp.notPunchedIn'), to: '/workhours' })
  }

  const nextEvent = (events ?? [])
    .filter((e) => !e.all_day && new Date(e.start_time).getTime() > now.getTime())
    .sort((a, b) => new Date(a.start_time).getTime() - new Date(b.start_time).getTime())[0]
  if (nextEvent) {
    const time = new Intl.DateTimeFormat(i18n.language, { hour: '2-digit', minute: '2-digit', hour12: false }).format(new Date(nextEvent.start_time))
    items.push({ id: 'event', rank: 20, icon: <CalendarDays size={16} className={`${icon} text-blue-400`} />, text: t('widgets.nextUp.event', { time, title: nextEvent.title }), to: '/calendar' })
  }

  const choresLeft = (chores ?? []).filter(isOpenChore).length
  if (choresLeft > 0) {
    items.push({ id: 'chores', rank: 30, icon: <ListChecks size={16} className={`${icon} text-amber-300`} />, text: t('widgets.chores.left', { count: choresLeft }), to: '/chores' })
  }
  if (approvals) {
    items.push({ id: 'approvals', rank: 30, icon: <ClipboardCheck size={16} className={`${icon} text-amber-300`} />, text: t('widgets.chores.toApprove', { count: approvals }), to: '/allowance' })
  }

  const session = todaysSession(plan)
  if (session?.description) {
    items.push({ id: 'session', rank: 40, icon: <Zap size={16} className={`${icon} text-blue-400`} />, text: t('widgets.nextUp.session', { session: session.description }), to: '/training/stride' })
  }

  const lang = toLang(i18n.language)
  for (const trip of upcomingTrips(trips ?? [], now)) {
    if (trip.tentative) continue
    const name = txt(trip.title, lang) || trip.route.join(' → ')
    const today = dateIn(trip.dest_tz, now)
    const plane = <Plane size={16} className={`${icon} text-blue-400`} />
    if (tripState(trip, now) === 'during') {
      const text = t('widgets.nextUp.tripDay', {
        trip: name, day: daysBetween(trip.start_date, today) + 1, total: daysBetween(trip.start_date, trip.end_date) + 1,
      })
      items.push({ id: `trip-${trip.id}`, rank: 50, icon: plane, text, to: `/trips/${trip.id}` })
      continue
    }
    const days = daysBetween(today, trip.start_date)
    if (days > TRIP_HORIZON_DAYS) continue
    const text = days === 1 ? t('widgets.nextUp.tripTomorrow', { trip: name }) : t('widgets.nextUp.tripLeaving', { trip: name, count: days })
    items.push({ id: `trip-${trip.id}`, rank: LATER + days, icon: plane, text, to: `/trips/${trip.id}` })
  }

  const myTurn = (games ?? []).filter((g) => g.is_my_turn).length
  if (myTurn > 0) {
    items.push({ id: 'wordfeud', rank: 60, icon: <Gamepad2 size={16} className={`${icon} text-green-400`} />, text: t('widgets.wordfeud.yourTurn', { count: myTurn }), to: '/wordfeud' })
  }

  const unread = (conversations ?? []).reduce((sum, c) => sum + c.unread_count, 0)
  if (unread > 0) {
    items.push({ id: 'chat', rank: 70, icon: <MessageCircle size={16} className={`${icon} text-green-400`} />, text: t('widgets.familyChat.unread', { count: unread }), to: '/family-chat' })
  }

  if (races) {
    const watched = watchedRaces(races)
    for (const { event, deadline } of watched.deadlines) {
      const days = daysUntil(deadline.due_date)
      if (days > DEADLINE_HORIZON_DAYS) continue
      // A deadline today or tomorrow goes to the very top: it cannot be made up later.
      const text = t('widgets.nextUp.deadline', { what: tRaces(`kind.${deadline.kind}`), race: event.name, when: daysLeft(deadline.due_date) })
      items.push({ id: `deadline-${deadline.id}`, rank: days <= 1 ? 5 : LATER + days, icon: <Clock size={16} className={`${icon} text-amber-300`} />, text, to: `/races/${event.id}` })
    }
    for (const { event, running } of watched.races) {
      const days = daysUntil(event.race_date)
      if (!running || days > RACE_HORIZON_DAYS) continue
      const text = t('widgets.nextUp.race', { race: event.name, when: daysLeft(event.race_date) })
      items.push({ id: `race-${event.id}`, rank: LATER + days, icon: <Flag size={16} className={`${icon} text-blue-400`} />, text, to: `/races/${event.id}` })
    }
  }

  if (items.length === 0) return null
  const shown = items.sort((a, b) => a.rank - b.rank).slice(0, MAX_ITEMS)

  return (
    <Widget title={t('widgets.nextUp.title')} className="col-span-full">
      <ul className="grid gap-x-8 sm:grid-cols-2">
        {shown.map((item) => (
          <li key={item.id}>
            <Link to={item.to} className="flex items-center gap-3 border-t border-gray-700/60 py-2.5 text-sm text-gray-100 hover:text-blue-300">
              <span aria-hidden="true">{item.icon}</span>
              <span className="min-w-0">{item.text}</span>
            </Link>
          </li>
        ))}
      </ul>
    </Widget>
  )
}
