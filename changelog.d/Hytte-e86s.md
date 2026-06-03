category: Changed
- **Refactor chat SSE streaming into a useChatStream hook** - Extracted the chat streaming state machine, optimistic message bookkeeping, abort handling, and post-stream conversation refetch out of `Chat.tsx` into a dedicated `useChatStream` hook with unit tests. No user-visible behavior change. (Hytte-e86s)
