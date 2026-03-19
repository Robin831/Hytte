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

export function getWeatherDescription(symbolCode: string): string {
  const code = symbolCode.replace(/_day|_night|_polartwilight/g, '')
  const descriptions: Record<string, string> = {
    clearsky: 'Clear sky',
    fair: 'Fair',
    partlycloudy: 'Partly cloudy',
    cloudy: 'Cloudy',
    lightrainshowers: 'Light rain showers',
    rainshowers: 'Rain showers',
    heavyrainshowers: 'Heavy rain showers',
    lightrainshowersandthunder: 'Light rain & thunder',
    rainshowersandthunder: 'Rain & thunder',
    heavyrainshowersandthunder: 'Heavy rain & thunder',
    lightsleetshowers: 'Light sleet showers',
    sleetshowers: 'Sleet showers',
    heavysleetshowers: 'Heavy sleet showers',
    lightsnowshowers: 'Light snow showers',
    snowshowers: 'Snow showers',
    heavysnowshowers: 'Heavy snow showers',
    lightrain: 'Light rain',
    rain: 'Rain',
    heavyrain: 'Heavy rain',
    lightrainandthunder: 'Light rain & thunder',
    rainandthunder: 'Rain & thunder',
    heavyrainandthunder: 'Heavy rain & thunder',
    lightsleet: 'Light sleet',
    sleet: 'Sleet',
    heavysleet: 'Heavy sleet',
    lightsnow: 'Light snow',
    snow: 'Snow',
    heavysnow: 'Heavy snow',
    fog: 'Fog',
  }
  return descriptions[code] ?? code.replace(/_/g, ' ')
}
