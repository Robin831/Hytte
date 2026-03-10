import type { RecentLocation } from './recentLocations'

// Shared list of Norwegian cities used in Weather and Settings pages.
// Must stay in sync with NorwegianLocations in internal/weather/handler.go.
export const NORWEGIAN_CITIES = [
  'Alta',
  'Ålesund',
  'Bergen',
  'Bodø',
  'Drammen',
  'Fredrikstad',
  'Haugesund',
  'Kristiansand',
  'Lillehammer',
  'Molde',
  'Narvik',
  'Oslo',
  'Stavanger',
  'Trondheim',
  'Tromsø',
]

// Full location data with coordinates, matching internal/weather/handler.go.
export const NORWEGIAN_LOCATIONS: Record<string, RecentLocation> = {
  'Alta': { name: 'Alta', lat: 69.9689, lon: 23.2716 },
  'Ålesund': { name: 'Ålesund', lat: 62.4722, lon: 6.1495 },
  'Bergen': { name: 'Bergen', lat: 60.3913, lon: 5.3221 },
  'Bodø': { name: 'Bodø', lat: 67.2804, lon: 14.4049 },
  'Drammen': { name: 'Drammen', lat: 59.7441, lon: 10.2045 },
  'Fredrikstad': { name: 'Fredrikstad', lat: 59.2181, lon: 10.9298 },
  'Haugesund': { name: 'Haugesund', lat: 59.4138, lon: 5.2680 },
  'Kristiansand': { name: 'Kristiansand', lat: 58.1599, lon: 8.0182 },
  'Lillehammer': { name: 'Lillehammer', lat: 61.1153, lon: 10.4662 },
  'Molde': { name: 'Molde', lat: 62.7375, lon: 7.1591 },
  'Narvik': { name: 'Narvik', lat: 68.4385, lon: 17.4272 },
  'Oslo': { name: 'Oslo', lat: 59.9139, lon: 10.7522 },
  'Stavanger': { name: 'Stavanger', lat: 58.9700, lon: 5.7331 },
  'Trondheim': { name: 'Trondheim', lat: 63.4305, lon: 10.3951 },
  'Tromsø': { name: 'Tromsø', lat: 69.6492, lon: 18.9553 },
}
