category: Fixed
- **Cache-bust locale JSON files on deploy** - Added a build-time hash to i18next loadPath so browsers fetch fresh locale files after each deploy instead of serving stale cached versions. (Hytte-6l67)
