category: Added
- **Admin star award endpoint** - Added `POST /api/admin/stars/award` (admin-only) to manually award or adjust a child's star balance with a reason and optional description. Inserts into `star_transactions` and updates `star_balances`. Supports positive awards and negative deductions. (Hytte-nrr7)
- **Award Stars UI on Family page** - Admin users now see a sparkle button on each child card on the Family page that opens a modal form to award or deduct stars with a reason and description. Balance refreshes automatically after awarding. (Hytte-nrr7)
