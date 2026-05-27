category: Fixed
- **Loan LTV ceiling sourced from backend** - The loan list card now reads the LTV ceiling and locale-aware percentage label from the server's `ltv_max` field (via `GET /api/budget/loans`) instead of hardcoding `0.85` and the literal `"85%"`, matching the expanded amortization view. (Hytte-2m05)
