import { formatDate } from '../utils/formatDate'

export interface TimeseriesEntry {
  time: string
  data: {
    instant: {
      details: {
        air_temperature: number
        wind_speed: number
        relative_humidity: number
        air_pressure_at_sea_level?: number
        wind_from_direction?: number
      }
    }
    next_1_hours?: {
      summary: { symbol_code: string }
      details: { precipitation_amount: number }
    }
    next_6_hours?: {
      summary: { symbol_code: string }
      details: { precipitation_amount: number }
    }
    next_12_hours?: {
      summary: { symbol_code: string }
    }
  }
}

export interface DayForecast {
  date: string
  dayName: string
  symbolCode: string
  tempMin: number
  tempMax: number
  precipitation: number
  windSpeed: number
}

interface DaySymbolEntry {
  date: Date
  symbol: string
}

function localDateKey(dt: Date): string {
  return `${dt.getFullYear()}-${String(dt.getMonth() + 1).padStart(2, '0')}-${String(dt.getDate()).padStart(2, '0')}`
}

function minutesSinceMidnight(dt: Date): number {
  return dt.getHours() * 60 + dt.getMinutes()
}

function pickMiddaySymbol(entries: DaySymbolEntry[]): string {
  if (entries.length === 0) return 'cloudy'

  let best = entries[0]
  let bestDistance = Math.abs(minutesSinceMidnight(best.date) - 720)

  for (const entry of entries.slice(1)) {
    const distance = Math.abs(minutesSinceMidnight(entry.date) - 720)
    if (distance < bestDistance || (distance === bestDistance && entry.date.getTime() < best.date.getTime())) {
      best = entry
      bestDistance = distance
    }
  }

  return best.symbol
}

export function buildDailyForecasts(timeseries: TimeseriesEntry[], todayLabel: string): DayForecast[] {
  const dayMap = new Map<string, {
    temps: number[]
    winds: number[]
    precip: number
    symbolEntries: DaySymbolEntry[]
    date: Date
  }>()

  for (const entry of timeseries) {
    const dt = new Date(entry.time)
    const dateKey = localDateKey(dt)

    if (!dayMap.has(dateKey)) {
      dayMap.set(dateKey, { temps: [], winds: [], precip: 0, symbolEntries: [], date: dt })
    }
    const day = dayMap.get(dateKey)!

    day.temps.push(entry.data.instant.details.air_temperature)
    day.winds.push(entry.data.instant.details.wind_speed)

    const symbol =
      entry.data.next_1_hours?.summary.symbol_code ||
      entry.data.next_6_hours?.summary.symbol_code ||
      entry.data.next_12_hours?.summary.symbol_code

    if (symbol) day.symbolEntries.push({ date: dt, symbol })

    const precip =
      entry.data.next_1_hours?.details.precipitation_amount ??
      entry.data.next_6_hours?.details.precipitation_amount ??
      0
    day.precip += precip
  }

  const today = localDateKey(new Date())

  const sortedDays = [...dayMap.entries()].sort(([a], [b]) => a.localeCompare(b))

  return sortedDays.slice(0, 7).map(([dateKey, data]) => {
    const dayName =
      dateKey === today
        ? todayLabel
        : formatDate(data.date, { weekday: 'short' })

    return {
      date: dateKey,
      dayName,
      symbolCode: pickMiddaySymbol(data.symbolEntries),
      tempMin: Math.round(Math.min(...data.temps)),
      tempMax: Math.round(Math.max(...data.temps)),
      precipitation: Math.round(data.precip * 10) / 10,
      windSpeed: Math.round(data.winds.reduce((a, b) => a + b, 0) / data.winds.length * 10) / 10,
    }
  })
}
