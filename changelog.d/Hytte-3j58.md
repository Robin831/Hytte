category: Changed
- **Shared budget fetch/format helpers** - Extracted the budget sub-pages' duplicated date helper, NOK/number formatting, and the fetch + AbortController + loading/error lifecycle into a single `web/src/pages/budget/hooks.ts` module (`useBudgetResource`, `formatNOK`, `formatBudgetNumber`, `todayDate`). Pure internal refactor with no visible behavior change. (Hytte-3j58)
