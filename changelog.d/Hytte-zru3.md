category: Changed
- **Code-split the /today role variants** - The parent, kid, and guest Today views are now loaded with `React.lazy` behind a `Suspense` boundary, so each ships as its own chunk and only the variant matching the current user's role is fetched. (Hytte-zru3)
