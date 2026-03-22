# Hytte Internationalization (i18n) Plan

**Target languages:** English (en, existing), Norwegian Bokmal (nb), Thai (th)
**Date:** 2026-03-22

---

## 1. Recommended Library: react-i18next

**Choice:** `react-i18next` + `i18next` (with `i18next-browser-languagedetector`)

**Why react-i18next over alternatives:**

- **De facto standard for React i18n** -- by far the most widely used library in the React ecosystem (millions of weekly npm downloads). Large community, mature tooling, excellent docs.
- **Framework-agnostic core** -- Hytte uses plain React + Vite (not Next.js), so `next-intl` is out. `react-intl` (FormatJS) is viable but has a heavier API surface and forces `<FormattedMessage>` components for everything. react-i18next offers both hook (`useTranslation`) and component APIs with less boilerplate.
- **Namespace support** -- cleanly splits translations per page/feature, matching Hytte's page-per-file architecture.
- **Built-in interpolation, pluralization, and context** -- handles Norwegian and Thai plural rules natively via ICU-like syntax.
- **Lazy loading** -- namespaces can be loaded on demand, keeping the initial bundle small.
- **Vite-friendly** -- works out of the box with Vite, no special plugins required.
- **TypeScript support** -- full type safety for translation keys via `i18next` module augmentation.

**Packages to install:**

```bash
npm install i18next react-i18next i18next-browser-languagedetector i18next-http-backend
```

- `i18next` -- core library
- `react-i18next` -- React bindings (hooks, components, HOC)
- `i18next-browser-languagedetector` -- auto-detects user language from browser/cookie/localStorage
- `i18next-http-backend` -- lazy-loads translation JSON files from `/locales/` at runtime (avoids bundling all languages)

---

## 2. Setup and Configuration

### 2.1 i18n initialization file

Create `web/src/i18n.ts`:

```typescript
import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import Backend from 'i18next-http-backend'
import LanguageDetector from 'i18next-browser-languagedetector'

i18n
  .use(Backend)
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    fallbackLng: 'en',
    supportedLngs: ['en', 'nb', 'th'],
    defaultNS: 'common',
    ns: ['common'],
    interpolation: {
      escapeValue: false, // React already escapes
    },
    backend: {
      loadPath: '/locales/{{lng}}/{{ns}}.json',
    },
    detection: {
      order: ['localStorage', 'navigator'],
      caches: ['localStorage'],
      lookupLocalStorage: 'hytte-language',
    },
  })

export default i18n
```

### 2.2 Wire into app entry point

In `web/src/main.tsx`, import `./i18n` before rendering:

```typescript
import './i18n'       // <-- add this before App import
import App from './App.tsx'
// ... rest of the file unchanged
```

Add a `<Suspense>` fallback around `<App>` (i18next-http-backend loads async):

```typescript
import { Suspense } from 'react'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <Suspense fallback={<div className="flex items-center justify-center min-h-screen bg-gray-900" />}>
      <BrowserRouter>
        <AuthProvider>
          <App />
        </AuthProvider>
      </BrowserRouter>
    </Suspense>
  </StrictMode>,
)
```

### 2.3 TypeScript type safety

Create `web/src/i18next.d.ts` for key autocompletion:

```typescript
import 'i18next'
import type common from '../public/locales/en/common.json'

declare module 'i18next' {
  interface CustomTypeOptions {
    defaultNS: 'common'
    resources: {
      common: typeof common
    }
  }
}
```

---

## 3. Translation File Structure

### Directory layout

```
web/public/locales/
  en/
    common.json        # Shared: nav, buttons, errors, date labels
    weather.json       # Weather page strings + descriptions
    training.json      # Training page strings
    settings.json      # Settings page strings
    dashboard.json     # Dashboard/widgets strings
    lactate.json       # Lactate test pages
    infra.json         # Infra page
    notes.json         # Notes page
    chat.json          # Chat page
  nb/
    common.json
    weather.json
    training.json
    settings.json
    dashboard.json
    lactate.json
    infra.json
    notes.json
    chat.json
  th/
    common.json
    weather.json
    training.json
    settings.json
    dashboard.json
    lactate.json
    infra.json
    notes.json
    chat.json
```

**Why this namespace split:**
- Maps 1:1 to Hytte's page-per-file architecture
- Each namespace loads only when that page is visited (via `useTranslation('training')`)
- `common` namespace holds shared strings (nav labels, generic buttons like "Save", "Cancel", "Delete", error messages)
- Keeps individual files manageable (20-80 keys each rather than one 500-key monolith)

### Example: `en/common.json`

```json
{
  "appName": "Hytte",
  "tagline": "Your cozy corner of the web",
  "nav": {
    "home": "Home",
    "dashboard": "Dashboard",
    "weather": "Weather",
    "calendar": "Calendar",
    "webhooks": "Webhooks",
    "notes": "Notes",
    "chat": "Chat",
    "training": "Training",
    "lactate": "Lactate",
    "infra": "Infra",
    "links": "Links",
    "settings": "Settings",
    "admin": "Admin",
    "login": "Login",
    "logout": "Logout"
  },
  "actions": {
    "save": "Save",
    "cancel": "Cancel",
    "delete": "Delete",
    "confirm": "Confirm",
    "close": "Close",
    "refresh": "Refresh",
    "upload": "Upload",
    "search": "Search",
    "back": "Back"
  },
  "status": {
    "loading": "Loading...",
    "saving": "Saving...",
    "error": "Something went wrong",
    "offline": "Offline",
    "online": "Online",
    "checking": "Checking..."
  },
  "api": {
    "label": "API: {{status}}"
  },
  "greeting": {
    "morning": "Good morning",
    "afternoon": "Good afternoon",
    "evening": "Good evening"
  }
}
```

### Example: `nb/common.json`

```json
{
  "appName": "Hytte",
  "tagline": "Ditt koselige hjorne pa nettet",
  "nav": {
    "home": "Hjem",
    "dashboard": "Oversikt",
    "weather": "Vaer",
    "calendar": "Kalender",
    "training": "Trening",
    "settings": "Innstillinger",
    "login": "Logg inn",
    "logout": "Logg ut"
  },
  "greeting": {
    "morning": "God morgen",
    "afternoon": "God ettermiddag",
    "evening": "God kveld"
  }
}
```

### Namespace loading per page

Each page component loads its own namespace:

```typescript
// In Training.tsx
const { t } = useTranslation(['training', 'common'])
// t('training:pageTitle') -- from training namespace
// t('common:actions.save') -- from common namespace
```

---

## 4. Language Switcher UI

### Approach

Add a language selector to **two locations**:

1. **Sidebar footer** (compact) -- a small globe icon + current language code, clicking opens a dropdown
2. **Settings page** (full) -- a labeled dropdown under a "Language" section

### Implementation

Create `web/src/components/LanguageSwitcher.tsx`:

```typescript
import { useTranslation } from 'react-i18next'
import { Globe } from 'lucide-react'

const languages = [
  { code: 'en', label: 'English', flag: 'EN' },
  { code: 'nb', label: 'Norsk (Bokmal)', flag: 'NO' },
  { code: 'th', label: 'ไทย', flag: 'TH' },
]

export default function LanguageSwitcher({ compact = false }) {
  const { i18n } = useTranslation()

  const changeLanguage = (lng: string) => {
    i18n.changeLanguage(lng)
    // Also update document lang attribute for accessibility
    document.documentElement.lang = lng
  }

  // ... render dropdown with languages
}
```

### Key details

- Persist selection to `localStorage` key `hytte-language` (the detector plugin handles this automatically when `changeLanguage` is called)
- Set `<html lang="...">` attribute on language change for accessibility/SEO
- Show native script names in the dropdown: "English", "Norsk (Bokmal)", "ไทย" -- users should recognize their own language
- In the sidebar collapsed state, show just the globe icon; expanded state shows "EN" / "NO" / "TH" badge

### Integration with user preferences (optional enhancement)

Store preferred language as a user preference in the backend (`language` key in `user_preferences` table). On login, sync the server-side preference to the client. This way the language follows the user across devices.

---

## 5. Strategy for Extracting Existing Strings

### Current state analysis

- **~11,200 lines** across 20 pages and 16 components
- All strings are **inline hardcoded** in JSX (no existing abstraction)
- Date formatting uses `toLocaleDateString(undefined, ...)` (browser locale) -- already partially locale-aware
- Weather descriptions are in a `Record<string, string>` map in `weatherUtils.tsx`
- Greetings are in `GreetingWidget.tsx` with hardcoded English
- Training page has `formatDuration`, `formatDistance`, `formatPace` helpers with hardcoded unit strings (`h`, `m`, `km`, `/km`)
- The `NorwegianFunWidget` already has Norwegian-specific content (this could become locale-aware)

### Extraction approach

**Do NOT use automated extraction tools** (like `i18next-parser`) as the first pass. The codebase is small enough (20 pages) that manual extraction produces better key names and namespace organization.

**Recommended process per file:**

1. Add `useTranslation` import and hook call at top of component
2. Identify every user-visible string literal in JSX
3. Create a translation key with a meaningful name (not auto-generated)
4. Replace the string: `"Good morning"` becomes `t('greeting.morning')`
5. For strings with dynamic values, use interpolation: `t('api.label', { status })`
6. Add the English string to the appropriate namespace JSON file
7. Run the app and visually verify nothing broke

**Priority order for extraction** (see Phase plan in Section 8):

1. `common.json` strings (nav labels, shared buttons/status) -- highest reuse
2. `GreetingWidget` + `Home` -- small files, quick win
3. `Weather` + `weatherUtils` -- lots of translatable descriptions
4. `Training` pages -- unit formatting needs locale-awareness
5. `Settings` -- large file, many strings
6. Remaining pages one at a time

### Tooling to validate completeness

After extraction, use `i18next-parser` as a **validation** tool to scan for any missed hardcoded strings:

```bash
npx i18next-parser --config i18next-parser.config.js
```

This will flag any `t()` calls whose keys don't exist in the JSON files, and any JSON keys that are unused.

---

## 6. RTL Considerations

- **English** -- LTR
- **Norwegian (Bokmal)** -- LTR
- **Thai** -- LTR

**No RTL support is required** for the current target languages. Thai, despite being a complex script, is left-to-right.

However, if RTL languages (Arabic, Hebrew) are ever added in the future:

- Tailwind CSS v4 supports `rtl:` and `ltr:` variants
- The `dir` attribute on `<html>` would need to toggle based on language
- Flexbox `gap` and `space-x-*` utilities would need `rtl:space-x-reverse`
- This is **out of scope** for now but worth noting as a future consideration

---

## 7. Date, Number, and Unit Formatting

### Dates

The codebase currently uses `toLocaleDateString(undefined, ...)` which passes `undefined` as the locale, deferring to the browser. This is intentional per the CLAUDE.md conventions.

**Recommended change:** Replace `undefined` with the active i18next language code so date formatting always matches the selected UI language, not the browser locale:

```typescript
import { useTranslation } from 'react-i18next'

// In component:
const { i18n } = useTranslation()
const locale = i18n.language // 'en', 'nb', or 'th'

new Date(timestamp).toLocaleDateString(locale, {
  year: 'numeric', month: 'long', day: 'numeric'
})
```

Create a shared helper `web/src/utils/formatDate.ts`:

```typescript
import i18n from '../i18n'

export function formatDate(date: Date | string, options?: Intl.DateTimeFormatOptions): string {
  const d = typeof date === 'string' ? new Date(date) : date
  return d.toLocaleDateString(i18n.language, options)
}

export function formatTime(date: Date | string, options?: Intl.DateTimeFormatOptions): string {
  const d = typeof date === 'string' ? new Date(date) : date
  return d.toLocaleTimeString(i18n.language, options)
}
```

### Numbers

Similarly, use `i18n.language` for number formatting:

```typescript
export function formatNumber(n: number, options?: Intl.NumberFormatOptions): string {
  return n.toLocaleString(i18n.language, options)
}
```

Locale differences:
- English: `1,234.56`
- Norwegian: `1 234,56` (space as thousands separator, comma as decimal)
- Thai: `1,234.56` (same as English, but Thai digits can optionally be used)

### Units (distance, pace, duration)

The Training page has hardcoded unit strings. These should go into translation files with interpolation:

```json
{
  "units": {
    "km": "km",
    "m": "m",
    "hours_minutes": "{{h}}h {{m}}m",
    "minutes": "{{m}}m",
    "pace": "{{pace}} /km",
    "bpm": "{{value}} bpm",
    "mmol": "{{value}} mmol/L"
  }
}
```

For Norwegian, units are mostly the same (km, m, etc.) but label text around them changes. Thai uses the metric system so units remain the same.

### Weather descriptions

The `weatherUtils.tsx` `getWeatherDescription()` function has a hardcoded English description map. This should be replaced with translation keys:

```typescript
// Before:
const descriptions: Record<string, string> = {
  clearsky: 'Clear sky',
  fair: 'Fair',
  partlycloudy: 'Partly cloudy',
}

// After:
export function getWeatherDescription(symbolCode: string, t: TFunction): string {
  const code = symbolCode.replace(/_day|_night|_polartwilight/g, '')
  return t(`weather:descriptions.${code}`, code)
}
```

### Relative time ("time ago")

The `utils/timeAgo.ts` utility should use translated strings for relative time expressions ("2 hours ago", "yesterday", etc.). i18next has relative time formatting plugins, or this can be done with a small set of translation keys.

---

## 8. Implementation Phases

### Phase 1: Foundation (1-2 days)

- [ ] Install packages: `i18next`, `react-i18next`, `i18next-browser-languagedetector`, `i18next-http-backend`
- [ ] Create `web/src/i18n.ts` configuration
- [ ] Update `web/src/main.tsx` with `Suspense` wrapper and i18n import
- [ ] Create `web/src/i18next.d.ts` for TypeScript support
- [ ] Create `web/public/locales/en/common.json` with nav and shared strings
- [ ] Create stub `nb/common.json` and `th/common.json` (copy English as starting point)
- [ ] Create `LanguageSwitcher` component
- [ ] Add language switcher to Sidebar footer
- [ ] Create shared `formatDate`/`formatNumber` utilities

### Phase 2: Core Pages (2-3 days)

- [ ] Extract strings from `Sidebar.tsx` (nav labels from `common.json`)
- [ ] Extract strings from `Home.tsx` (small, quick win)
- [ ] Extract strings from `GreetingWidget.tsx` and other widgets
- [ ] Extract strings from `Weather.tsx` + `weatherUtils.tsx`
- [ ] Extract strings from `Dashboard.tsx` and dashboard widgets
- [ ] Translate `nb/common.json` and `nb/weather.json` and `nb/dashboard.json`

### Phase 3: Feature Pages (3-4 days)

- [ ] Extract strings from `Training.tsx`, `TrainingDetail.tsx`, `TrainingCompare.tsx`, `TrainingTrends.tsx`
- [ ] Extract strings from `Settings.tsx` (largest page, ~900 lines)
- [ ] Extract strings from `LactateTests.tsx`, `LactateNewTest.tsx`, `LactateTestDetail.tsx`, `LactateInsights.tsx`
- [ ] Extract strings from `Notes.tsx`, `Chat.tsx`, `Links.tsx`
- [ ] Extract strings from `Infra.tsx`, `Webhooks.tsx`, `Admin.tsx`
- [ ] Extract strings from `Login.tsx`, `CalendarPage.tsx`
- [ ] Update `formatDuration`, `formatDistance`, `formatPace` to use translation keys

### Phase 4: Norwegian Translation (2-3 days)

- [ ] Complete all `nb/*.json` translation files
- [ ] Review Norwegian translations with a native speaker
- [ ] Test all pages in Norwegian -- verify layout doesn't break (Norwegian words are often longer than English)
- [ ] Special attention to the `NorwegianFunWidget` -- this might show different content per locale or remain Norwegian-only as a charming feature

### Phase 5: Thai Translation (2-3 days)

- [ ] Complete all `th/*.json` translation files
- [ ] Review Thai translations with a native speaker
- [ ] Test Thai rendering (see Section 9 for character/font considerations)
- [ ] Verify date formatting with Thai locale (`th-TH`)
- [ ] Test that Thai text doesn't overflow or break layout (Thai words don't have spaces between them and can be long)

### Phase 6: Polish and Validation (1-2 days)

- [ ] Run `i18next-parser` to find any missed strings
- [ ] Add language preference to Settings page (full dropdown with labels)
- [ ] Optionally persist language preference to backend `user_preferences` table
- [ ] Update `<html lang>` attribute dynamically
- [ ] Test language switching on every page
- [ ] Verify date/number formatting in all three locales
- [ ] Add language to the user preferences API (backend: add `language` to allowed preference keys in `settings_handlers.go`)

**Estimated total: 11-17 days** (one developer, including translation review)

---

## 9. Language-Specific Considerations

### Norwegian Bokmal (nb)

- **Locale code:** Use `nb` (Bokmal), not `no` (generic Norwegian). Bokmal is the written standard used by ~85% of Norwegians. The `nb` locale has proper date/number formatting support in all browsers.
- **Character set:** Norwegian uses three additional letters: ae, o-with-stroke, a-with-ring (these are standard UTF-8, no special font needed).
- **Word length:** Norwegian words are often **longer** than English equivalents (compound words). Test that buttons and nav items don't overflow. Example: "Settings" = "Innstillinger" (14 chars vs 8).
- **Pluralization:** Norwegian has two plural forms (one, other) -- same as English. No special plural rules needed beyond `_one` / `_other` suffixes in i18next.
- **Date format:** `dd.MM.yyyy` (period-separated) -- handled automatically by `Intl.DateTimeFormat` with `nb` locale.
- **Number format:** Space as thousands separator, comma as decimal separator (`1 234,56`).
- **`NorwegianFunWidget`:** This widget presumably shows Norwegian fun facts or phrases. Consider whether it should show in all locales (as a "Hytte is Norwegian" cultural feature) or only when the UI is in Norwegian. Recommend keeping it visible in all locales but with locale-appropriate explanatory text.

### Thai (th)

- **Locale code:** `th` or `th-TH`. Use `th` for simplicity.
- **Character set:** Thai script (Unicode range U+0E00-U+0E7F). All modern browsers and the default Tailwind font stack render Thai correctly. However, **test that the app's font renders Thai well** -- if using a custom font, ensure it has Thai glyphs or add a Thai fallback font.
- **Font considerations:** Tailwind's default `font-sans` includes system fonts that support Thai on all major platforms (Segoe UI on Windows, SF Pro on macOS, Noto Sans Thai on Linux/Android). No extra font installation should be needed, but verify on actual devices.
- **Word boundaries:** Thai does not use spaces between words. Line breaking relies on dictionary-based word segmentation built into browsers (`word-break: normal` works). However, very long Thai text in narrow containers (sidebar labels, buttons) might not break where expected. Test with `overflow-wrap: break-word` as a safety net.
- **Text length:** Thai translations are often **shorter** than English in terms of visual width (Thai characters are compact). This is unlikely to cause layout issues but means there may be more whitespace in some areas.
- **Pluralization:** Thai has **no plural forms** -- there is only one form for all quantities. In i18next, this means Thai translation files don't need `_one`/`_other` variants; a single key suffices.
- **Thai numerals:** Thai has its own numeral system (e.g., 0123456789). The `Intl.NumberFormat('th')` API returns Arabic numerals by default, which is the modern convention in Thailand. Using `numberingSystem: 'thai'` is optional and generally not expected in web apps. Stick with Arabic numerals.
- **Buddhist Era calendar:** Thailand officially uses the Buddhist Era (BE), which is 543 years ahead of the Gregorian calendar (2026 CE = 2569 BE). `Intl.DateTimeFormat('th-TH')` will output Buddhist Era years by default. If this is undesirable, force Gregorian with `calendar: 'gregory'` option. Recommend testing both and deciding based on user preference -- for an international app, Gregorian is usually less confusing.
- **Politeness particles:** Thai has gendered politeness particles (khrap/kha). For UI text, use gender-neutral phrasing. Avoid translations that would require knowing the user's gender.

### General

- **Fallback behavior:** If a translation key is missing in `nb` or `th`, i18next falls back to `en` automatically. This means you can ship incrementally -- untranslated strings appear in English rather than as broken keys.
- **ICU message format:** For complex pluralization or gender-aware messages in the future, consider adding `i18next-icu` plugin. Not needed for the initial three languages.
- **Content from the backend:** API responses (error messages, event labels, etc.) are currently in English. Backend i18n is out of scope for this plan but worth noting. The frontend should handle English-only API responses gracefully regardless of UI language.
- **User-generated content:** Notes, workout titles, chat messages, etc. are user-generated and should NOT be translated. Only UI chrome (labels, buttons, headings, status text) gets translated.

---

## 10. Summary

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Library | react-i18next | Most popular, hook-based API, lazy loading, great TS support |
| Translation format | JSON files in `public/locales/` | Simple, standard, loaded at runtime via HTTP backend |
| Namespace strategy | One per page + common shared | Matches existing page-per-file architecture |
| Language detection | Browser preference + localStorage | Auto-detect on first visit, remember choice |
| Date/number formatting | `Intl` APIs with i18n.language as locale | Leverages browser built-ins, no extra library needed |
| Extraction approach | Manual, page by page | Better key naming, manageable codebase size (~11k lines) |
| Thai calendar | Gregorian (with option for Buddhist Era) | Less confusing for international app |

I'd say this plan is pretty *suite* -- get it? Because we're translating a whole suite of pages. I'll see myself out.
