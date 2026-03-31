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
      // Target browsers that lack ES module support or are missing ES2020 syntax support.
      // This generates a separate legacy (nomodule) bundle with Babel transpilation and
      // core-js/regenerator-based polyfills for language features as configured here.
      // Web APIs (e.g. fetch, AbortController) require explicit polyfills if needed.
      targets: ['defaults', 'not IE 11', 'Firefox ESR', 'Chrome >= 37'],
      additionalLegacyPolyfills: ['regenerator-runtime/runtime'],
    }),
  ],
  server: {
    proxy: {
      '/api': 'http://localhost:8080'
    }
  }
})
