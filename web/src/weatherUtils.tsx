import type { TFunction } from 'i18next'

export function getWeatherIcon(symbolCode: string, size = 24, alt = '') {
  return (
    <img
      src={`/weather-icons/${symbolCode}.svg`}
      alt={alt}
      aria-hidden={alt === '' ? true : undefined}
      width={size}
      height={size}
      className="shrink-0"
      onError={(e) => {
        const img = e.currentTarget
        if (!img.src.endsWith('/weather-icons/cloudy.svg')) {
          img.src = '/weather-icons/cloudy.svg'
        }
      }}
    />
  )
}

export function getWeatherDescription(symbolCode: string, t: TFunction<'weather'>): string {
  const code = symbolCode.replace(/_day|_night|_polartwilight/g, '')
  const fallback = code.replace(/_/g, ' ')
  return t(`descriptions.${code}`, { defaultValue: fallback })
}
