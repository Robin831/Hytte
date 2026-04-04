category: Added
- **Re-apply merchant rules to existing transactions** - Added a "Re-apply rules" button in the group management panel on the credit card page. Clicking it runs all merchant_group_rules against transactions that are ungrouped or in the Diverse catch-all group, and reports how many were reassigned. Endpoint: `POST /api/credit-card/transactions/reapply-rules`. (Hytte-cjz8)
