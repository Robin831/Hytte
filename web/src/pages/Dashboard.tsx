import { useTranslation } from 'react-i18next'
import { useAuth } from '../auth'
import WidgetBoundary from '../components/WidgetBoundary'
import GreetingWidget from '../components/widgets/GreetingWidget'
import WeatherWidget from '../components/widgets/WeatherWidget'
import DaylightWidget from '../components/widgets/DaylightWidget'
import NorwegianFunWidget from '../components/widgets/NorwegianFunWidget'
import QuickLinksWidget from '../components/widgets/QuickLinksWidget'
import FitnessWidget from '../components/widgets/FitnessWidget'
import LactateSummaryWidget from '../components/widgets/LactateSummaryWidget'
import ActivityFeedWidget from '../components/widgets/ActivityFeedWidget'
import InfraStatusWidget from '../components/widgets/InfraStatusWidget'
import GitHubStatusWidget from '../components/widgets/GitHubStatusWidget'
import NetatmoWidget from '../components/widgets/NetatmoWidget'
import CalendarWidget from '../components/widgets/CalendarWidget'

function Dashboard() {
  const { hasFeature } = useAuth()
  const { t } = useTranslation('dashboard')

  return (
    <div className="p-6">
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
        <WidgetBoundary className="col-span-full">
          <GreetingWidget />
        </WidgetBoundary>
        <WidgetBoundary label={t('widgets.weather.title')}>
          <WeatherWidget />
        </WidgetBoundary>
        <WidgetBoundary label={t('widgets.daylight.title')}>
          <DaylightWidget />
        </WidgetBoundary>
        {hasFeature('calendar') && (
          <WidgetBoundary label={t('widgets.calendar.title')}>
            <CalendarWidget />
          </WidgetBoundary>
        )}
        {hasFeature('netatmo') && (
          <WidgetBoundary label={t('widgets.netatmo.title')}>
            <NetatmoWidget />
          </WidgetBoundary>
        )}
        {hasFeature('training') && (
          <WidgetBoundary label={t('widgets.training.title')}>
            <FitnessWidget />
          </WidgetBoundary>
        )}
        {hasFeature('lactate') && (
          <WidgetBoundary label={t('widgets.lactate.title')}>
            <LactateSummaryWidget />
          </WidgetBoundary>
        )}
        <WidgetBoundary label={t('widgets.activity.title')}>
          <ActivityFeedWidget />
        </WidgetBoundary>
        {hasFeature('infra') && (
          <WidgetBoundary label={t('widgets.infra.title')}>
            <InfraStatusWidget />
          </WidgetBoundary>
        )}
        {hasFeature('infra') && (
          <WidgetBoundary label={t('widgets.github.title')}>
            <GitHubStatusWidget />
          </WidgetBoundary>
        )}
        <WidgetBoundary label={t('widgets.norwegianWord.title')}>
          <NorwegianFunWidget />
        </WidgetBoundary>
        <WidgetBoundary label={t('widgets.quickLinks.title')}>
          <QuickLinksWidget />
        </WidgetBoundary>
      </div>
    </div>
  )
}

export default Dashboard
