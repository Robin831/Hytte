import { lazy, type ComponentType } from 'react'
import type { ParseKeys } from 'i18next'
import GreetingWidget from './GreetingWidget'
import WeatherWidget from './WeatherWidget'
import DaylightWidget from './DaylightWidget'
import NorwegianFunWidget from './NorwegianFunWidget'
import QuickLinksWidget from './QuickLinksWidget'
import FitnessWidget from './FitnessWidget'
import LactateSummaryWidget from './LactateSummaryWidget'
import ActivityFeedWidget from './ActivityFeedWidget'
import CalendarWidget from './CalendarWidget'

// Below-the-fold widgets that fetch on mount are code-split so they do not sit
// in the initial dashboard chunk. Every lazy entry must set `lazy: true` so the
// dashboard renders it inside a Suspense boundary.
const InfraStatusWidget = lazy(() => import('./InfraStatusWidget'))
const GitHubStatusWidget = lazy(() => import('./GitHubStatusWidget'))
const NetatmoWidget = lazy(() => import('./NetatmoWidget'))

/** Feature key shared by the server-status and GitHub Actions widgets. */
const INFRA = 'infra'

export interface WidgetDef {
  /** Stable id persisted in the dashboard_widgets preference. */
  id: string
  component: ComponentType
  /** Key in the `dashboard` i18n namespace holding the widget's display name. */
  titleKey: ParseKeys<'dashboard'>
  /** Feature flag that must be enabled for the widget to render at all. */
  feature?: string
  /** Grid span class for the widget's slot, mirroring the widget's own layout. */
  colSpanClass?: string
  /** Widgets the user cannot hide (the greeting header). */
  alwaysVisible?: boolean
  /** True when `component` is a React.lazy wrapper and needs a Suspense boundary. */
  lazy?: boolean
}

/**
 * The dashboard's widgets in their default order. Ids are persisted, so
 * renaming one drops that widget out of a stored layout — add a new entry
 * instead.
 */
export const WIDGET_REGISTRY: WidgetDef[] = [
  {
    id: 'greeting',
    component: GreetingWidget,
    titleKey: 'widgets.greeting.title',
    colSpanClass: 'col-span-full',
    alwaysVisible: true,
  },
  { id: 'weather', component: WeatherWidget, titleKey: 'widgets.weather.title' },
  { id: 'daylight', component: DaylightWidget, titleKey: 'widgets.daylight.title' },
  { id: 'calendar', component: CalendarWidget, titleKey: 'widgets.calendar.title', feature: 'calendar' },
  { id: 'netatmo', component: NetatmoWidget, titleKey: 'widgets.netatmo.title', feature: 'netatmo', lazy: true },
  { id: 'training', component: FitnessWidget, titleKey: 'widgets.training.title', feature: 'training' },
  { id: 'lactate', component: LactateSummaryWidget, titleKey: 'widgets.lactate.title', feature: 'lactate' },
  { id: 'activity', component: ActivityFeedWidget, titleKey: 'widgets.activity.title' },
  { id: 'infra', component: InfraStatusWidget, titleKey: 'widgets.infra.title', feature: INFRA, lazy: true },
  { id: 'github', component: GitHubStatusWidget, titleKey: 'widgets.github.title', feature: INFRA, lazy: true },
  { id: 'norwegian_word', component: NorwegianFunWidget, titleKey: 'widgets.norwegianWord.title' },
  { id: 'quick_links', component: QuickLinksWidget, titleKey: 'widgets.quickLinks.title' },
]

/** Persisted shape of the `dashboard_widgets` preference. */
export interface DashboardLayout {
  order: string[]
  hidden: string[]
}

export const EMPTY_LAYOUT: DashboardLayout = { order: [], hidden: [] }

/**
 * Parses the raw `dashboard_widgets` preference value. Anything that is not a
 * `{order, hidden}` object of string arrays is treated as "not set" so the user
 * falls back to the default layout instead of an empty dashboard.
 */
export function parseLayout(raw: unknown): DashboardLayout | null {
  if (typeof raw !== 'string' || raw === '') return null
  let parsed: unknown
  try {
    parsed = JSON.parse(raw)
  } catch {
    return null
  }
  // Tolerate a legacy double-encoded value (a JSON string wrapping the object).
  if (typeof parsed === 'string') {
    try {
      parsed = JSON.parse(parsed)
    } catch {
      return null
    }
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return null
  const obj = parsed as Record<string, unknown>
  const order = Array.isArray(obj.order) ? obj.order.filter((v): v is string => typeof v === 'string') : []
  const hidden = Array.isArray(obj.hidden) ? obj.hidden.filter((v): v is string => typeof v === 'string') : []
  return { order, hidden }
}

/**
 * Returns every registry widget in the user's order: stored ids first (unknown
 * ids dropped, duplicates collapsed), then any registry widget the stored order
 * does not mention, in default order. Hidden state and feature gating are not
 * applied here.
 */
export function orderedWidgets(
  layout: DashboardLayout | null,
  registry: WidgetDef[] = WIDGET_REGISTRY,
): WidgetDef[] {
  const byId = new Map(registry.map((def) => [def.id, def]))
  const result: WidgetDef[] = []
  const seen = new Set<string>()
  for (const id of layout?.order ?? []) {
    const def = byId.get(id)
    if (!def || seen.has(id)) continue
    seen.add(id)
    result.push(def)
  }
  for (const def of registry) {
    if (!seen.has(def.id)) result.push(def)
  }
  return result
}

/** True when the user has hidden this widget (always-visible widgets never are). */
export function isHidden(def: WidgetDef, layout: DashboardLayout | null): boolean {
  if (def.alwaysVisible) return false
  return (layout?.hidden ?? []).includes(def.id)
}

/**
 * The widgets to render, in order: stored order resolved against the registry,
 * minus hidden ones. Feature gating is applied by the caller so a disabled
 * feature can never be rendered from a stored layout.
 */
export function resolveLayout(
  layout: DashboardLayout | null,
  registry: WidgetDef[] = WIDGET_REGISTRY,
): WidgetDef[] {
  return orderedWidgets(layout, registry).filter((def) => !isHidden(def, layout))
}

/** The widgets the user has hidden, in registry order. */
export function hiddenWidgets(
  layout: DashboardLayout | null,
  registry: WidgetDef[] = WIDGET_REGISTRY,
): WidgetDef[] {
  return orderedWidgets(layout, registry).filter((def) => isHidden(def, layout))
}

/**
 * Moves `draggedId` next to `targetId` in a full order array, preserving the
 * direction of the move (dropping downwards lands after the target, dropping
 * upwards lands before it).
 */
export function reorderTo(order: string[], draggedId: string, targetId: string): string[] {
  if (draggedId === targetId) return order
  const from = order.indexOf(draggedId)
  const to = order.indexOf(targetId)
  if (from === -1 || to === -1) return order
  const next = order.filter((id) => id !== draggedId)
  const at = next.indexOf(targetId)
  next.splice(from < to ? at + 1 : at, 0, draggedId)
  return next
}

/**
 * Moves a widget one slot up or down among the ids currently displayed, then
 * writes that move back into the full order array. Using the displayed ids
 * keeps a single keypress to a single visible step even when hidden or
 * feature-gated widgets sit in between.
 */
export function moveWidget(
  order: string[],
  displayedIds: string[],
  id: string,
  delta: -1 | 1,
): string[] {
  const from = displayedIds.indexOf(id)
  const to = from + delta
  if (from === -1 || to < 0 || to >= displayedIds.length) return order
  return reorderTo(order, id, displayedIds[to])
}
