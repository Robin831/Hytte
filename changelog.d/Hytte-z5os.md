category: Fixed
- **Kiosk: fix 't is undefined' on Android 5 / old Firefox** - Added `whatwg-fetch` polyfill to the legacy bundle so that `i18next-http-backend` (which uses the `fetch` Web API to load locale JSON files) initialises correctly on browsers that lack native `fetch`. Without this, `useTranslation()` silently returned `t=undefined`, crashing the kiosk. (Hytte-z5os)
