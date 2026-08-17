category: Changed
- **Package updates** - Bumped Go modules (golang.org/x/net, google.golang.org/api, github.com/muktihari/fit) and npm packages (lucide-react, vite, eslint, typescript-eslint, globals, happy-dom, terser, @types/node, @vitejs/plugin-legacy, @testing-library/jest-dom, eslint-plugin-react-refresh) to their latest patch/minor releases. The TypeScript 7 major upgrade is still held back because typescript-eslint requires `typescript <6.1.0`. (Hytte-92xyu)

category: Fixed
- **Green frontend test suite again** - The training-list tests still drove the tag filter chips that were deliberately removed when the list was decluttered, and the Stride and workout-detail tests hit the stride-eval SSE subscription without an `EventSource` stub (happy-dom provides none). Obsolete tag assertions are gone, the remaining sport/text filter coverage is intact, and both pages now stub `EventSource` like the other SSE suites. (Hytte-92xyu)
