category: Fixed
- **Fix floating point rounding on allowance page** - Currency amounts (chore amounts, extra task amounts, payout totals, base and bonus breakdowns) are now formatted with exactly 2 decimal places using `Intl.NumberFormat`, preventing display of values like `13.200000000000001 kr`. (Hytte-k64p)
