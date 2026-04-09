import type { TFunction } from 'i18next'

export function getWeatherIcon(symbolCode: string, size = 24) {
  return (
    <img
      src={`/weather-icons/${symbolCode}.svg`}
      alt={symbolCode.replace(/_/g, ' ')}
      width={size}
      height={size}
      className="shrink-0"
    />
  )
}

export function getWeatherDescription(symbolCode: string, t: TFunction<'weather'>): string {
  const code = symbolCode.replace(/_day|_night|_polartwilight/g, '')
  const fallback = code.replace(/_/g, ' ')
  return t(`descriptions.${code}`, { defaultValue: fallback })
}
