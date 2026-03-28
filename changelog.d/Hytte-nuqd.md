category: Added
- **Allowance parent dashboard** - New `/allowance` page for parents with three tabs: Today (pending chore completions to approve or reject), Chores (create and manage chore definitions with name, amount, frequency, and icon), and Payouts (weekly earnings summaries with mark-as-paid). (Hytte-nuqd)
- **Quality bonus endpoint** - `POST /api/allowance/quality-bonus/{id}` allows parents to grant an extra NOK bonus on any completion; the amount is included in weekly earnings calculations. (Hytte-nuqd)
