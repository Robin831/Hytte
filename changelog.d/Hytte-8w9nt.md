category: Added
- **Un-approve chore completions** - Parents can move an approved chore completion back to pending, one at a time (`POST /api/allowance/unapprove/{id}`) or in bulk (`POST /api/allowance/unapprove/batch`). The quality bonus and approval details are cleared, the auto-approve window restarts, and the request is refused with 409 once that week has been paid out. (Hytte-8w9nt)
