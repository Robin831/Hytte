category: Added
- **Partner income preference** - Added `partner_income` (monthly salary) to user preferences, alongside `income_split_percentage`. Exposed via the settings API with getter/setter functions in the budget package. Defaults to 0. Required by the regning calculator to compute post-transfer balances for both partners. (Hytte-udwg)
