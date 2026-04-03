category: Added
- **Norwegian progressive tax brackets** - Tax bracket table with proper marginal rate calculation. Norwegian 2025/2026 defaults are seeded automatically; brackets are updatable per-year via GET/PUT `/api/salary/tax-table?year=`. GET `/api/salary/tax-table/defaults?year=` exposes built-in defaults as single source of truth. (Hytte-65qy)
- **Vacation day tracking** - Feriedager used vs remaining (25-day statutory allowance) and feriepenger accrual at 10.2% of confirmed gross earnings, via GET `/api/salary/vacation`. (Hytte-65qy)
- **Salary-to-budget sync** - POST `/api/salary/records/{month}/sync-budget` creates or replaces an income transaction in the budget system for the month's net salary. Auto-creates a "Salary" income category if none exists. (Hytte-65qy)
