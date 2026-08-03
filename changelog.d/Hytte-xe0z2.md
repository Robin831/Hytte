category: Removed
- **Removed unused annuityPayment365 helper** - Deleted the dead actual/365 annuity solver in the budget loan engine; the amortization schedule already uses the inline r/12 annuity formula, so the unused helper only invited confusion about which payment formula applies. (Hytte-xe0z2)
