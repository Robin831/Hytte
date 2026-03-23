category: Changed
- **Internationalize relative time strings** - `timeAgo` utility now uses i18n translation keys for relative time labels ("just now", "3h ago", "yesterday", etc.) with translations for English, Norwegian Bokmål, and Thai. The utility accepts a `t` function from the `common` namespace so strings update reactively on language change. (Hytte-n8ea)
