category: Changed
- **Family chat conversation refresh via context** - Replaced the `conversationListRefreshKey` counter prop threaded from FamilyChat into ConversationList with a small FamilyChatContext exposing `refreshConversations()`, so any mutation can request a list refresh without prop threading. No user-visible behavior change. (Hytte-v2q1)
