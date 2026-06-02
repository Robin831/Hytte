category: Fixed
- **Restore body scroll and focus when leaving the allowance photo preview** - Navigating away from the allowance page while a photo is enlarged now always resets the body scroll lock, and focus is only returned to the trigger when it is still attached to the DOM, preventing a stuck-scroll bug and stray focus warnings on mobile. (Hytte-4zth)
