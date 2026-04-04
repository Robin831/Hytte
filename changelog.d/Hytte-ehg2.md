category: Fixed
- **Fix React error on credit card page** - Moved group management `useState` hooks to the top of the component, above conditional early returns, to comply with the Rules of Hooks. The hooks were incorrectly placed after early returns, causing React to detect an inconsistent hook call order and throw on the credit card page. (Hytte-ehg2)
