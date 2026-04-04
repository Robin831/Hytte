category: Fixed
- **Fix floating point display in MyChoresPage** - `formatAmount` now uses `Intl.NumberFormat` with `minimumFractionDigits: 2` / `maximumFractionDigits: 2` so amounts render with proper decimal formatting instead of raw JavaScript numbers. (Hytte-q8if)
