chore: update npm dependencies — vite 8, @vitejs/plugin-react 6, @types/node (Hytte-59d, Hytte-avr, Hytte-50k)

- vite 7.3.1 → 8.0.0 (now uses rolldown bundler)
- @vitejs/plugin-react 5.1.1 → 6.0.1
- @types/node 25.3.5 → 25.5.0
- react-is added as explicit dep (required by recharts under rolldown's
  stricter module resolution)
