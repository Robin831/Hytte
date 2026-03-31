import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import legacy from '@vitejs/plugin-legacy'

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    tailwindcss(),
    react(),
    legacy({
      // Target browsers that lack ES module support or missing ES2020 features.
      // This generates a legacy bundle with Babel transpilation and polyfills
      // (optional chaining, nullish coalescing, AbortController, fetch, etc.)
      // so the kiosk page works on old Android / Firefox ESR devices.
      targets: ['defaults', 'not IE 11', 'Firefox ESR', 'Chrome >= 49'],
      additionalLegacyPolyfills: ['regenerator-runtime/runtime'],
    }),
  ],
  server: {
    proxy: {
      '/api': 'http://localhost:8080'
    }
  }
})
