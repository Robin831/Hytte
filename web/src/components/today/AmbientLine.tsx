import { useTranslation } from 'react-i18next'
import { useForecast } from '../../hooks/useForecast'
import { getWeatherDescription } from '../../weatherUtils'

function greetingPeriod(hour: number): 'morning' | 'afternoon' | 'evening' | 'night' {
  if (hour >= 5 && hour <= 11) return 'morning'
  if (hour >= 12 && hour <= 17) return 'afternoon'
  if (hour >= 18 && hour <= 21) return 'evening'
  return 'night'
}

export default function AmbientLine({ firstName }: { firstName?: string }) {
  const { t, i18n } = useTranslation('today')
  const { t: tWeather } = useTranslation('weather')
  const { loading, error, data } = useForecast()

  const period = greetingPeriod(new Date().getHours())
  const greeting = firstName
    ? t(`greeting.${period}`, { name: firstName })
    : t(`greeting.${period}Guest`)

  const current = data?.properties?.timeseries?.[0]
  const temp = current?.data.instant.details.air_temperature
  const symbol =
    current?.data.next_1_hours?.summary.symbol_code ??
    current?.data.next_6_hours?.summary.symbol_code

  let ambient: string | null = null
  if (temp !== undefined && symbol) {
    const condition = getWeatherDescription(symbol, tWeather)
    const tempStr = new Intl.NumberFormat(i18n.language, { maximumFractionDigits: 0 }).format(temp)
    ambient = t('ambient.summary', { condition, temp: tempStr })
  } else if (loading && !error) {
    ambient = t('ambient.loading')
  }

  return (
    <p className="text-xs text-gray-500 mt-1">
      {greeting}
      {ambient && <span> · {ambient}</span>}
    </p>
  )
}
