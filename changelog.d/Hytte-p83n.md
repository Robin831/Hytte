category: Fixed
- **Track in-flight chat sends per conversation** - The Chat page now tracks pending sends in a set keyed by conversation id instead of a single scalar, so starting a send in one conversation no longer clears the "thinking" indicator or re-enables the input/send button of another conversation that is still waiting on its own response. (Hytte-p83n)
