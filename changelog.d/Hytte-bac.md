category: Added
- **Feature permission system** - Admins can now control which features each user can access via a new user_features table and API. Features like training, notes, links, infra, lactate, webhooks, and Claude AI are gated per-user, with sensible defaults for new users. Admin users bypass all feature checks. The /api/auth/me endpoint now includes the user's feature map. (Hytte-bac)
