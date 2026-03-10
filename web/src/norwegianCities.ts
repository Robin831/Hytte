// Shared list of Norwegian cities with coordinates, used in Weather and Settings pages.
// Must stay in sync with NorwegianLocations in internal/weather/handler.go.

export interface CityLocation {
  name: string
  lat: number
  lon: number
}

export const NORWEGIAN_CITY_DATA: CityLocation[] = [
  { name: 'Alta', lat: 69.9689, lon: 23.2716 },
  { name: 'Ålesund', lat: 62.4722, lon: 6.1495 },
  { name: 'Bergen', lat: 60.3913, lon: 5.3221 },
  { name: 'Bodø', lat: 67.2804, lon: 14.4049 },
  { name: 'Drammen', lat: 59.7441, lon: 10.2045 },
  { name: 'Fredrikstad', lat: 59.2181, lon: 10.9298 },
  { name: 'Haugesund', lat: 59.4138, lon: 5.2680 },
  { name: 'Kristiansand', lat: 58.1599, lon: 8.0182 },
  { name: 'Lillehammer', lat: 61.1153, lon: 10.4662 },
  { name: 'Molde', lat: 62.7375, lon: 7.1591 },
  { name: 'Narvik', lat: 68.4385, lon: 17.4272 },
  { name: 'Oslo', lat: 59.9139, lon: 10.7522 },
  { name: 'Stavanger', lat: 58.9700, lon: 5.7331 },
  { name: 'Trondheim', lat: 63.4305, lon: 10.3951 },
  { name: 'Tromsø', lat: 69.6492, lon: 18.9553 },
]

// Simple name array for backwards compatibility.
export const NORWEGIAN_CITIES = NORWEGIAN_CITY_DATA.map((c) => c.name)
