import {
  Cloud,
  CloudDrizzle,
  CloudFog,
  CloudLightning,
  CloudRain,
  CloudSnow,
  CloudSun,
  Sun,
} from 'lucide-react'

export function getWeatherIcon(symbolCode: string, size = 24) {
  const code = symbolCode.replace(/_day|_night|_polartwilight/g, '')
  const props = { size, className: 'shrink-0' }
  if (code.includes('thunder')) return <CloudLightning {...props} />
  if (code.includes('snow') || code.includes('sleet')) return <CloudSnow {...props} />
  if (code.includes('drizzle') || code.includes('lightrain')) return <CloudDrizzle {...props} />
  if (code.includes('heavyrain') || code.includes('rain')) return <CloudRain {...props} />
  if (code.includes('fog')) return <CloudFog {...props} />
  if (code === 'clearsky') return <Sun {...props} />
  if (code === 'fair' || code.includes('partlycloudy')) return <CloudSun {...props} />
  return <Cloud {...props} />
}

export function getWeatherDescription(symbolCode: string, t: (key: string, options?: Record<string, unknown>) => string): string {
  const code = symbolCode.replace(/_day|_night|_polartwilight/g, '')
  const fallback = code.replace(/_/g, ' ')
  return t(`descriptions.${code}`, { defaultValue: fallback })
}
